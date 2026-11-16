package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type migrationIssue struct {
	File string `json:"file"`
	Code string `json:"code"`
}

type migrationSlot struct {
	Slot                int      `json:"slot"`
	Port                int      `json:"port"`
	Stopped             bool     `json:"stopped"`
	LegacyPolicy        bool     `json:"legacy_policy"`
	Country             string   `json:"country"`
	AllowedIPTypes      []string `json:"allowed_ip_types"`
	GenerateCredentials bool     `json:"generate_credentials_on_start"`
}

type migrationReport struct {
	ConfigValid            bool             `json:"config_valid"`
	Scope                  string           `json:"scope"`
	LiveValidationRequired bool             `json:"live_validation_required"`
	Slots                  []migrationSlot  `json:"slots"`
	InboundCount           int              `json:"inbound_count"`
	Issues                 []migrationIssue `json:"issues"`
}

// Inspect a stopped service's private COPY. No loaders that create credentials,
// recovery writes, subprocesses, sockets, netns or global mutations are used.
// Reports contain only fixed error codes and selected non-secret facts.
func (r *migrationReport) addIssue(file, code string) {
	r.Issues = append(r.Issues, migrationIssue{File: file, Code: code})
}

// Inspect a stopped service's private COPY. No loaders that create credentials,
// recovery writes, subprocesses, sockets, netns or global mutations are used.
// Reports contain only fixed error codes and selected non-secret facts.
func checkMigrationState(dir string, maxSlots, defaultPort int) migrationReport {
	r := migrationReport{Scope: "native_persisted_structure_only", LiveValidationRequired: true,
		Slots: []migrationSlot{}, Issues: []migrationIssue{}}
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		r.addIssue("directory", "unreadable_directory")
		return r
	}
	// A journal means files may represent different points in a transaction.
	// Do not guess the intended version or perform recovery in a read-only mode.
	if checkMigrationJournals(dir, &r) {
		return r
	}
	expectedSlots := checkMigrationPlan(dir, maxSlots, &r)
	checkMigrationSstp(dir, &r)
	checkMigrationSettings(dir, defaultPort, &r)
	checkMigrationPassword(dir, &r)
	checkMigrationBasePath(dir, &r)
	stateBlob, hasState := readMigrationFile(dir, "state.json", true, &r)
	if hasState {
		r.Slots = migrationSlots(stateBlob, maxSlots, &r)
	}
	checkMigrationNative(dir, &r, &expectedSlots)
	// A new direct-only installation has never allocated VPN Gate state.
	// Missing state is acceptable only when no plan or entry refers to a Slot.
	if !hasState && expectedSlots {
		r.addIssue("state.json", "missing_slot_state")
	}
	r.ConfigValid = len(r.Issues) == 0
	return r
}

func checkMigrationJournals(dir string, r *migrationReport) bool {
	for _, name := range []string{managementJournal, stateJournal, nativeJournal, consoleJournal} {
		if _, err := os.Lstat(filepath.Join(dir, name)); err == nil {
			r.addIssue(name, "pending_transaction")
		} else if !os.IsNotExist(err) {
			r.addIssue(name, "unreadable_file")
		}
	}
	return len(r.Issues) > 0
}

func readMigrationFile(dir, name string, optional bool, r *migrationReport) ([]byte, bool) {
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if optional && os.IsNotExist(err) {
		return nil, false
	}
	if err != nil {
		r.addIssue(name, "missing_or_unreadable_file")
		return nil, false
	}
	if !info.Mode().IsRegular() || info.Size() > 16<<20 {
		r.addIssue(name, "not_a_bounded_regular_file")
		return nil, false
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		r.addIssue(name, "unreadable_file")
		return nil, false
	}
	return blob, true
}

func checkMigrationPlan(dir string, maxSlots int, r *migrationReport) bool {
	blob, ok := readMigrationFile(dir, "console.json", true, r)
	if !ok {
		return false
	}
	var plan *ExitPlan
	if json.Unmarshal(blob, &plan) != nil || plan == nil || validateExitPlan(plan, maxSlots) != nil {
		r.addIssue("console.json", "invalid_exit_plan")
		return false
	}
	return plan.Count > 0
}

func checkMigrationSstp(dir string, r *migrationReport) {
	blob, ok := readMigrationFile(dir, "sstp.json", true, r)
	if !ok {
		return
	}
	var state sstpState
	if json.Unmarshal(blob, &state) != nil || validateSstpSettings(state.Settings) != nil {
		r.addIssue("sstp.json", "invalid_sstp_settings")
	}
}

func checkMigrationSettings(dir string, defaultPort int, r *migrationReport) {
	blob, ok := readMigrationFile(dir, "settings.json", false, r)
	if !ok {
		return
	}
	settings, err := decodeWebSettings(blob, defaultPort)
	if err != nil {
		r.addIssue("settings.json", "invalid_settings")
		return
	}
	if strings.TrimSpace(settings.SubToken) == "" {
		r.addIssue("settings.json", "missing_subscription_token")
	}
}

func checkMigrationPassword(dir string, r *migrationReport) {
	blob, ok := readMigrationFile(dir, "password", false, r)
	if ok && strings.TrimSpace(string(blob)) == "" {
		r.addIssue("password", "empty_management_password")
	}
}

func checkMigrationBasePath(dir string, r *migrationReport) {
	blob, ok := readMigrationFile(dir, "basepath", false, r)
	if !ok {
		return
	}
	if _, err := validateBasePath(strings.TrimSpace(string(blob))); err != nil {
		r.addIssue("basepath", "invalid_base_path")
	}
}

func migrationSlots(blob []byte, maxSlots int, r *migrationReport) []migrationSlot {
	var original *persistedState
	if json.Unmarshal(blob, &original) != nil || original == nil {
		r.addIssue("state.json", "invalid_state")
		return nil
	}
	state, err := decodePersistedState(blob, maxSlots)
	if err != nil {
		r.addIssue("state.json", "invalid_state")
		return nil
	}
	slots := make([]migrationSlot, 0, len(state.Tunnels))
	for i, p := range state.Tunnels {
		slots = append(slots, migrationSlot{Slot: p.Slot, Port: p.Port, Stopped: p.Stopped,
			LegacyPolicy: original.Tunnels[i].Policy == nil, Country: p.Policy.Country,
			AllowedIPTypes: p.Policy.AllowedIPTypes, GenerateCredentials: p.SocksUser == ""})
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Slot < slots[j].Slot })
	return slots
}

func checkMigrationNative(dir string, r *migrationReport, expectedSlots *bool) {
	for _, name := range []string{"native.json", "native.previous.json"} {
		blob, ok := readMigrationFile(dir, name, name == "native.previous.json", r)
		if !ok {
			continue
		}
		store, err := decodeNativeStore(blob)
		if err != nil {
			r.addIssue(name, "invalid_native_config")
			continue
		}
		if name == "native.json" {
			r.InboundCount = len(store.Inbounds)
		}
		for _, ib := range store.Inbounds {
			if ib.BoundSlot != 0 || ib.BoundTo != "" || ib.SocksSlot != 0 {
				*expectedSlots = true
			}
		}
	}
}

func runMigrationCheck(w io.Writer, dir string, maxSlots, defaultPort int) int {
	report := checkMigrationState(dir, maxSlots, defaultPort)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil || !report.ConfigValid {
		return 1
	}
	return 0
}
