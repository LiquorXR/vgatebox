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
func freeRandomPort(taken map[int]bool) (int, error) {
	return freeRandomPortFor("tcp", taken)
}

// freeRandomPortFor 按传输方式挑端口：Hysteria2 使用 UDP，其他使用 TCP。
func freeRandomPortFor(network string, taken map[int]bool) (int, error) {
	for i := 0; i < 200; i++ {
		port := randPortMin + rand.Intn(randPortMax-randPortMin)
		if taken[port] {
			continue
		}
		if portAvailableFor(network, port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("找不到可用端口（已尝试 200 次）")
}

// portAvailable 通过尝试监听来判断 TCP 端口是否真的空闲。
func portAvailable(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

// portAvailableUDP 判断 UDP 端口是否空闲；Hysteria2 必须用它。
func portAvailableUDP(port int) bool {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// portAvailableFor 按传输方式选择 TCP 或 UDP 可用性检查。
func portAvailableFor(network string, port int) bool {
	if network == "hysteria" || network == "mkcp" {
		return portAvailableUDP(port)
	}
	return portAvailable(port)
}

// parsePortRange 解析单个 UDP 端口范围，例如 "20000-30000" 或 "20000"。
// 面板只接受单个连续范围，避免配置和防火墙规则过于复杂。
func parsePortRange(raw string) (start, end int, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, 0, fmt.Errorf("端口范围为空")
	}
	if strings.Contains(raw, ",") {
		return 0, 0, fmt.Errorf("端口范围只支持一个连续区间")
	}
	parts := strings.SplitN(raw, "-", 2)
	start, err = strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || start < 1 || start > 65535 {
		return 0, 0, fmt.Errorf("端口范围起点无效")
	}
	end = start
	if len(parts) == 2 {
		end, err = strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || end < start || end > 65535 {
			return 0, 0, fmt.Errorf("端口范围终点无效")
		}
	}
	return start, end, nil
}

// freeUDPPortInRange 在范围内找一个空闲的 UDP 端口作为主端口。
func freeUDPPortInRange(start, end int, taken map[int]bool) (int, error) {
	for port := start; port <= end && port < start+256; port++ {
		if taken[port] {
			continue
		}
		if portAvailableUDP(port) {
			return port, nil
		}
	}
	return 0, fmt.Errorf("端口范围 %d-%d 内找不到可用 UDP 端口", start, end)
}

// udpRangeAvailable 检查范围内端口是否可用。范围较大时只抽查首尾，
// 避免创建入口时扫描上千个端口；最终仍由 Xray 静态校验/启动兜底。
func udpRangeAvailable(start, end int) bool {
	if end-start > 256 {
		return portAvailableUDP(start) && portAvailableUDP(end)
	}
	for port := start; port <= end; port++ {
		if !portAvailableUDP(port) {
			return false
		}
	}
	return true
}

// portInRange 判断端口是否落在 "start-end" 范围内。
func portInRange(raw string, port int) bool {
	start, end, err := parsePortRange(raw)
	return err == nil && port >= start && port <= end
}
