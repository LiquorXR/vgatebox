package main

import (
	"context"
	"fmt"
	"math"
	"net"
	"os/exec"
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

func (t *Tunnel) probeSocksExitIP() (string, error) {
	ip, _, err := t.probeSocksExitIPWithLatency()
	return ip, err
}

func (t *Tunnel) probeSocksExitIPWithLatency() (string, float64, error) {
	return t.probeSocksWithContext(t.workContext())
}

func (t *Tunnel) probeSocksWithContext(ctx context.Context) (string, float64, error) {
	cred := t.probeCredential()
	proxy := fmt.Sprintf("127.0.0.1:%d", t.Port)
	userpass := cred.User + ":" + cred.Pass
	return probeSocksPair(func(url string) ([]byte, error) {
		return cmdOutput(exec.CommandContext(ctx, "curl", "--disable", "-4fsS", "--noproxy", "", "--max-time", "15", "--socks5-hostname", proxy, "--proxy-user", userpass, "--write-out", "\n%{time_total}", url))
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
