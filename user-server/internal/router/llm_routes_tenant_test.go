// llm_routes_tenant_test.go AV-04a 验收：两个真实 LLM 列表路由入口走认证上下文的租户作用域。
//
// 主入口 GET /api/llm/models（setupLLMRoutingRoutes 构造点）与别名入口
// GET /api/llm-routing/models（setupFrontendAliases 构造点）各自经 JWT
// 铸造的租户 A / 租户 B 令牌请求，断言返回的 provider 名集合**相等**于
// 该租户名册：别家 provider、谁名册都不在的 ghost、平台默认 provider
// （deepseek）一概不出现，APIKey 明文绝不出现在响应体。
//
// 复用 cross_tenant_scan_test.go 的 a16Engine/a16Token/a16Get（同包，
// 与生产同一张路由表），不另造第二套路由装配。
package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/app"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	llmRtTenantA = uint(931)
	llmRtTenantB = uint(932)
)

type llmRtEnvelope struct {
	Code int `json:"code"`
	Data []struct {
		Name      string `json:"name"`
		APIKey    string `json:"api_key"`
		APIKeySet bool   `json:"api_key_set"`
	} `json:"data"`
}

// llmRtSeed 走租户 ctx 播种（create callback 自动填 tenant_id，闸三同款前提）。
func llmRtSeed(t *testing.T, db *gorm.DB, tenantID uint, name, apiKey string) {
	t.Helper()
	row := &model.LLMProvider{
		Name:        name,
		DisplayName: "Same Display",
		BaseURL:     "https://shared.example.com/v1",
		Model:       "same-model",
		APIKey:      apiKey,
		Enabled:     true,
	}
	if err := db.WithContext(tenant.WithTenantID(context.Background(), tenantID)).Create(row).Error; err != nil {
		t.Fatalf("播种租户 %d 的 %s 失败: %v", tenantID, name, err)
	}
}

func llmRtFetch(t *testing.T, engine *gin.Engine, url, token string) llmRtEnvelope {
	t.Helper()
	code, body := a16Get(t, engine, url, token)
	if code != http.StatusOK {
		t.Fatalf("GET %s 应 200，实际 %d body=%s", url, code, body)
	}
	var env llmRtEnvelope
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("响应体不是约定信封: %v body=%s", err, body)
	}
	return env
}

func llmRtNameSet(env llmRtEnvelope) map[string]bool {
	set := make(map[string]bool, len(env.Data))
	for _, m := range env.Data {
		set[m.Name] = true
	}
	return set
}

func TestLLMRoutes_TenantScopedThroughAuthContext(t *testing.T) {
	db := testutil.NewTestDB(t, &model.LLMProvider{})
	if db == nil {
		t.Fatal("测试库不可达：路由入口租户作用域判不了（不 Skip，跳过等于没锁）")
	}
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	llmRtSeed(t, db, llmRtTenantA, "pg04a-a1", "sk-secret-a1")
	llmRtSeed(t, db, llmRtTenantA, "pg04a-a2", "")
	llmRtSeed(t, db, llmRtTenantB, "pg04a-b1", "sk-secret-b1")

	// 进程内 dispatcher 全平台共享：挂 A/B 行 + ghost（谁名册都不在）。
	// NewDispatcher 自带的平台默认 provider（deepseek 等）不在任何名册，
	// 与 ghost 一样必须被过滤——否则就是退回全平台列表。
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
	prevDispatcher := app.GetGlobalDispatcher()
	app.SetGlobalDispatcher(d)
	t.Cleanup(func() { app.SetGlobalDispatcher(prevDispatcher) })

	engine := a16Engine(t, db)
	tokA := a16Token(t, llmRtTenantA)
	tokB := a16Token(t, llmRtTenantB)

	t.Run("主入口/api/llm/models_租户A集合相等", func(t *testing.T) {
		env := llmRtFetch(t, engine, "/api/llm/models", tokA)
		got := llmRtNameSet(env)
		want := map[string]bool{"pg04a-a1": true, "pg04a-a2": true}
		if len(got) != len(want) {
			t.Fatalf("租户 A 名单集合应恰为 %v，实际 %v", want, got)
		}
		for n := range want {
			if !got[n] {
				t.Fatalf("租户 A 名单缺 %q，实际 %v", n, got)
			}
		}
		for _, leak := range []string{"pg04a-b1", "pg04a-ghost", "deepseek"} {
			if got[leak] {
				t.Errorf("租户 A 名单泄漏 %q（别家/无主/平台默认 provider）", leak)
			}
		}
		for _, m := range env.Data {
			if m.APIKey != "" {
				t.Errorf("provider %s 的 api_key 出现在响应体: %q", m.Name, m.APIKey)
			}
		}
	})

	t.Run("别名入口/api/llm-routing/models_租户B集合相等", func(t *testing.T) {
		env := llmRtFetch(t, engine, "/api/llm-routing/models", tokB)
		got := llmRtNameSet(env)
		if len(got) != 1 || !got["pg04a-b1"] {
			t.Fatalf("别名入口租户 B 名单应恰为 {pg04a-b1}，实际 %v", got)
		}
		for _, leak := range []string{"pg04a-a1", "pg04a-a2", "pg04a-ghost", "deepseek"} {
			if got[leak] {
				t.Errorf("别名入口租户 B 名单泄漏 %q", leak)
			}
		}
		for _, m := range env.Data {
			if m.APIKey != "" {
				t.Errorf("别名入口 provider %s 的 api_key 出现在响应体: %q", m.Name, m.APIKey)
			}
		}
	})
}
