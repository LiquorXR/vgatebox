package main

import (
	"testing"
)

func TestVersionLess(t *testing.T) {
	cases := []struct {
		cur, latest string
		want        bool
	}{
		{"v1.0.5", "v1.0.6", true},
		{"v1.0.6", "v1.0.6", false},
		{"v1.0.6", "v1.0.5", false},
		{"1.0.0", "v1.2.0", true},
		{"v2.0.0", "v1.9.9", false},
		{"dev", "v1.0.0", true},
		{"", "v1.0.0", true},
		{"v1.0.0", "", false},
		{"v1.0", "v1.0.1", true},
	}
	for _, c := range cases {
		if got := versionLess(c.cur, c.latest); got != c.want {
			t.Errorf("versionLess(%q,%q)=%v，想要 %v", c.cur, c.latest, got, c.want)
		}
	}
}

func TestParseSemver(t *testing.T) {
	if v, ok := parseSemver("v1.2.3"); !ok || v != [3]int{1, 2, 3} {
		t.Fatalf("parseSemver v1.2.3 => %v %v", v, ok)
	}
	if v, ok := parseSemver("1.4.0-rc1"); !ok || v != [3]int{1, 4, 0} {
		t.Fatalf("parseSemver 预发布后缀应被忽略 => %v %v", v, ok)
	}
	if _, ok := parseSemver("nightly"); ok {
		t.Fatal("非法版本应解析失败")
	}
}
