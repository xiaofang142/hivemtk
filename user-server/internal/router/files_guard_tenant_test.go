package router

// /files 的访问凭证（SaaS 审计 A1 / A12，2026-10-23 定稿）。
//
// 上一轮（P0-2）的修法是"路径首段 tenant_{id} 必须等于请求 ctx 里的租户"，配了四态用例。
// 本轮实测发现那条判据**在生产上走不到**：/files 是 Setup 里最先注册的路由，而 gin 的
// r.Use 只对之后登记的路由生效 ⇒ 它的链上只有一个 handler，没有 JWTAuth/TenantMiddleware
// ⇒ 请求 ctx 里永远没有租户绑定 ⇒ 那段 if 恒走"无绑定 ⇒ 放行"。实测（改前）：
// 带「租户 6」JWT 请求 /files/tenant_5/x.pdf ⇒ 200；不带 token ⇒ 200。上一轮的用例之所以绿，
// 是因为它在进程里手动注入了一个生产路径上不存在的 ctx 状态。
//
// 现在的判据只剩一条，且它是**唯一**的：on 模式下 /files 必须有签名凭证（?s=），
// 验不过一律 404。租户归属由签名承载（不写在 URL 里 = A12），跨租户访问在结构上不可表达
// —— 签名自指它自己的租户目录，路径改一个字节就验不过。
//
// 本文件钉五件事：
//  1. on 模式：无签名 ⇒ 404（**带不带租户 JWT 都一样**，钉死"ctx 不是凭证"）
//  2. on 模式：有效签名 ⇒ 200，且读到的正是签名指定的租户目录里的那个文件
//  3. on 模式：篡改路径段 / 篡改签名字节 / 过期 ⇒ 404
//  4. on 模式：URL 里没有租户段（A12），租户 5 的签名读不到租户 6 的文件
//  5. off 模式：逐字节等价历史行为（含 tenant_5/ 明文路径照旧可读）

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/storage"

	"github.com/gin-gonic/gin"
)

func newTenantFilesGuardServer(t *testing.T) (*gin.Engine, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	mustWrite := func(rel string) {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("content:"+rel), 0o640); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("tenant_5/materials/2026/10/a.pdf")
	mustWrite("tenant_6/materials/2026/10/b.pdf")
	mustWrite("materials/legacy.pdf") // 存量根域文件（系统级上传）
	r := gin.New()
	r.GET("/files/*filepath", middleware.FilesGuard(dir))
	return r, dir
}

func doFilesRequest(t *testing.T, r *gin.Engine, path string, tenantID uint) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if tenantID != 0 {
		req = req.WithContext(tenant.WithTenantID(req.Context(), tenantID))
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.String()
}

// signedURLFor 用与守卫同一套签名器给"某个租户的存储路径"签一条 URL。
func signedURLFor(t *testing.T, storagePath string) string {
	t.Helper()
	url := storage.NewLocalDriver(t.TempDir(), "").PublicURL(storagePath)
	if !strings.Contains(url, "?s=") {
		t.Fatalf("没签出 URL（%q）：签名器未装配或路径里没有租户段", url)
	}
	return url
}

