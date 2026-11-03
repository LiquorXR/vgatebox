package main

import (
	"testing"
	"time"
)

func TestProfileFromIPPureRequiresExplicitResidentialEvidence(t *testing.T) {
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
	raw := ippureResponse{
		IP: "70.79.253.35", ASN: 7922, ASOrganization: "Comcast",
		Country: "United States", CountryCode: "US", City: "Philadelphia",
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
