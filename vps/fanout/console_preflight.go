package main

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"
)

// Be conservative: preflight only simple literal-IP TCP profiles. UDP,
// hostnames, connection blocks and unknown syntax go to full verification;
// an incomplete parser must never reject a profile that OpenVPN can use.
func preflightEndpoints(config string) []string {
	proto := "udp"
	inline := false
	var lines [][]string
	for _, raw := range strings.Split(config, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "<connection") {
			return nil
		}
		if strings.HasPrefix(line, "</") {
			inline = false
			continue
		}
		if strings.HasPrefix(line, "<") {
			inline = true
			continue
		}
		if inline || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "proto" && len(fields) == 2 {
			proto = fields[1]
		}
		if fields[0] == "remote" {
			lines = append(lines, fields)
		}
	}
	if len(lines) == 0 || len(lines) > 8 {
		return nil
	}
	var endpoints []string
	for _, fields := range lines {
		if len(fields) < 3 || len(fields) > 4 {
			return nil
		}
		p := proto
		if len(fields) == 4 {
			p = fields[3]
		}
		if p != "tcp" && p != "tcp-client" && p != "tcp4" && p != "tcp4-client" {
			return nil
		}
		ip := net.ParseIP(fields[1])
		port, err := strconv.Atoi(fields[2])
		if ip == nil || ip.To4() == nil || err != nil || port < 1 || port > 65535 {
			return nil
		}
		endpoints = append(endpoints, net.JoinHostPort(ip.To4().String(), strconv.Itoa(port)))
	}
	return endpoints
}

func preflightCandidate(parent context.Context, node Node) error {
	endpoints := preflightEndpoints(node.Config)
	if len(endpoints) == 0 {
		return parent.Err()
	}
	ctx, cancel := context.WithTimeout(parent, 6*time.Second)
	defer cancel()
	return preflightDial(ctx, endpoints, func(ctx context.Context, endpoint string) error {
		return inMainNetns(func() error {
			dialer := net.Dialer{Timeout: 2 * time.Second}
			conn, err := dialer.DialContext(ctx, "tcp4", endpoint)
			if err == nil {
				_ = conn.Close()
			}
			return err
		})
	})
}

func preflightDial(ctx context.Context, endpoints []string, dial func(context.Context, string) error) error {
	var last error
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		last = dial(ctx, endpoint)
		if last == nil {
			return nil
		}
	}
	return last
}

// preflightAll 并发预检一批候选，返回每 hostname 的预检结果（nil=通过或
// 非 TCP 保守放行）。调用方循环内直接查表，不再逐个 6s 串行等待：
// 死节点多时一轮预检从 N×6s 降到约 6s。只做只读 TCP 拨号，不碰 netns，
// 与 claimNode/teardown 无交互，顺序由调用方循环保持。
func preflightAll(parent context.Context, candidates []Node) map[string]error {
	out := make(map[string]error, len(candidates))
	if len(candidates) == 0 {
		return out
	}
	type answer struct {
		host string
		err  error
	}
	answers := make(chan answer, len(candidates))
	for _, node := range candidates {
		go func(node Node) {
			answers <- answer{host: node.HostName, err: preflightCandidate(parent, node)}
		}(node)
	}
	for range candidates {
		select {
		case a := <-answers:
			if _, dup := out[a.host]; !dup {
				out[a.host] = a.err
			}
		case <-parent.Done():
			// 剩下的按取消算，调用方循环见到 ctx.Err 会直接退出。
			for _, node := range candidates {
				if _, ok := out[node.HostName]; !ok {
					out[node.HostName] = parent.Err()
				}
			}
			return out
		}
	}
	return out
}
