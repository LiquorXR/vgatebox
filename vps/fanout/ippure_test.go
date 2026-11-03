package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func intPtr(value int) *int { return &value }

func TestProfileFromIPPureRequiresExplicitResidentialEvidence(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	raw := ippureResponse{
		IP: "70.79.253.35", ASN: 7922, ASOrganization: "Comcast",
		CountryCode: "US", City: "Philadelphia",
		FraudScore: intPtr(12), IsResidential: func() *bool { v := true; return &v }(),
	}
	image, err := profileFromIPPure(raw, raw.IP, now)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "strict_residential" || image.Reason != "ippure_explicit_residential" {
		t.Fatalf("explicit residential was not confirmed: %+v", image)
	}
	if image.Country == nil || *image.Country != "US" || image.ASN == nil || *image.ASN != "AS7922" || image.ISP == nil || *image.ISP != "Comcast" {
		t.Fatalf("residential evidence lost fields: %+v", image)
	}
	if image.Residential == nil || !*image.Residential || image.FraudScore == nil || *image.FraudScore != 12 {
		t.Fatalf("residential flags lost: %+v", image)
	}

	raw.IsResidential = func() *bool { v := false; return &v }()
	image, err = profileFromIPPure(raw, raw.IP, now)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_not_residential" {
		t.Fatalf("non-residential answer became a type: %+v", image)
	}

	raw.IsResidential = nil
	image, err = profileFromIPPure(raw, raw.IP, now)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_missing_residential" {
		t.Fatalf("missing residential answer became a type: %+v", image)
	}

	raw.IsResidential = func() *bool { v := true; return &v }()
	raw.ASN = 0
	image, err = profileFromIPPure(raw, raw.IP, now)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_incomplete_residential" {
		t.Fatalf("incomplete residential evidence was confirmed: %+v", image)
	}
}

func TestProfileFromIPPureRejectsWrongCallerIP(t *testing.T) {
	raw := ippureResponse{IP: "8.8.8.8", IsResidential: func() *bool { v := true; return &v }()}
	if _, err := profileFromIPPure(raw, "70.79.253.35", time.Now()); err == nil {
		t.Fatal("wrong IP should not be accepted as exit evidence")
	}
}

func completeIPPureResponse(ip string, residential bool) ippureResponse {
	return ippureResponse{
		IP: ip, ASN: 7922, ASOrganization: "Comcast", CountryCode: "US", City: "Philadelphia",
		FraudScore: intPtr(12), IsResidential: func() *bool { v := residential; return &v }(),
	}
}

func TestIPPureCacheStoresOnlyCompletePassedAnswers(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", false)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || !ippureCacheable(raw, image, raw.IP) {
		t.Fatalf("successful non-residential answer should cache as unknown: %+v", image)
	}
	m.storeIPPure(raw.IP, image)
	got, ok := m.cachedIPPure(raw.IP, now.Add(time.Hour))
	if !ok || got.IP != raw.IP || got.IPType != "unknown" || !got.CacheUsed {
		t.Fatalf("same exit IP did not reuse cache: %+v ok=%v", got, ok)
	}
	if _, ok := m.cachedIPPure("8.8.8.8", now.Add(time.Hour)); ok {
		t.Fatal("different exit IP reused another IP's cache")
	}
	if _, ok := m.cachedIPPure(raw.IP, now.Add(25*time.Hour)); ok {
		t.Fatal("expired IPPure cache was reused")
	}
	stats := m.ippureCacheStats()
	if stats.Entries != 0 || stats.Hits != 1 {
		t.Fatalf("cache stats: %+v", stats)
	}
	info, err := os.Stat(filepath.Join(m.workDir, "ippure-cache.json"))
	if err == nil && info.Mode().Perm() != 0600 {
		t.Fatal("IPPure cache file is not private")
	}
}

func TestIPPureCacheRejectsFailuresMismatchAndIncompleteFields(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", false)
	image, _ := profileFromIPPureTTL(raw, raw.IP, now, 24*time.Hour)

	for name, mutate := range map[string]func(*ippureResponse, *EgressProfile){
		"wrong-ip":        func(r *ippureResponse, _ *EgressProfile) { r.IP = "8.8.8.8" },
		"missing-type":    func(r *ippureResponse, _ *EgressProfile) { r.IsResidential = nil },
		"missing-asn":     func(r *ippureResponse, _ *EgressProfile) { r.ASN = 0 },
		"missing-isp":     func(r *ippureResponse, _ *EgressProfile) { r.ASOrganization = "" },
		"missing-country": func(r *ippureResponse, _ *EgressProfile) { r.CountryCode = "" },
		"failed-status":   func(_ *ippureResponse, p *EgressProfile) { p.IntelligenceStatus = "failed" },
		"bad-type":        func(_ *ippureResponse, p *EgressProfile) { p.IPType = "likely_residential" },
	} {
		changedRaw := raw
		changedImage := image
		mutate(&changedRaw, &changedImage)
		if ippureCacheable(changedRaw, changedImage, raw.IP) {
			t.Fatalf("%s answer was cached", name)
		}
		if name == "failed-status" || name == "bad-type" {
			m.storeIPPure(raw.IP, changedImage)
			if _, ok := m.cachedIPPure(raw.IP, now.Add(time.Hour)); ok {
				t.Fatalf("%s answer was stored", name)
			}
		}
	}
}

func TestIPPureCacheManualClearAndConfiguredTTL(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expiry, _ := time.Parse(time.RFC3339Nano, image.ExpiresAt)
	if expiry.Sub(now) != 2*time.Hour {
		t.Fatalf("configured IPPure TTL not applied: %s", expiry.Sub(now))
	}
	m.setIPPureTTL(2 * time.Hour)
	m.storeIPPure(raw.IP, image)
	if _, ok := m.cachedIPPure(raw.IP, now.Add(time.Hour)); !ok {
		t.Fatal("fresh cache entry missing")
	}
	if err := m.clearIPPureCache(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.cachedIPPure(raw.IP, now); ok {
		t.Fatal("manual clear left an entry")
	}
	if _, err := os.Stat(filepath.Join(m.workDir, "ippure-cache.json")); !os.IsNotExist(err) {
		t.Fatal("manual clear left the cache file")
	}
	stats := m.ippureCacheStats()
	if stats.Entries != 0 || stats.Hits != 0 || stats.Queries != 0 {
		t.Fatalf("manual clear did not reset stats: %+v", stats)
	}
}
