package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/config"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// PLT-01 刀3 镜像读路径判据（真库）：
//  1. llm_wallets 唯一索引实态 = (tenant_id, scope) 复合（二审候选缺陷核查：
//     模型 Scope 单列 uniqueIndex 标签经 tenancyidx.Normalize 原位复合化，
//     与 ApplyMirror 的 ON CONFLICT (tenant_id, scope) 匹配 —— 本文件用
//     pg_indexes 真库查证钉死，若形状漂移本用例先红）；
//  2. 多租户负例：各租户各一行，互不踩、不 42P10；
//  3. Wallet() 离线分支读镜像：可解释余额 + synced_at 透出；镜像表无
//     Low/Expired 列，离线档不冒充判定（两字段恒零值）。

// TestLLMWalletsUniqueIndexIsTenantComposite 前置核查：唯一索引实态真库查证。
func TestLLMWalletsUniqueIndexIsTenantComposite(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMWallet{})
	// NewTestDB 装配链：AutoMigrate（按模型标签建单列 uniqueIndex）→
	// tenancyidx.Normalize（把租户表的唯一索引原位重建为 (tenant_id, 原列)）。
	// 这里直接查库内实态，不信任任何一侧的"应该"。
	rows := []struct {
		Indexname string `gorm:"column:indexname"`
		Indexdef  string `gorm:"column:indexdef"`
	}{}
	if err := database.WithContext(context.Background()).Raw(
		`SELECT indexname, indexdef FROM pg_indexes
		 WHERE tablename = 'llm_wallets' AND indexdef LIKE 'CREATE UNIQUE %'
		   AND indexname NOT LIKE '%\_pkey'`, // 主键唯一性与 ON CONFLICT (tenant_id, scope) 无关，排除
	).Scan(&rows).Error; err != nil {
		t.Fatalf("查 pg_indexes 失败: %v", err)
	}
	if len(rows) == 0 {
		t.Fatal("llm_wallets 上没有任何业务唯一索引（ApplyMirror 的 ON CONFLICT 将 42P10）")
	}
	for _, r := range rows {
		if !containsTenantIDLeading(r.Indexdef) {
			t.Fatalf("唯一索引必须复合含 tenant_id（否则 on 模式第二租户写入 42P10 或互踩），实际: %s", r.Indexdef)
		}
	}
}

func containsTenantIDLeading(def string) bool {
	// 形如 CREATE UNIQUE INDEX xxx ON llm_wallets (tenant_id, scope)
	open := -1
	for i := 0; i < len(def); i++ {
		if def[i] == '(' {
			open = i
			break
		}
	}
	if open < 0 {
		return false
	}
	rest := def[open+1:]
	const want = "tenant_id"
	if len(rest) < len(want) || rest[:len(want)] != want {
		return false
	}
	return true
}

