package main

import (
	"context"
	"encoding/json"
	"fanout/internal/paths"
	"fanout/internal/store"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IPPure only describes the caller's IP, so unlike a normal /{ip} provider it
// must be queried through the exit that is being verified. The tunnel's local
// SOCKS listener provides that path without exposing the proxy publicly.
const (
	defaultIPPureURL  = "https://my.ippure.com/v1/info"
	defaultIPTypeTTL  = 24 * time.Hour
	ippureRuleVersion = "ippure-1"
	// ipTypeStatsWindowHours 是命中/查询计数的滑动窗口：24 个小时桶定长存放，
	// 旧数据自然过期，数字不再随运行时间无限累计。
	ipTypeStatsWindowHours = 24
)

type ippureResponse struct {
	IP             string `json:"ip"`
	ASN            int    `json:"asn"`
	ASOrganization string `json:"asOrganization"`
	CountryCode    string `json:"countryCode"`
	City           string `json:"city"`
	FraudScore     *int   `json:"fraudScore"`
	IsResidential  *bool  `json:"isResidential"`
}

type ipTypeCacheEntry struct {
	Profile  EgressProfile `json:"profile"`
	CachedAt time.Time     `json:"cached_at"`
}

type ipTypeCacheFile struct {
	Version int                         `json:"version"`
	Entries map[string]ipTypeCacheEntry `json:"entries"`
}

// ipTypeStatsBucket 是一小时的命中/查询计数。hour 为 Unix 小时，
// 桶被相隔 24 小时的另一小时复用时先清零（见 statsBucketLocked）。
type ipTypeStatsBucket struct {
	hour    int64
	hits    int
	queries int
}

// ipTypeCache is the single in-process cache shared by production exits and
// candidate checks. A helper Manager must share this pointer, not create its
// own map; otherwise entries and windowed hit/query stats diverge between
// surfaces.
type ipTypeCache struct {
	mu           sync.Mutex
	entries      map[string]ipTypeCacheEntry
	loaded       bool
	statsBuckets [ipTypeStatsWindowHours]ipTypeStatsBucket
	ttl          time.Duration
	storeRef     store.MapStore
	workDir      string
	path         string
}

type ipTypeCacheStats struct {
	Entries int `json:"entries"`
	Hits    int `json:"hits"`
	Queries int `json:"queries"`
}

func ippureEnabled() bool {
	return strings.TrimSpace(os.Getenv("IPPURE_ENABLED")) == "1"
}

// intelligenceProvider is the single switch for convenient replacement.
// auto/empty keeps legacy behaviour (IPPURE_ENABLED + template) so existing
// deployments keep working. ippure forces IPPure, custom skips IPPure,
// off disables both without a query.
func intelligenceProvider() string {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("INTELLIGENCE_PROVIDER"))) {
	case "", "auto":
		return "auto"
	case "disabled":
		return "off"
	case "ippure", "custom", "off":
		return strings.ToLower(strings.TrimSpace(os.Getenv("INTELLIGENCE_PROVIDER")))
	default:
		return "auto"
	}
}

func customIntelligenceConfigured() bool {
	return strings.TrimSpace(os.Getenv("INTELLIGENCE_URL_TEMPLATE")) != ""
}

// reloadIntelligenceEnv re-reads the private env file before each lookup so a
// file change applies on the next connection without restarting the manager.
// Unlike loadEnvFile (startup only, never overwrites), this overwrites the
// allowlisted intelligence keys when the file contains a non-empty value;
// empty file entries never clear an existing systemd/test variable (disable
// via INTELLIGENCE_PROVIDER=off or INTELLIGENCE_AUTH_MODE=none instead).
func reloadIntelligenceEnv() {
	path := paths.IntelEnv()
	blob, err := os.ReadFile(path)
	if err != nil {
		return
	}
	allow := map[string]bool{
		"INTELLIGENCE_PROVIDER": true, "INTELLIGENCE_URL_TEMPLATE": true,
		"INTELLIGENCE_TOKEN": true, "INTELLIGENCE_AUTH_MODE": true,
		"INTELLIGENCE_AUTH_HEADER": true, "INTELLIGENCE_TOKEN_PARAM": true,
		"IPPURE_ENABLED": true, "IPPURE_URL": true, "IPPURE_TIMEOUT_SECONDS": true,
		"COUNTRY_URL_TEMPLATE": true,
	}
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || !allow[key] {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
			value = value[1 : len(value)-1]
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
}

func ippureURL() string {
	return envOr("IPPURE_URL", defaultIPPureURL)
}

func ippureTimeout() time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv("IPPURE_TIMEOUT_SECONDS")))
	if err != nil || seconds < 3 || seconds > 60 {
		seconds = 15
	}
	return time.Duration(seconds) * time.Second
}

