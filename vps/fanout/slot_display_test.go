package main

import (
	"testing"
	"time"
)

// publishUp 记录上次可用出口：重连清空 ExitIP 后界面仍可展示。
func TestPublishUpRecordsLastExit(t *testing.T) {
	m := NewManager(4, t.TempDir())
	tun := &Tunnel{Slot: 1, Port: 18001, Status: "starting",
		Node: Node{HostName: "h", Config: "c"}, ExitIP: "203.0.113.9",
		egress: &EgressProfile{}}
	tun.resetWork()
	m.tunnels[1] = tun
	if !m.publishUp(tun, tun.workContext()) {
		t.Fatal("publishUp refused a healthy tunnel")
	}
	view := tun.snapshot()
	if view.LastExitIP != "203.0.113.9" || view.LastExitAt == "" {
		t.Fatalf("last exit not recorded: %+v", view)
	}
	// 重连清空当前出口，上次可用保留。
	tun.mu.Lock()
	tun.Status, tun.ExitIP = "starting", ""
	tun.mu.Unlock()
	view = tun.snapshot()
	if view.ExitIP != "" || view.LastExitIP != "203.0.113.9" {
		t.Fatalf("reconnect dropped the last exit: %+v", view)
	}
}

// waiting_for_nodes 只在有未来重试时刻时暴露 retry_at。
func TestWaitingExposesRetryAtOnlyWhenFuture(t *testing.T) {
	tun := &Tunnel{Slot: 1, Status: "waiting_for_nodes"}
	tun.mu.Lock()
	tun.nextRetryAt = time.Now().Add(30 * time.Second)
	tun.mu.Unlock()
	if view := tun.snapshot(); view.RetryAt == "" {
		t.Fatal("future retry_at omitted")
	}
	tun.mu.Lock()
	tun.nextRetryAt = time.Now().Add(-time.Second)
	tun.mu.Unlock()
	if view := tun.snapshot(); view.RetryAt != "" {
		t.Fatalf("expired retry_at leaked: %q", view.RetryAt)
	}
	tun.mu.Lock()
	tun.Status, tun.nextRetryAt = "up", time.Now().Add(time.Minute)
	tun.mu.Unlock()
	if view := tun.snapshot(); view.RetryAt != "" {
		t.Fatalf("retry_at leaked outside waiting: %q", view.RetryAt)
	}
}
