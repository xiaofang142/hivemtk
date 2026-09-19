package url

import (
	"context"
	"strings"
	"testing"
)

// 全部使用 IP 字面量：LookupIPAddr 对字面 IP 直返不发网络请求，测试离线可复现。
func TestValidateURL_RejectsNonHTTPScheme(t *testing.T) {
	cases := []string{
		"ftp://8.8.8.8/",
		"file:///etc/passwd",
		"gopher://127.0.0.1:6379/_INFO",
		"http:/example.com",
		"HTTP://8.8.8.8/", // 当前实现区分大小写，大写协议一律拒绝
		"",
	}
	for _, raw := range cases {
		err := ValidateURL(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "http") {
			t.Errorf("ValidateURL(%q) 应报协议错误, got %v", raw, err)
		}
	}
}

func TestValidateURL_RejectsMissingHost(t *testing.T) {
	for _, raw := range []string{"http://", "https://"} {
		err := ValidateURL(context.Background(), raw)
		if err == nil || !strings.Contains(err.Error(), "主机名") {
			t.Errorf("ValidateURL(%q) 应报缺少主机名, got %v", raw, err)
		}
	}
}

func TestValidateURL_BlocksInternalAndReservedIPs(t *testing.T) {
	cases := []struct {
		raw    string
		expect string // 错误信息应包含的 IP
	}{
		{"http://127.0.0.1/x", "127.0.0.1"},                             // loopback
		{"http://127.0.0.1:8080/admin", "127.0.0.1"},                    // loopback + 端口
		{"http://[::1]/", "::1"},                                        // IPv6 loopback
		{"http://10.1.2.3/", "10.1.2.3"},                                // private A
		{"http://192.168.0.254/", "192.168.0.254"},                      // private C
		{"http://172.16.5.5/", "172.16.5.5"},                            // private B
		{"http://[fc00::10]/", "fc00::10"},                              // IPv6 ULA private
		{"http://169.254.169.254/latest/meta-data/", "169.254.169.254"}, // 云元数据
		{"http://[fe80::1]/", "fe80::1"},                                // link-local
		{"http://224.0.0.5/", "224.0.0.5"},                              // link-local multicast
		{"http://0.0.0.0/", "0.0.0.0"},                                  // unspecified
		{"http://[::]/", "::"},                                          // IPv6 unspecified
	}
	for _, c := range cases {
		err := ValidateURL(context.Background(), c.raw)
		if err == nil {
			t.Fatalf("ValidateURL(%q) 应被 SSRF 拦截，实际放行", c.raw)
		}
		if !strings.Contains(err.Error(), "禁止访问内网/保留地址") || !strings.Contains(err.Error(), c.expect) {
			t.Errorf("ValidateURL(%q) 错误信息应含拦截标记与 IP %s, got %v", c.raw, c.expect, err)
		}
	}
}

func TestValidateURL_PassesPublicLiteralIP(t *testing.T) {
	// 8.8.8.8 为公网字面 IP，不触发 DNS，可离线断言放行路径
	if err := ValidateURL(context.Background(), "http://8.8.8.8/ok"); err != nil {
		t.Fatalf("公网 IP 应放行, got %v", err)
	}
}
