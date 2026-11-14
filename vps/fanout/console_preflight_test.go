package main

import (
	"context"
	"testing"
	"time"
)

// preflightAll 必须与逐个预检语义一致：TCP 字面量不通算失败，
// UDP/域名/解析不出端点的保守放行；只是把 N×6s 变成约 6s。
func TestPreflightAllMatchesSequential(t *testing.T) {
	deadTCP := Node{HostName: "dead", Config: "proto tcp\nremote 127.0.0.1 1\n"}
	hostname := Node{HostName: "name", Config: "proto tcp\nremote example.com 443\n"}
	udp := Node{HostName: "udp", Config: "proto udp\nremote 203.0.113.1 1194\n"}
	candidates := []Node{deadTCP, hostname, udp}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	got := preflightAll(ctx, candidates)
	if len(got) != len(candidates) {
		t.Fatalf("preflightAll returned %d results, want %d", len(got), len(candidates))
	}
	if got["dead"] == nil {
		t.Fatal("closed TCP port passed preflight")
	}
	if got["name"] != nil {
		t.Fatalf("hostname profile blocked: %v", got["name"])
	}
	if got["udp"] != nil {
		t.Fatalf("UDP profile blocked: %v", got["udp"])
	}
	// 逐个对照：同一输入必须同结论。
	for _, n := range candidates {
		want := preflightCandidate(ctx, n) == nil
		if (got[n.HostName] == nil) != want {
			t.Fatalf("host %s diverged: cached=%v sequential=%v", n.HostName, got[n.HostName], want)
		}
	}
}

func TestPreflightAllEmpty(t *testing.T) {
	got := preflightAll(context.Background(), nil)
	if len(got) != 0 {
		t.Fatalf("empty input returned %d results", len(got))
	}
}
