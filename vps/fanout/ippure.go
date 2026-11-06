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
	"time"
)

// IPPure only describes the caller's IP, so unlike a normal /{ip} provider it
// must be queried through the exit that is being verified. The tunnel's local
// SOCKS listener provides that path without exposing the proxy publicly.
const (
	defaultIPPureURL  = "https://my.ippure.com/v1/info"
	defaultIPPureTTL  = 24 * time.Hour
	ippureRuleVersion = "ippure-1"
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

type ippureCacheEntry struct {
	Profile  EgressProfile `json:"profile"`
	CachedAt time.Time     `json:"cached_at"`
}

type ippureCacheFile struct {
	Version int                         `json:"version"`
	Entries map[string]ippureCacheEntry `json:"entries"`
}

type ippureCacheStats struct {
	Entries int `json:"entries"`
	Hits    int `json:"hits"`
	Queries int `json:"queries"`
}

func ippureEnabled() bool {
	return strings.TrimSpace(os.Getenv("IPPURE_ENABLED")) == "1"
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

// profileFromIPPure turns one validated caller-IP response into the shared
// OpenVPN profile. isResidential is the only explicit residential signal
// IPPure provides; missing or false never becomes strict_residential.
func profileFromIPPure(raw ippureResponse, expectedIP string, now time.Time) (EgressProfile, error) {
	return profileFromIPPureTTL(raw, expectedIP, now, defaultIPPureTTL)
}

func profileFromIPPureTTL(raw ippureResponse, expectedIP string, now time.Time, ttl time.Duration) (EgressProfile, error) {
	image := unknownOpenVPNEgress(expectedIP, "ippure_unavailable")
	image.Protocol = "openvpn"
	if raw.IP != expectedIP {
		return image, fmt.Errorf("ippure_ip_mismatch")
	}
	if ttl <= 0 {
		ttl = defaultIPPureTTL
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

// ippureCacheable accepts only a complete, passed answer. A successful
// "not residential" answer is cached as unknown; failures, IP mismatches and
// incomplete evidence are queried again next time.
func ippureCacheable(raw ippureResponse, image EgressProfile, expectedIP string) bool {
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
	return image, raw, ippureCacheable(raw, image, expectedIP)
}

func (m *Manager) ippureTTLValue() time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.ippureTTL > 0 {
		return m.ippureTTL
	}
	return defaultIPPureTTL
}

func (m *Manager) setIPPureTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultIPPureTTL
	}
	m.mu.Lock()
	m.ippureTTL = ttl
	m.mu.Unlock()
}

func (m *Manager) ippureCachePath() string {
	dir := m.evidenceDir
	if strings.TrimSpace(dir) == "" {
		dir = m.workDir
	}
	return paths.IPPureCache(dir)
}

// legacyIPPureFile 是旧 ippure-cache.json 形状的 MapStore 适配：
// 文件里是 {"version":1,"entries":{ip:{profile,cached_at}}}，
// 进出时与 store.Entry 互转。只在 SQLite 不可用时使用。
type legacyIPPureFile struct{ path string }

func (s legacyIPPureFile) LoadAll() (map[string]store.Entry, error) {
	out := map[string]store.Entry{}
	blob, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	var file ippureCacheFile
	if err := json.Unmarshal(blob, &file); err != nil || file.Entries == nil {
		return nil, fmt.Errorf("invalid ippure cache file: %w", err)
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

func (s legacyIPPureFile) SaveAll(entries map[string]store.Entry) error {
	file := ippureCacheFile{Version: 1, Entries: map[string]ippureCacheEntry{}}
	for ip, e := range entries {
		var p EgressProfile
		if err := json.Unmarshal(e.Data, &p); err != nil {
			return err
		}
		file.Entries[ip] = ippureCacheEntry{Profile: p, CachedAt: e.CheckedAt}
	}
	blob, err := json.Marshal(file)
	if err != nil {
		return err
	}
	return atomicConfigWrite(s.path, blob)
}

func (m *Manager) ippureStore() store.MapStore {
	if m.ippureMap != nil {
		return m.ippureMap
	}
	return legacyIPPureFile{path: m.ippureCachePath()}
}

func (m *Manager) loadIPPureCacheLocked() {
	if m.ippureCacheLoaded {
		return
	}
	m.ippureCacheLoaded = true
	if m.ippureCache == nil {
		m.ippureCache = map[string]ippureCacheEntry{}
	}
	entries, err := m.ippureStore().LoadAll()
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
		m.ippureCache[ip] = ippureCacheEntry{Profile: p, CachedAt: e.CheckedAt}
	}
}

func (m *Manager) saveIPPureCacheLocked() error {
	if m.ippureCache == nil {
		m.ippureCache = map[string]ippureCacheEntry{}
	}
	// 缓存目录可能随布局补建：先补再写，权限问题仍 loud 失败。
	if err := paths.Ensure(m.workDir); err != nil {
		return err
	}
	entries := make(map[string]store.Entry, len(m.ippureCache))
	for ip, e := range m.ippureCache {
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
	s := m.ippureStore()
	if err := s.SaveAll(entries); err != nil {
		return err
	}
	// DB 模式下删残留旧文件，避免两处缓存来源混淆；文件模式跳过
	//（刚写入的就是它）。失败只忽略：缓存已落库，可再生。
	if _, ok := s.(legacyIPPureFile); !ok {
		_ = os.Remove(m.ippureCachePath())
	}
	return nil
}

func (m *Manager) cachedIPPure(ip string, now time.Time) (EgressProfile, bool) {
	if strings.TrimSpace(ip) == "" {
		return EgressProfile{}, false
	}
	m.ippureMu.Lock()
	defer m.ippureMu.Unlock()
	m.loadIPPureCacheLocked()
	entry, ok := m.ippureCache[ip]
	if !ok {
		return EgressProfile{}, false
	}
	expiry, err := time.Parse(time.RFC3339Nano, entry.Profile.ExpiresAt)
	if err != nil || !expiry.After(now) || entry.Profile.IP != ip || entry.Profile.IntelligenceStatus != "passed" ||
		entry.Profile.Provider != "ippure" || entry.Profile.RuleVersion != ippureRuleVersion {
		delete(m.ippureCache, ip)
		_ = m.saveIPPureCacheLocked()
		return EgressProfile{}, false
	}
	image := entry.Profile
	image.Raw = nil
	image.CacheUsed = true
	m.ippureHits++
	return image, true
}

func (m *Manager) storeIPPure(ip string, image EgressProfile) {
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
	m.ippureMu.Lock()
	defer m.ippureMu.Unlock()
	m.loadIPPureCacheLocked()
	m.ippureCache[ip] = ippureCacheEntry{Profile: image, CachedAt: time.Now().UTC()}
	_ = m.saveIPPureCacheLocked()
}

func (m *Manager) noteIPPureQuery() {
	m.ippureMu.Lock()
	m.ippureQueries++
	m.ippureMu.Unlock()
}

func (m *Manager) ippureCacheStats() ippureCacheStats {
	m.ippureMu.Lock()
	defer m.ippureMu.Unlock()
	m.loadIPPureCacheLocked()
	now := time.Now().UTC()
	changed := false
	for ip, entry := range m.ippureCache {
		expiry, err := time.Parse(time.RFC3339Nano, entry.Profile.ExpiresAt)
		if err != nil || !expiry.After(now) || entry.Profile.IP != ip || entry.Profile.IntelligenceStatus != "passed" ||
			entry.Profile.Provider != "ippure" || entry.Profile.RuleVersion != ippureRuleVersion {
			delete(m.ippureCache, ip)
			changed = true
		}
	}
	if changed {
		_ = m.saveIPPureCacheLocked()
	}
	return ippureCacheStats{Entries: len(m.ippureCache), Hits: m.ippureHits, Queries: m.ippureQueries}
}

// clearIPPureCache is the explicit operator action; it never affects running
// exits, only the next profile lookup.
func (m *Manager) clearIPPureCache() error {
	m.ippureMu.Lock()
	defer m.ippureMu.Unlock()
	m.ippureCache = map[string]ippureCacheEntry{}
	m.ippureCacheLoaded = true
	m.ippureHits, m.ippureQueries = 0, 0
	if err := m.ippureStore().SaveAll(map[string]store.Entry{}); err != nil {
		return err
	}
	// 升级残留与文件模式统一：缓存文件不存在才算干净。
	if err := os.Remove(m.ippureCachePath()); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *Manager) lookupEgressProfile(t *Tunnel, ip string) EgressProfile {
	if ippureEnabled() {
		if image, ok := m.cachedIPPure(ip, time.Now().UTC()); ok {
			return image
		}
		m.noteIPPureQuery()
		image, _, cacheable := t.probeIPPure(t.workContext(), ip, m.ippureTTLValue())
		if cacheable {
			m.storeIPPure(ip, image)
		}
		// A failed IPPure query may fall back to an explicitly configured
		// provider, but a successful "not residential" answer never does.
		if image.IntelligenceStatus == "failed" && strings.TrimSpace(os.Getenv("INTELLIGENCE_URL_TEMPLATE")) != "" {
			return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
		}
		return image
	}
	return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
}

func (c *Console) apiIPPureCache(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, 200, c.m.ippureCacheStats())
	case http.MethodPost, http.MethodDelete:
		if err := c.m.clearIPPureCache(); err != nil {
			writeJSON(w, 500, map[string]string{"error": "IPPure 缓存清除失败"})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "cache": c.m.ippureCacheStats()})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}
