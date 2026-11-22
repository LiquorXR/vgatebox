package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"sync"
)

const managementJournal = ".management-transaction.json"

var managementConfigFiles = []string{"settings.json", "password", "basepath"}
var managementMu sync.Mutex

type managementSettingsRequest struct {
	Password        *string `json:"password"`
	CurrentPassword *string `json:"current_password"`
	BasePath        *string `json:"base_path"`
	Port            *int    `json:"port"`
	ListenAddr      *string `json:"listen_addr"`
	ResidentialOnly *bool   `json:"residential_only"`
}

type preparedManagementListener struct {
	activate func()
	cancel   func()
}

func (s *webServer) prepareManagementListener(next WebSettings) (*preparedManagementListener, error) {
	if s.prepareListener != nil {
		return s.prepareListener(next)
	}
	addr := next.listenAddrString()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("无法监听 %s: %w", addr, err)
	}
	return &preparedManagementListener{activate: func() { s.activateListener(ln, addr) }, cancel: func() { ln.Close() }}, nil
}

func applyManagementSettings(auth *Auth, srv *webServer, in managementSettingsRequest) error {
	managementMu.Lock()
	defer managementMu.Unlock()
	next := getWebSettings()
	if in.Port != nil {
		next.Port = *in.Port
	}
	if in.ListenAddr != nil {
		next.ListenAddr = *in.ListenAddr
	}
	if in.ResidentialOnly != nil {
		v := *in.ResidentialOnly
		next.ResidentialOnly = &v
	}
	password := in.Password
	if password != nil && *password == "" {
		password = nil
	}
	return commitManagementSettings(auth, srv, next, password, in.BasePath)
}

// Bind the replacement port before touching any credential/file; start serving
// only after the entire durable transaction commits. Old listener stays live
// when validation, bind or any write fails.
//
// Exception: a same-port listen-address switch cannot pre-bind (the old socket
// covers the new address), so it closes first and restores the old listener if
// the transaction fails; see commitListenAddrChange.
func commitManagementSettings(auth *Auth, srv *webServer, next WebSettings, password, base *string) error {
	next, password, base, err := normalizeManagementSettings(next, password, base)
	if err != nil {
		return err
	}
	dir, err := managementConfigDir(auth)
	if err != nil {
		return err
	}
	current := getWebSettings()
	if next.Port == current.Port && next.ListenAddr != current.ListenAddr {
		// This branch bypasses the srv.prepareListener test seam on purpose:
		// pre-binding is exactly what cannot work here.
		return commitListenAddrChange(auth, srv, dir, next, password, base)
	}
	prepared, err := prepareManagementListenerChange(srv, next, current)
	if err != nil {
		return err
	}
	defer func() {
		if prepared != nil {
			prepared.cancel()
		}
	}()
	if err := writeManagementConfig(auth, dir, next, password, base, srv); err != nil {
		return err
	}
	applyManagementRuntime(auth, next, password, base, dir)
	if prepared != nil {
		prepared.activate()
		prepared = nil
	}
	return nil
}

func normalizeManagementSettings(next WebSettings, password, base *string) (WebSettings, *string, *string, error) {
	if err := validatePort(next.Port); err != nil {
		return next, password, base, err
	}
	norm, err := normalizeListenAddr(next.ListenAddr)
	if err != nil {
		return next, password, base, err
	}
	next.ListenAddr = norm
	if password != nil {
		v := strings.TrimSpace(*password)
		if len(v) < minPasswordLength {
			return next, password, base, fmt.Errorf("口令至少%d位", minPasswordLength)
		}
		password = &v
	}
	if base != nil {
		v, err := validateBasePath(*base)
		if err != nil {
			return next, password, base, err
		}
		base = &v
	}
	return next, password, base, nil
}

func managementConfigDir(auth *Auth) (string, error) {
	dir := filepath.Dir(webSettingsPath)
	if auth != nil && paths.Password(auth.dir) != filepath.Join(dir, "password") {
		// 同实例守卫：auth 的工作目录必须正好落到这份 settings 所在的目录，
		// 直接比文件路径，不假设布局（根/子目录）。
		return "", fmt.Errorf("管理配置目录不一致")
	}
	return dir, nil
}

func prepareManagementListenerChange(srv *webServer, next, current WebSettings) (*preparedManagementListener, error) {
	if next.Port == current.Port && next.ListenAddr == current.ListenAddr {
		return nil, nil
	}
	return srv.prepareManagementListener(next)
}

// commitListenAddrChange handles same-port listen-address switches
// (e.g. :8899 -> 172.17.0.1:8899 for a Docker-bridged reverse proxy).
// Pre-binding the new address while the old listener is alive always fails
// when the two overlap, so the old socket is closed first and the new address
// bound before the config is written; any later failure rolls the listener
// back so disk config and live listener never diverge.
func commitListenAddrChange(auth *Auth, srv *webServer, dir string, next WebSettings, password, base *string) error {
	if srv == nil {
		return fmt.Errorf("管理监听未就绪，无法切换监听地址")
	}
	ln, addr, rollback, err := srv.swapListenerSamePort(next)
	if err != nil {
		return err
	}
	if err := writeManagementConfig(auth, dir, next, password, base, srv); err != nil {
		if rerr := rollback(); rerr != nil {
			return fmt.Errorf("%v；回滚监听失败: %v", err, rerr)
		}
		return err
	}
	applyManagementRuntime(auth, next, password, base, dir)
	srv.activateListener(ln, addr)
	return nil
}

func writeManagementConfig(auth *Auth, dir string, next WebSettings, password, base *string, srv *webServer) error {
	tx, err := beginConfigTransaction(dir, managementJournal, managementConfigFiles)
	if err != nil {
		return err
	}
	fail := func(cause error) error {
		if err := tx.rollback(); err != nil {
			return fmt.Errorf("管理配置回滚未完成；旧入口保留，重启需恢复记录: %w", err)
		}
		return cause
	}
	blob, err := json.MarshalIndent(next, "", "  ")
	if err != nil {
		return fail(err)
	}
	if err := atomicConfigWrite(webSettingsPath, blob); err != nil {
		return fail(err)
	}
	if err := runManagementHook(srv, "after_settings_write"); err != nil {
		return fail(err)
	}
	if password != nil {
		if auth == nil {
			return fail(fmt.Errorf("缺少管理认证对象"))
		}
		if err := atomicConfigWrite(filepath.Join(dir, "password"), []byte(*password+"\n")); err != nil {
			return fail(err)
		}
	}
	if err := runManagementHook(srv, "after_password_write"); err != nil {
		return fail(err)
	}
	if base != nil {
		if err := atomicConfigWrite(filepath.Join(dir, "basepath"), []byte(strings.TrimPrefix(*base, "/")+"\n")); err != nil {
			return fail(err)
		}
	}
	if err := runManagementHook(srv, "before_commit"); err != nil {
		return fail(err)
	}
	if err := tx.commit(); err != nil {
		return fail(err)
	}
	return nil
}

func runManagementHook(srv *webServer, phase string) error {
	if srv.managementHook == nil {
		return nil
	}
	return srv.managementHook(phase)
}

func applyManagementRuntime(auth *Auth, next WebSettings, password, base *string, dir string) {
	webSettingsMu.Lock()
	webSettingsCur = next
	webSettingsMu.Unlock()
	if password != nil && auth != nil {
		auth.mu.Lock()
		auth.password = *password
		auth.mu.Unlock()
	}
	if base != nil {
		basePathMu.Lock()
		basePathCur = *base
		basePathDir = dir
		basePathMu.Unlock()
	}
}
