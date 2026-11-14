package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updateRepo = "LiquorXR/vgatebox"

// releaseInfo 是 GitHub Releases API 里我们关心的字段。
type releaseInfo struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

// UpdateStatus 回给界面：当前版本、最新版本、有没有新版、更新内容。
type UpdateStatus struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"has_update"`
	Notes     string `json:"notes"`
	URL       string `json:"url"`
	Supported bool   `json:"supported"`
}

// goarch 把 runtime.GOARCH 映射成 release 资产用的名字。
func assetArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	default:
		return runtime.GOARCH
	}
}

// fetchLatestRelease 拉取最新 release 元数据。
func fetchLatestRelease() (*releaseInfo, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", updateRepo)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "vgatebox-updater")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub 返回 HTTP %d", resp.StatusCode)
	}
	var rel releaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		return nil, err
	}
	if !validReleaseTag(rel.TagName) || rel.Draft || rel.Prerelease {
		return nil, fmt.Errorf("正式发布信息无效")
	}
	return &rel, nil
}

// versionLess 判断 cur 是否比 latest 旧。解析 vX.Y.Z 做数值比较；
// dev 或无法解析时保守认为"有更新"（让用户能装上正式版）。
func versionLess(cur, latest string) bool {
	if latest == "" {
		return false
	}
	if cur == "" || cur == "dev" {
		return true
	}
	cn, cok := parseSemver(cur)
	ln, lok := parseSemver(latest)
	if !cok || !lok {
		return cur != latest
	}
	for i := 0; i < 3; i++ {
		if cn[i] != ln[i] {
			return cn[i] < ln[i]
		}
	}
	return false
}

func parseSemver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	// 去掉预发布/构建后缀
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return out, false
	}
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
