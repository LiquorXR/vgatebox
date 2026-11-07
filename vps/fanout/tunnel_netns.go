package main

// tunnel_netns.go：netns/iptables/OpenVPN 进程启停。
import (
	"fanout/internal/paths"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// run 执行一条网络配置命令。
//
// 走 cmdCombined 而不是直接 exec：建 veth、改 iptables 都是"在当前网络
// 命名空间里生效"的操作，线程可能已经被 dialerInNetns 带进某条隧道，
// 那样规则会加到隧道内部，母机上什么也没有（见 netnsguard.go）。
func run(name string, args ...string) error {
	out, err := cmdCombined(exec.Command(name, args...))
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runQuiet 执行清理类命令，忽略"本来就不存在"这类错误。
func runQuiet(name string, args ...string) {
	_ = cmdRun(exec.Command(name, args...))
}

// setupNetns 建立 netns 与 veth 链路，并配好 NAT 与转发放行。
func (t *Tunnel) setupNetns() error {
	if t.linkIndex() < 0 || t.linkIndex() >= networkLinkCount {
		return fmt.Errorf("网络地址分配无效")
	}
	ns := t.nsName()
	veth, peer := t.vethNames()

	t.teardownNetns()

	if err := run("ip", "netns", "add", ns); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "link", "set", "lo", "up"); err != nil {
		return err
	}
	if err := run("ip", "link", "add", veth, "type", "veth", "peer", "name", peer); err != nil {
		return err
	}
	if err := run("ip", "link", "set", peer, "netns", ns); err != nil {
		return err
	}
	if err := run("ip", "addr", "add", t.linkAddress(1)+"/30", "dev", veth); err != nil {
		return err
	}
	if err := run("ip", "link", "set", veth, "up"); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "addr", "add", t.linkAddress(2)+"/30", "dev", peer); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "link", "set", peer, "up"); err != nil {
		return err
	}
	if err := run("ip", "netns", "exec", ns, "ip", "route", "add", "default", "via", t.linkAddress(1)); err != nil {
		return err
	}

	// netns 内的 DNS：ip netns exec 会把这个文件挂进命名空间，openvpn/curl
	// 等子进程用它解析；进程内解析（netns_dns.go）也用同一组服务器。
	nsDir := filepath.Join("/etc/netns", ns)
	if err := os.MkdirAll(nsDir, 0755); err != nil {
		return fmt.Errorf("创建 %s 失败: %w", nsDir, err)
	}
	if err := os.WriteFile(filepath.Join(nsDir, "resolv.conf"), []byte(resolvConfContent()), 0644); err != nil {
		return fmt.Errorf("写 resolv.conf 失败: %w", err)
	}

	cidr := t.linkCIDR()
	ensureRule("nat", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
	if err := installKillSwitch(t); err != nil {
		return err
	}
	return nil
}

// ensureRule 幂等追加一条 iptables 规则。
func ensureRule(table, chain string, spec ...string) {
	check := append([]string{"-w", "5", "-t", table, "-C", chain}, spec...)
	if cmdRun(exec.Command("iptables", check...)) == nil {
		return
	}
	add := append([]string{"-w", "5", "-t", table, "-A", chain}, spec...)
	runQuiet("iptables", add...)
}

// ensureRuleInsert 幂等插入规则到链首。
// FORWARD 链末尾常有兜底 REJECT，必须插到最前面才生效。
func ensureRuleInsert(table, chain string, spec ...string) {
	check := append([]string{"-w", "5", "-t", table, "-C", chain}, spec...)
	if cmdRun(exec.Command("iptables", check...)) == nil {
		return
	}
	ins := append([]string{"-w", "5", "-t", table, "-I", chain, "1"}, spec...)
	runQuiet("iptables", ins...)
}

func (t *Tunnel) teardownNetns() {
	t.clearEgress()
	ns := t.nsName()
	cidr := t.linkCIDR()
	veth, _ := t.vethNames()
	// Candidate rotation can reach teardown after OpenVPN has failed before
	// startOpenVPN returned. Kill that process here as well; otherwise the
	// next candidate inherits the same tun name and stale OpenVPN children
	// remain outside the manager's state.
	if t.ovpn != nil && t.ovpn.Process != nil {
		_ = t.ovpn.Process.Kill()
		t.ovpn = nil
	}
	removeKillSwitch(t)
	runQuiet("ip", "netns", "del", ns)
	// 出口换节点后地区变了，旧出口解析出的地址必须丢弃。
	clearNetnsDNSCache(ns)
	runQuiet("ip", "link", "del", veth)
	runQuiet("iptables", "-w", "5", "-t", "nat", "-D", "POSTROUTING", "-s", cidr, "-j", "MASQUERADE")
}

// startOpenVPN 在 netns 内拉起 openvpn，并等待 tun0 拿到地址。
func (t *Tunnel) startOpenVPN(dir string) error {
	ns := t.nsName()
	// 诊断用临时 Manager 的 workDir 是裸 TempDir（见 probeCandidate），
	// run 子目录不存在；生产 workDir 安装时已建好，此处幂等无影响。
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("建运行目录失败: %w", err)
	}
	cfgPath := paths.SlotOVPN(dir, ns)
	if err := os.WriteFile(cfgPath, []byte(t.Node.Config), 0600); err != nil {
		return fmt.Errorf("写配置失败: %w", err)
	}
	authPath := paths.SlotAuth(dir)
	if err := os.WriteFile(authPath, []byte("vpn\nvpn\n"), 0600); err != nil {
		return fmt.Errorf("写凭据失败: %w", err)
	}

	logPath := paths.SlotLog(dir, ns)
	ctx := t.workContext()
	cmd := exec.CommandContext(ctx, "ip", "netns", "exec", ns, "openvpn",
		"--config", cfgPath,
		"--auth-user-pass", authPath,
		"--auth-nocache",
		"--dev", "tun0",
		"--connect-retry-max", "2",
		"--connect-timeout", "20",
		"--tls-timeout", "10",
		"--data-ciphers", "AES-128-CBC:AES-256-GCM:AES-128-GCM:CHACHA20-POLY1305",
		"--verb", "3",
		"--log", logPath,
	)
	if err := cmdStart(cmd); err != nil {
		return fmt.Errorf("启动 openvpn 失败: %w", err)
	}
	t.ovpn = cmd
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()

	// openvpn 建好 tun0 前 SOCKS5 无法正常出网，这里等它就绪
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if out, err := cmdOutput(exec.CommandContext(ctx, "ip", "netns", "exec", ns, "ip", "-4", "addr", "show", "tun0")); err == nil {
			if strings.Contains(string(out), "inet ") {
				return nil
			}
		}
		select {
		case <-exited:
			return fmt.Errorf("openvpn 提前退出，详见 %s", logPath)
		default:
		}
		if !waitForWork(ctx, time.Second) {
			return ctx.Err()
		}
	}
	return fmt.Errorf("等待 tun0 就绪超时，详见 %s", logPath)
}
