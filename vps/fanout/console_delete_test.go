package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConsoleDeleteSlotAPIKeepsOtherSlotsAndDoesNotRebuild(t *testing.T) {
	c := defaultTargetFixture(t)
	c.m.nodes = []Node{
		{HostName: "delete-one", Config: "fixture"},
		{HostName: "delete-two", Config: "fixture"},
		{HostName: "delete-three", Config: "fixture"},
	}
	if err := c.applyTarget(ExitPlan{Count: 3, Policy: defaultSlotPolicyForMode("", false)}); err != nil {
		t.Fatal(err)
	}
	before := c.m.Tunnels()
	if len(before) != 3 {
		t.Fatalf("fixture allocated %d exits", len(before))
	}
	deleted := before[1].Slot
	retained := map[int]bool{}
	for _, tunnel := range before {
		if tunnel.Slot != deleted {
			retained[tunnel.Slot] = true
		}
	}

	w := httptest.NewRecorder()
	c.apiDeleteSlot(w, httptest.NewRequest(http.MethodPost, "/api/console/slot/delete", strings.NewReader(fmt.Sprintf(`{"slot":%d}`, deleted))))
	if w.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", w.Code, w.Body.String())
	}
	if c.plan == nil || c.plan.Count != 2 || c.plan.RemoveSlot != 0 {
		t.Fatalf("plan not decreased cleanly: %+v", c.plan)
	}
	for _, tunnel := range c.m.Tunnels() {
		if tunnel.Slot == deleted {
			t.Fatal("deleted slot was rebuilt immediately")
		}
		if !retained[tunnel.Slot] {
			t.Fatalf("unexpected slot %d", tunnel.Slot)
		}
	}
	if len(c.m.Tunnels()) != 2 {
		t.Fatalf("retained %d exits", len(c.m.Tunnels()))
	}

	if err := c.reconcile(); err != nil {
		t.Fatal(err)
	}
	if c.plan.Count != 2 || len(c.m.Tunnels()) != 2 {
		t.Fatal("background reconcile recreated the deleted slot")
	}
	for _, tunnel := range c.m.Tunnels() {
		if tunnel.Slot == deleted {
			t.Fatal("deleted slot reappeared after reconcile")
		}
	}
}

type cleanupPanel struct {
	*fakePanel
	deleted []int
}

func (p *cleanupPanel) DeleteInbounds(ids []int, _ []*Tunnel) error {
	p.deleted = append(p.deleted, ids...)
	drop := map[int]bool{}
	for _, id := range ids {
		drop[id] = true
	}
	kept := p.inbounds[:0]
	for _, ib := range p.inbounds {
		if !drop[ib.ID] {
			kept = append(kept, ib)
		}
	}
	p.inbounds = kept
	return nil
}

func deleteSlotFixture(t *testing.T) (*Console, *cleanupPanel) {
	t.Helper()
	c := consoleFixture(t)
	p := &cleanupPanel{fakePanel: &fakePanel{}}
	usePanel(t, p)
	c.m.nodes = []Node{
		{HostName: "cleanup-one", Config: "fixture"},
		{HostName: "cleanup-two", Config: "fixture"},
		{HostName: "cleanup-three", Config: "fixture"},
	}
	if err := c.applyTarget(ExitPlan{Count: 3, Policy: defaultSlotPolicyForMode("", false)}); err != nil {
		t.Fatal(err)
	}
	return c, p
}

func TestConsoleDeleteSlotPreservesArchiveByDefault(t *testing.T) {
	c, p := deleteSlotFixture(t)
	deleted := c.m.Tunnels()[1].Slot
	p.inbounds = []Inbound{{ID: 77, ManagedSlot: true, BoundSlot: deleted}}
	w := httptest.NewRecorder()
	c.apiDeleteSlot(w, httptest.NewRequest(http.MethodPost, "/api/console/slot/delete", strings.NewReader(fmt.Sprintf(`{"slot":%d}`, deleted))))
	if w.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", w.Code, w.Body.String())
	}
	if len(p.deleted) != 0 || len(p.inbounds) != 1 {
		t.Fatal("archive entry removed without requesting cleanup")
	}
}

func TestConsoleDeleteSlotCleansArchiveWhenRequested(t *testing.T) {
	c, p := deleteSlotFixture(t)
	slots := c.m.Tunnels()
	deleted := slots[1].Slot
	kept := slots[2].Slot
	p.inbounds = []Inbound{
		{ID: 77, ManagedSlot: true, BoundSlot: deleted},
		{ID: 78, ManagedSlot: true, BoundSlot: kept},
	}
	w := httptest.NewRecorder()
	c.apiDeleteSlot(w, httptest.NewRequest(http.MethodPost, "/api/console/slot/delete", strings.NewReader(fmt.Sprintf(`{"slot":%d,"cleanup_archive":true}`, deleted))))
	if w.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", w.Code, w.Body.String())
	}
	if len(p.deleted) != 1 || p.deleted[0] != 77 {
		t.Fatalf("deleted inbounds: %v", p.deleted)
	}
	if len(p.inbounds) != 1 || p.inbounds[0].ID != 78 {
		t.Fatalf("cleanup touched another slot: %+v", p.inbounds)
	}
	var out struct {
		OK           bool   `json:"ok"`
		CleanupError string `json:"cleanup_error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.CleanupError != "" {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestConsoleDeleteSlotAPIRejectsInvalidRequests(t *testing.T) {
	c := defaultTargetFixture(t)
	for _, tc := range []struct {
		name   string
		method string
		body   string
	}{
		{name: "get", method: http.MethodGet},
		{name: "missing", method: http.MethodPost, body: `{}`},
		{name: "zero", method: http.MethodPost, body: `{"slot":0}`},
		{name: "unknown slot", method: http.MethodPost, body: `{"slot":99}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c.apiDeleteSlot(w, httptest.NewRequest(tc.method, "/api/console/slot/delete", strings.NewReader(tc.body)))
			if tc.method == http.MethodGet {
				if w.Code != http.StatusMethodNotAllowed {
					t.Fatalf("got %d", w.Code)
				}
				return
			}
			if w.Code != http.StatusBadRequest && w.Code != http.StatusConflict {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestConsoleSummaryExposesRegionInventory(t *testing.T) {
	c := consoleFixture(t)
	usePanel(t, &fakePanel{})
	c.m.nodes = []Node{
		{HostName: "region-jp-1", CountryCode: "JP", Country: "Japan", Ping: 40, SpeedMbps: 12},
		{HostName: "region-jp-2", CountryCode: "JP", Country: "Japan", Ping: 25, SpeedMbps: 30},
		{HostName: "region-kr-1", CountryCode: "KR", Country: "South Korea", Ping: 60, SpeedMbps: 8},
	}
	w := httptest.NewRecorder()
	c.apiConsole(w, httptest.NewRequest(http.MethodGet, "/api/console", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("console returned %d", w.Code)
	}
	var out struct {
		Regions []RegionStat `json:"regions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Regions) != 2 {
		t.Fatalf("got %d regions: %+v", len(out.Regions), out.Regions)
	}
	if out.Regions[0].Code != "JP" || out.Regions[0].Available != 2 || out.Regions[0].BestPing != 25 || out.Regions[0].BestSpeed != 30 {
		t.Fatalf("JP inventory mismatch: %+v", out.Regions[0])
	}
}