// TestApplyMirror_MultiTenantEachRowPerTenant 多租户负例补格：每个租户一行镜像，
// upsert 幂等只更新本租户行，第二租户写入不报 42P10、不互踩。
func TestApplyMirror_MultiTenantEachRowPerTenant(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMWallet{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})

	const (
		tenantA = uint(951)
		tenantB = uint(952)
	)
	repoA := repository.NewLLMWalletRepositoryWithDB(database)
	ctxA := tenant.WithTenantID(context.Background(), tenantA)
	ctxB := tenant.WithTenantID(context.Background(), tenantB)

	if err := repoA.ApplyMirror(ctxA, 1000, 2000); err != nil {
		t.Fatalf("租户 A 落镜像失败: %v", err)
	}
	// 第二租户写入：索引若只有单列 scope unique，这里要么 42P10 要么把 A 的行覆盖掉。
	if err := repoA.ApplyMirror(ctxB, 3000, 4000); err != nil {
		t.Fatalf("租户 B 落镜像失败（唯一索引与 ON CONFLICT 目标不匹配的典型红点）: %v", err)
	}

	var count int64
	sysCtx := tenant.WithSystemScope(context.Background())
	if err := database.WithContext(sysCtx).Model(&model.LLMWallet{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("两租户应各一行（共 2 行），实际 %d", count)
	}

	mA, err := repoA.GetMirror(ctxA)
	if err != nil || mA == nil {
		t.Fatalf("租户 A 读镜像: %v", err)
	}
	if mA.InputTokensRemaining != 1000 || mA.OutputTokensRemaining != 2000 {
		t.Fatalf("租户 A 应读到自己的余额 (1000,2000)，实际 (%d,%d)",
			mA.InputTokensRemaining, mA.OutputTokensRemaining)
	}
	mB, err := repoA.GetMirror(ctxB)
	if err != nil || mB == nil {
		t.Fatalf("租户 B 读镜像: %v", err)
	}
	if mB.InputTokensRemaining != 3000 || mB.OutputTokensRemaining != 4000 {
		t.Fatalf("租户 B 应读到自己的余额 (3000,4000)，实际 (%d,%d)",
			mB.InputTokensRemaining, mB.OutputTokensRemaining)
	}

	// 幂等重放：同租户再落镜像 = upsert 更新，不新增行、不覆盖别租户。
	if err := repoA.ApplyMirror(ctxA, 1111, 2222); err != nil {
		t.Fatalf("租户 A 重放失败: %v", err)
	}
	if err := database.WithContext(sysCtx).Model(&model.LLMWallet{}).Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("重放后仍应 2 行，实际 %d", count)
	}
	mA2, _ := repoA.GetMirror(ctxA)
	if mA2.InputTokensRemaining != 1111 {
		t.Fatalf("重放应更新本租户余额，实际 %d", mA2.InputTokensRemaining)
	}
}

// TestWalletOfflineReadsMirror 离线分支：读镜像给可解释余额 + synced_at；
// 无镜像 = 空钱包；Low/Expired 恒零值（不冒充平台判定）。
func TestWalletOfflineReadsMirror(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMWallet{})
	t.Setenv("PLATFORM_ENABLED", "false")
	svc := NewLLMWalletServiceWithRepo(repository.NewLLMWalletRepositoryWithDB(database))
	ctx := context.Background()

	// 无镜像：exists=false（从未在线查过余额），不报错。
	v, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatalf("离线无镜像不应报错: %v", err)
	}
	if v.Exists {
		t.Fatal("无镜像时 exists 应为 false")
	}

	// 落镜像后离线读：余额 + synced_at 可解释。
	if err := NewLLMWalletServiceWithRepo(repository.NewLLMWalletRepositoryWithDB(database)).
		repo.ApplyMirror(ctx, 700, 300); err != nil {
		t.Fatalf("落镜像失败: %v", err)
	}
	v2, err := svc.Wallet(ctx)
	if err != nil {
		t.Fatalf("离线读镜像不应报错: %v", err)
	}
	if !v2.Exists {
		t.Fatal("有镜像时 exists 应为 true")
	}
	if v2.PlatformEnabled {
		t.Fatal("离线档 PlatformEnabled 应为 false")
	}
	if v2.InputRemaining != 700 || v2.OutputRemaining != 300 {
		t.Fatalf("离线余额应来自镜像 (700,300)，实际 (%d,%d)",
			v2.InputRemaining, v2.OutputRemaining)
	}
	if v2.UpdatedAt == "" {
		t.Fatal("离线档必须透出 synced_at（否则用户无法判断余额的新鲜度）")
	}
	if _, err := time.Parse(time.RFC3339, v2.UpdatedAt); err != nil {
		t.Fatalf("synced_at 应为 RFC3339，实际 %q: %v", v2.UpdatedAt, err)
	}
	// 镜像表无 Low/Expired 列：离线档不冒充判定。
	if v2.Low || v2.Expired {
		t.Fatal("离线档不得冒充 Low/Expired 判定（镜像表没有这两列，恒零值）")
	}
	_ = config.PlatformEnabled // 引用防 unused（env 已由 t.Setenv 钉住关态）
}
