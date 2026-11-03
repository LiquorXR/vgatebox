package main

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCheckIPTypeContextDefaultsOnAndCanBeDisabled(t *testing.T) {
	if !checkIPTypeFromContext(context.Background()) {
		t.Fatal("candidate checks must check IP type by default")
	}
	ctx := context.WithValue(context.Background(), checkIPTypeContextKey{}, false)
	if checkIPTypeFromContext(ctx) {
		t.Fatal("candidate check type switch was ignored")
	}
}

func TestCheckSettingsTTLDefaultsPersistAndValidate(t *testing.T) {
	c := consoleFixture(t)
	w := httptest.NewRecorder()
	c.apiCheckSettings(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, `"check_result_ttl_minutes":0`) || !strings.Contains(body, `"check_result_ttl_effective_minutes":90`) ||
		!strings.Contains(body, `"observation_ttl_minutes":30`) || !strings.Contains(body, `"ippure_ttl_hours":24`) {
		t.Fatalf("wrong TTL defaults: %s", body)
	}
	w = httptest.NewRecorder()
	c.apiCheckSettings(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"parallelism":2,"ip_type_check":false,"check_result_ttl_minutes":120,"observation_ttl_minutes":60,"ippure_ttl_hours":12}`)))
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"check_result_ttl_effective_minutes":120`) || !strings.Contains(w.Body.String(), `"observation_ttl_effective_minutes":60`) {
		t.Fatalf("TTL settings were not saved: %d %s", w.Code, w.Body.String())
	}
	reloaded, err := newConsole(NewManager(maximumSlots, c.m.workDir))
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.cancel()
	reloaded.ttlMu.RLock()
	result, observation, ippure := reloaded.checkResultTTLMinutes, reloaded.observationTTLMinutes, reloaded.ippureTTLHours
	reloaded.ttlMu.RUnlock()
	if result != 120 || observation != 60 || ippure != 12 {
		t.Fatalf("TTL settings did not survive restart: %d %d %d", result, observation, ippure)
	}
	reloaded.m.mu.RLock()
	managerObservationTTL := reloaded.m.observationTTLValueLocked()
	reloaded.m.mu.RUnlock()
	if managerObservationTTL != 60*time.Minute {
		t.Fatalf("manager observation TTL: %s", managerObservationTTL)
	}
	for _, body := range []string{
		`{"parallelism":2,"ip_type_check":false,"check_result_ttl_minutes":1441}`,
		`{"parallelism":2,"ip_type_check":false,"check_result_ttl_minutes":29}`,
		`{"parallelism":2,"ip_type_check":false,"check_result_ttl_minutes":40}`,
		`{"parallelism":2,"ip_type_check":false,"observation_ttl_minutes":0}`,
		`{"parallelism":2,"ip_type_check":false,"ippure_ttl_hours":0}`,
	} {
		w = httptest.NewRecorder()
		c.apiCheckSettings(w, httptest.NewRequest("POST", "/", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("invalid TTL accepted: %s", body)
		}
	}
}
