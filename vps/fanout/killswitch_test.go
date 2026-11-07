package main

import (
	"testing"
	"time"
)

func TestMissingRemotesDedupes(t *testing.T) {
	installed := []vpnRemote{{ip: "1.1.1.1", port: "443", proto: "tcp"}}
	fresh := []vpnRemote{
		{ip: "1.1.1.1", port: "443", proto: "tcp"},
		{ip: "2.2.2.2", port: "443", proto: "tcp"},
		{ip: "2.2.2.2", port: "443", proto: "tcp"},
		{ip: "1.1.1.1", port: "1194", proto: "udp"},
	}
	got := missingRemotes(installed, fresh)
	if len(got) != 2 || got[0].ip != "2.2.2.2" || got[1].port != "1194" {
		t.Fatalf("delta wrong: %+v", got)
	}
	if got := missingRemotes(installed, installed); len(got) != 0 {
		t.Fatalf("stable DNS reported as delta: %+v", got)
	}
}

func TestStoredRemotesFallbackAndDue(t *testing.T) {
	tun := &Tunnel{}
	if got := tun.storedRemotes(); len(got) != 0 {
		t.Fatalf("empty config should yield no remotes: %+v", got)
	}
	tun.vpnRemotes = []vpnRemote{{ip: "9.9.9.9", port: "443", proto: "tcp"}}
	tun.Node.Config = "proto udp\nremote 192.0.2.1 1194"
	if got := tun.storedRemotes(); len(got) != 1 || got[0].ip != "9.9.9.9" {
		t.Fatalf("snapshot not preferred: %+v", got)
	}
	if !tun.killswitchDue() {
		t.Fatal("never-refreshed tunnel not due")
	}
	tun.killswitchRefreshedAt = time.Now()
	if tun.killswitchDue() {
		t.Fatal("freshly refreshed tunnel due")
	}
}
