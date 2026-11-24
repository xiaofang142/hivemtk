// provider_test.go 外部审核源（L3）的适配层断言。
//
// 本包的定位是"默认关闭的增强项"，所以测试分成两半：
//   - 构造期校验（配置错了要在启动时炸，不是等线上第一条消息）；
//   - 调用期行为（超时/非 200/重定向/业务码失败都必须回错误，由服务层降级）。
package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewHTTPAuditor_RejectsBadConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  HTTPConfig
		want error
	}{
		{"空 endpoint", HTTPConfig{}, ErrNotConfigured},
		{"只空白", HTTPConfig{Endpoint: "   "}, ErrNotConfigured},
		{"协议不支持", HTTPConfig{Endpoint: "ftp://example.com/audit", AllowInsecure: true}, nil},
		{"缺主机", HTTPConfig{Endpoint: "https:///audit"}, nil},
		{"http 未显式放行", HTTPConfig{Endpoint: "http://example.com/audit"}, nil},
		{"回环地址走 https 未放行", HTTPConfig{Endpoint: "https://127.0.0.1/audit"}, nil},
		{"私网地址未放行", HTTPConfig{Endpoint: "https://10.0.0.8/audit"}, nil},
		{"主机不在白名单", HTTPConfig{
			Endpoint: "https://evil.example.com/audit", AllowedHosts: []string{"audit.example.com"},
		}, nil},
	}
	for _, tc := range cases {
		_, err := NewHTTPAuditor(tc.cfg)
		if err == nil {
			t.Fatalf("%s：应报错", tc.name)
		}
		if tc.want != nil && !errors.Is(err, tc.want) {
			t.Fatalf("%s：错误 = %v，期望 %v", tc.name, err, tc.want)
		}
	}

	// 合法配置：白名单命中 + 显式放行 http。
	if _, err := NewHTTPAuditor(HTTPConfig{
		Endpoint: "http://audit.internal:8080/x", AllowedHosts: []string{"audit.internal:8080"},
		AllowInsecure: true,
	}); err != nil {
		t.Fatalf("合法配置应通过：%v", err)
	}
}

func TestAudit_Success(t *testing.T) {
	var gotAuth, gotPath string
	var gotReq Request

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		_, _ = w.Write([]byte(`{"code":0,"data":{"risk_level":2,"labels":["porn","ad"],"score":0.92,"request_id":"req-1"}}`))
	}))
	defer srv.Close()

	a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL + "/audit", Token: "sec" + "ret", AllowInsecure: true})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	if a.Name() != "api" {
		t.Fatalf("审查源名 = %q，期望 api", a.Name())
	}

	res, err := a.Audit(context.Background(), Request{
		TenantID: 7, SubjectType: "user", SubjectID: "u_1",
		Scene: "inbound_message", Content: "待审内容", TraceID: "trace-1",
	})
	if err != nil {
		t.Fatalf("Audit 报错：%v", err)
	}
	if res.RiskLevel != 2 || res.Score != 0.92 || res.RequestID != "req-1" || len(res.Labels) != 2 {
		t.Fatalf("结论不对：%+v", res)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if gotPath != "/audit" {
		t.Fatalf("请求路径 = %q", gotPath)
	}
	if gotReq.Content != "待审内容" || gotReq.TraceID != "trace-1" || gotReq.TenantID != 7 {
		t.Fatalf("请求体不对：%+v", gotReq)
	}
}

func TestAudit_ClampsRiskLevel(t *testing.T) {
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{"code":0,"data":{"risk_level":9}}`, 3},
		{`{"code":0,"data":{"risk_level":-4}}`, 0},
		{`{"code":0,"data":{"risk_level":1}}`, 1},
	} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(tc.body))
		}))
		a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL, AllowInsecure: true})
		if err != nil {
			t.Fatalf("构造失败：%v", err)
		}
		res, err := a.Audit(context.Background(), Request{Content: "x"})
		srv.Close()
		if err != nil {
			t.Fatalf("Audit 报错：%v（body=%s）", err, tc.body)
		}
		// risk_level 与本地 severity 必须同口径（0..3），否则两套阈值无法比较。
		if res.RiskLevel != tc.want {
			t.Fatalf("risk_level 夹取 = %d，期望 %d（body=%s）", res.RiskLevel, tc.want, tc.body)
		}
	}
}

func TestAudit_ErrorResponses(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"非 200", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream down"))
		}},
		{"业务码非 0", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"code":4001,"message":"bad token"}`))
		}},
		{"响应不是 JSON", func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>oops</html>"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL, AllowInsecure: true})
			if err != nil {
				t.Fatalf("构造失败：%v", err)
			}
			// 一律回错误，由服务层降级为"仅本地结论"——绝不静默当成 0 风险。
			if _, err := a.Audit(context.Background(), Request{Content: "x"}); err == nil {
				t.Fatal("应返回错误")
			}
		})
	}
}

func TestAudit_DoesNotFollowRedirects(t *testing.T) {
	// 审核网关一旦被 302 到别处，就是把用户原文送到白名单之外 ⇒ 必须拒绝。
	var reached bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		_, _ = w.Write([]byte(`{"code":0,"data":{"risk_level":0}}`))
	}))
	defer target.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()

	a, err := NewHTTPAuditor(HTTPConfig{Endpoint: redirector.URL, AllowInsecure: true})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	if _, err := a.Audit(context.Background(), Request{Content: "x"}); err == nil {
		t.Fatal("重定向必须报错")
	}
	if reached {
		t.Fatal("不得跟随重定向（否则原文会被送到白名单之外）")
	}
}

func TestAudit_Timeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	defer close(release)

	a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL, AllowInsecure: true, Timeout: 60 * time.Millisecond})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	start := time.Now()
	_, err = a.Audit(context.Background(), Request{Content: "x"})
	if err == nil {
		t.Fatal("超时必须报错")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("超时未生效，耗时 %s", elapsed)
	}
}

func TestAudit_TruncatesHugeResponseBody(t *testing.T) {
	// 响应体上限防"被上游拖死内存"：超出部分被截断后解析失败 ⇒ 降级，而不是吃满内存。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"request_id":"` + strings.Repeat("x", 5000) + `"}}`))
	}))
	defer srv.Close()
	a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL, AllowInsecure: true, MaxBodyBytes: 64})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	if _, err := a.Audit(context.Background(), Request{Content: "x"}); err == nil {
		t.Fatal("超限响应的解析应失败（截断后 JSON 不完整）")
	}
}

func TestAudit_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()
	a, err := NewHTTPAuditor(HTTPConfig{Endpoint: srv.URL, AllowInsecure: true})
	if err != nil {
		t.Fatalf("构造失败：%v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := a.Audit(ctx, Request{Content: "x"}); err == nil {
		t.Fatal("上下文取消应报错")
	}
}

func TestNilAuditor(t *testing.T) {
	var a *HTTPAuditor
	if _, err := a.Audit(context.Background(), Request{Content: "x"}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("nil 审核源应回 ErrNotConfigured，得 %v", err)
	}
}
