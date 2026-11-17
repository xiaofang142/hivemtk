package etl

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"

	"path/filepath"
	"strings"
	"testing"
)

func TestParseDocument_BuiltinUnchanged(t *testing.T) {
	// sidecar 未启用（测试默认 provider 返回空）→ 与 ExtractText 逐字节同路
	// （PARSER_SIDECAR_URL env 已退役 B6，取值走 provider seam）
	if sidecarURL() != "" {
		t.Skip("预置了 sidecar provider，跳过 builtin 对照")
	}
	got, err := ParseDocument(context.Background(), "a.txt", []byte("hello\nworld"))
	if err != nil || got != "hello\nworld" {
		t.Errorf("txt 直通: %q %v", got, err)
	}
	if _, err := ParseDocument(context.Background(), "broken.pdf", []byte("not pdf")); err == nil {
		t.Error("坏 PDF 仍应报错（builtin）")
	}
}

func TestParseDocument_SidecarEnabled(t *testing.T) {
	var gotName string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			t.Errorf("multipart 解析: %v", err)
		}
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Errorf("file 字段: %v", err)
			return
		}
		defer f.Close()
		gotName = h.Filename
		gotBody = make([]byte, 64)
		n, _ := f.Read(gotBody)
		gotBody = gotBody[:n]
		_, _ = w.Write([]byte("PARSED TEXT from sidecar"))
	}))
	defer srv.Close()

	SetSidecarURLProvider(func() string { return srv.URL }) // env 已退役 B6，测试走 provider
	defer SetSidecarURLProvider(nil)

	got, err := ParseDocument(context.Background(), "scan.pdf", []byte("PDFBYTES"))
	if err != nil || !strings.Contains(got, "PARSED TEXT") {
		t.Fatalf("sidecar 路径: %q %v", got, err)
	}
	if gotName != "scan.pdf" || string(gotBody) != "PDFBYTES" {
		t.Errorf("上传内容: name=%s body=%q", gotName, gotBody)
	}
	// 纯文本仍走 builtin（不进 sidecar）
	got2, err := ParseDocument(context.Background(), "a.txt", []byte("plain"))
	if err != nil || got2 != "plain" {
		t.Errorf("txt 应直通: %q %v", got2, err)
	}
}

func TestParseDocument_SidecarFailureNoFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("sidecar down"))
	}))
	defer srv.Close()
	SetSidecarURLProvider(func() string { return srv.URL })
	defer SetSidecarURLProvider(nil)

	// sidecar 挂了不能静默回退 builtin（builtin 对扫描件必然也失败，回退只会掩盖故障）
	_, err := ParseDocument(context.Background(), "scan.pdf", []byte("PDFBYTES"))
	if err == nil || !strings.Contains(err.Error(), "sidecar 解析失败") {
		t.Errorf("sidecar 失败应显式报错: %v", err)
	}
}

func TestImageDescribe_DisabledAndEnabled(t *testing.T) {
	SetImageDescribeConfigProvider(func() (string, string) { return "", "" })
	_, err := DescribeImage(context.Background(), "a.png", []byte("png"))
	if !errors.Is(err, ErrImageDescribeDisabled) {
		t.Errorf("未配置应返回哨兵错: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content []map[string]any `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) == 0 || len(req.Messages[0].Content) != 2 {
			t.Errorf("消息形状: %+v", req.Messages)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"图中是一张产品价目表，含价格 4999 元"}}]}`))
	}))
	defer srv.Close()
	SetImageDescribeConfigProvider(func() (string, string) { return srv.URL, "vl" }) // env 已退役 B6
	defer SetImageDescribeConfigProvider(nil)

	desc, err := DescribeImage(context.Background(), "price.png", []byte("PNGDATA"))
	if err != nil || !strings.Contains(desc, "4999") {
		t.Errorf("描述: %q %v", desc, err)
	}
	if !IsImageExt("b.PNG") || IsImageExt("b.txt") {
		t.Error("IsImageExt 判定")
	}
}

func TestIsImageExt(t *testing.T) {
	for _, e := range []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"} {
		if !IsImageExt("f" + e) {
			t.Errorf("%s 应为图片", e)
		}
	}
	if IsImageExt(filepath.Join("x", "f.pdf")) {
		t.Error("pdf 不应为图片")
	}

}