// loadEnvFile mirrors the Python adapter's private KEY=VALUE loading. It only
// fills variables that are not already present, so systemd and tests win.
func loadEnvFile(path string) {
	blob, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(blob), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
			value = value[1 : len(value)-1]
		}
		_ = os.Setenv(key, value)
	}
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func asnPtr(asn int) *string {
	if asn <= 0 {
		return nil
	}
	value := fmt.Sprintf("AS%d", asn)
	return &value
}

func profileFromIPPureTTL(raw ippureResponse, expectedIP string, now time.Time, ttl time.Duration) (EgressProfile, error) {
	image := unknownOpenVPNEgress(expectedIP, "ippure_unavailable")
	image.Protocol = "openvpn"
	if raw.IP != expectedIP {
		return image, fmt.Errorf("ippure_ip_mismatch")
	}
	if ttl <= 0 {
		ttl = defaultIPTypeTTL
	}
	expires := now.Add(ttl)
	country := strings.ToUpper(strings.TrimSpace(raw.CountryCode))
	if flagOf(country) == "" || country == "ZZ" || country == "XX" {
		country = ""
	}
	image.Country = stringPtr(country)
	image.City = stringPtr(raw.City)
	image.ASN = asnPtr(raw.ASN)
	image.ISP = stringPtr(raw.ASOrganization)
	image.Residential = raw.IsResidential
	image.FraudScore = raw.FraudScore
	image.Provider = "ippure"
	image.QueriedAt = now.Format(time.RFC3339Nano)
	image.ExpiresAt = expires.Format(time.RFC3339Nano)
	image.RuleVersion = ippureRuleVersion
	image.IntelligenceStatus = "passed"
	image.IntelligenceError = ""
	image.IdentityAssurance = "normal_https_certificate_validation"
	if country != "" {
		image.CountryStatus = "passed"
		image.CountryEvidence = &CountryEvidence{
			IP: expectedIP, Country: country, Status: "passed", Provider: "ippure",
			QueriedAt: image.QueriedAt, ExpiresAt: image.ExpiresAt,
			IdentityAssurance: image.IdentityAssurance,
		}
	}
	switch {
	case raw.IsResidential == nil:
		image.IPType, image.Reason, image.Confidence = "unknown", "ippure_missing_residential", "unknown"
	case *raw.IsResidential:
		if image.Country == nil || image.ASN == nil || image.ISP == nil {
			image.IPType, image.Reason, image.Confidence = "unknown", "ippure_incomplete_residential", "unknown"
		} else {
			image.IPType, image.Reason, image.Confidence = "strict_residential", "ippure_explicit_residential", "high"
		}
	default:
		image.IPType, image.Reason, image.Confidence = "unknown", "ippure_not_residential", "high"
	}
	return image, nil
}

// ipTypeCacheable accepts only a complete, passed answer. A successful
// "not residential" answer is cached as unknown; failures, IP mismatches and
// incomplete evidence are queried again next time.
func ipTypeCacheable(raw ippureResponse, image EgressProfile, expectedIP string) bool {
	if expectedIP == "" || raw.IP != expectedIP || image.IP != expectedIP || image.IntelligenceStatus != "passed" {
		return false
	}
	if raw.IsResidential == nil || raw.ASN <= 0 || strings.TrimSpace(raw.ASOrganization) == "" {
		return false
	}
	country := strings.ToUpper(strings.TrimSpace(raw.CountryCode))
	if flagOf(country) == "" || country == "ZZ" || country == "XX" {
		return false
	}
	if image.IPType != "strict_residential" && image.IPType != "unknown" {
		return false
	}
	expiry, err := time.Parse(time.RFC3339Nano, image.ExpiresAt)
	return err == nil && expiry.After(time.Now())
}

