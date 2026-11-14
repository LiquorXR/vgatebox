package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

var exitIPEchoURLs = []string{
	"https://api.ipify.org",
	"https://ipv4.icanhazip.com",
}

type exitIPProbe struct {
	ip        string
	latencyMS float64
}

func probeIPPair(command func(string) ([]byte, error)) (string, error) {
	var got string
	for _, url := range exitIPEchoURLs {
		out, err := command(url)
		if err != nil {
			return "", err
		}
		ip := strings.TrimSpace(string(out))
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			return "", fmt.Errorf("出口 IP 返回异常: %q", ip)
		}
		if got == "" {
			got = parsed.To4().String()
		} else if got != parsed.To4().String() {
			return "", fmt.Errorf("两个出口 IP 查询不一致: %s / %s", got, parsed)
		}
	}
	return got, nil
}

// probePairConcurrent runs both IP echoes independently. Every echo gets its
// own timeout; one slow or failed echo cancels the sibling instead of waiting
// for the whole chain timeout.
func probePairConcurrent(ctx context.Context, perEcho time.Duration, command func(context.Context, string) (exitIPProbe, error)) (string, float64, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type answer struct {
		probe exitIPProbe
		err   error
	}
	answers := make(chan answer, len(exitIPEchoURLs))
	for _, url := range exitIPEchoURLs {
		go func(url string) {
			echoCtx, echoCancel := context.WithTimeout(ctx, perEcho)
			defer echoCancel()
			probe, err := command(echoCtx, url)
			answers <- answer{probe: probe, err: err}
		}(url)
	}
	var got string
	maximum := 0.0
	var firstErr error
	for range exitIPEchoURLs {
		result := <-answers
		if result.err != nil {
			if firstErr == nil {
				firstErr = result.err
				cancel()
			}
			continue
		}
		if got == "" {
			got = result.probe.ip
		} else if got != result.probe.ip {
			if firstErr == nil {
				firstErr = fmt.Errorf("两个出口 IP 查询不一致: %s / %s", got, result.probe.ip)
				cancel()
			}
			continue
		}
		maximum = math.Max(maximum, result.probe.latencyMS)
	}
	if firstErr != nil {
		return "", 0, firstErr
	}
	if got == "" {
		return "", 0, fmt.Errorf("出口 IP 返回异常")
	}
	return got, maximum, nil
}

func parseSocksProbeOutput(out []byte) (exitIPProbe, error) {
	// curl appends the timing after the response body. Echo services may
	// already end that body with a newline (for example icanhazip), so
	// only the final separator belongs to --write-out.
	response := strings.TrimSpace(string(out))
	separator := strings.LastIndexByte(response, '\n')
	if separator < 0 {
		return exitIPProbe{}, fmt.Errorf("SOCKS 出口和延迟响应异常")
	}
	seconds, err := strconv.ParseFloat(strings.TrimSpace(response[separator+1:]), 64)
	if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return exitIPProbe{}, fmt.Errorf("SOCKS 延迟响应异常")
	}
	ip := strings.TrimSpace(response[:separator])
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return exitIPProbe{}, fmt.Errorf("出口 IP 返回异常: %q", ip)
	}
	return exitIPProbe{ip: parsed.To4().String(), latencyMS: seconds * 1000}, nil
}

func (t *Tunnel) probeExpectedExitIP() (string, error) {
	return probeIPPair(func(url string) ([]byte, error) {
		return cmdOutput(exec.CommandContext(t.workContext(), "ip", "netns", "exec", t.nsName(), "curl", "--disable", "-4fsS", "--max-time", "15", url))
	})
}

func (t *Tunnel) probeSocksWithContext(ctx context.Context) (string, float64, error) {
	cred := t.probeCredential()
	proxy := fmt.Sprintf("127.0.0.1:%d", t.Port)
	userpass := cred.User + ":" + cred.Pass
	return probeSocksPair(func(url string) ([]byte, error) {
		return cmdOutput(exec.CommandContext(ctx, "curl", "--disable", "-4fsS", "--noproxy", "", "--max-time", "15", "--socks5-hostname", proxy, "--proxy-user", userpass, "--write-out", "\n%{time_total}", url))
	})
}

// probeSocksMedian 在建连时对两回显各做多次短采、取中位数。
// 正确性语义与单采一致：两回显 IP 必须一致且采样内不漂移。
func (t *Tunnel) probeSocksMedian() (string, float64, *latencyDetail, error) {
	cred := t.probeCredential()
	proxy := fmt.Sprintf("127.0.0.1:%d", t.Port)
	userpass := cred.User + ":" + cred.Pass
	return probeSocksMedianPair(func(url string) ([]exitIPProbeDetailed, error) {
		var out []exitIPProbeDetailed
		for i := 0; i < medianBuildSamples; i++ {
			if ctx := t.workContext(); ctx.Err() != nil {
				return nil, ctx.Err()
			}
			tryCtx, cancel := context.WithTimeout(t.workContext(), medianBuildPerTry)
			blob, err := cmdOutput(exec.CommandContext(tryCtx, "curl", "--disable", "-4fsS", "--noproxy", "", "--max-time", medianBuildCurlMax, "--socks5-hostname", proxy, "--proxy-user", userpass, "--write-out", "\n%{time_appconnect} %{time_starttransfer} %{time_total}", url))
			cancel()
			if err != nil {
				continue
			}
			sample, err := parseSocksProbeDetailed(blob)
			if err != nil {
				continue
			}
			out = append(out, sample)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("出口延迟采样失败")
		}
		return out, nil
	})
}

