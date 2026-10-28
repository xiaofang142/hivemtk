package kbconnector

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// 上游清单 URL 指向普通网页时，必须归为「配置不合法」而不是服务端内部错误。
func TestWebhttpProviderHTMLManifestIsInvalidInput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!doctype html><html><body>not a manifest</body></html>"))
	}))
	defer srv.Close()

	p, err := LookupProvider("webhttp")
	if err != nil {
		t.Fatalf("lookup provider: %v", err)
	}
	_, err = p.List(context.Background(), map[string]any{"manifest_url": srv.URL})
	if err == nil {
		t.Fatal("期望返回错误，实际成功")
	}
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望 ErrInvalidInput，实际 %v", err)
	}
}

// 缺少 manifest_url 也得是配置错误（400），不能落到 500。
func TestWebhttpProviderMissingManifestURLIsInvalidInput(t *testing.T) {
	p, _ := LookupProvider("webhttp")
	if _, err := p.List(context.Background(), map[string]any{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("期望 ErrInvalidInput，实际 %v", err)
	}
}

// 合法 JSON 清单仍应正常解析（含 UTF-8 BOM）。
func TestWebhttpProviderValidManifest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("\xEF\xBB\xBF{\"docs\":[{\"title\":\"a\",\"url\":\"https://example.com/a.md\"}]}"))
	}))
	defer srv.Close()

	p, _ := LookupProvider("webhttp")
	docs, err := p.List(context.Background(), map[string]any{"manifest_url": srv.URL})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(docs) != 1 || docs[0].Title != "a" {
		t.Fatalf("unexpected docs: %+v", docs)
	}
}
