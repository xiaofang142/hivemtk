package llm

import (
	"context"
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestLoadProvidersFromDB_TenantFiltered P1-5：system scope 全量 Find 会把其它租户的
// provider/route 装进 dispatcher 全局单键 map，造成跨租户 Key/路由泄漏。
// 这里钉死 WHERE tenant_id = 默认租户 的下限：tenant_id≠1 的行不进 map。
func TestLoadProvidersFromDB_TenantFiltered(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{}, &model.LLMRoutingRule{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 默认租户（1）的 provider：应被装载
	if err := database.WithContext(tenant.WithTenantID(context.Background(), tenant.DefaultTenantID)).
		Create(&model.LLMProvider{
			Name:    "default-tenant-provider",
			BaseURL: "https://api.example.com/v1",
			Model:   "m1",
			Enabled: true,
		}).Error; err != nil {
		t.Fatalf("seed 默认租户 provider 失败: %v", err)
	}
	// 其它租户（2）的 provider：不得进 dispatcher（含 APIKey，泄漏即跨租户 Key 泄漏）
	if err := database.WithContext(tenant.WithTenantID(context.Background(), 2)).
		Create(&model.LLMProvider{
			Name:    "other-tenant-provider",
			BaseURL: "https://other.example.com/v1",
			APIKey:  "sk-ot" + "her-te" + "nant-secret",
			Model:   "m2",
			Enabled: true,
		}).Error; err != nil {
		t.Fatalf("seed 其它租户 provider 失败: %v", err)
	}

	setAuditDB(database)
	defer setAuditDB(nil)

	d := newDispatcherBase(nil)
	if err := d.LoadProvidersFromDB(); err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}

	var sawDefault, sawOther bool
	for _, p := range d.GetAllProviders() {
		switch p.Name {
		case "default-tenant-provider":
			sawDefault = true
		case "other-tenant-provider":
			sawOther = true
		}
	}
	if !sawDefault {
		t.Error("默认租户（tenant_id=1）的 provider 应被装载")
	}
	if sawOther {
		t.Error("tenant_id≠1 的 provider 不应进入 dispatcher 全局 map（防跨租户 Key 泄漏）")
	}
}

// TestLoadRoutesFromDB_TenantFiltered 路由装载同样只装默认租户
func TestLoadRoutesFromDB_TenantFiltered(t *testing.T) {
	database := testutil.NewTestDB(t, &model.LLMProvider{}, &model.LLMRoutingRule{})
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	// 其它租户的同场景路由：不得覆盖默认租户的路由
	otherRoute := ScenarioRoute{Scenario: ScenarioLowCost, Provider: "other-tenant-provider", Version: 99}
	buf, err := json.Marshal(otherRoute)
	if err != nil {
		t.Fatalf("marshal route: %v", err)
	}
	if err := database.WithContext(tenant.WithTenantID(context.Background(), 2)).
		Create(&model.LLMRoutingRule{
			Scenario:  string(ScenarioLowCost),
			RouteJSON: string(buf),
			Version:   otherRoute.Version,
		}).Error; err != nil {
		t.Fatalf("seed 其它租户路由失败: %v", err)
	}

	setAuditDB(database)
	defer setAuditDB(nil)

	d := newDispatcherBase(nil)
	if err := d.LoadRoutesFromDB(); err != nil {
		t.Fatalf("LoadRoutesFromDB: %v", err)
	}
	if got := d.GetRoute(ScenarioLowCost); got != nil && got.Provider == "other-tenant-provider" {
		t.Error("tenant_id≠1 的路由不应覆盖 dispatcher 路由（防跨租户路由泄漏）")
	}
}
