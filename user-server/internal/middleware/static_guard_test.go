package middleware

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// staticGuardEngine 按生产装配方式（GET+HEAD 双注册）挂载 StaticGuard。
func staticGuardEngine(t *testing.T, root string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	guard := StaticGuard(root)
	r.GET("/assets/*filepath", guard)
	r.HEAD("/assets/*filepath", guard)
	return r
}

func staticGuardRequest(t *testing.T, r *gin.Engine, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, nil)
	r.ServeHTTP(w, req)
	return w
}

// TestStaticGuardServesBuildAssets 钉住 F-4-2：.js/.mjs/.css/.svg 是合法构建产物，
// 必须放行且带 nosniff + CSP sandbox；此前误用上传面的危险扩展名清单会把 .js
// 全量 403（dist/assets 实测 408 个 .js，生产 SPA 会整站白屏）。
func TestStaticGuardServesBuildAssets(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"index-abc.js": "console.log(1)",
		"chunk.mjs":    "export default 1",
		"style.css":    "body{}",
		"icon.svg":     "<svg/>",
		"font.woff2":   "font",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := staticGuardEngine(t, root)

	for _, name := range []string{"index-abc.js", "chunk.mjs", "style.css", "icon.svg", "font.woff2"} {
		w := staticGuardRequest(t, r, http.MethodGet, "/assets/"+name)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200（构建产物被误拦）body=%s", name, w.Code, w.Body.String())
		}
		if got := w.Header().Get("X-Content-Type-Options"); got != "nosniff" {
			t.Errorf("GET %s: X-Content-Type-Options=%q, want nosniff", name, got)
		}
		if got := w.Header().Get("Content-Security-Policy"); got != "sandbox" {
			t.Errorf("GET %s: CSP=%q, want sandbox", name, got)
		}
	}
}

// TestStaticGuardHeadParity 钉住 F-4-3：r.Static 原本 GET+HEAD 双注册，
// 换守卫后 HEAD 不应 404（CDN/监控的 HEAD 探测面）。
func TestStaticGuardHeadParity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "app.js"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := staticGuardEngine(t, root)
	w := staticGuardRequest(t, r, http.MethodHead, "/assets/app.js")
	if w.Code != http.StatusOK {
		t.Fatalf("HEAD /assets/app.js = %d, want 200", w.Code)
	}
}

// TestStaticGuardBlocksNonBuildTypes 构建目录不应出现服务端可执行/HTML 文档：
// 出现即视为目录被篡改，403。
func TestStaticGuardBlocksNonBuildTypes(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"shell.php", "page.html", "x.xhtml", "run.sh", ".htaccess"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := staticGuardEngine(t, root)
	for _, name := range []string{"shell.php", "page.html", "x.xhtml", "run.sh", ".htaccess"} {
		w := staticGuardRequest(t, r, http.MethodGet, "/assets/"+name)
		if w.Code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403", name, w.Code)
		}
		if strings.Contains(w.Body.String(), "x") && !strings.Contains(w.Body.String(), "not allowed") {
			t.Errorf("GET %s 泄露文件内容", name)
		}
	}
}
