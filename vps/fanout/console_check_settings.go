package main

import (
	"encoding/json"
	"fanout/internal/paths"
	"fmt"
	"net/http"
	"os"
	"time"
)

type checkSettings struct {
	Parallelism           int  `json:"parallelism"`
	IPTypeCheck           bool `json:"ip_type_check"`
	ObservationTTLMinutes int  `json:"observation_ttl_minutes"`
	IPTypeTTLHours        int  `json:"ip_type_ttl_hours"`
	// LegacyIPTypeTTLHours is read-only migration input from the pre-rename
	// settings file; saves never write it.
	LegacyIPTypeTTLHours *int `json:"ippure_ttl_hours,omitempty"`
}

type checkSettingsRequest struct {
	Parallelism           int   `json:"parallelism"`
	IPTypeCheck           *bool `json:"ip_type_check"`
	ObservationTTLMinutes *int  `json:"observation_ttl_minutes"`
	IPTypeTTLHours        *int  `json:"ip_type_ttl_hours"`
	// LegacyIPTypeTTLHours keeps older clients working during the rename.
	LegacyIPTypeTTLHours *int `json:"ippure_ttl_hours"`
}

func validateCheckParallelism(n int) error {
	if n < 1 || n > networkLinkCount {
		return fmt.Errorf("检测并发须为正整数，不能超过实例 /30 地址容量 %d", networkLinkCount)
	}
	return nil
}

func validateTTLSettings(observationMinutes, ipTypeHours int) error {
	if observationMinutes < 1 || observationMinutes > maxObservationTTLMinutes {
		return fmt.Errorf("排序观察有效期须为 1–1440 分钟")
	}
	if ipTypeHours < 1 || ipTypeHours > maxIPTypeTTLHours {
		return fmt.Errorf("IP 类型缓存有效期须为 1–168 小时")
	}
	return nil
}

func normalizeTTLSettings(observationMinutes, ipTypeHours int) (int, int) {
	if observationMinutes <= 0 || observationMinutes > maxObservationTTLMinutes {
		observationMinutes = defaultObservationTTLMinutes
	}
	if ipTypeHours <= 0 || ipTypeHours > maxIPTypeTTLHours {
		ipTypeHours = defaultIPTypeTTLHours
	}
	return observationMinutes, ipTypeHours
}

func (c *Console) loadCheckSettings() error {
	c.checkParallelism = defaultCheckParallelism
	c.checkIPType = false
	c.observationTTLMinutes = defaultObservationTTLMinutes
	c.ipTypeTTLHours = defaultIPTypeTTLHours
	c.m.connectBudget.setLimit(defaultCheckParallelism)
	blob, err := os.ReadFile(paths.CheckSettings(c.m.workDir))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var s checkSettings
	if json.Unmarshal(blob, &s) != nil || validateCheckParallelism(s.Parallelism) != nil {
		return fmt.Errorf("检测设置损坏，保留原文件")
	}
	ipTypeHours := s.IPTypeTTLHours
	if ipTypeHours <= 0 && s.LegacyIPTypeTTLHours != nil {
		ipTypeHours = *s.LegacyIPTypeTTLHours
	}
	observation, ipType := normalizeTTLSettings(s.ObservationTTLMinutes, ipTypeHours)
	if err := validateTTLSettings(observation, ipType); err != nil {
		return fmt.Errorf("检测设置损坏，保留原文件")
	}
	c.checkParallelism = s.Parallelism
	c.checkIPType = s.IPTypeCheck
	c.observationTTLMinutes = observation
	c.ipTypeTTLHours = ipType
	c.m.connectBudget.setLimit(s.Parallelism)
	return nil
}

// Caller holds checksMu; atomic persistence precedes publication.
func (c *Console) saveCheckSettings(n int, ipTypeCheck bool) error {
	c.ttlMu.RLock()
	observation, ipType := c.observationTTLMinutes, c.ipTypeTTLHours
	c.ttlMu.RUnlock()
	return c.saveCheckSettingsAll(n, ipTypeCheck, observation, ipType)
}

func (c *Console) saveCheckSettingsAll(n int, ipTypeCheck bool, observationMinutes, ipTypeHours int) error {
	if err := validateCheckParallelism(n); err != nil {
		return err
	}
	if err := validateTTLSettings(observationMinutes, ipTypeHours); err != nil {
		return err
	}
	blob, _ := json.Marshal(checkSettings{Parallelism: n, IPTypeCheck: ipTypeCheck, ObservationTTLMinutes: observationMinutes, IPTypeTTLHours: ipTypeHours})
	if err := atomicConfigWrite(paths.CheckSettings(c.m.workDir), blob); err != nil {
		return fmt.Errorf("检测设置保存失败")
	}
	c.checkParallelism = n
	c.checkIPType = ipTypeCheck
	c.ttlMu.Lock()
	c.observationTTLMinutes = observationMinutes
	c.ipTypeTTLHours = ipTypeHours
	c.ttlMu.Unlock()
	c.m.connectBudget.setLimit(n)
	c.bumpConsoleVersion()
	return nil
}

func (c *Console) checkSettingsView(now time.Time) map[string]any {
	c.checksMu.Lock()
	parallelism, ipTypeCheck := c.checkParallelism, c.checkIPType
	c.checksMu.Unlock()
	c.ttlMu.RLock()
	observationMinutes, ipTypeHours := c.observationTTLMinutes, c.ipTypeTTLHours
	c.ttlMu.RUnlock()
	resultTTL := c.checkResultTTLAt(now)
	observationTTL := c.observationTTLAt(now)
	return map[string]any{
		"parallelism":                        parallelism,
		"ip_type_check":                      ipTypeCheck,
		"check_result_ttl_effective_minutes": int(resultTTL.Minutes()),
		"observation_ttl_minutes":            observationMinutes,
		"observation_ttl_effective_minutes":  int(observationTTL.Minutes()),
		"check_cycle_seconds":                int(c.checkCycleDuration().Seconds()),
		"ip_type_ttl_hours":                  ipTypeHours,
		"ip_type_cache":                      c.m.ipTypeCacheStats(),
	}
}

func (c *Console) apiCheckSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		writeJSON(w, 200, c.checkSettingsView(time.Now().UTC()))
		return
	}
	var in checkSettingsRequest
	if err := readConsoleJSON(w, r, &in); err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	c.checksMu.Lock()
	if c.ctx.Err() != nil {
		c.checksMu.Unlock()
		writeJSON(w, 503, map[string]string{"error": "管理服务正在停止"})
		return
	}
	ipTypeCheck := c.checkIPType
	if in.IPTypeCheck != nil {
		ipTypeCheck = *in.IPTypeCheck
	}
	c.ttlMu.RLock()
	observationMinutes, ipTypeHours := c.observationTTLMinutes, c.ipTypeTTLHours
	c.ttlMu.RUnlock()
	if in.ObservationTTLMinutes != nil {
		observationMinutes = *in.ObservationTTLMinutes
	}
	if in.IPTypeTTLHours != nil {
		ipTypeHours = *in.IPTypeTTLHours
	} else if in.LegacyIPTypeTTLHours != nil {
		ipTypeHours = *in.LegacyIPTypeTTLHours
	}
	err := c.saveCheckSettingsAll(in.Parallelism, ipTypeCheck, observationMinutes, ipTypeHours)
	c.checksMu.Unlock()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	c.applyTTLSettings(time.Now().UTC())
	writeJSON(w, 200, c.checkSettingsView(time.Now().UTC()))
}
