package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

// TestPWARootFiles_ServedNoCache 守护 Go 托管模式下的 PWA 五件套
// （2026-11-13 O 轮二次审核补修，勿回退）。
//
// 背景：setupEmbedStaticRoutes 此前只注册 /assets/*、favicon.ico、logo.png、/、
// /index.html、/chat/embed*，/sw.js、/version.json、/manifest.webmanifest、
// /workbox-*.js、/favicon.svg 全部落入 NoRoute ⇒ StaticController.NotFound 的
// SPA 回落返回 index.html（200 HTML）⇒ navigator.serviceWorker.register('/sw.js')
// 的 MIME 校验失败、main.js 拉 version.json 解析抛错 ⇒ SW 永不注册、版本自检
// 永不运行，整套 PWA 静默死亡。生产走 nginx 静态站不受影响，但
// DEPLOYMENT_GUIDE.md 声明 Go 托管受支持 ⇒ 本测试守补修不回归。
func TestPWARootFiles_ServedNoCache(t *testing.T) {
	dir := t.TempDir()
	files := []string{"sw.js", "version.json", "manifest.webmanifest", "favicon.svg", "workbox-9e85e06f.js", "index.html"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("pwa-test:"+f), 0o644); err != nil {
			t.Fatalf("写 %s 失败: %v", f, err)
		}
	}
	t.Setenv("USER_WEB_DIST", dir)
	// 指到不存在的路径，跳过 embed SDK 注册（与本测试无关）
	t.Setenv("EMBED_SDK_DIST", filepath.Join(dir, "no-embed-sdk"))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	setupEmbedStaticRoutes(r)

	// 1) 五件套必须 200 + no-cache + 原样文件内容（不是 index.html）
	pwa := []string{"/sw.js", "/version.json", "/manifest.webmanifest", "/favicon.svg", "/workbox-9e85e06f.js"}
	for _, p := range pwa {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d，want 200（body 前 80 字: %q）", p, w.Code, w.Body.String()[:min(80, w.Body.Len())])
			continue
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q，want no-cache", p, cc)
		}
		if got, want := w.Body.String(), "pwa-test:"+filepath.Base(p); got != want {
			t.Errorf("GET %s body = %q，want %q（若返回 index.html 说明又落回 SPA 回落）", p, got, want)
		}
	}

	// 2) HEAD 平价（http.ServeFile 语义）
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/version.json", nil))
	if w.Code != http.StatusOK {
		t.Errorf("HEAD /version.json = %d，want 200", w.Code)
	}

	// 3) 目录穿越不得逃逸出 dist（Clean+Join 后文件不存在 ⇒ 交回 SPA 回落）
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/workbox-../../etc/passwd.js", nil))
	if w.Code == http.StatusOK && w.Body.String() != "pwa-test:index.html" {
		t.Errorf("GET /workbox-../../etc/passwd.js 返回非预期内容: %q", w.Body.String()[:min(80, w.Body.Len())])
	}

	// 4) 非 PWA 路径仍走 SPA 回落（index.html），既有行为不回退
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/messageHub/list", nil))
	if w.Code != http.StatusOK || w.Body.String() != "pwa-test:index.html" {
		t.Errorf("GET /messageHub/list = %d body=%q，want 200 + index.html（SPA 回落）",
			w.Code, w.Body.String()[:min(80, w.Body.Len())])
	}
}