// probeSocksHealth checks both echoes in parallel with a per-echo timeout.
// The caller supplies the overall tunnel budget.
func (t *Tunnel) probeSocksHealth(ctx context.Context, perEcho time.Duration) (string, float64, error) {
	cred := t.probeCredential()
	proxy := fmt.Sprintf("127.0.0.1:%d", t.Port)
	userpass := cred.User + ":" + cred.Pass
	maxTime := strconv.Itoa(max(1, int(perEcho.Seconds())))
	return probePairConcurrent(ctx, perEcho, func(echoCtx context.Context, url string) (exitIPProbe, error) {
		out, err := cmdOutput(exec.CommandContext(echoCtx, "curl", "--disable", "-4fsS", "--noproxy", "", "--max-time", maxTime, "--socks5-hostname", proxy, "--proxy-user", userpass, "--write-out", "\n%{time_total}", url))
		if err != nil {
			return exitIPProbe{}, err
		}
		return parseSocksProbeOutput(out)
	})
}

func probeSocksPair(command func(string) ([]byte, error)) (string, float64, error) {
	maximum := 0.0
	ip, err := probeIPPair(func(url string) ([]byte, error) {
		out, err := command(url)
		if err != nil {
			return nil, err
		}
		probe, err := parseSocksProbeOutput(out)
		if err != nil {
			return nil, err
		}
		maximum = math.Max(maximum, probe.latencyMS)
		return []byte(probe.ip), nil
	})
	return ip, maximum, err
}

// latencyDetail 是建连时多次短采的中位数与分段耗时：
// tlsMS=建链到 TLS 完成，ttfbMS=TLS 完成到首字节，medianMS=全程中位数
// （即 LatencyMS 口径）。分段用于区分"隧道慢"还是"回显服务慢"。
type latencyDetail struct {
	MedianMS float64
	TLSMS    float64
	TTFBMS   float64
	Samples  int
}

type exitIPProbeDetailed struct {
	ip      string
	appMS   float64
	startMS float64
	totalMS float64
}

// medianBuildSamples 是建连时每回显的短采次数，medianBuildPerTry 是单次超时。
// 两回显并发采，墙钟上限约 3*6s；稳态健康轮仍用单采，不增加常态负载。
const (
	medianBuildSamples = 3
	medianBuildPerTry  = 6 * time.Second
	medianBuildCurlMax = "5"
)

func parseSocksProbeDetailed(out []byte) (exitIPProbeDetailed, error) {
	response := strings.TrimSpace(string(out))
	separator := strings.LastIndexByte(response, '\n')
	if separator < 0 {
		return exitIPProbeDetailed{}, fmt.Errorf("SOCKS 出口和延迟响应异常")
	}
	fields := strings.Fields(strings.TrimSpace(response[separator+1:]))
	if len(fields) != 3 {
		return exitIPProbeDetailed{}, fmt.Errorf("SOCKS 延迟分段响应异常")
	}
	timings := make([]float64, 3)
	for i, text := range fields {
		seconds, err := strconv.ParseFloat(text, 64)
		if err != nil || seconds < 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
			return exitIPProbeDetailed{}, fmt.Errorf("SOCKS 延迟响应异常")
		}
		timings[i] = seconds * 1000
	}
	ip := strings.TrimSpace(response[:separator])
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.To4() == nil {
		return exitIPProbeDetailed{}, fmt.Errorf("出口 IP 返回异常: %q", ip)
	}
	if timings[1] < timings[0] || timings[2] < timings[1] {
		return exitIPProbeDetailed{}, fmt.Errorf("SOCKS 延迟分段不一致")
	}
	return exitIPProbeDetailed{ip: parsed.To4().String(), appMS: timings[0], startMS: timings[1], totalMS: timings[2]}, nil
}

// probeSocksMedianPair 对两回显各取多组样本：每组中位数代表该回显，
// 两回显 IP 必须一致且组内不漂移；延迟取两组中位数的较大者（与单采口径一致）。
func probeSocksMedianPair(command func(url string) ([]exitIPProbeDetailed, error)) (string, float64, *latencyDetail, error) {
	type answer struct {
		samples []exitIPProbeDetailed
		err     error
	}
	answers := make(chan answer, len(exitIPEchoURLs))
	for _, url := range exitIPEchoURLs {
		go func(url string) {
			samples, err := command(url)
			answers <- answer{samples: samples, err: err}
		}(url)
	}
	got := ""
	median := 0.0
	var detail *latencyDetail
	for range exitIPEchoURLs {
		result := <-answers
		if result.err != nil {
			return "", 0, nil, result.err
		}
		if len(result.samples) == 0 {
			return "", 0, nil, fmt.Errorf("出口延迟采样不足")
		}
		sorted := append([]exitIPProbeDetailed(nil), result.samples...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].totalMS < sorted[j].totalMS })
		mid := sorted[len(sorted)/2]
		for _, s := range sorted {
			if s.ip != mid.ip {
				return "", 0, nil, fmt.Errorf("出口 IP 在采样中漂移: %s / %s", mid.ip, s.ip)
			}
		}
		if got == "" {
			got = mid.ip
		} else if got != mid.ip {
			return "", 0, nil, fmt.Errorf("两个出口 IP 查询不一致: %s / %s", got, mid.ip)
		}
		if mid.totalMS > median {
			median = mid.totalMS
			detail = &latencyDetail{MedianMS: mid.totalMS, TLSMS: mid.appMS, TTFBMS: mid.startMS - mid.appMS, Samples: len(sorted)}
		}
	}
	if got == "" || detail == nil {
		return "", 0, nil, fmt.Errorf("出口 IP 返回异常")
	}
	return got, median, detail, nil
}
