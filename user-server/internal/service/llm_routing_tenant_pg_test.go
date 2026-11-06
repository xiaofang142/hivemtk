// llm_routing_tenant_pg_test.go AV-04a 验收：ListModels 端到端（真库 + 真仓储 + 注入）。
//
// 与 llm_routing_test.go（fake 依赖）互补：本文件在 8217 专用测试库上，
// 以「多租户 on + 真实 LLMProviderRepository + 进程内 dispatcher」跑 ListModels，
// 验证注入链路端到端成立：A/B 各只见自己的 provider；dispatcher 里谁的名册
// 都不在（pg04a-ghost）与平台默认 provider 一律被过滤；读库失败 fail-closed。
//
// 数据清理：testutil.NewTestDB 每用例独立连接池 + TRUNCATE + 进程级临时库，
// 不外溢（同 repository/llm_provider_tenant_test.go）。
package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

const (
	pg04aTenantA = uint(921)
	pg04aTenantB = uint(922)
)

// pg04aSeed 播一行 provider（SystemScope 装配期写入通道 + 显式行自证 tenant_id）。
// DisplayName/BaseURL/Model 两租户刻意同值，只有 Name 不同——易混淆对照。
func pg04aSeed(t *testing.T, db *gorm.DB, tenantID uint, name, apiKey string) {
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
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(row).Error; err != nil {
		t.Fatalf("播种租户 %d 的 %s 失败: %v", tenantID, name, err)
	}
}

// pg04aSetup 建库、开多租户、播 A/B 行、装 dispatcher 与被测服务。
// dispatcher 额外挂一个 pg04a-ghost：内存有、谁的名册都没有——它若出现在
// 任何租户的 ListModels 里，说明过滤失效退回了全平台列表。
func pg04aSetup(t *testing.T) (*llm.Dispatcher, *LLMRoutingService, *gorm.DB) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：端到端租户作用域判不了（不 Skip，跳过等于没锁）")
	}
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})

	pg04aSeed(t, db, pg04aTenantA, "pg04a-a1", "sk-secret-a1")
	pg04aSeed(t, db, pg04aTenantA, "pg04a-a2", "")
	pg04aSeed(t, db, pg04aTenantB, "pg04a-b1", "sk-secret-b1")

	d := llm.NewDispatcher(llm.NewLLMService())
	d.AddProvider(llm.ProviderConfig{
		Name: "pg04a-a1", DisplayName: "Same Display", BaseURL: "https://shared.example.com/v1",
		Model: "same-model", APIKey: "sk-secret-a1", Enabled: true,
	})
	d.AddProvider(llm.ProviderConfig{
		Name: "pg04a-a2", DisplayName: "Same Display", BaseURL: "https://shared.example.com/v1",
		Model: "same-model", APIKey: "", Enabled: true,
	})
	d.AddProvider(llm.ProviderConfig{
		Name: "pg04a-b1", DisplayName: "Same Display", BaseURL: "https://shared.example.com/v1",
		Model: "same-model", APIKey: "sk-secret-b1", Enabled: true,
	})
	d.AddProvider(llm.ProviderConfig{
		Name: "pg04a-ghost", DisplayName: "Ghost", BaseURL: "https://ghost.example.com/v1",
		Model: "ghost-model", APIKey: "sk-ghost", Enabled: true,
	})

	svc := NewLLMRoutingService(d, repository.NewLLMProviderRepositoryWithDB(db))
	return d, svc, db
}

func TestLLMRouting_ListModels_PGMultiTenantEndToEnd(t *testing.T) {
	_, svc, db := pg04aSetup(t)

	t.Run("租户A只见自己", func(t *testing.T) {
		got := svc.ListModels(tenant.WithTenantID(context.Background(), pg04aTenantA))
		assertModelNames(t, got, "pg04a-a1", "pg04a-a2")
		for _, m := range got {
			if m.APIKey != "" {
				t.Errorf("provider %s 的 APIKey 泄漏进响应: %q", m.Name, m.APIKey)
			}
		}
		byName := map[string]LLMProviderInfo{}
		for _, m := range got {
			byName[m.Name] = m
		}
		if !byName["pg04a-a1"].APIKeySet || byName["pg04a-a2"].APIKeySet {
			t.Error("APIKeySet 布尔语义失真（配 Key 为 true / 未配为 false）")
		}
	})

	t.Run("租户B只见自己", func(t *testing.T) {
		got := svc.ListModels(tenant.WithTenantID(context.Background(), pg04aTenantB))
		assertModelNames(t, got, "pg04a-b1")
	})

	t.Run("读库失败fail-closed", func(t *testing.T) {
		sqlDB, err := db.DB()
		if err != nil {
			t.Fatal(err)
		}
		if cerr := sqlDB.Close(); cerr != nil {
			t.Fatalf("关闭连接池失败: %v", cerr)
		}
		got := svc.ListModels(tenant.WithTenantID(context.Background(), pg04aTenantA))
		if len(got) != 0 {
			t.Fatalf("读库失败应 fail-closed 空名单，实际 %v", namesOfModels(got))
		}
	})
}
