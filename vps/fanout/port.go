package main

import (
	"fmt"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
)

// fixedSlotPort returns a stable port when the validated-pool integration is
// configured.  A slot is an operator-facing identity, so replacing its VPN
// node must never change the SOCKS port.
func fixedSlotPort(slot int) (int, bool, error) {
	baseText := strings.TrimSpace(os.Getenv("FANOUT_SLOT_PORT_BASE"))
	if baseText == "" {
		return 0, false, nil
	}
	base, err := strconv.Atoi(baseText)
	if err != nil || base < 1024 || base > 65535 {
		return 0, true, fmt.Errorf("FANOUT_SLOT_PORT_BASE 无效: %q", baseText)
	}
	port := base + slot - 1
	if port < 1024 || port > 65535 {
		return 0, true, fmt.Errorf("槽位 %d 的固定端口超出范围", slot)
	}
	return port, true, nil
}

// 随机端口的取值范围，落在 IANA 动态端口区间内，避开常见服务。
const (
	randPortMin = 20000
	randPortMax = 60000
)

// freeRandomPort 随机挑一个当前空闲的 TCP 端口。
//
// taken 里的端口会被跳过，用于避开本进程已经分配但还没真正监听的端口。
// 实际可用性以能否 bind 为准，这样不会和系统上其他进程抢。
func freeRandomPort(taken map[int]bool) (int, error) {
	for i := 0; i < 200; i++ {
		port := randPortMin + rand.Intn(randPortMax-randPortMin)
		if taken[port] {
			continue
		}
		if portAvailable(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("找不到可用端口（已尝试 200 次）")
}

// portAvailable 通过尝试监听来判断端口是否真的空闲。
func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}
