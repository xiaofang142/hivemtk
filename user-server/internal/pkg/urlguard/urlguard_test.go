package urlguard

// urlguard_test.go G-1：出站 URL 公共防线的单元测试。
// 全部用 IP 字面量断言（离线环境无 DNS，域名用例会落在"解析失败"而非网段判据上）。

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidatePublicHTTPURL(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool // true=应通过
	}{
		{"loopback IP", "http://127.0.0.1:8080", false},
		{"localhost 域名", "http://localhost:9200", false},
		{"ipv6 loopback", "http://[::1]:8080", false},
		{"私网 A 192.168", "http://192.168.1.1", false},
		{"私网 A 10", "http://10.0.0.1", false},
		{"私网 B 172.16", "http://172.16.0.1", false},
		{"云元数据", "http://169.254.169.254/latest/meta-data", false},
		{"CGNAT 共享段", "http://100.64.0.1", false},
		{"未指定地址", "http://0.0.0.0:80", false},
		{"0.0.0.0/8 段（环回绕行面）", "http://0.1.2.3", false},
		{"受限广播", "http://255.255.255.255", false},
		{"IPv6 ULA fd00::/7", "http://[fd00::1]:8080", false},
		{"IPv6 链路本地", "http://[fe80::1]:8080", false},
		{"非 http scheme", "ftp://example.com", false},
		{"无 scheme", "127.0.0.1:8080", false},
		{"空串", "", false},
		{"公网 IP 字面量", "http://192.0.2.1:9999", true}, // TEST-NET-1 文档地址，解析期零网络
		{"公网 IP https", "https://8.8.8.8", true},
	}
	for _, tc := range cases {
		err := ValidatePublicHTTPURL(tc.raw)
		if tc.want && err != nil {
			t.Errorf("%s: 期望通过却拒绝: %v", tc.name, err)
		}
		if !tc.want && err == nil {
			t.Errorf("%s: 期望拒绝却通过", tc.name)
		}
	}
}

func TestDialControl(t *testing.T) {
	if err := DialControl("tcp", "127.0.0.1:8080", nil); err == nil {
		t.Error("loopback 应拒绝")
	}
	if err := DialControl("tcp", "192.168.0.1:443", nil); err == nil {
		t.Error("私网应拒绝")
	}
	if err := DialControl("tcp", "8.8.8.8:443", nil); err != nil {
		t.Errorf("公网 IP 应通过: %v", err)
	}
}

func TestGuardedTransportRejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	client := &http.Client{Transport: GuardedTransport(), Timeout: 5 * time.Second}
	if _, err := client.Get(srv.URL); err == nil {
		t.Fatal("loopback 出站应被 Control 回调拒绝（httptest 绑 127.0.0.1）")
	}
}
