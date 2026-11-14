package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"fanout/internal/store"
	"fanout/internal/storesqlite"
	"os"
	"sync"
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
	image, err := profileFromIPPureTTL(raw, raw.IP, now, defaultIPTypeTTL)
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
	image, err = profileFromIPPureTTL(raw, raw.IP, now, defaultIPTypeTTL)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_not_residential" {
		t.Fatalf("non-residential answer became a type: %+v", image)
	}

	raw.IsResidential = nil
	image, err = profileFromIPPureTTL(raw, raw.IP, now, defaultIPTypeTTL)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_missing_residential" {
		t.Fatalf("missing residential answer became a type: %+v", image)
	}

	raw.IsResidential = func() *bool { v := true; return &v }()
	raw.ASN = 0
	image, err = profileFromIPPureTTL(raw, raw.IP, now, defaultIPTypeTTL)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || image.Reason != "ippure_incomplete_residential" {
		t.Fatalf("incomplete residential evidence was confirmed: %+v", image)
	}
}

func TestProfileFromIPPureRejectsWrongCallerIP(t *testing.T) {
	raw := ippureResponse{IP: "8.8.8.8", IsResidential: func() *bool { v := true; return &v }()}
	if _, err := profileFromIPPureTTL(raw, "70.79.253.35", time.Now(), defaultIPTypeTTL); err == nil {
		t.Fatal("wrong IP should not be accepted as exit evidence")
	}
}

func completeIPPureResponse(ip string, residential bool) ippureResponse {
	return ippureResponse{
		IP: ip, ASN: 7922, ASOrganization: "Comcast", CountryCode: "US", City: "Philadelphia",
		FraudScore: intPtr(12), IsResidential: func() *bool { v := residential; return &v }(),
	}
}

func TestIPTypeCacheStoresOnlyCompletePassedAnswers(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", false)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if image.IPType != "unknown" || !ipTypeCacheable(raw, image, raw.IP) {
		t.Fatalf("successful non-residential answer should cache as unknown: %+v", image)
	}
	m.storeIPType(raw.IP, image)
	got, ok := m.cachedIPType(raw.IP, now.Add(time.Hour))
	if !ok || got.IP != raw.IP || got.IPType != "unknown" || !got.CacheUsed {
		t.Fatalf("same exit IP did not reuse cache: %+v ok=%v", got, ok)
	}
	if _, ok := m.cachedIPType("8.8.8.8", now.Add(time.Hour)); ok {
		t.Fatal("different exit IP reused another IP's cache")
	}
	if _, ok := m.cachedIPType(raw.IP, now.Add(25*time.Hour)); ok {
		t.Fatal("expired IP type cache was reused")
	}
	stats := m.ipTypeState().statsAt(now.Add(time.Hour))
	if stats.Entries != 0 || stats.Hits != 1 || stats.Queries != 0 {
		t.Fatalf("expired cache should clear entries but keep windowed hits: %+v", stats)
	}
	info, err := os.Stat(paths.IPTypeCache(m.workDir))
	if err == nil && info.Mode().Perm() != 0600 {
		t.Fatal("IP type cache file is not private")
	}
}

