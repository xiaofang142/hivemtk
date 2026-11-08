package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/platform"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// 上架链路（POST /api/asset-bundle/:id/submit-platform）的 :id 语义回归。
//
// 坑：前端 Playground 传的是 bundle.id（数字主键），而 SubmitToPlatform 需要的是
// asset_id（业务 slug）。历史上 handler 把 :id 原样当 asset_id 用，
// 于是数字主键永远查不到资产包，上架链路 100% 报 "record not found"。
// 兄弟路由（publish/archive/enable/disable）一律把 :id 当数字主键解析，
// 只有上架这一条不解析 —— 这就是"同一组路由两种语义"的证据。
//
// 本测试把判据钉死：数字主键必须先归一化成 asset_id 再去查，
// 且最终落到平台侧的 changelog 里必须是 asset_id（不是数字主键）。

// fakeBundleRepo 只实现本测试需要的读方法，其余方法 panic 兜底，
// 保证"测试通过"不能靠"根本没查库"蒙混过关。
type fakeBundleRepo struct {
	repository.AssetBundleRepository

	bundles   map[int64]*model.AssetBundle
	byAssetID map[string]*model.AssetBundle

	findByIDCalls     []int64
	findByAssetIDKeys []string
}

func newFakeBundleRepo() *fakeBundleRepo {
	return &fakeBundleRepo{
		bundles:   map[int64]*model.AssetBundle{},
		byAssetID: map[string]*model.AssetBundle{},
	}
}

func (f *fakeBundleRepo) FindByID(_ context.Context, id int64) (*model.AssetBundle, error) {
	f.findByIDCalls = append(f.findByIDCalls, id)
	b, ok := f.bundles[id]
	if !ok {
		return nil, fmt.Errorf("record not found")
	}
	return b, nil
}

func (f *fakeBundleRepo) FindByAssetID(_ context.Context, assetID string) (*model.AssetBundle, error) {
	f.findByAssetIDKeys = append(f.findByAssetIDKeys, assetID)
	b, ok := f.byAssetID[assetID]
	if !ok {
		return nil, fmt.Errorf("record not found")
	}
	return b, nil
}

// platformStub 记录平台侧收到的"创建资产"请求，用于断言上架用的到底是哪套标识。
type platformStub struct {
	srv *httptest.Server

	createdBodies []map[string]any
}

func newPlatformStub(t *testing.T) *platformStub {
	t.Helper()
	ps := &platformStub{}
	ps.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/contributor-api/v1/auth/login", "/contributor-api/v1/auth/register":
			_, _ = fmt.Fprintf(w, `{"code":200,"msg":"ok","data":{"token":"ct"}}`)
		case "/contributor-api/v1/assets":
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			ps.createdBodies = append(ps.createdBodies, m)
			_, _ = w.Write([]byte(`{"code":200,"msg":"创建成功","data":{"id":77}}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"msg":"提交成功"}`))
		}
	}))
	t.Cleanup(ps.srv.Close)
	return ps
}

// withSubmitPlatformEnv 把平台侧依赖（开关 / 地址 / 密钥 / 贡献者身份）全部摆好，
// 让测试只考察"handler 怎么解析 :id"这一件事。
func withSubmitPlatformEnv(t *testing.T, apiURL string) {
	t.Helper()
	t.Setenv("PLATFORM_ENABLED", "true")

	orig := config.PlatformCfg
	t.Cleanup(func() { config.PlatformCfg = orig })
	config.PlatformCfg = &config.PlatformConfig{APIURL: apiURL, Secret: "s3cr3t"}

	platform.SetMerchantKeyForTest("mk-apitest")
}

func newSubmitPlatformRouter(svc *service.AssetBundleService) http.Handler {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ctrl := NewAssetBundleController(svc)
	r.POST("/api/asset-bundle/:id/submit-platform", ctrl.SubmitToPlatform)
	return r
}

// TestSubmitToPlatformResolvesNumericBundleIDToAssetID 数字主键必须归一化成 asset_id。
//
// 判据分三截：
//  1. handler 按数字主键查了一次 FindByID（而不是把 "160" 直接当 asset_id 去查）
//  2. 传给平台的 changelog 里落的是 asset_id
//  3. HTTP 200 + platform_asset_id
func TestSubmitToPlatformResolvesNumericBundleIDToAssetID(t *testing.T) {
	repo := newFakeBundleRepo()
	repo.bundles[160] = &model.AssetBundle{
		ID: 160, AssetID: "starter-bundle", Title: "起步资产包", Version: "1.0.0",
		Messages: []model.AssetBundleMessage{{Role: "system", Content: "欢迎语"}},
	}
	repo.byAssetID["starter-bundle"] = repo.bundles[160]

	ps := newPlatformStub(t)
	withSubmitPlatformEnv(t, ps.srv.URL)

	svc := service.NewAssetBundleService(repo, nil)
	router := newSubmitPlatformRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/asset-bundle/160/submit-platform", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("数字主键上架应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if len(repo.findByIDCalls) != 1 || repo.findByIDCalls[0] != 160 {
		t.Errorf("应按数字主键查一次 FindByID(160)，实际=%v", repo.findByIDCalls)
	}
	if len(repo.findByAssetIDKeys) != 1 || repo.findByAssetIDKeys[0] != "starter-bundle" {
		t.Errorf("应按 asset_id 查一次 FindByAssetID(starter-bundle)，实际=%v", repo.findByAssetIDKeys)
	}
	if len(ps.createdBodies) != 1 {
		t.Fatalf("应向平台创建 1 次资产，实际 %d 次", len(ps.createdBodies))
	}
	changelog, _ := ps.createdBodies[0]["changelog"].(string)
	if !strings.Contains(changelog, "starter-bundle") {
		t.Errorf("平台侧 changelog 必须带 asset_id(starter-bundle)，实际=%q", changelog)
	}
	if strings.Contains(changelog, "160") {
		t.Errorf("平台侧 changelog 不得回落成数字主键，实际=%q", changelog)
	}
	if !strings.Contains(w.Body.String(), "platform_asset_id") {
		t.Errorf("响应应回 platform_asset_id，body=%s", w.Body.String())
	}
}

