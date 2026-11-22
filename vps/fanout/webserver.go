package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// webServer 管理 HTTP 监听，支持在运行时切换端口/监听地址而不重启进程。
// 切换端口或监听地址会新起一个 net.Listener，旧的优雅关闭。
type webServer struct {
	handler http.Handler

	mu              sync.Mutex
	ln              net.Listener
	srv             *http.Server
	addr            string
	prepareListener func(WebSettings) (*preparedManagementListener, error)
	managementHook  func(string) error
}

func newWebServer(h http.Handler) *webServer {
	return &webServer{handler: h}
}

// newHTTPServer 给管理监听加上资源上限，避免公网慢连接和超大请求头耗尽资源。
func newHTTPServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

// serve 用当前 WebSettings 起第一个监听并阻塞。返回时说明监听彻底退出。
func (s *webServer) serve() error {
	cfg := getWebSettings()
	if err := s.reload(cfg); err != nil {
		return err
	}
	// 主 goroutine 就地阻塞，等监听被 reload 或退出替换。
	// 这里靠一个永不返回的 select 挂住：真正的 Serve 在 reload 里各自的 goroutine 跑。
	select {}
}

// reload 切换到新的监听地址：先探测能否绑上，绑得上再关旧的、启新的。
// 绑不上就保持旧监听不动，返回错误让调用方回报给用户。
func (s *webServer) reload(cfg WebSettings) error {
	prepared, err := s.prepareManagementListener(cfg)
	if err != nil {
		return err
	}
	prepared.activate()
	return nil
}

// swapListenerSamePort switches the listen address without changing the port
// (e.g. :8899 -> 127.0.0.1:8899). Pre-binding the new address while the old
// listener is still alive always hits EADDRINUSE when the two overlap, so the
// old socket is closed first and the new address bound immediately.
//
// It returns the new listener/address plus a rollback that restores the old
// listener if a later commit step (config write) fails. The caller starts
// serving via activateListener and must drop the rollback after activation.
func (s *webServer) swapListenerSamePort(next WebSettings) (net.Listener, string, func() error, error) {
	s.mu.Lock()
	oldLn, oldAddr := s.ln, s.addr
	s.mu.Unlock()

	addr := next.listenAddrString()
	if oldLn == nil {
		// No active listener (e.g. fresh test server): plain bind. Rollback
		// must still release it, otherwise a failed config write leaks the
		// port with nobody serving it.
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, "", nil, fmt.Errorf("无法监听 %s: %w", addr, err)
		}
		return ln, addr, func() error { return ln.Close() }, nil
	}

	// Stop accepting on the old socket so the port is free for the new bind.
	// Established connections (including the settings request in flight) are
	// unaffected; they drain via the old server's graceful shutdown after the
	// swap, same as the regular reload path.
	_ = oldLn.Close()
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// New address unusable: bring the old listener back so the panel
		// stays reachable and matches the still-uncommitted on-disk config.
		rln, rerr := net.Listen("tcp", oldAddr)
		if rerr != nil {
			return nil, "", nil, fmt.Errorf("无法监听 %s: %w；恢复旧监听 %s 也失败: %v", addr, err, oldAddr, rerr)
		}
		s.activateListener(rln, oldAddr)
		return nil, "", nil, fmt.Errorf("无法监听 %s: %w", addr, err)
	}
	rollback := func() error {
		_ = ln.Close()
		rln, rerr := net.Listen("tcp", oldAddr)
		if rerr != nil {
			return fmt.Errorf("回滚监听 %s 失败: %w", oldAddr, rerr)
		}
		s.activateListener(rln, oldAddr)
		return nil
	}
	return ln, addr, rollback, nil
}

// isListenerClosedError reports the benign Accept error from a listener that
// was closed for a same-port address switch. The swap path closes the old
// socket before binding the new one, so this is expected, not a failure.
func isListenerClosedError(err error) bool {
	return errors.Is(err, net.ErrClosed)
}

func (s *webServer) activateListener(ln net.Listener, addr string) {

	s.mu.Lock()
	oldSrv := s.srv
	oldLn := s.ln
	srv := newHTTPServer(s.handler)
	s.srv = srv
	s.ln = ln
	s.addr = addr
	s.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed && !isListenerClosedError(err) {
			log.Printf("HTTP 监听 %s 退出: %v", addr, err)
		}
	}()

	// 关掉旧监听。给正在处理的请求一点收尾时间，
	// 尤其是触发这次 reload 的那个请求本身要先把响应写完。
	if oldSrv != nil {
		go func() {
			time.Sleep(1 * time.Second)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = oldSrv.Shutdown(ctx)
			_ = oldLn.Close()
		}()
	}

	log.Printf("管理界面监听已切换到 %s", addr)
}