func TestIPTypeCacheWindowedStatsSurviveEntryDrain(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	expire := func(ip string) {
		c := m.ipTypeState()
		c.mu.Lock()
		defer c.mu.Unlock()
		e := c.entries[ip]
		e.Profile.ExpiresAt = now.Add(-time.Hour).Format(time.RFC3339Nano)
		c.entries[ip] = e
	}

	rawA := completeIPPureResponse("70.79.253.35", false)
	imageA, err := profileFromIPPureTTL(rawA, rawA.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.storeIPType(rawA.IP, imageA)
	rawB := completeIPPureResponse("8.8.4.4", false)
	imageB, err := profileFromIPPureTTL(rawB, rawB.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.storeIPType(rawB.IP, imageB)
	m.noteIPTypeQuery()
	if _, ok := m.cachedIPType(rawA.IP, now); !ok {
		t.Fatal("fresh cache entry missing")
	}

	// 一条过期、一条有效：条目减一，窗口计数保留。
	expire(rawB.IP)
	stats := m.ipTypeCacheStats()
	if stats.Entries != 1 || stats.Hits != 1 || stats.Queries != 1 {
		t.Fatalf("partial expiry should keep windowed stats: %+v", stats)
	}

	// 最后一条也过期：条目清零，但窗口计数保留（不再随条目归零）。
	expire(rawA.IP)
	stats = m.ipTypeCacheStats()
	if stats.Entries != 0 || stats.Hits != 1 || stats.Queries != 1 {
		t.Fatalf("entry drain should keep windowed stats: %+v", stats)
	}

	// 新查询继续在同一窗口累计。
	m.noteIPTypeQuery()
	stats = m.ipTypeCacheStats()
	if stats.Entries != 0 || stats.Hits != 1 || stats.Queries != 2 {
		t.Fatalf("new query should accumulate in the window: %+v", stats)
	}
}

func TestIPTypeStatsWindowAgesOutOldBuckets(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC().Truncate(time.Hour)
	c := m.ipTypeState()
	// 26 小时前的旧数据：查询与命中各记一次，统计时都要过期。
	c.noteQueryAt(now.Add(-26 * time.Hour))
	c.noteQueryAt(now.Add(-26 * time.Hour))
	c.noteQueryAt(now)
	raw := completeIPPureResponse("70.79.253.35", false)
	image, err := profileFromIPPureTTL(raw, raw.IP, now.Add(-26*time.Hour), 48*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.storeIPType(raw.IP, image)
	if _, ok := m.cachedIPType(raw.IP, now.Add(-25*time.Hour)); !ok {
		t.Fatal("old hit missing")
	}
	if _, ok := m.cachedIPType(raw.IP, now); !ok {
		t.Fatal("fresh hit missing")
	}
	stats := c.statsAt(now)
	if stats.Hits != 1 || stats.Queries != 1 {
		t.Fatalf("old buckets leaked into the window: %+v", stats)
	}
	if stats.Entries != 1 {
		t.Fatalf("entries = %d, want 1", stats.Entries)
	}
}

func TestIPTypeStatsBucketReusedAfter24Hours(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC().Truncate(time.Hour)
	c := m.ipTypeState()
	// 同一下标 24 小时后复用：旧计数被清零，只计最新。
	c.noteQueryAt(now.Add(-24 * time.Hour))
	c.noteQueryAt(now.Add(-24 * time.Hour))
	c.noteQueryAt(now)
	if stats := c.statsAt(now); stats.Queries != 1 {
		t.Fatalf("rotated bucket kept stale counts: %+v", stats)
	}
}

func TestIPTypeCacheRejectsFailuresMismatchAndIncompleteFields(t *testing.T) {
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
		if ipTypeCacheable(changedRaw, changedImage, raw.IP) {
			t.Fatalf("%s answer was cached", name)
		}
		if name == "failed-status" || name == "bad-type" {
			m.storeIPType(raw.IP, changedImage)
			if _, ok := m.cachedIPType(raw.IP, now.Add(time.Hour)); ok {
				t.Fatalf("%s answer was stored", name)
			}
		}
	}
}

func TestIPTypeCacheFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := paths.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	m := NewManager(4, dir)
	s := m.ipTypeState().storeRef
	if _, ok := s.(legacyIPTypeFile); !ok {
		t.Fatal("bare Manager should use the legacy file")
	}
	now := time.Now().UTC().Truncate(time.Second)
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(image)
	in := map[string]store.Entry{"70.79.253.35": {Data: data, CheckedAt: now, ExpiresAt: now.Add(24 * time.Hour)}}
	if err := s.SaveAll(in); err != nil {
		t.Fatal(err)
	}
	// 旧形状可读：文件仍是 {"version":1,"entries":...}。
	blob, err := os.ReadFile(paths.IPTypeCache(dir))
	if err != nil {
		t.Fatal(err)
	}
	var f ipTypeCacheFile
	if err := json.Unmarshal(blob, &f); err != nil || f.Version != 1 || len(f.Entries) != 1 {
		t.Fatalf("legacy shape lost: %s", blob)
	}
	got, err := s.LoadAll()
	if err != nil || len(got) != 1 {
		t.Fatalf("reload = %v, %v", got, err)
	}
	if string(got["70.79.253.35"].Data) != string(data) || !got["70.79.253.35"].CheckedAt.Equal(now) {
		t.Fatalf("entry mismatch: %+v", got["70.79.253.35"])
	}
	if err := s.SaveAll(map[string]store.Entry{"x": {Data: json.RawMessage(`{broken`)}}); err == nil {
		t.Fatal("invalid entry accepted")
	}
	if err := os.WriteFile(paths.IPTypeCache(dir), []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LoadAll(); err == nil {
		t.Fatal("corrupt file accepted")
	}
}

func TestIPTypeCacheUsesSharedDBWhenConsolePresent(t *testing.T) {
	c := consoleFixture(t)
	m := c.m
	if m.ipTypeMap == nil {
		t.Fatal("ip type store not wired")
	}
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 24*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	m.storeIPType(raw.IP, image)
	got, ok := m.cachedIPType(raw.IP, now.Add(time.Hour))
	if !ok || !got.CacheUsed || got.IP != raw.IP {
		t.Fatalf("DB cache miss: %+v ok=%v", got, ok)
	}
	if _, err := os.Stat(paths.IPTypeCache(m.workDir)); !os.IsNotExist(err) {
		t.Fatal("legacy IP type file retained in DB mode")
	}
	entries, err := m.ipTypeState().storeRef.LoadAll()
	if err != nil || len(entries) != 1 {
		t.Fatalf("store = %v, %v", entries, err)
	}
	if err := m.clearIPTypeCache(); err != nil {
		t.Fatal(err)
	}
	if entries, err := m.ipTypeState().storeRef.LoadAll(); err != nil || len(entries) != 0 {
		t.Fatalf("clear = %v %v", entries, err)
	}
}

func TestIPTypeCacheManualClearAndConfiguredTTL(t *testing.T) {
	m := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, 2*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	expiry, _ := time.Parse(time.RFC3339Nano, image.ExpiresAt)
	if expiry.Sub(now) != 2*time.Hour {
		t.Fatalf("configured IP type TTL not applied: %s", expiry.Sub(now))
	}
	m.setIPTypeTTL(2 * time.Hour)
	m.storeIPType(raw.IP, image)
	if _, ok := m.cachedIPType(raw.IP, now.Add(time.Hour)); !ok {
		t.Fatal("fresh cache entry missing")
	}
	if err := m.clearIPTypeCache(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.cachedIPType(raw.IP, now); ok {
		t.Fatal("manual clear left an entry")
	}
	if _, err := os.Stat(paths.IPTypeCache(m.workDir)); !os.IsNotExist(err) {
		t.Fatal("manual clear left the cache file")
	}
	stats := m.ipTypeCacheStats()
	if stats.Entries != 0 || stats.Hits != 0 || stats.Queries != 0 {
		t.Fatalf("manual clear did not reset stats: %+v", stats)
	}
}

func TestIPTypeCacheSharedByCandidateHelper(t *testing.T) {
	c := consoleFixture(t)
	parent := c.m
	helper := NewManager(1, t.TempDir())
	helper.evidenceDir = parent.evidenceDir
	helper.shareIPTypeCache(parent)

	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", false)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	helper.noteIPTypeQuery()
	helper.storeIPType(raw.IP, image)

	stats := parent.ipTypeCacheStats()
	if stats.Entries != 1 || stats.Queries != 1 || stats.Hits != 0 {
		t.Fatalf("helper activity not visible to parent: %+v", stats)
	}
	if _, ok := helper.cachedIPType(raw.IP, now.Add(time.Minute)); !ok {
		t.Fatal("helper did not read the shared entry")
	}
	stats = parent.ipTypeCacheStats()
	if stats.Hits != 1 || stats.Queries != 1 {
		t.Fatalf("shared hit counter diverged: %+v", stats)
	}
}

func TestIPTypeSharedCacheSerializesConcurrentCandidateWrites(t *testing.T) {
	parent := NewManager(4, t.TempDir())
	now := time.Now().UTC()
	ips := []string{"70.79.253.35", "70.79.253.36"}
	var wg sync.WaitGroup
	for _, ip := range ips {
		helper := NewManager(1, t.TempDir())
		helper.shareIPTypeCache(parent)
		raw := completeIPPureResponse(ip, false)
		image, err := profileFromIPPureTTL(raw, ip, now, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			helper.noteIPTypeQuery()
			helper.storeIPType(ip, image)
		}()
	}
	wg.Wait()
	stats := parent.ipTypeCacheStats()
	if stats.Entries != len(ips) || stats.Queries != len(ips) {
		t.Fatalf("concurrent candidate writes were lost: %+v", stats)
	}
}

func TestLegacyIPTypeCacheFileMigratesToIPTypePath(t *testing.T) {
	dir := t.TempDir()
	if err := paths.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	file := ipTypeCacheFile{Version: 1, Entries: map[string]ipTypeCacheEntry{
		raw.IP: {Profile: image, CachedAt: now},
	}}
	blob, err := json.Marshal(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.LegacyIPTypeCache(dir), blob, 0600); err != nil {
		t.Fatal(err)
	}
	m := NewManager(4, dir)
	if got, ok := m.cachedIPType(raw.IP, now.Add(time.Minute)); !ok || got.IP != raw.IP {
		t.Fatalf("legacy cache entry not migrated: %+v ok=%v", got, ok)
	}
	if _, err := os.Stat(paths.IPTypeCache(dir)); err != nil {
		t.Fatalf("new cache file missing: %v", err)
	}
	if _, err := os.Stat(paths.LegacyIPTypeCache(dir)); !os.IsNotExist(err) {
		t.Fatal("legacy cache file retained after migration")
	}
}

func TestLegacyIPTypeStoreMigratesToIPTypeMapStore(t *testing.T) {
	dir := t.TempDir()
	if err := paths.Ensure(dir); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw := completeIPPureResponse("70.79.253.35", true)
	image, err := profileFromIPPureTTL(raw, raw.IP, now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	db, err := storesqlite.Open(paths.DBFile(dir))
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(image)
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.MapStore("ippure").SaveAll(map[string]store.Entry{
		raw.IP: {Data: data, CheckedAt: now, ExpiresAt: now.Add(time.Hour)},
	}); err != nil {
		db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	m := NewManager(maximumSlots, dir)
	c, err := newConsole(m)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { c.shutdown(); c.closeStores() }()
	entries, err := c.checkDB.MapStore("ip_type").LoadAll()
	if err != nil || len(entries) != 1 {
		t.Fatalf("migrated store = %v, %v", entries, err)
	}
	legacyEntries, err := c.checkDB.MapStore("ippure").LoadAll()
	if err != nil || len(legacyEntries) != 0 {
		t.Fatalf("legacy store not cleared = %v, %v", legacyEntries, err)
	}
	if got, ok := m.cachedIPType(raw.IP, now.Add(time.Minute)); !ok || got.IP != raw.IP {
		t.Fatalf("migrated entry not readable: %+v ok=%v", got, ok)
	}
}
