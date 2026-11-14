package service

import (
	"context"
	"errors"
	"testing"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/storage"

	"gorm.io/gorm"
)

// TestTenantIsolationBusinessChains 业务链路级双租户对撞（审计实测轮）。
//
// 与单元测试的区别：不是 mock 仓储，而是**真实 PostgreSQL + TENANT_MODE=on +
// tenancyidx 复合唯一索引规范化**，让两个租户用**同一个业务键**
// （asset_id=duijiang_pkg / agent_code=duijiang_agent / username=duijiang_user）
// 各自走完一条完整业务链路，逐环节断言数据流向互不可见、互不影响：
//
//	生成/制造 → 读取 → 越权 ID 直查 → 发布门禁 → 织布生效与计数
//	→ 绑定/人设解析 → 停用(kill-switch)/恢复 → 归档/恢复
//	→ 评分（含复合唯一索引）→ 热启用 KV 租户隔离
//	→ 登录用户名歧义 → 文件签名租户钉死
func TestTenantIsolationBusinessChains(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.Tenant{}, &model.SystemUser{}, &model.AssetBundle{},
		&model.AssetBundleVersionLog{}, &model.AssetBundleRating{}, &model.AIAgent{},
	)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 双租户：id=1(A) / id=2(B)
	for _, tn := range []*model.Tenant{
		{ID: 1, Code: "duijiang_a", Name: "对撞租户A", Status: model.TenantStatusActive},
		{ID: 2, Code: "duijiang_b", Name: "对撞租户B", Status: model.TenantStatusActive},
	} {
		if err := db.Create(tn).Error; err != nil {
			t.Fatalf("建租户 %s: %v", tn.Code, err)
		}
	}
	ctxA := tenant.WithTenantID(context.Background(), 1)
	ctxB := tenant.WithTenantID(context.Background(), 2)

	repo := repository.NewAssetBundleRepository(db)
	bundleSvc := NewAssetBundleService(repo, repository.NewAssetBundleVersionLogRepository(db))
	bundleSvc.SetConfigKVRepo(repository.NewSystemConfigKVRepositoryWithDB(db))
	agentSvc := NewAIAgentServiceWithRepo(repository.NewAIAgentRepositoryWithDB(db))

	mkBundle := func(title, persona string) *model.AssetBundle {
		return &model.AssetBundle{
			AssetID: "duijiang_pkg", Title: title,
			Messages: []model.AssetBundleMessage{{Role: "system", Content: persona}},
		}
	}

	// ========== 链路1 生成/制造：同 asset_id 双租户并存（复合唯一索引） ==========
	pkgA := mkBundle("A链路包", "A人设：只服务A租户")
	if err := bundleSvc.CreateBundle(ctxA, pkgA); err != nil {
		t.Fatalf("[链路1] A 建包: %v", err)
	}
	pkgB := mkBundle("B链路包", "B人设：只服务B租户")
	if err := bundleSvc.CreateBundle(ctxB, pkgB); err != nil {
		t.Fatalf("[链路1] B 同 asset_id 建包应成功（复合唯一索引按租户分段）: %v", err)
	}

	// ========== 链路2 读取隔离：同键各取各的 ==========
	gotA, err := bundleSvc.GetBundleByAssetID(ctxA, "duijiang_pkg")
	if err != nil || gotA.Title != "A链路包" || gotA.ID != pkgA.ID {
		t.Fatalf("[链路2] A 按键读取应命中 A 行: got=%v err=%v", gotA, err)
	}
	gotB, err := bundleSvc.GetBundleByAssetID(ctxB, "duijiang_pkg")
	if err != nil || gotB.Title != "B链路包" || gotB.ID != pkgB.ID {
		t.Fatalf("[链路2] B 按键读取应命中 B 行: got=%v err=%v", gotB, err)
	}

	// ========== 链路3 越权 ID 直查/改/删 ==========
	if _, err := bundleSvc.GetBundle(ctxB, pkgA.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("[链路3] B 按数字 ID 读 A 包必须 not found, got %v", err)
	}
	if err := bundleSvc.DeleteBundle(ctxB, pkgA.ID); err == nil || !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("[链路3] B 删 A 包必须 not found, got %v", err)
	}
	// 数据未被破坏
	if _, err := bundleSvc.GetBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路3] 越权删除尝试后 A 包必须原样存在: %v", err)
	}

	// ========== 链路4 发布门禁 → 织布生效 → 计数各归各租户 ==========
	if err := bundleSvc.PublishBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路4] A 发布: %v", err)
	}
	if err := bundleSvc.PublishBundle(ctxB, pkgB.ID); err != nil {
		t.Fatalf("[链路4] B 发布: %v", err)
	}
	outA, err := bundleSvc.WeaveForRequest(ctxA, "duijiang_pkg", "你好", &WeaveInput{})
	if err != nil || len(outA) == 0 || outA[0].Content != "A人设：只服务A租户" {
		t.Fatalf("[链路4] A 织布应取 A 人设: out=%v err=%v", outA, err)
	}
	outB, err := bundleSvc.WeaveForRequest(ctxB, "duijiang_pkg", "你好", &WeaveInput{})
	if err != nil || len(outB) == 0 || outB[0].Content != "B人设：只服务B租户" {
		t.Fatalf("[链路4] B 织布应取 B 人设（数据流不串）: out=%v err=%v", outB, err)
	}
	gotA, _ = bundleSvc.GetBundle(ctxA, pkgA.ID)
	gotB, _ = bundleSvc.GetBundle(ctxB, pkgB.ID)
	if gotA.UseCount != 1 || gotB.UseCount != 1 {
		t.Fatalf("[链路4] use_count 应各计各的: A=%d B=%d", gotA.UseCount, gotB.UseCount)
	}

	// ========== 链路5 绑定 + 人设解析（同 agent_code 并存） ==========
	agentA := &model.AIAgent{AgentCode: "duijiang_agent", Name: "A智能体", Persona: "A原始人设", AssetBundleID: "duijiang_pkg"}
	if err := agentSvc.Create(ctxA, agentA); err != nil {
		t.Fatalf("[链路5] A 建智能体: %v", err)
	}
	agentB := &model.AIAgent{AgentCode: "duijiang_agent", Name: "B智能体", Persona: "B原始人设", AssetBundleID: "duijiang_pkg"}
	if err := agentSvc.Create(ctxB, agentB); err != nil {
		t.Fatalf("[链路5] B 同 agent_code 建智能体应成功: %v", err)
	}
	promptA, err := bundleSvc.ResolveSystemPrompt(ctxA, "duijiang_pkg")
	if err != nil || promptA != "A人设：只服务A租户" {
		t.Fatalf("[链路5] A 人设解析: %q err=%v", promptA, err)
	}
	promptB, err := bundleSvc.ResolveSystemPrompt(ctxB, "duijiang_pkg")
	if err != nil || promptB != "B人设：只服务B租户" {
		t.Fatalf("[链路5] B 人设解析不得拿到 A 的内容: %q err=%v", promptB, err)
	}

	// ========== 链路6 停用(kill-switch) 只停本租户 ==========
	if _, err := bundleSvc.DisableBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路6] A 停用: %v", err)
	}
	if _, err := bundleSvc.ResolveSystemPrompt(ctxA, "duijiang_pkg"); !errors.Is(err, ErrBundleDisabled) {
		t.Fatalf("[链路6] A 停用后解析必须 ErrBundleDisabled, got %v", err)
	}
	if _, err := bundleSvc.ResolveSystemPrompt(ctxB, "duijiang_pkg"); err != nil {
		t.Fatalf("[链路6] A 停用不得影响 B: %v", err)
	}
	if _, err := bundleSvc.EnableBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路6] A 恢复: %v", err)
	}
	if p, err := bundleSvc.ResolveSystemPrompt(ctxA, "duijiang_pkg"); err != nil || p != "A人设：只服务A租户" {
		t.Fatalf("[链路6] A 恢复后应重新生效: %q err=%v", p, err)
	}

	// ========== 链路7 归档隔离 + 发布恢复 ==========
	if err := bundleSvc.ArchiveBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路7] A 归档: %v", err)
	}
	if _, err := bundleSvc.ResolveSystemPrompt(ctxA, "duijiang_pkg"); !errors.Is(err, ErrBundleNotActive) {
		t.Fatalf("[链路7] A 归档后解析必须 ErrBundleNotActive, got %v", err)
	}
	if _, err := bundleSvc.ResolveSystemPrompt(ctxB, "duijiang_pkg"); err != nil {
		t.Fatalf("[链路7] A 归档不得影响 B: %v", err)
	}
	if err := bundleSvc.PublishBundle(ctxA, pkgA.ID); err != nil {
		t.Fatalf("[链路7] A 归档后重新发布: %v", err)
	}

	// ========== 链路8 评分（复合唯一索引 + 聚合按租户裁剪） ==========
	if err := bundleSvc.RateBundle(ctxA, "duijiang_pkg", 7, 5); err != nil {
		t.Fatalf("[链路8] A 用户评分: %v", err)
	}
	if err := bundleSvc.RateBundle(ctxB, "duijiang_pkg", 7, 1); err != nil {
		t.Fatalf("[链路8] B 同用户号评分应落 B 租户段: %v", err)
	}
	avgA, cntA, err := repo.RatingSummary(ctxA, "duijiang_pkg")
	if err != nil || cntA != 1 || avgA != 5 {
		t.Fatalf("[链路8] A 聚合应只有自己那票: avg=%v cnt=%d err=%v", avgA, cntA, err)
	}
	avgB, cntB, err := repo.RatingSummary(ctxB, "duijiang_pkg")
	if err != nil || cntB != 1 || avgB != 1 {
		t.Fatalf("[链路8] B 聚合应只有自己那票: avg=%v cnt=%d err=%v", avgB, cntB, err)
	}

	// ========== 链路9 热启用列表按租户分桶 ==========
	// A 已 enable（链路6）；B 从未 enable 自己的包
	enabledA, err := bundleSvc.GetEnabledBundles(ctxA)
	if err != nil || len(enabledA) != 1 || enabledA[0].AssetID != "duijiang_pkg" {
		t.Fatalf("[链路9] A 热启用列表应含自己的包: n=%d err=%v", len(enabledA), err)
	}
	enabledB, err := bundleSvc.GetEnabledBundles(ctxB)
	if err != nil || len(enabledB) != 0 {
		t.Fatalf("[链路9] B 热启用列表不得看到 A 的启用状态: n=%d err=%v", len(enabledB), err)
	}

	// ========== 链路10 登录用户名跨租户歧义 ==========
	for _, u := range []*model.SystemUser{
		{TenantID: 1, Username: "duijiang_user", Password: "x", Enabled: true, Status: 1},
		{TenantID: 2, Username: "duijiang_user", Password: "x", Enabled: true, Status: 1},
	} {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("[链路10] 建同名用户 tenant=%d: %v", u.TenantID, err)
		}
	}
	authSvc := NewAuthService()
	_, err = authSvc.resolveLoginIdentity(context.Background(), &LoginRequest{Username: "duijiang_user", Password: "whatever"})
	var biz *bizerr.BizError
	if err == nil || !errors.As(err, &biz) || biz.Code != bizerr.CodeTenantAmbiguousUsername {
		t.Fatalf("[链路10] 跨租户同名登录必须报 4108 歧义（不猜租户）, got %v", err)
	}
	// 带租户短码定向登录：各自解析到各自租户的用户行
	idA, err := authSvc.resolveLoginIdentity(ctxA, &LoginRequest{Username: "duijiang_user", Password: "whatever", TenantCode: "duijiang_a"})
	if err != nil || idA.TenantID != 1 {
		t.Fatalf("[链路10] 短码定向登录应解析到租户1用户: tenant=%d err=%v", idA.TenantID, err)
	}
	idB, err := authSvc.resolveLoginIdentity(ctxB, &LoginRequest{Username: "duijiang_user", Password: "whatever", TenantCode: "duijiang_b"})
	if err != nil || idB.TenantID != 2 {
		t.Fatalf("[链路10] 短码定向登录应解析到租户2用户: tenant=%d err=%v", idB.TenantID, err)
	}

	// ========== 链路11 文件签名租户钉死 ==========
	_, sigA, ok := storage.SignStoredPath("tenant_1/covers/duijiang.jpg")
	if !ok {
		t.Fatal("[链路11] A 租户路径应能签出凭证")
	}
	// 合法请求：签名解出的租户必须是 1（凭证即租户归属）
	tid, path, err := storage.VerifyFileURL("covers/duijiang.jpg", sigA, time.Now())
	if err != nil || tid != 1 || path != "covers/duijiang.jpg" {
		t.Fatalf("[链路11] 合法签名应解出租户1: tid=%d path=%q err=%v", tid, path, err)
	}
	// 路径篡改：A 的签名贴到 B 的路径形态 → P 全等比对拒绝
	if _, _, err := storage.VerifyFileURL("tenant_2/covers/duijiang.jpg", sigA, time.Now()); err == nil {
		t.Fatal("[链路11] A 签名贴 B 路径形态必须被拒")
	}
	// 换文件名同样拒绝
	if _, _, err := storage.VerifyFileURL("covers/other.jpg", sigA, time.Now()); err == nil {
		t.Fatal("[链路11] 签名换文件名必须被拒")
	}
	// B 自己签自己的文件，归属是 2
	_, sigB, ok := storage.SignStoredPath("tenant_2/covers/duijiang_b.jpg")
	if !ok {
		t.Fatal("[链路11] B 租户路径应能签出凭证")
	}
	tidB, _, err := storage.VerifyFileURL("covers/duijiang_b.jpg", sigB, time.Now())
	if err != nil || tidB != 2 {
		t.Fatalf("[链路11] B 签名应解出租户2: tid=%d err=%v", tidB, err)
	}
}

