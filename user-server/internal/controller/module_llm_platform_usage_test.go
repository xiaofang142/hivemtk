package controller

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
)

// PLT-01 刀2 最小读面：GET /api/llm/platform-usage 的 HTTP 出口回归。
//
// platform_llm_usage_records 此前全仓零读端，本文件钉三件事：
//  1. 租户作用域走真实隔离 callback（on 模式）：租户 A 只见自己的明细，
//     租户 B 的行一概不出现 —— 这是"谁在什么场景用了多少"的归属底线；
//  2. 分页参数钳制后语义正确（total 稳定、页大小生效）；
//  3. 字段=既有记录列，成本三列原样透出（读侧不重算不回填）。
//
// 模块闸归属（llm-cloud）钉在 middleware/module_gate_test.go 的
// TestModuleOwnerOf 用例表里，此处不重复。

type puEnvelope struct {
	Code int `json:"code"`
	Data struct {
		List  []model.PlatformLLMUsageRecord `json:"list"`
		Total int64                          `json:"total"`
	} `json:"data"`
}

func puRouter(t *testing.T, tenantID uint) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// 复刻 TenantMiddleware 的最小语义：登录用户 → ctx 绑定租户。
	r.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(tenant.WithTenantID(c.Request.Context(), tenantID))
		c.Next()
	})
	ctrl := NewModuleMarketController()
	r.GET("/api/llm/platform-usage", ctrl.LLMPlatformUsage)
	return r
}

func puGet(t *testing.T, r *gin.Engine, url string) puEnvelope {
	t.Helper()
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, url, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s 应 200，实际 %d body=%s", url, w.Code, w.Body.String())
	}
	var env puEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("响应体不是约定信封: %v body=%s", err, w.Body.String())
	}
	return env
}

func TestLLMPlatformUsage_TenantScopedAndPaged(t *testing.T) {
	db := testutil.NewTestDB(t, &model.PlatformLLMUsageRecord{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})

	const (
		tenantA = uint(941)
		tenantB = uint(942)
	)
	// 租户 A 3 行 + 租户 B 1 行：走租户 ctx 播种（create callback 自动填 tenant_id）。
	for i, tc := range []struct {
		tenantID uint
		trace    string
	}{
		{tenantA, "pu-a-1"}, {tenantA, "pu-a-2"}, {tenantA, "pu-a-3"}, {tenantB, "pu-b-1"},
	} {
		row := &model.PlatformLLMUsageRecord{
			RequestID:        tc.trace,
			Provider:         "plat-pu",
			Model:            "m",
			PlanCode:         "plan-pu",
			Scenario:         "sop_reply",
			TraceID:          tc.trace,
			PromptTokens:     int64(1000 + i),
			CompletionTokens: 100,
			InputCostCents:   10,
			OutputCostCents:  7,
			TotalCostCents:   17,
			OccurredAt:       time.Now().Add(time.Duration(i) * time.Minute),
		}
		if err := db.WithContext(tenant.WithTenantID(context.Background(), tc.tenantID)).Create(row).Error; err != nil {
			t.Fatalf("播种 %s 失败: %v", tc.trace, err)
		}
	}

	// 租户 A：只见自己的 3 行，分页 total=3。
	envA := puGet(t, puRouter(t, tenantA), "/api/llm/platform-usage?page=1&page_size=2")
	if envA.Code != 0 {
		t.Fatalf("业务码应为 0，实际 %d", envA.Code)
	}
	if envA.Data.Total != 3 {
		t.Fatalf("租户 A total 应=3（不见租户 B 的行），实际 %d", envA.Data.Total)
	}
	if len(envA.Data.List) != 2 {
		t.Fatalf("page_size=2 应返回 2 行，实际 %d", len(envA.Data.List))
	}
	for _, row := range envA.Data.List {
		if row.TraceID == "pu-b-1" {
			t.Fatal("租户 A 的列表混入了租户 B 的明细（隔离失效）")
		}
		if row.TenantID != tenantA {
			t.Fatalf("返回行 tenant_id 应=%d，实际 %d", tenantA, row.TenantID)
		}
	}
	// 第二页拿剩下的 1 行；occurred_at DESC → 最新在前。
	envA2 := puGet(t, puRouter(t, tenantA), "/api/llm/platform-usage?page=2&page_size=2")
	if len(envA2.Data.List) != 1 || envA2.Data.List[0].TraceID != "pu-a-1" {
		t.Fatalf("第二页应剩 pu-a-1 一行，实际 %+v", envA2.Data.List)
	}
	if envA.Data.List[0].TraceID != "pu-a-3" {
		t.Fatalf("第一页首行应为最新一条 pu-a-3，实际 %s", envA.Data.List[0].TraceID)
	}

	// 租户 B：只见自己的 1 行。
	envB := puGet(t, puRouter(t, tenantB), "/api/llm/platform-usage")
	if envB.Data.Total != 1 || envB.Data.List[0].TraceID != "pu-b-1" {
		t.Fatalf("租户 B 应只见 pu-b-1 一行，实际 total=%d list=%+v", envB.Data.Total, envB.Data.List)
	}

	// 成本三列原样透出（读侧不重算）。
	if envB.Data.List[0].InputCostCents != 10 || envB.Data.List[0].OutputCostCents != 7 ||
		envB.Data.List[0].TotalCostCents != 17 {
		t.Fatalf("成本三列应原样透出 (10,7,17)，实际 (%d,%d,%d)",
			envB.Data.List[0].InputCostCents, envB.Data.List[0].OutputCostCents, envB.Data.List[0].TotalCostCents)
	}
}
