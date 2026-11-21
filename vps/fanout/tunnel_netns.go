package main

// tunnel_netns.go：netns/iptables/OpenVPN 进程启停。
import (
	"bytes"
	"fanout/internal/paths"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"
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

// runBatchIP 把多条 iproute2 命令一次性交给 `ip -batch -` 执行，
// 将 setupNetns 主机侧的多次 fork 合并为一次子进程调用。
// 脚本中每行一条完整 ip 命令（不含前导 `ip`），失败时返回合并输出。
func runBatchIP(script string) error {
	var out []byte
	err := inMainNetns(func() error {
		cmd := exec.Command("ip", "-batch", "-")
		cmd.Stdin = strings.NewReader(script)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		e := cmd.Run()
		out = buf.Bytes()
		return e
	})
	if err != nil {
		return fmt.Errorf("ip -batch: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// runNetnsBatch 在指定 netns 内批量执行 iproute2 命令，将
// `ip netns exec <ns> ip ...` 的多次外部 fork 合并为一次。
func runNetnsBatch(ns, script string) error {
	var out []byte
	err := inMainNetns(func() error {
		cmd := exec.Command("ip", "netns", "exec", ns, "ip", "-batch", "-")
		cmd.Stdin = strings.NewReader(script)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		e := cmd.Run()
		out = buf.Bytes()
		return e
	})
	if err != nil {
		return fmt.Errorf("ip netns exec %s ip -batch: %v: %s", ns, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// tun0Ready 进程内检查指定 netns 的 tun0 是否已拿到 IPv4 地址，
// 替代轮询 `ip netns exec <ns> ip -4 addr show tun0` 的每秒 fork。
// 在独立锁定线程中短暂切入目标 ns 检查，完成后立即切回母机 ns。
func tun0Ready(ns string) bool {
	ready := make(chan bool, 1)
	go func() {
		runtime.LockOSThread()
		// 注意：进入隧道 netns 后，只有成功切回母机 netns 才可 UnlockOSThread；
		// 切回失败时不解锁直接退出，让 Go runtime 回收该线程（见 netnsguard.go）。
		origin, err := os.Open("/proc/self/ns/net")
		if err != nil {
			runtime.UnlockOSThread()
			ready <- false
			return
		}
		defer origin.Close()
		target, err := os.Open("/var/run/netns/" + ns)
		if err != nil {
			runtime.UnlockOSThread()
			ready <- false
			return
		}
		defer target.Close()
		if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
			runtime.UnlockOSThread()
			ready <- false
			return
		}
		hasAddr := false
		iface, err := net.InterfaceByName("tun0")
		if err == nil {
			addrs, aerr := iface.Addrs()
			if aerr == nil {
				for _, a := range addrs {
					var ip net.IP
					switch v := a.(type) {
					case *net.IPNet:
						ip = v.IP
					case *net.IPAddr:
						ip = v.IP
					}
					if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
						hasAddr = true
						break
					}
				}
			}
		}
		// 切回母机 ns：优先使用启动时记录的 mainNetns，与 inMainNetns 一致；
		// 仅在切回成功后才解锁线程，否则不解锁直接退出以销毁被污染线程。
		restoreFD := int(origin.Fd())
		if mainNetns != nil {
			restoreFD = int(mainNetns.Fd())
		}
		if err := unix.Setns(restoreFD, unix.CLONE_NEWNET); err != nil {
			ready <- false
			return
		}
		runtime.UnlockOSThread()
		ready <- hasAddr
	}()
	return <-ready
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
	// 主机侧 veth 创建/移交/配地址/启用：一次 ip -batch 合并 4 次 fork。
	// 注意 `link set <peer> netns <ns>` 必须在同一批次内紧随 add，顺序不可调换。
	hostBatch := strings.Join([]string{
		"link add " + veth + " type veth peer name " + peer,
		"link set " + peer + " netns " + ns,
		"addr add " + t.linkAddress(1) + "/30 dev " + veth,
		"link set " + veth + " up",
	}, "\n") + "\n"
	if err := runBatchIP(hostBatch); err != nil {
		return err
	}
	// netns 内 lo/地址/链路/路由：一次 `ip netns exec <ns> ip -batch` 合并
	// 原先 3 次外部 fork + 3 次内部 ip 调用。
	nsBatch := strings.Join([]string{
		"link set lo up",
		"addr add " + t.linkAddress(2) + "/30 dev " + peer,
		"link set " + peer + " up",
		"route add default via " + t.linkAddress(1),
	}, "\n") + "\n"
	if err := runNetnsBatch(ns, nsBatch); err != nil {
		return err
	}

	// netns 内的 DNS：ip netns exec 会把这个文件挂进命名空间，openvpn
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

	// openvpn 建好 tun0 前 SOCKS5 无法正常出网，这里等它就绪。
	// 进程内检查 tun0 是否拿到 IPv4，避免每秒 fork `ip netns exec`。
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if tun0Ready(ns) {
			return nil
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