func (t *Tunnel) probeIPPure(parent context.Context, expectedIP string, ttl time.Duration) (EgressProfile, ippureResponse, bool) {
	profile := unknownOpenVPNEgress(expectedIP, "ippure_unavailable")
	profile.Protocol = "openvpn"
	cred := t.probeCredential()
	timeout := ippureTimeout()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	out, err := cmdOutput(exec.CommandContext(ctx, "curl",
		"--disable", "-4fsS", "--noproxy", "", "--max-time", strconv.Itoa(int(timeout.Seconds())),
		"--socks5-hostname", fmt.Sprintf("127.0.0.1:%d", t.Port),
		"--proxy-user", cred.User+":"+cred.Pass,
		ippureURL(),
	))
	if err != nil {
		profile.IntelligenceError = "ippure_http_failed"
		return profile, ippureResponse{}, false
	}
	var raw ippureResponse
	if err := json.Unmarshal(out, &raw); err != nil {
		profile.IntelligenceError = "ippure_invalid_json"
		return profile, ippureResponse{}, false
	}
	image, err := profileFromIPPureTTL(raw, expectedIP, time.Now().UTC(), ttl)
	if err != nil {
		profile.IntelligenceError = err.Error()
		return profile, raw, false
	}
	image.Raw = out
	return image, raw, ipTypeCacheable(raw, image, expectedIP)
}

func newIPTypeCache(storeRef store.MapStore, workDir, path string, ttl time.Duration) *ipTypeCache {
	if ttl <= 0 {
		ttl = defaultIPTypeTTL
	}
	return &ipTypeCache{
		entries:  map[string]ipTypeCacheEntry{},
		ttl:      ttl,
		storeRef: storeRef,
		workDir:  workDir,
		path:     path,
	}
}

// ipTypeState lazily binds the cache to the manager's store and TTL. newConsole
// installs the SQLite MapStore before the first lookup, so construction stays
// lazy and bare Manager tests keep the legacy JSON file fallback.
func (m *Manager) ipTypeState() *ipTypeCache {
	m.ipTypeInitMu.Lock()
	defer m.ipTypeInitMu.Unlock()
	if m.ipType != nil {
		return m.ipType
	}
	dir := m.ipTypeCacheDir()
	path := paths.IPTypeCache(dir)
	legacyPath := paths.LegacyIPTypeCache(dir)
	s := m.ipTypeMap
	if s == nil {
		migrateLegacyIPTypeFile(legacyPath, path)
		s = legacyIPTypeFile{path: path}
	} else {
		// DB 模式不读取旧文件；迁移完旧记录集后清掉残留文件。
		_ = os.Remove(legacyPath)
	}
	ttl := m.ipTypeTTL
	if ttl <= 0 {
		ttl = defaultIPTypeTTL
	}
	m.ipType = newIPTypeCache(s, m.workDir, path, ttl)
	return m.ipType
}

func (m *Manager) setIPTypeMap(s store.MapStore) {
	m.ipTypeInitMu.Lock()
	m.ipTypeMap = s
	m.ipType = nil
	m.ipTypeInitMu.Unlock()
}

// shareIPTypeCache makes a candidate-check helper account against the
// long-lived manager: entries, windowed hit/query stats and TTL are all shared.
func (m *Manager) shareIPTypeCache(parent *Manager) {
	if parent == nil {
		return
	}
	shared := parent.ipTypeState()
	m.ipTypeInitMu.Lock()
	m.ipType = shared
	m.ipTypeInitMu.Unlock()
}

func (m *Manager) ipTypeTTLValue() time.Duration {
	c := m.ipTypeState()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ttl > 0 {
		return c.ttl
	}
	return defaultIPTypeTTL
}

func (m *Manager) setIPTypeTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultIPTypeTTL
	}
	m.ipTypeInitMu.Lock()
	m.ipTypeTTL = ttl
	c := m.ipType
	if c != nil {
		c.mu.Lock()
		c.ttl = ttl
		c.mu.Unlock()
	}
	m.ipTypeInitMu.Unlock()
}

func (m *Manager) ipTypeCacheDir() string {
	dir := m.evidenceDir
	if strings.TrimSpace(dir) == "" {
		dir = m.workDir
	}
	return dir
}

// legacyIPTypeFile 是 ippure-cache.json 时代的文件形状适配：
// 文件里是 {"version":1,"entries":{ip:{profile,cached_at}}}，
// 进出时与 store.Entry 互转。只在 SQLite 不可用时使用。
type legacyIPTypeFile struct{ path string }

func (s legacyIPTypeFile) LoadAll() (map[string]store.Entry, error) {
	out := map[string]store.Entry{}
	blob, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var file ipTypeCacheFile
	if err := json.Unmarshal(blob, &file); err != nil || file.Entries == nil {
		return nil, fmt.Errorf("invalid ip type cache file: %w", err)
	}
	for ip, e := range file.Entries {
		data, err := json.Marshal(e.Profile)
		if err != nil {
			return nil, err
		}
		var expires time.Time
		if t, err := time.Parse(time.RFC3339Nano, e.Profile.ExpiresAt); err == nil {
			expires = t
		}
		out[ip] = store.Entry{Data: data, CheckedAt: e.CachedAt, ExpiresAt: expires}
	}
	return out, nil
}

