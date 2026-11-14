package main

import (
	"context"
	"net"
	"os"
	"runtime"

	"golang.org/x/sys/unix"
)

// dialerInNetns 返回一个在指定 netns 内建立出站连接的 dial 函数。
// 域名先在这个 netns 内解析成 IPv4（见 netns_dns.go），再在 ns 内连接；
// 解析失败直接返回错误，绝不回退到母机解析。
func dialerInNetns(nsName string) func(network, addr string) (net.Conn, error) {
	raw := rawDialerInNetns(nsName)
	return func(network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if net.ParseIP(host) == nil {
			ips, err := netnsDNSFor(nsName).resolve(context.Background(), host)
			if err != nil {
				return nil, err
			}
			var lastErr error
			for _, ip := range ips {
				conn, err := raw(forceIPv4Network(network), net.JoinHostPort(ip, port))
				if err == nil {
					return conn, nil
				}
				lastErr = err
			}
			if lastErr == nil {
				lastErr = errDNSNoRecord
			}
			return nil, lastErr
		}
		return raw(forceIPv4Network(network), addr)
	}
}

// rawDialerInNetns 只做 setns 和拨号，不处理域名。
// 每次拨号都要切一次 netns，因为 socket 的归属在创建时确定。
func rawDialerInNetns(nsName string) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)

		go func() {
			runtime.LockOSThread()

			origin, err := os.Open("/proc/self/ns/net")
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer origin.Close()

			target, err := os.Open("/var/run/netns/" + nsName)
			if err != nil {
				ch <- result{nil, err}
				return
			}
			defer target.Close()

			if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
				ch <- result{nil, err}
				return
			}

			// 隧道内只有 IPv4 路由。不限定的话 net.Dial 可能选中 AAAA 记录，
			// 那条连接会绕开隧道从母机的 IPv6 出去，暴露真实地址。
			conn, dialErr := net.Dial(forceIPv4Network(network), addr)

			if err := unix.Setns(int(origin.Fd()), unix.CLONE_NEWNET); err != nil {
				if conn != nil {
					conn.Close()
				}
				ch <- result{nil, err}
				return
			}

			runtime.UnlockOSThread()
			ch <- result{conn, dialErr}
		}()

		r := <-ch
		return r.conn, r.err
	}
}

// forceIPv4Network 把 tcp/udp 收敛成 tcp4/udp4，已经指定版本的原样返回。
func forceIPv4Network(network string) string {
	switch network {
	case "tcp":
		return "tcp4"
	case "udp":
		return "udp4"
	}
	return network
}