// TestTenantIsolationSandboxAndStore 非管理员 sandbox 降级 + 商店归属校验（审计实测轮）。
func TestTenantIsolationSandboxAndStore(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.Tenant{}, &model.AssetBundle{}, &model.AssetBundleVersionLog{},
		&model.AssetListing{}, &model.AssetVersion{},
	)
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })
	if err := db.Create(&model.Tenant{ID: 1, Code: "sb_a", Name: "沙箱租户", Status: model.TenantStatusActive}).Error; err != nil {
		t.Fatalf("建租户: %v", err)
	}
	ctxA := tenant.WithTenantID(context.Background(), 1)
	ctxB := tenant.WithTenantID(context.Background(), 2)

	repo := repository.NewAssetBundleRepository(db)
	bundleSvc := NewAssetBundleService(repo, repository.NewAssetBundleVersionLogRepository(db))
	bundleSvc.SetConfigKVRepo(repository.NewSystemConfigKVRepositoryWithDB(db))

	// ---- 非管理员 sandbox:true 读 draft 包 → 仍被状态闸拦截（sandbox 降级生效） ----
	draft := &model.AssetBundle{AssetID: "sb_pkg", Title: "草稿包",
		Messages: []model.AssetBundleMessage{{Role: "system", Content: "草稿提示词"}}}
	if err := bundleSvc.CreateBundle(ctxA, draft); err != nil {
		t.Fatalf("建 draft 包: %v", err)
	}
	// 拒绝形态允许两种（同进程全量跑时，前一用例的热启用 KV 索引可能已被
	// 本租户采用 → 命中白名单闸；单跑则命中状态闸）——两种都证明普通用户
	// 的 sandbox 无法读取 draft 包。
	if _, err := bundleSvc.WeaveForRequest(ctxA, "sb_pkg", "q", &WeaveInput{Sandbox: true, AdminBypass: false}); err == nil ||
		(!errors.Is(err, ErrBundleNotActive) && !errors.Is(err, ErrBundleNotHotEnabled)) {
		t.Fatalf("非管理员 sandbox 织 draft 包必须被拒绝, got %v", err)
	}
	// 管理员 sandbox 才放行（Playground 调试台口径）
	if _, err := bundleSvc.WeaveForRequest(ctxA, "sb_pkg", "q", &WeaveInput{Sandbox: true, AdminBypass: true}); err != nil {
		t.Fatalf("管理员 sandbox 调试 draft 包应放行: %v", err)
	}

	// ---- 商店提审/流转归属校验（二轮修复实测） ----
	storeSvc := NewAssetStoreService()
	l, err := storeSvc.CreateListing(ctxA, 1, 100, "sb_listing_001", "extension", "沙箱上架", "通用", "s", []byte(`{"collect":"none"}`), nil)
	if err != nil {
		t.Fatalf("建 listing: %v", err)
	}
	// 非本人提审 → 拒绝
	if err := storeSvc.SubmitListing(ctxA, 1, l.ID, 999); err == nil {
		t.Fatal("非本人提审必须被拒绝")
	}
	// 非本人流转（白名单内的 submitted 也不行）→ 拒绝
	if err := storeSvc.MerchantTransitionListing(ctxA, 1, l.ID, model.AssetListingSubmitted, 999); err == nil {
		t.Fatal("非本人流转必须被拒绝")
	}
	// 越权审核类流转（→ published）即使本人也不允许 → 拒绝
	if err := storeSvc.MerchantTransitionListing(ctxA, 1, l.ID, model.AssetListingPublished, 100); err == nil {
		t.Fatal("贡献者侧不允许审核类流转（published）")
	}
	// 本人白名单流转（draft → submitted）→ 允许
	if err := storeSvc.MerchantTransitionListing(ctxA, 1, l.ID, model.AssetListingSubmitted, 100); err != nil {
		t.Fatalf("本人白名单流转应放行: %v", err)
	}
	// 租户隔离：B ctx 读不到 A 的 listing（GetListing 走租户 callback）
	if err := storeSvc.MerchantTransitionListing(ctxB, 2, l.ID, model.AssetListingDraft, 100); err == nil {
		t.Fatal("B 租户不得操作 A 的 listing")
	}
}
