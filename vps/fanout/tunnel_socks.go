package main

// tunnel_socks.go：本地 SOCKS5 监听与连接跟踪。
import (
	"fmt"
	"net"
	"time"
)

// serve 在母机上监听 SOCKS5 端口，出站连接则在 netns 内建立。
// 监听必须留在母机侧：netns 内的 loopback 与母机彼此独立，
// 监听在 netns 里的话外部根本连不上。
func (t *Tunnel) serve() error {
	// 端口要尽量保持不变，否则用户已经分发出去的客户端配置会失效。
	// 进程刚重启时旧监听可能还在 TIME_WAIT，这里给几秒重试窗口。
	var ln net.Listener
	var err error
	for i := 0; i < 6; i++ {
		if err := t.workContext().Err(); err != nil {
			return err
		}
		// SOCKS is an internal hand-off to the built-in Xray. Never expose it
		// on the VPS public interface; Xray's inbound ports remain separate.
		ln, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", t.Port))
		if err == nil {
			break
		}
		if !waitForWork(t.workContext(), time.Second) {
			return t.workContext().Err()
		}
	}
	if err != nil {
		return fmt.Errorf("Slot 固定端口 %d 监听失败: %w", t.Port, err)
	}
	t.listener = ln
	dial := dialerInNetns(t.nsName())

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Keep the stable listener during startup/reconnect, but refuse new
			// business until the current node has passed expected/actual checks.
			// This prevents callers from using a half-built namespace or a slot
			// whose OpenVPN process has just been killed.
			if !t.trackClient(conn) {
				_ = conn.Close()
				continue
			}
			// 每次连接现取凭据：改口令后不必重建监听，新连接立刻按新凭据校验
			cred := t.probeCredential()
			ctx := t.workContext()
			go func() {
				defer t.untrackClient(conn)
				serveSocks(conn, &cred, func(network, addr string) (net.Conn, error) {
					if err := ctx.Err(); err != nil {
						return nil, err
					}
					remote, err := dial(network, addr)
					if err == nil && ctx.Err() != nil {
						remote.Close()
						return nil, ctx.Err()
					}
					return remote, err
				})
			}()
		}
	}()
	return nil
}

func (t *Tunnel) acceptsSocks() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.acceptsSocksLocked()
}

func (t *Tunnel) acceptsSocksLocked() bool {
	if t.workCtx != nil && t.workCtx.Err() != nil {
		return false
	}
	if t.validating {
		return t.validationCred != nil
	}
	if !servingStatus(t.Status) {
		return false
	}
	if t.Policy != nil {
		// 正确性门只看画像证据：延迟超限走 degraded 观察，由健康检查驱动
		// 升级为重连，不在这里拒新连接（建连时首个样本已验过上限）。
		if t.egress == nil || t.Policy.checkImage(*t.egress) != nil {
			return false
		}
	}
	return true
}

func (t *Tunnel) trackClient(conn net.Conn) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.acceptsSocksLocked() {
		return false
	}
	if t.clients == nil {
		t.clients = map[net.Conn]bool{}
	}
	t.clients[conn] = true
	return true
}

func (t *Tunnel) untrackClient(conn net.Conn) {
	t.mu.Lock()
	delete(t.clients, conn)
	t.mu.Unlock()
}
