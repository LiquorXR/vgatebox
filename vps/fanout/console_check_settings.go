package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type checkSettings struct {
	Parallelism           int  `json:"parallelism"`
	IPTypeCheck           bool `json:"ip_type_check"`
	CheckResultTTLMinutes int  `json:"check_result_ttl_minutes"`
	ObservationTTLMinutes int  `json:"observation_ttl_minutes"`
	IPPureTTLHours        int  `json:"ippure_ttl_hours"`
}

type checkSettingsRequest struct {
	Parallelism           int   `json:"parallelism"`
	IPTypeCheck           *bool `json:"ip_type_check"`
	CheckResultTTLMinutes *int  `json:"check_result_ttl_minutes"`
	ObservationTTLMinutes *int  `json:"observation_ttl_minutes"`
	IPPureTTLHours        *int  `json:"ippure_ttl_hours"`
}

func validateCheckParallelism(n int) error {
	if n < 1 || n > networkLinkCount {
		return fmt.Errorf("检测并发须为正整数，不能超过实例 /30 地址容量 %d", networkLinkCount)
	}
	return nil
}

func validateTTLSettings(resultMinutes, observationMinutes, ippureHours int) error {
	if resultMinutes != 0 && (resultMinutes < minCheckResultTTLMinutes || resultMinutes > maxCheckResultTTLMinutes) {
		return fmt.Errorf("结果有效期须为 0（自动）或 30–1440 分钟")
	}
	if observationMinutes < 1 || observationMinutes > maxObservationTTLMinutes {
		return fmt.Errorf("排序观察有效期须为 1–1440 分钟")
	}
	if ippureHours < 1 || ippureHours > maxIPPureTTLHours {
		return fmt.Errorf("IPPure 缓存有效期须为 1–168 小时")
	}
	return nil
}

func normalizeTTLSettings(resultMinutes, observationMinutes, ippureHours int) (int, int, int) {
	if resultMinutes < 0 || resultMinutes > maxCheckResultTTLMinutes {
		resultMinutes = 0
	}
	if resultMinutes > 0 && resultMinutes < minCheckResultTTLMinutes {
		resultMinutes = minCheckResultTTLMinutes
	}
	if observationMinutes <= 0 || observationMinutes > maxObservationTTLMinutes {
		observationMinutes = defaultObservationTTLMinutes
	}
	if ippureHours <= 0 || ippureHours > maxIPPureTTLHours {
		ippureHours = defaultIPPureTTLHours
	}
	return resultMinutes, observationMinutes, ippureHours
}

func (c *Console) loadCheckSettings() error {
	c.checkParallelism = defaultCheckParallelism
	c.checkIPType = false
	c.checkResultTTLMinutes = 0
	c.observationTTLMinutes = defaultObservationTTLMinutes
	c.ippureTTLHours = defaultIPPureTTLHours
	c.m.connectBudget.setLimit(defaultCheckParallelism)
	blob, err := os.ReadFile(filepath.Join(c.m.workDir, "check-settings.json"))
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
	result, observation, ippure := normalizeTTLSettings(s.CheckResultTTLMinutes, s.ObservationTTLMinutes, s.IPPureTTLHours)
	if err := validateTTLSettings(result, observation, ippure); err != nil {
		return fmt.Errorf("检测设置损坏，保留原文件")
	}
	c.checkParallelism = s.Parallelism
	c.checkIPType = s.IPTypeCheck
	c.checkResultTTLMinutes = result
	c.observationTTLMinutes = observation
	c.ippureTTLHours = ippure
	c.m.connectBudget.setLimit(s.Parallelism)
	return nil
}

// Caller holds checksMu; atomic persistence precedes publication.
func (c *Console) saveCheckSettings(n int, ipTypeCheck bool) error {
	c.ttlMu.RLock()
	result, observation, ippure := c.checkResultTTLMinutes, c.observationTTLMinutes, c.ippureTTLHours
	c.ttlMu.RUnlock()
	return c.saveCheckSettingsAll(n, ipTypeCheck, result, observation, ippure)
}

func (c *Console) saveCheckSettingsAll(n int, ipTypeCheck bool, resultMinutes, observationMinutes, ippureHours int) error {
	if err := validateCheckParallelism(n); err != nil {
		return err
	}
	if err := validateTTLSettings(resultMinutes, observationMinutes, ippureHours); err != nil {
		return err
	}
	blob, _ := json.Marshal(checkSettings{Parallelism: n, IPTypeCheck: ipTypeCheck, CheckResultTTLMinutes: resultMinutes, ObservationTTLMinutes: observationMinutes, IPPureTTLHours: ippureHours})
	if err := atomicConfigWrite(filepath.Join(c.m.workDir, "check-settings.json"), blob); err != nil {
		return fmt.Errorf("检测设置保存失败")
	}
	c.checkParallelism = n
	c.checkIPType = ipTypeCheck
	c.ttlMu.Lock()
	c.checkResultTTLMinutes = resultMinutes
	c.observationTTLMinutes = observationMinutes
	c.ippureTTLHours = ippureHours
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
	resultMinutes, observationMinutes, ippureHours := c.checkResultTTLMinutes, c.observationTTLMinutes, c.ippureTTLHours
	c.ttlMu.RUnlock()
	resultTTL := c.checkResultTTLAt(now)
	observationTTL := c.observationTTLAt(now)
	return map[string]any{
		"parallelism":                        parallelism,
		"ip_type_check":                      ipTypeCheck,
		"check_result_ttl_minutes":           resultMinutes,
		"check_result_ttl_effective_minutes": int(resultTTL.Minutes()),
		"observation_ttl_minutes":            observationMinutes,
		"observation_ttl_effective_minutes":  int(observationTTL.Minutes()),
		"check_cycle_seconds":                int(c.checkCycleDuration().Seconds()),
		"ippure_ttl_hours":                   ippureHours,
		"ippure_cache":                       c.m.ippureCacheStats(),
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
	resultMinutes, observationMinutes, ippureHours := c.checkResultTTLMinutes, c.observationTTLMinutes, c.ippureTTLHours
	c.ttlMu.RUnlock()
	if in.CheckResultTTLMinutes != nil {
		resultMinutes = *in.CheckResultTTLMinutes
	}
	if in.ObservationTTLMinutes != nil {
		observationMinutes = *in.ObservationTTLMinutes
	}
	if in.IPPureTTLHours != nil {
		ippureHours = *in.IPPureTTLHours
	}
	enabled, minutes := c.automationCheckSchedule()
	if err := validateResultTTLForSchedule(resultMinutes, enabled, minutes); err != nil {
		c.checksMu.Unlock()
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	err := c.saveCheckSettingsAll(in.Parallelism, ipTypeCheck, resultMinutes, observationMinutes, ippureHours)
	c.checksMu.Unlock()
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	c.applyTTLSettings(time.Now().UTC())
	writeJSON(w, 200, c.checkSettingsView(time.Now().UTC()))
}
