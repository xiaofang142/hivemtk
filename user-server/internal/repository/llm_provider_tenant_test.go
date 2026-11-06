// llm_provider_tenant_test.go AV-04a 验收：ListProviderNamesByTenant 的真实 SQL 作用域。
//
// 用 8217 专用测试库（testutil.NewTestDB）验证三件事：
//  1. A/B 两租户各有 provider 行（Name 全局互异、DisplayName/Model/BaseURL 故意
//     完全相同形成易混淆对照），查 A 只回 A 的 Name，查 B 只回 B 的——租户谓词
//     是本仓储自己的 SQL（本文件 tenant mode=off、隔离 callback 直通，
//     过滤只能来自 Where("tenant_id = ?")，作用域归属无可推诿）；
//  2. 同 (tenant_id, name) 第二次插入沿现唯一索引失败——tenancyidx.Normalize 把
//     模型标签 uniqueIndex(name) 原位复合化为 (tenant_id, name)，本测试不改索引；
//  3. 读库失败（连接池已关）原样上抛，调用方据此 fail-closed。
//
// 数据清理：testutil.NewTestDB 每用例独立连接池 + 用例开始时 TRUNCATE、进程级
// 临时库（user_db_test_proc*）随测试进程管理，测试数据不外溢。
package repository

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const (
	lpTenantA = uint(911)
	lpTenantB = uint(912)
)

// lpSeed 播一行 provider。三个易混淆列刻意全租户同值，只有 Name 不同——
// 若作用域失效，对照会以「同名同款但别家」的形态混进结果。
func lpSeed(t *testing.T, db *gorm.DB, tenantID uint, name, apiKey string) {
	t.Helper()
	row := &model.LLMProvider{
		TenantID:    tenantID,
		Name:        name,
		DisplayName: "Same Display",
		BaseURL:     "https://shared.example.com/v1",
		Model:       "same-model",
		APIKey:      apiKey,
		Enabled:     true,
	}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("播种租户 %d 的 %s 失败: %v", tenantID, name, err)
	}
}

func lpNameSet(t *testing.T, db *gorm.DB, tenantID uint) map[string]bool {
	t.Helper()
	names, err := NewLLMProviderRepositoryWithDB(db).ListProviderNamesByTenant(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ListProviderNamesByTenant(%d) 失败: %v", tenantID, err)
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if set[n] {
			t.Errorf("名单出现重复 Name %q", n)
		}
		set[n] = true
	}
	return set
}

func TestLLMProviderRepo_ListNamesTenantScoped(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：SQL 作用域判不了（不 Skip，跳过等于没锁）")
	}
	lpSeed(t, db, lpTenantA, "pg04a-a1", "sk-a1")
	lpSeed(t, db, lpTenantA, "pg04a-a2", "")
	lpSeed(t, db, lpTenantB, "pg04a-b1", "sk-b1")

	gotA := lpNameSet(t, db, lpTenantA)
	if len(gotA) != 2 || !gotA["pg04a-a1"] || !gotA["pg04a-a2"] {
		t.Fatalf("租户 A 应只见自己的两个 Name，实际 %v", gotA)
	}
	if gotA["pg04a-b1"] {
		t.Fatal("跨租户泄漏：租户 A 的名单里出现租户 B 的 pg04a-b1")
	}

	gotB := lpNameSet(t, db, lpTenantB)
	if len(gotB) != 1 || !gotB["pg04a-b1"] {
		t.Fatalf("租户 B 应只见 pg04a-b1，实际 %v", gotB)
	}
	if gotB["pg04a-a1"] || gotB["pg04a-a2"] {
		t.Fatal("跨租户泄漏：租户 B 的名单里出现租户 A 的行")
	}
}

// 同 (tenant_id, name) 第二次插入必须沿现唯一索引失败（23505）。
// 索引形状是 tenancyidx.Normalize 的 (tenant_id, name) 复合态，本测试不改索引。
func TestLLMProviderRepo_DuplicateNameViolatesUniqueConstraint(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：唯一约束判不了（不 Skip，跳过等于没锁）")
	}
	lpSeed(t, db, lpTenantA, "pg04a-dup", "sk-1")

	dup := &model.LLMProvider{
		TenantID: lpTenantA, Name: "pg04a-dup",
		DisplayName: "Same Display", BaseURL: "https://shared.example.com/v1", Model: "same-model",
	}
	if err := db.Create(dup).Error; err == nil {
		t.Fatal("同 (tenant_id, name) 第二次插入应沿现唯一约束失败，实际成功")
	}

	// 失败后不得留下半行：表内仍只有首次那行
	var count int64
	if err := db.Model(&model.LLMProvider{}).Where("name = ?", "pg04a-dup").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("唯一冲突后应仍只有 1 行，实际 %d", count)
	}
}

// 读库失败必须原样上抛（Service 层据此 fail-closed），不许吞错返回空名单。
func TestLLMProviderRepo_QueryErrorPropagates(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：错误语义判不了（不 Skip，跳过等于没锁）")
	}
	lpSeed(t, db, lpTenantA, "pg04a-a1", "sk-a1")

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if cerr := sqlDB.Close(); cerr != nil {
		t.Fatalf("关闭连接池失败: %v", cerr)
	}
	if _, err := NewLLMProviderRepositoryWithDB(db).ListProviderNamesByTenant(context.Background(), lpTenantA); err == nil {
		t.Fatal("连接池已关，查询应报错上抛（吞错返回空名单会把「读不了」伪装成「没有」）")
	}
}
