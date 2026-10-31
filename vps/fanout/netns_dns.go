package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SOCKS5 收到的域名必须在出口 netns 里解析。
//
// dialerInNetns 只做了 setns，挂载命名空间没变，进程读到的仍是母机的
// /etc/resolv.conf；而发布包固定 CGO_ENABLED=0，Go 的纯 Go resolver 会
// 在拨号 goroutine 之外另起线程做 A/AAAA 查询，那些线程还在母机 netns 里。
// 结果是查询从 VPS 自己的网络出去（泄漏域名和 VPS IP），返回的地址也按
// VPS 所在地区挑选，而不是出口地区。
//
// 这里显式在 netns 内建 DNS socket：查询从 tun0 出去，解析结果按出口地区
// 返回。解析失败直接报错，绝不回退到母机解析。

const (
	defaultDNSList     = "1.1.1.1:53,8.8.8.8:53"
	defaultDNSCacheTTL = 60 * time.Second
	defaultDNSNegTTL   = 5 * time.Second
	dnsLookupTimeout   = 5 * time.Second
	dnsAttemptTimeout  = 2 * time.Second
)

// errDNSNoRecord 表示解析成功但没有任何 A 记录。
var errDNSNoRecord = errors.New("netns 内没有 A 记录")

type netnsDNSEntry struct {
	ips    []string
	expire time.Time
	gen    uint64
}

// netnsDNSCall 是 singleflight 的一次在途查询。
type netnsDNSCall struct {
	done chan struct{}
	ips  []string
	err  error
}

// netnsDNS 是一个 netns 的解析器，带缓存、负缓存与 singleflight。
type netnsDNS struct {
	ns   string
	dial func(network, addr string) (net.Conn, error)

	servers []string
	ttl     time.Duration
	negTTL  time.Duration
	lookup  func(ctx context.Context, host string, servers []string) ([]string, error)

	mu       sync.Mutex
	gen      uint64
	cache    map[string]netnsDNSEntry
	inflight map[string]*netnsDNSCall
}

var (
	netnsDNSRegistryMu sync.Mutex
	netnsDNSRegistry   = map[string]*netnsDNS{}
)

// netnsDNSFor 取（或创建）某个 netns 的解析器；同一个 ns 共用缓存。
func netnsDNSFor(ns string) *netnsDNS {
	netnsDNSRegistryMu.Lock()
	defer netnsDNSRegistryMu.Unlock()
	if d, ok := netnsDNSRegistry[ns]; ok {
		return d
	}
	d := &netnsDNS{
		ns:       ns,
		dial:     rawDialerInNetns(ns),
		servers:  dnsServersFromEnv(),
		ttl:      dnsCacheTTLFromEnv(),
		negTTL:   defaultDNSNegTTL,
		cache:    map[string]netnsDNSEntry{},
		inflight: map[string]*netnsDNSCall{},
	}
	d.lookup = d.lookupInNetns
	netnsDNSRegistry[ns] = d
	return d
}

// clearNetnsDNSCache 丢弃某个 ns 的解析结果。
// 换节点后出口地区变了，旧地址必须失效；gen 递增同时保证在途查询的结果
// 不会再写回缓存。
func clearNetnsDNSCache(ns string) {
	netnsDNSRegistryMu.Lock()
	d := netnsDNSRegistry[ns]
	netnsDNSRegistryMu.Unlock()
	if d != nil {
		d.clear()
	}
}

func (d *netnsDNS) clear() {
	d.mu.Lock()
	d.gen++
	d.cache = map[string]netnsDNSEntry{}
	d.mu.Unlock()
}

