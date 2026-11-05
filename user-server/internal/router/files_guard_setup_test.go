package router

// /files 在**真装配**（Setup）下的凭证契约（SaaS 审计 A1 / A12，2026-10-23）。
//
// 为什么这组用例必须走 Setup 而不是直接调 middleware.FilesGuard：
// 上一轮 P0-2 的修法（首段 tenant_{id} 必须等于请求 ctx 里的租户）在**隔离环境里测得出绿、
// 在生产路径上一步都走不到** —— /files 是 Setup 里最先注册的路由，而 gin 的 r.Use 只对之后
// 登记的路由生效，所以它的 handler 链上没有 TenantMiddleware，请求 ctx 里永远没有租户绑定，
// 那段 if 恒走"无绑定 ⇒ 放行"。上一轮的四条用例之所以绿，是因为它在进程里手动
// req.WithContext(tenant.WithTenantID(...)) 注入了一个生产上不存在的状态。
//
// 所以本文件的判据只有一种形状：**用真实例、真路由表、真 HTTP 请求**去问"能不能读到"。
// 这也是"判定存在，但没有任何一条生产路径能走到它"这个失效模式的固定解药：
// 只要一条判据能靠 ctx 注入变绿，就必须再有一条用例证明这个 ctx 在生产上不会被填。

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/storage"

	"github.com/gin-gonic/gin"
)

// setupFilesServer 真装配一个实例，并把上传根指向临时目录。
func setupFilesServer(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("STORAGE_LOCAL_BASE_DIR", dir)
	t.Setenv("USER_WEB_DIST", t.TempDir())

	full := filepath.Join(dir, "tenant_5", "x.pdf")
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("tenant-5-secret"), 0o640); err != nil {
		t.Fatal(err)
	}

	database := testutil.NewTestDB(t)
	prev := dbutil.GetDB()
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(prev) })

	gin.SetMode(gin.TestMode)
	e := gin.New()
	Setup(e, database)
	return e, dir
}

// setMode 切换部署形态；同时装好签名器（on 模式的凭证来源）。
func setMode(t *testing.T, on bool) {
	t.Helper()
	if on {
		tenant.SetMode(tenant.ModeOn)
		if err := storage.ConfigureFileURLSigner("router-setup-test-secret-32+chars!!", 0); err != nil {
			t.Fatalf("装配签名器失败: %v", err)
		}
		t.Cleanup(func() {
			tenant.SetMode(tenant.ModeOff)
			storage.ResetFileURLSignerForTest()
		})
		return
	}
	tenant.SetMode(tenant.ModeOff)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
}

func setupRequest(t *testing.T, e *gin.Engine, url, bearer string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	w := httptest.NewRecorder()
	e.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// ① 真装配下，on 模式的 /files 在**没有签名**时一律 404 —— 带别的租户的 JWT 也一样。
//
// 这条是上一轮失效模式的正面钉子：如果哪天有人再把"ctx 租户 vs 路径租户"写成判据，
// 这里会立刻红（因为真链上拿不到 ctx 租户，那种判据会退化成放行）。
func TestSetupFiles_OnModeRequiresSignature(t *testing.T) {
	e, _ := setupFilesServer(t)
	setMode(t, true)

	foreignJWT := a16Token(t, 6)
	for _, tc := range []struct {
		url    string
		bearer string
		why    string
	}{
		{"/files/tenant_5/x.pdf", "", "无凭证"},
		{"/files/tenant_5/x.pdf", foreignJWT, "外租户 JWT 也不是凭证"},
		{"/files/x.pdf", "", "剥掉租户段的明文路径同样要凭证"},
	} {
		if code, body := setupRequest(t, e, tc.url, tc.bearer); code != http.StatusNotFound {
			t.Errorf("%s（%s）应 404，实际 %d body=%.120q", tc.url, tc.why, code, body)
		}
	}
}

// ② 真装配下，on 模式的 /files 认签名：签了就 200，且内容是签名指定租户目录里的那个文件。
func TestSetupFiles_OnModeSignedURLOk(t *testing.T) {
	e, _ := setupFilesServer(t)
	setMode(t, true)

	url := storage.NewLocalDriver("", "").PublicURL("tenant_5/x.pdf")
	if !strings.Contains(url, "?s=") {
		t.Fatalf("没签出 URL：%q", url)
	}
	if strings.Contains(url, "tenant_5") {
		t.Fatalf("签名 URL 里仍有租户段：%q（A12）", url)
	}
	code, body := setupRequest(t, e, url, "")
	if code != http.StatusOK {
		t.Fatalf("带有效签名应 200，实际 %d body=%.120q", code, body)
	}
	if body != "tenant-5-secret" {
		t.Fatalf("读到的不是租户 5 的那个文件：%q", body)
	}
}

// ③ 过期即 404（凭证的时效是活体判据，不是文档承诺）。
func TestSetupFiles_ExpiredSignatureIsNotFound(t *testing.T) {
	e, _ := setupFilesServer(t)
	setMode(t, true)
	if err := storage.ConfigureFileURLSigner("router-setup-test-secret-32+chars!!", time.Millisecond); err != nil {
		t.Fatalf("装配短 TTL 签名器失败: %v", err)
	}
	url := storage.NewLocalDriver("", "").PublicURL("tenant_5/x.pdf")
	// 过期戳是 Unix 秒 ⇒ 必须整秒跨过去才验得出来。
	time.Sleep(1100 * time.Millisecond)
	if code, _ := setupRequest(t, e, url, ""); code != http.StatusNotFound {
		t.Errorf("过期签名应 404，实际 %d", code)
	}
}

// ④ off 模式：真装配下与历史逐字节一致（含 tenant_* 明文路径，离线单机零回归）。
func TestSetupFiles_OffModeUnchanged(t *testing.T) {
	e, _ := setupFilesServer(t)
	setMode(t, false)

	for _, url := range []string{"/files/tenant_5/x.pdf"} {
		if code, body := setupRequest(t, e, url, a16Token(t, 6)); code != http.StatusOK || body != "tenant-5-secret" {
			t.Errorf("off 模式 %s 应 200 且返回文件内容，实际 %d body=%.120q", url, code, body)
		}
	}
}
