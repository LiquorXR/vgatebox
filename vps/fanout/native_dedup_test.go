package main

import (
	"testing"
)

// 运行配置无变化时 apply 必须跳过重启：健康抖动、状态翻转等只改 Slot
// 不改入站的调用到这里收敛为 no-op，其他 Slot 的已有连接不被连带中断。
// 首刷（进程非本实例拉起）与配置变化时仍必须重启。
func TestApplySkipsRestartWhenConfigUnchanged(t *testing.T) {
	n, runtime, tunnels := rollbackFixture(t)
	if err := n.apply(tunnels); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 1 || runtime.stops != 0 {
		t.Fatalf("首刷应重启一次: starts=%d stops=%d", runtime.starts, runtime.stops)
	}
	if err := n.apply(tunnels); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 1 || runtime.stops != 0 {
		t.Fatalf("无变化 apply 应跳过重启: starts=%d stops=%d", runtime.starts, runtime.stops)
	}
	// 改动入站端口：运行配置变化，必须重启。
	n.store.Inbounds[0].Port = 30002
	if err := n.apply(tunnels); err != nil {
		t.Fatal(err)
	}
	if runtime.starts != 2 {
		t.Fatalf("配置变化后应重启: starts=%d", runtime.starts)
	}
	if !n.procLive {
		t.Fatal("成功重启后 procLive 应为 true")
	}
}