// TestSubmitToPlatformAcceptsAssetIDSlug 历史调用方若直接传 asset_id slug，
// 仍要能走通（不能因为这次修复把老用法打断）。
func TestSubmitToPlatformAcceptsAssetIDSlug(t *testing.T) {
	repo := newFakeBundleRepo()
	repo.byAssetID["starter-bundle"] = &model.AssetBundle{
		ID: 160, AssetID: "starter-bundle", Title: "起步资产包", Version: "1.0.0",
		Messages: []model.AssetBundleMessage{{Role: "system", Content: "欢迎语"}},
	}

	ps := newPlatformStub(t)
	withSubmitPlatformEnv(t, ps.srv.URL)

	svc := service.NewAssetBundleService(repo, nil)
	router := newSubmitPlatformRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/asset-bundle/starter-bundle/submit-platform", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("asset_id slug 上架应返回 200，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if len(repo.findByIDCalls) != 0 {
		t.Errorf("slug 入参不应触发 FindByID，实际=%v", repo.findByIDCalls)
	}
	if len(repo.findByAssetIDKeys) != 1 || repo.findByAssetIDKeys[0] != "starter-bundle" {
		t.Errorf("slug 入参应直接按 asset_id 查，实际=%v", repo.findByAssetIDKeys)
	}
}

// TestSubmitToPlatformUnknownNumericIDReportsNotFound 数字主键查不到时必须报"不存在"，
// 而不是把 "999" 当 asset_id 去撞库、最后吐一条含糊的 record not found。
func TestSubmitToPlatformUnknownNumericIDReportsNotFound(t *testing.T) {
	repo := newFakeBundleRepo()

	ps := newPlatformStub(t)
	withSubmitPlatformEnv(t, ps.srv.URL)

	svc := service.NewAssetBundleService(repo, nil)
	router := newSubmitPlatformRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/asset-bundle/999/submit-platform", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Fatalf("不存在的资产包不得返回 200，body=%s", w.Body.String())
	}
	if len(repo.findByAssetIDKeys) != 0 {
		t.Errorf("主键查不到时不得再拿数字主键当 asset_id 撞库，实际=%v", repo.findByAssetIDKeys)
	}
}

// TestSubmitToPlatformUnconfiguredReports503 平台集成缺配置时必须报 503「暂时不可用」，
// 不能落进 ErrorFromDB 的兜底 500 —— 那是"我们服务坏了"的语义，会诱导客户端重试。
func TestSubmitToPlatformUnconfiguredReports503(t *testing.T) {
	repo := newFakeBundleRepo()
	repo.byAssetID["starter-bundle"] = &model.AssetBundle{
		ID: 160, AssetID: "starter-bundle", Title: "起步资产包", Version: "1.0.0",
		Messages: []model.AssetBundleMessage{{Role: "system", Content: "欢迎语"}},
	}

	ps := newPlatformStub(t)
	t.Setenv("PLATFORM_ENABLED", "true")
	orig := config.PlatformCfg
	oldMK := platform.GetMerchantKey()
	t.Cleanup(func() {
		config.PlatformCfg = orig
		platform.SetMerchantKeyForTest(oldMK)
		platform.ResetContributorTokenForTest()
	})
	// 地址有、密钥空：contributorIdentity 会在发请求前就判定未配置。
	config.PlatformCfg = &config.PlatformConfig{APIURL: ps.srv.URL}
	platform.SetMerchantKeyForTest("mk-apitest")
	platform.ResetContributorTokenForTest()

	svc := service.NewAssetBundleService(repo, nil)
	router := newSubmitPlatformRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/asset-bundle/starter-bundle/submit-platform", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("平台未配置应返回 503，实际 %d，body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "平台集成未配置") {
		t.Errorf("响应应说明平台集成未配置，body=%s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "SERVICE_UNAVAILABLE_6003") {
		t.Errorf("响应业务码应为 SERVICE_UNAVAILABLE_6003，body=%s", w.Body.String())
	}
	if len(ps.createdBodies) != 0 {
		t.Errorf("未配置时不应向平台发出创建请求，实际 %d 次", len(ps.createdBodies))
	}
}

// TestRespondSubmitPlatformErrorMapping 错误分流必须按哨兵错误走，不能被包一层就失效。
func TestRespondSubmitPlatformErrorMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name     string
		err      error
		wantHTTP int
		wantCode string
	}{
		{"platform-not-configured", fmt.Errorf("wrap: %w", platform.ErrPlatformNotConfigured), http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE_6003"},
		{"contributor-unauthorized", fmt.Errorf("wrap: %w", platform.ErrContributorUnauthorized), http.StatusBadGateway, "UPSTREAM_FAILURE_5005"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			respondSubmitPlatformError(ctx, c.err)
			if w.Code != c.wantHTTP {
				t.Fatalf("HTTP=%d, want %d, body=%s", w.Code, c.wantHTTP, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), c.wantCode) {
				t.Errorf("body 缺业务码 %s: %s", c.wantCode, w.Body.String())
			}
		})
	}
}