func (s legacyIPTypeFile) SaveAll(entries map[string]store.Entry) error {
	file := ipTypeCacheFile{Version: 1, Entries: map[string]ipTypeCacheEntry{}}
	for ip, e := range entries {
		var p EgressProfile
		if err := json.Unmarshal(e.Data, &p); err != nil {
			return err
		}
		file.Entries[ip] = ipTypeCacheEntry{Profile: p, CachedAt: e.CheckedAt}
	}
	blob, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return atomicConfigWrite(s.path, blob)
}

func (c *ipTypeCache) loadLocked() {
	if c.loaded {
		return
	}
	c.loaded = true
	if c.entries == nil {
		c.entries = map[string]ipTypeCacheEntry{}
	}
	entries, err := c.storeRef.LoadAll()
	if err != nil {
		return
	}
	for ip, e := range entries {
		var p EgressProfile
		if json.Unmarshal(e.Data, &p) != nil {
			continue
		}
		if p.IP != ip || p.IntelligenceStatus != "passed" ||
			p.Provider != "ippure" || p.RuleVersion != ippureRuleVersion {
			continue
		}
		c.entries[ip] = ipTypeCacheEntry{Profile: p, CachedAt: e.CheckedAt}
	}
}

func (c *ipTypeCache) saveLocked() error {
	if c.entries == nil {
		c.entries = map[string]ipTypeCacheEntry{}
	}
	// 缓存目录可能随布局补建：先补再写，权限问题仍 loud 失败。
	if err := paths.Ensure(c.workDir); err != nil {
		return err
	}
	entries := make(map[string]store.Entry, len(c.entries))
	for ip, e := range c.entries {
		data, err := json.Marshal(e.Profile)
		if err != nil {
			return err
		}
		var expires time.Time
		if t, err := time.Parse(time.RFC3339Nano, e.Profile.ExpiresAt); err == nil {
			expires = t
		}
		entries[ip] = store.Entry{Data: data, CheckedAt: e.CachedAt, ExpiresAt: expires}
	}
	if err := c.storeRef.SaveAll(entries); err != nil {
		return err
	}
	// DB 模式下删残留旧文件，避免两处缓存来源混淆；文件模式跳过
	//（刚写入的就是它）。失败只忽略：缓存已落库，可再生。
	if _, ok := c.storeRef.(legacyIPTypeFile); !ok {
		_ = os.Remove(c.path)
	}
	return nil
}

