package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func newFilesGuardServer(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	// 模拟 uploads 目录里已存在的历史文件（落盘层上线前存入的 .html/.svg）
	mustWrite := func(rel, content string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("materials/evil.html", "<!DOCTYPE html><script>alert(1)</script>")
	mustWrite("materials/evil.htm", "<script>alert(1)</script>")
	mustWrite("materials/icon.svg", "<svg onload=alert(1)></svg>")
	mustWrite("materials/app.js", "alert(1)")
	mustWrite("materials/ok.png", "\x89PNG fake-bytes")
	mustWrite("materials/doc.pdf", "%PDF-1.4 fake")
	// baseDir 之外的机密文件（穿越探针）
	if err := os.WriteFile(filepath.Join(filepath.Dir(dir), "secret-for-guard-test.txt"), []byte("TOP-SECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(filepath.Join(filepath.Dir(dir), "secret-for-guard-test.txt"))
	})

	r := gin.New()
	r.GET("/files/*filepath", serveUploadsGuarded(dir))
	return r, dir
}

func TestFilesGuard_BlocksWebExecutableExts(t *testing.T) {
	r, _ := newFilesGuardServer(t)
	for _, p := range []string{
		"/files/materials/evil.html",
		"/files/materials/evil.htm",
		"/files/materials/icon.svg",
		"/files/materials/app.js",
	} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusForbidden {
			t.Errorf("危险扩展名应 403: %s -> %d body=%s", p, w.Code, w.Body.String())
		}
	}
}

func TestFilesGuard_ServesNormalFilesWithHardeningHeaders(t *testing.T) {
	r, _ := newFilesGuardServer(t)
	for _, p := range []string{"/files/materials/ok.png", "/files/materials/doc.pdf"} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, p, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("正常文件应 200: %s -> %d", p, w.Code)
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("%s 缺 nosniff 头: %q", p, got)
		}
		if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
			t.Errorf("%s 缺 CSP sandbox 头: %q", p, got)
		}
	}
}

func TestFilesGuard_BlocksTraversal(t *testing.T) {
	r, dir := newFilesGuardServer(t)
	secret := filepath.Join(filepath.Dir(dir), "secret-for-guard-test.txt")
	rel, err := filepath.Rel(dir, secret)
	if err != nil {
		t.Fatal(err)
	}
	// 直接构造含 ../ 的 URL 段（gin 路由匹配后 c.Param 原样携带）
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/x", nil)
	req.URL.Path = "/files/" + rel
	r.ServeHTTP(w, req)
	if w.Code == http.StatusOK {
		t.Fatalf("穿越读取成功（应被阻断）: %d body=%s", w.Code, w.Body.String())
	}
	if w.Code != http.StatusNotFound {
		t.Logf("穿越请求返回非 404（也接受）: %d", w.Code)
	}
}