// resolve 返回 host 的 A 记录（IPv4，去重，按应答顺序）。
func (d *netnsDNS) resolve(ctx context.Context, host string) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if host == "" {
		return nil, errors.New("空主机名")
	}

	d.mu.Lock()
	if e, ok := d.cache[host]; ok && time.Now().Before(e.expire) && e.gen == d.gen {
		ips := append([]string(nil), e.ips...)
		d.mu.Unlock()
		if len(ips) == 0 {
			return nil, errDNSNoRecord
		}
		return ips, nil
	}
	if c, ok := d.inflight[host]; ok {
		d.mu.Unlock()
		select {
		case <-c.done:
			if c.err != nil {
				return nil, c.err
			}
			return append([]string(nil), c.ips...), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	call := &netnsDNSCall{done: make(chan struct{})}
	d.inflight[host] = call
	gen := d.gen
	d.mu.Unlock()

	lookupCtx, cancel := context.WithTimeout(ctx, dnsLookupTimeout)
	ips, err := d.lookup(lookupCtx, host, d.servers)
	cancel()
	d.mu.Lock()
	// 在途期间隧道重建过：结果属于旧出口，不写缓存。
	if d.gen == gen {
		expire := time.Now().Add(d.ttl)
		if err != nil {
			expire = time.Now().Add(d.negTTL)
		}
		d.cache[host] = netnsDNSEntry{ips: append([]string(nil), ips...), expire: expire, gen: gen}
	}
	delete(d.inflight, host)
	d.mu.Unlock()

	call.ips, call.err = ips, err
	close(call.done)

	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, errDNSNoRecord
	}
	return append([]string(nil), ips...), nil
}

// lookupInNetns 依次尝试每个服务器的 UDP 53，整轮失败后再退到 TCP 53。
// 每次尝试单独限时，避免一台服务器把总预算吃光、后面的回退没机会执行。
func (d *netnsDNS) lookupInNetns(ctx context.Context, host string, servers []string) ([]string, error) {
	var lastErr error
	for _, tcpOnly := range []bool{false, true} {
		for _, server := range servers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			attemptCtx, cancel := context.WithTimeout(ctx, dnsAttemptTimeout)
			ips, err := d.lookupVia(attemptCtx, host, server, tcpOnly)
			cancel()
			if err == nil && len(ips) > 0 {
				return ips, nil
			}
			if err != nil {
				lastErr = err
			}
		}
	}
	if lastErr == nil {
		lastErr = errDNSNoRecord
	}
	return nil, lastErr
}

// lookupVia 用指定服务器查一次 A 记录。
// 服务器地址由我们固定；net.Resolver 从母机 resolv.conf 读到的地址被忽略，
// 只借用它的协议选择（UDP 优先、应答截断时自动改 TCP）。
func (d *netnsDNS) lookupVia(ctx context.Context, host, server string, tcpOnly bool) ([]string, error) {
	dial := d.dial
	r := &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			if tcpOnly {
				network = "tcp"
			}
			return dial(forceIPv4Network(network), server)
		},
	}
	ips, err := r.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ips))
	seen := make(map[string]bool, len(ips))
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		s := v4.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out, nil
}

// dnsServersFromEnv 读 FANOUT_DNS_SERVERS，支持 "ip" 或 "ip:port" 列表。
func dnsServersFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("FANOUT_DNS_SERVERS"))
	if raw == "" {
		raw = defaultDNSList
	}
	out := make([]string, 0, 2)
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, _, err := net.SplitHostPort(part); err != nil {
			ip := net.ParseIP(part)
			if ip == nil || ip.To4() == nil {
				continue
			}
			part = net.JoinHostPort(part, "53")
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		out = strings.Split(defaultDNSList, ",")
	}
	return out
}

// dnsCacheTTLFromEnv 读 FANOUT_DNS_TTL（秒）。
func dnsCacheTTLFromEnv() time.Duration {
	v := strings.TrimSpace(os.Getenv("FANOUT_DNS_TTL"))
	if v == "" {
		return defaultDNSCacheTTL
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || n > 3600 {
		return defaultDNSCacheTTL
	}
	return time.Duration(n) * time.Second
}

// resolvConfContent 生成 netns 内的 resolv.conf。
// ip netns exec 会把它挂进命名空间，openvpn/curl 等子进程用它解析；
// 进程内解析（netnsDNS）用同一组服务器，两边行为保持一致。
func resolvConfContent() string {
	var b strings.Builder
	for _, server := range dnsServersFromEnv() {
		host, _, err := net.SplitHostPort(server)
		if err != nil {
			host = server
		}
		fmt.Fprintf(&b, "nameserver %s\n", host)
	}
	b.WriteString("options timeout:2 attempts:2\n")
	return b.String()
}
