package main

import (
	"context"
	"encoding/json"
	"fmt"
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
	ippureTTL         = 24 * time.Hour
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
	image := unknownOpenVPNEgress(expectedIP, "ippure_unavailable")
	image.Protocol = "openvpn"
	if raw.IP != expectedIP {
		return image, fmt.Errorf("ippure_ip_mismatch")
	}
	expires := now.Add(ippureTTL)
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

func (t *Tunnel) probeIPPure(parent context.Context, expectedIP string) EgressProfile {
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
		return profile
	}
	var raw ippureResponse
	if err := json.Unmarshal(out, &raw); err != nil {
		profile.IntelligenceError = "ippure_invalid_json"
		return profile
	}
	image, err := profileFromIPPure(raw, expectedIP, time.Now().UTC())
	if err != nil {
		profile.IntelligenceError = err.Error()
		return profile
	}
	image.Raw = out
	return image
}

func (m *Manager) lookupEgressProfile(t *Tunnel, ip string) EgressProfile {
	if ippureEnabled() {
		image := t.probeIPPure(t.workContext(), ip)
		// A failed IPPure query may fall back to an explicitly configured
		// provider, but a successful "not residential" answer never does.
		if image.IntelligenceStatus == "failed" && strings.TrimSpace(os.Getenv("INTELLIGENCE_URL_TEMPLATE")) != "" {
			return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
		}
		return image
	}
	return lookupOpenVPNEgressContext(t.workContext(), m.evidenceDir, ip)
}
