package router

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"hivemtk-user/internal/storage"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"

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

// 上传写入方（controller/upload.go）、邮件外发侧与 /files 托管侧三处必须落在同一个磁盘根上。
// 本例把文件铺在 storage.LocalSource() 解析出的那棵树下，再断 /files 取得到它。
//
// 限定：这条腿在"托管侧自己抄一份解析、但抄的键恰好等价"时不会红（那属可读性问题，
// 由 internal/storage/attachment_source_test.go 的 TestFilesRouteReadsEnvThroughLocalSource
// 静态锁负责）。它红的是"两处根本不是同一棵树"这一类 —— 那才是历史上真实发生过的形状。
func TestRegisterFilesRouteServesTheUploadWritersRoot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", dir)
	// 退役键（第三十八轮）：就算环境里还留着它，也不许把托管根带走。
	t.Setenv("UPLOAD_DIR", filepath.Join(dir, "legacy-must-not-be-read"))

	writersRoot, _, _ := storage.LocalSource()
	if writersRoot != dir {
		t.Fatalf("夹具未成立：上传侧解析出的根 = %q，期望 %q", writersRoot, dir)
	}
	full := filepath.Join(writersRoot, "materials", "served-by-same-root.txt")
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("ok"), 0o640); err != nil {
		t.Fatal(err)
	}

	r := gin.New()
	RegisterFilesRoute(r)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files/materials/served-by-same-root.txt", nil))
	if w.Code != http.StatusOK {
		t.Errorf("/files 取不到上传方写进同一棵根的文件: code=%d body=%s", w.Code, w.Body.String())
	}
}

// TestSetupRegistersFilesRoute 装配腿：RegisterFilesRoute 存在 ≠ 它在 Setup 里被调用。
//
// 本仓已有先例说明这一格不是杞人忧天 —— reach registry 的 22 处装配里 21 处的 getter
// 从来没有活消费方（第三十七轮）。摘掉 Setup 里那一行，/files 整条腿静默消失、
// 上传功能照常、只有公开链接 404。
func TestSetupRegistersFilesRoute(t *testing.T) {
	database := testutil.NewTestDB(t)
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	dir := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", dir)
	full := filepath.Join(dir, "materials", "assembled-by-setup.txt")
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("ok"), 0o640); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()
	Setup(r, database)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/files/materials/assembled-by-setup.txt", nil))
	if w.Code != http.StatusOK {
		t.Errorf("Setup 未挂载 /files（或挂的不是同一个根）: code=%d body=%s", w.Code, w.Body.String())
	}
}