func (c *ipTypeCache) cached(ip string, now time.Time) (EgressProfile, bool) {
	if strings.TrimSpace(ip) == "" {
		return EgressProfile{}, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	entry, ok := c.entries[ip]
	if !ok {
		return EgressProfile{}, false
	}
	if !ipTypeEntryValid(entry.Profile, ip, now) {
		delete(c.entries, ip)
		_ = c.saveLocked()
		return EgressProfile{}, false
	}
	image := entry.Profile
	image.Raw = nil
	image.CacheUsed = true
	c.statsBucketLocked(now).hits++
	return image, true
}

func (c *ipTypeCache) store(ip string, image EgressProfile) {
	if image.IntelligenceStatus != "passed" || image.IP != ip {
		return
	}
	if image.IPType != "strict_residential" && image.IPType != "unknown" {
		return
	}
	expiry, err := time.Parse(time.RFC3339Nano, image.ExpiresAt)
	if err != nil || !expiry.After(time.Now()) {
		return
	}
	image.Raw = nil
	image.CacheUsed = false
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	c.entries[ip] = ipTypeCacheEntry{Profile: image, CachedAt: time.Now().UTC()}
	_ = c.saveLocked()
}

func (c *ipTypeCache) noteQuery() {
	c.noteQueryAt(time.Now().UTC())
}

// noteQueryAt 是 noteQuery 的可测入口：线上走当前时间，测试可回填旧小时。
func (c *ipTypeCache) noteQueryAt(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.statsBucketLocked(now).queries++
}

// statsBucketLocked 返回 now 所在小时的统计桶，小时滚动时先清零复用。
// 调用方必须已持 c.mu。
func (c *ipTypeCache) statsBucketLocked(now time.Time) *ipTypeStatsBucket {
	h := now.Unix() / 3600
	b := &c.statsBuckets[int(h%ipTypeStatsWindowHours)]
	if b.hour != h {
		*b = ipTypeStatsBucket{hour: h}
	}
	return b
}

// statsSumsLocked 汇总最近 24 小时命中的桶：超过一天没碰过的桶即使残留
// 也不计入。调用方必须已持 c.mu。
func (c *ipTypeCache) statsSumsLocked(now time.Time) (hits, queries int) {
	h := now.Unix() / 3600
	for i := range c.statsBuckets {
		b := &c.statsBuckets[i]
		if age := h - b.hour; age >= 0 && age < ipTypeStatsWindowHours {
			hits += b.hits
			queries += b.queries
		}
	}
	return hits, queries
}

// ipTypeEntryValid reports whether a cached profile is still usable:
// unexpired, addressed to this IP, and from the current evidence pipeline.
func ipTypeEntryValid(profile EgressProfile, ip string, now time.Time) bool {
	expiry, err := time.Parse(time.RFC3339Nano, profile.ExpiresAt)
	return err == nil && expiry.After(now) && profile.IP == ip &&
		profile.IntelligenceStatus == "passed" &&
		profile.Provider == "ippure" && profile.RuleVersion == ippureRuleVersion
}

func (c *ipTypeCache) stats() ipTypeCacheStats {
	return c.statsAt(time.Now().UTC())
}

// statsAt 是 stats 的可测入口：线上走当前时间，测试可指定时钟。
func (c *ipTypeCache) statsAt(now time.Time) ipTypeCacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.loadLocked()
	changed := false
	for ip, entry := range c.entries {
		if !ipTypeEntryValid(entry.Profile, ip, now) {
			delete(c.entries, ip)
			changed = true
		}
	}
	if changed {
		_ = c.saveLocked()
	}
	hits, queries := c.statsSumsLocked(now)
	return ipTypeCacheStats{Entries: len(c.entries), Hits: hits, Queries: queries}
}

// clear is the explicit operator action; it never affects running exits, only
// the next profile lookup.
func (c *ipTypeCache) clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = map[string]ipTypeCacheEntry{}
	c.loaded = true
	c.statsBuckets = [ipTypeStatsWindowHours]ipTypeStatsBucket{}
	if err := c.storeRef.SaveAll(map[string]store.Entry{}); err != nil {
		return err
	}
	// 升级残留与文件模式统一：缓存文件不存在才算干净。
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) cachedIPType(ip string, now time.Time) (EgressProfile, bool) {
	return m.ipTypeState().cached(ip, now)
}

func (m *Manager) storeIPType(ip string, image EgressProfile) {
	m.ipTypeState().store(ip, image)
}

func (m *Manager) noteIPTypeQuery() {
	m.ipTypeState().noteQuery()
}

func (m *Manager) ipTypeCacheStats() ipTypeCacheStats {
	return m.ipTypeState().stats()
}

func (m *Manager) clearIPTypeCache() error {
	return m.ipTypeState().clear()
}

func (m *Manager) lookupEgressProfile(t *Tunnel, ip string) EgressProfile {
	reloadIntelligenceEnv()
	provider := intelligenceProvider()
	if provider == "off" {
		return EgressProfile{IP: ip, Protocol: "openvpn", IPType: "unknown",
			Reason: "intelligence_not_configured", Confidence: "unknown",
			IntelligenceStatus: "not_configured", IntelligenceError: "intelligence_not_configured"}
	}
	if provider == "custom" {
		return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
	}
	useIPPure := ippureEnabled()
	if provider == "ippure" {
		useIPPure = true
	}
	if useIPPure {
		if image, ok := m.cachedIPType(ip, time.Now().UTC()); ok {
			return image
		}
		m.noteIPTypeQuery()
		image, _, cacheable := t.probeIPPure(t.workContext(), ip, m.ipTypeTTLValue())
		if cacheable {
			m.storeIPType(ip, image)
		}
		// A failed IPPure query may fall back to an explicitly configured
		// provider, but a successful "not residential" answer never does.
		if image.IntelligenceStatus == "failed" && customIntelligenceConfigured() {
			return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
		}
		return image
	}
	return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
}

func (c *Console) apiIPTypeCache(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, c.m.ipTypeCacheStats())
	case http.MethodPost, http.MethodDelete:
		if err := c.m.clearIPTypeCache(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "IP 类型缓存清除失败"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "cache": c.m.ipTypeCacheStats()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
