package main

import (
	"bytes"
	"encoding/json"
	"fanout/internal/paths"
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const nativeJournal = ".native-transaction.json"

var nativeConfigFiles = []string{"native.json", "xray.json", "native.previous.json"}

type nativeRuntime interface {
	verify(string) error
	restart(string) error
	stop()
}

func cloneNativeStore(s *nativeStore) *nativeStore {
	copy := *s
	copy.Inbounds = make([]*nativeInbound, 0, len(s.Inbounds))
	for _, in := range s.Inbounds {
		ib := *in
		ib.Clients = append([]nativeClient(nil), in.Clients...)
		if in.ThirdParty != nil {
			p := *in.ThirdParty
			ib.ThirdParty = &p
		}
		if in.TLS != nil {
			v := *in.TLS
			ib.TLS = &v
		}
		if in.Reality != nil {
			v := *in.Reality
			v.ServerNames = append([]string(nil), v.ServerNames...)
			v.ShortIDs = append([]string(nil), v.ShortIDs...)
			ib.Reality = &v
		}
		if in.KCP != nil {
			v := *in.KCP
			ib.KCP = &v
		}
		if in.VLESSEncryption != nil {
			v := *in.VLESSEncryption
			ib.VLESSEncryption = &v
		}
		copy.Inbounds = append(copy.Inbounds, &ib)
	}
	return &copy
}

// Public mutations hold n.mu and edit a private copy. Even an early return
// before apply (for example during bulk clone allocation) discards that copy.
func (n *Native) beginChange() func() {
	if n.committed == nil {
		n.committed = n.store
	}
	n.store = cloneNativeStore(n.committed)
	candidate := n.store
	return func() {
		if candidate != n.committed {
			n.cleanupCandidateCerts(candidate, n.committed)
		}
		n.store = n.committed
	}
}
func (n *Native) engine() nativeRuntime {
	if n.runtime != nil {
		return n.runtime
	}
	return n.proc
}
func (n *Native) transactionStep(stage string) error {
	if n.transactionHook != nil {
		return n.transactionHook(stage)
	}
	return nil
}

func (n *Native) cleanupCandidateCerts(candidate, accepted *nativeStore) {
	kept := map[string]bool{}
	for _, ib := range accepted.Inbounds {
		if ib.TLS != nil {
			kept[ib.TLS.CertFile] = true
			kept[ib.TLS.KeyFile] = true
		}
	}
	// A successful manual rollback swaps current/previous. Its predecessor's
	// generated certificates are still needed if the operator restores it.
	if blob, err := os.ReadFile(paths.NativePrevious(n.dir)); err == nil {
		var history nativeStore
		if json.Unmarshal(blob, &history) == nil {
			for _, ib := range history.Inbounds {
				if ib != nil && ib.TLS != nil {
					kept[ib.TLS.CertFile] = true
					kept[ib.TLS.KeyFile] = true
				}
			}
		}
	}
	certDir := paths.CertsDir(n.dir)
	for _, ib := range candidate.Inbounds {
		if ib.TLS == nil || !ib.TLS.SelfSigned {
			continue
		}
		for _, path := range []string{ib.TLS.CertFile, ib.TLS.KeyFile} {
			if !kept[path] && filepath.Dir(path) == certDir {
				_ = os.Remove(path)
			}
		}
	}
}

func (n *Native) stageConfig(store *nativeStore, tunnels []*Tunnel) (string, bool, error) {
	cfg := buildXrayConfig(store.sorted(), tunnels)
	blob, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", false, err
	}
	f, err := os.CreateTemp(paths.RunDir(n.dir), ".xray-candidate-*.json")
	if err != nil {
		return "", false, err
	}
	path := f.Name()
	defer f.Close()
	if err := f.Chmod(0600); err != nil {
		os.Remove(path)
		return "", false, err
	}
	if _, err := f.Write(blob); err != nil {
		os.Remove(path)
		return "", false, err
	}
	if err := f.Sync(); err != nil {
		os.Remove(path)
		return "", false, err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return "", false, err
	}
	return path, len(cfg["inbounds"].([]any)) > 0, nil
}

