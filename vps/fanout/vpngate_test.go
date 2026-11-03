package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func sampleNodeCSV(host string) string {
	cfg := base64.StdEncoding.EncodeToString([]byte("client\nremote 1.2.3.4 443\n"))
	return "*vpn_servers\n" +
		"#HostName,IP,Score,Ping,Speed,CountryLong,CountryShort,NumVpnSessions,Uptime,TotalUsers,TotalTraffic,LogType,Operator,Message,OpenVPN_ConfigData_Base64\n" +
		fmt.Sprintf("%s,1.2.3.4,100,20,10000000,Japan,JP,3,1,1,1,2,op,,%s\n", host, cfg) +
		"*\n"
}

// 生产路径消费共享 Pool Builder 的快照；这个直连读取只用于解析和错误分支。
func TestFetchNodesUsesDirectSource(t *testing.T) {
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, sampleNodeCSV("direct-node"))
	}))
	defer direct.Close()

	nodes, err := fetchNodesWith(direct.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("直连应该成功: %v", err)
	}
	if len(nodes) != 1 || nodes[0].HostName != "direct-node" {
		t.Fatalf("拿到的不是直连结果: %+v", nodes)
	}
}

// 被拦截或内容被劫持时直接报错，不再回退任何第三方反代。
func TestFetchNodesRejectsBlockedAndGarbageSources(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{
		"forbidden": func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "blocked", http.StatusForbidden)
		},
		"garbage": func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, "<html><body>请先登录网络</body></html>")
		},
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(handler)
			defer server.Close()
			if _, err := fetchNodesWith(server.URL, 5*time.Second); err == nil {
				t.Fatal("异常来源应直接失败")
			}
		})
	}
}