// ① on 模式：无签名 ⇒ 404。ctx 里有租户也不行 —— ctx 不是凭证。
func TestFilesGuard_OnModeUnsignedIsNotFound(t *testing.T) {
	r, _ := newTenantFilesGuardServer(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	for _, tc := range []struct {
		path     string
		tenantID uint
		why      string
	}{
		{"/files/tenant_5/materials/2026/10/a.pdf", 0, "无 token、无签名"},
		{"/files/tenant_5/materials/2026/10/a.pdf", 5, "有本租户 ctx，但 ctx 不是凭证"},
		{"/files/tenant_5/materials/2026/10/a.pdf", 6, "跨租户且无签名"},
		{"/files/materials/2026/10/a.pdf", 5, "去掉租户段的明文路径（文件在 tenant 树里，读不到）"},
		{"/files/materials/legacy.pdf", 5, "存量根域文件在 on 模式下同样要凭证"},
	} {
		if code, _ := doFilesRequest(t, r, tc.path, tc.tenantID); code != http.StatusNotFound {
			t.Errorf("%s（%s）应 404，实际 %d", tc.path, tc.why, code)
		}
	}
}

// ② on 模式：有效签名 ⇒ 200，且内容来自签名指定的租户目录。
func TestFilesGuard_OnModeSignedURLOk(t *testing.T) {
	r, _ := newTenantFilesGuardServer(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	url := signedURLFor(t, "tenant_5/materials/2026/10/a.pdf")
	if strings.Contains(url, "tenant_5") {
		t.Fatalf("签名 URL 里仍有租户段：%q（A12）", url)
	}
	code, body := doFilesRequest(t, r, url, 0) // 无 ctx 租户：访客 <img> 的真实形态
	if code != http.StatusOK {
		t.Fatalf("带有效签名应 200，实际 %d", code)
	}
	if !strings.Contains(body, "tenant_5/materials/2026/10/a.pdf") {
		t.Fatalf("读到的不是签名指定的那个文件：%q", body)
	}
}

// ③ 篡改 / 过期 ⇒ 404（凭证的三条失效通道各有断言）。
func TestFilesGuard_SignedURLTamperedOrExpiredIsNotFound(t *testing.T) {
	r, _ := newTenantFilesGuardServer(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	url := signedURLFor(t, "tenant_5/materials/2026/10/a.pdf")
	head, sig, _ := strings.Cut(url, "?s=")

	// 篡改路径段
	if code, _ := doFilesRequest(t, r, "/files/materials/2026/10/b.pdf?s="+sig, 0); code != http.StatusNotFound {
		t.Errorf("换掉路径段应 404，实际 %d", code)
	}
	// 篡改签名字节
	bad := []byte(sig)
	bad[len(bad)/2] ^= 0x01
	if code, _ := doFilesRequest(t, r, head+"?s="+string(bad), 0); code != http.StatusNotFound {
		t.Errorf("改一个签名字节应 404，实际 %d", code)
	}
	// 过期：另装一个 1ms 的签名器签一条，等到过期再验。
	// 签名器是包级状态，用完必须装回去，否则同包后续用例会因为 TTL 变成"1ms"而随机红。
	prevSecret, prevTTL := "router-test-secret-32+chars-long-enough!!", time.Second
	t.Cleanup(func() {
		if err := storage.ConfigureFileURLSigner(prevSecret, 0); err != nil {
			t.Logf("恢复签名器 TTL 失败: %v", err)
		}
	})
	if err := storage.ConfigureFileURLSigner(prevSecret, prevTTL); err != nil {
		t.Fatalf("装短 TTL 签名器失败: %v", err)
	}
	shortLived := storage.NewLocalDriver(t.TempDir(), "").PublicURL("tenant_5/materials/2026/10/a.pdf")
	// 过期戳是 Unix **秒**（URL 里的时间不需要毫秒精度），所以这里必须跨过一个整秒才验得出来。
	time.Sleep(1100 * time.Millisecond)
	if code, _ := doFilesRequest(t, r, shortLived, 0); code != http.StatusNotFound {
		t.Errorf("过期签名应 404，实际 %d", code)
	}
}

// ④ 签名自指租户：租户 5 的签名只能读到 tenant_5 树里的文件。
func TestFilesGuard_SignedURLIsBoundToItsTenantDir(t *testing.T) {
	r, dir := newTenantFilesGuardServer(t)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 同一个相对路径在租户 5 与租户 6 下各有一个文件，内容不同
	if err := os.MkdirAll(filepath.Join(dir, "tenant_6/materials/2026/10"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tenant_6/materials/2026/10/a.pdf"), []byte("content:tenant_6"), 0o640); err != nil {
		t.Fatal(err)
	}
	code, body := doFilesRequest(t, r, signedURLFor(t, "tenant_5/materials/2026/10/a.pdf"), 0)
	if code != http.StatusOK || !strings.Contains(body, "tenant_5/materials/2026/10/a.pdf") {
		t.Fatalf("租户 5 的签名应读到 tenant_5 的文件，实际 %d body=%q", code, body)
	}
	code, body = doFilesRequest(t, r, signedURLFor(t, "tenant_6/materials/2026/10/a.pdf"), 0)
	if code != http.StatusOK || !strings.Contains(body, "content:tenant_6") {
		t.Fatalf("租户 6 的签名应读到 tenant_6 的文件，实际 %d body=%q", code, body)
	}
}

// ⑤ off 模式：逐字节等价（离线单机零回归）——包括tenant_* 明文路径照旧可读。
func TestFilesGuard_OffModeUnchanged(t *testing.T) {
	r, _ := newTenantFilesGuardServer(t)
	tenant.SetMode(tenant.ModeOff)

	for _, tc := range []struct {
		path     string
		tenantID uint
	}{
		{"/files/tenant_5/materials/2026/10/a.pdf", 6}, // off 不判租户
		{"/files/tenant_6/materials/2026/10/b.pdf", 0},
		{"/files/materials/legacy.pdf", 5},
	} {
		if code, _ := doFilesRequest(t, r, tc.path, tc.tenantID); code != http.StatusOK {
			t.Errorf("off 模式 %s 应 200（与历史一致），实际 %d", tc.path, code)
		}
	}
}

// 危险扩展名在两种模式下都要 403（与租户判定正交互，别被签名改造带掉）。
func TestFilesGuard_DangerousExtStillForbidden(t *testing.T) {
	r, dir := newTenantFilesGuardServer(t)
	if err := os.WriteFile(filepath.Join(dir, "materials", "x.html"), []byte("<script>"), 0o640); err != nil {
		t.Fatal(err)
	}

	tenant.SetMode(tenant.ModeOff)
	if code, _ := doFilesRequest(t, r, "/files/materials/x.html", 0); code != http.StatusForbidden {
		t.Errorf("off 模式危险扩展名应 403，实际 %d", code)
	}

	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	// on 模式下同样 403（扩展名判据在凭证之前，不要用 404 掩盖它）
	if code, _ := doFilesRequest(t, r, "/files/materials/x.html", 0); code != http.StatusForbidden {
		t.Errorf("on 模式危险扩展名应 403，实际 %d", code)
	}
}