// Rebuild rollback routing against CURRENT Slots, never replay stale SOCKS
// bindings from an old xray.json or restore a past healthy flag.
func (n *Native) restoreRuntime(store *nativeStore, tunnels []*Tunnel) error {
	path, enabled, err := n.stageConfig(store, tunnels)
	if err != nil {
		return err
	}
	defer os.Remove(path)
	if err := n.engine().verify(path); err != nil {
		return err
	}
	if enabled {
		if err := n.engine().restart(path); err != nil {
			return err
		}
		n.procLive = true
		noteXrayRestart()
	} else {
		n.engine().stop()
		n.procLive = false
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return atomicConfigWrite(paths.Xray(n.dir), blob)
}

func (n *Native) apply(tunnels []*Tunnel) error {
	if err := paths.Ensure(n.dir); err != nil {
		return err
	}
	if n.committed == nil {
		n.committed = cloneNativeStore(n.store)
	}
	previous := n.committed
	wasUnavailable := n.unavailable
	path, enabled, err := n.stageConfig(n.store, tunnels)
	if err != nil {
		n.store = previous
		return err
	}
	defer os.Remove(path)
	if err := n.engine().verify(path); err != nil {
		n.store = previous
		return err
	}
	// 运行配置与磁盘逐字节一致、且进程是本实例拉起的，就跳过重启：
	// 健康抖动、状态翻转等只改 Slot 不改入站的 apply 到这里全部收敛为 no-op，
	// 其他 Slot 的已有客户端连接不再被连带中断。文件提交流程照常走
	// （native.json 元数据仍需落盘），失败语义不变（touched 照常置位）。
	sameConfig := false
	if staged, serr := os.ReadFile(path); serr == nil {
		if current, cerr := os.ReadFile(paths.Xray(n.dir)); cerr == nil {
			sameConfig = bytes.Equal(staged, current)
		}
	}
	tx, err := beginConfigTransaction(paths.StateDir(n.dir), nativeJournal, nativeConfigFiles)
	if err != nil {
		n.store = previous
		return err
	}
	touched := false
	fail := func(cause error) error {
		n.store = previous
		// Reap the failed candidate before any filesystem recovery attempt.
		if touched {
			n.engine().stop()
			n.procLive = false
		}
		if err := tx.rollback(); err != nil {
			n.engine().stop()
			n.procLive = false
			n.unavailable = true
			return fmt.Errorf("配置应用失败且磁盘回滚未完成；服务已阻断，重启需恢复记录: %w", err)
		}
		if touched {
			if err := n.restoreRuntime(previous, tunnels); err != nil {
				n.engine().stop()
				n.procLive = false
				n.unavailable = true
				return fmt.Errorf("配置已回滚，旧配置运行恢复失败；服务保持阻断: %w", err)
			}
		}
		if touched {
			n.unavailable = false
		} else {
			n.unavailable = wasUnavailable
		}
		return fmt.Errorf("配置应用失败，已恢复上次提交配置: %w", cause)
	}
	if err := n.transactionStep("before_restart"); err != nil {
		return fail(err)
	}
	touched = true
	if enabled {
		if sameConfig && n.procLive {
			log.Printf("Xray 运行配置无变化，跳过重启")
		} else if err = n.engine().restart(path); err != nil {
			return fail(err)
		} else {
			n.procLive = true
			noteXrayRestart()
		}
	} else {
		n.engine().stop()
		n.procLive = false
	}
	if err := n.transactionStep("after_restart"); err != nil {
		return fail(err)
	}
	blob, err := json.MarshalIndent(n.store, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := atomicConfigWrite(nativeStatePath(n.dir), blob); err != nil {
		return fail(err)
	}
	if err := n.transactionStep("after_native_write"); err != nil {
		return fail(err)
	}
	previousBlob, _ := json.MarshalIndent(previous, "", "  ")
	if string(blob) != string(previousBlob) {
		if err := atomicConfigWrite(paths.NativePrevious(n.dir), previousBlob); err != nil {
			return fail(err)
		}
	}
	config, err := os.ReadFile(path)
	if err != nil {
		return fail(err)
	}
	if err := atomicConfigWrite(paths.Xray(n.dir), config); err != nil {
		return fail(err)
	}
	if err := n.transactionStep("before_commit"); err != nil {
		return fail(err)
	}
	if err := tx.commit(); err != nil {
		return fail(err)
	}
	n.committed = n.store
	n.unavailable = false
	return nil
}

func (n *Native) Rollback(tunnels []*Tunnel) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	blob, err := os.ReadFile(paths.NativePrevious(n.dir))
	if err != nil {
		return fmt.Errorf("没有可读的上一次内置配置")
	}
	before, err := decodeNativeStore(blob)
	if err != nil {
		return err
	}
	finish := n.beginChange()
	defer finish()
	n.store = cloneNativeStore(before)
	return n.apply(tunnels)
}
