package controller

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// 公司关系 Controller 入口必须消费员工数据范围：未授权客户/公司返回 403，且响应体不含私有内容。
func TestCompanyController_RelationScope(t *testing.T) {
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })

	const tid = uint(6401)
	db := testutil.NewTestDB(t, &model.Company{}, &model.CustomerCompanyRelation{}, &model.Customer{})
	if db == nil {
		t.Fatal("真实 PostgreSQL 测试库不可达：公司 Controller 数据范围不能跳过")
	}
	ctx := tenant.WithTenantID(context.Background(), tid)
	sysCtx := tenant.WithSystemScope(context.Background())
	company := &model.Company{TenantID: tid, ID: 64001, Name: "Co", Industry: "A"}
	customer := &model.Customer{TenantID: tid, ID: "co-http-a", UnifiedID: "co-http-a", Name: "A", SourceAccountID: "acct-a", Industry: "A"}
	if err := db.WithContext(sysCtx).Create(company).Error; err != nil {
		t.Fatalf("准备公司失败: %v", err)
	}
	if err := db.WithContext(sysCtx).Create(customer).Error; err != nil {
		t.Fatalf("准备客户失败: %v", err)
	}

	svc := service.NewCompanyServiceDefault()
	// 用权限服务构造空授权 scope：Controller 从 ctx 解析 uid 后应全部拒绝。
	ctrl := NewCompanyController(svc)

	newCtx := func(method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
		c.Request = c.Request.WithContext(ctx)
		c.Set("user_id", uint(0)) // 无登录态 → 空授权
		return c, rec
	}

	// 空授权不能列出客户关系。
	c, rec := newCtx(http.MethodGet, "/customers/co-http-a/company-relations", "")
	c.Params = gin.Params{{Key: "id", Value: "co-http-a"}}
	ctrl.ListCustomerRelations(c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("空授权列关系应 403，实际 %d body=%s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("co-http-a")) {
		t.Fatalf("拒绝响应不应包含私有客户内容: %s", rec.Body.String())
	}

	// 空授权不能写客户关系。
	c, rec = newCtx(http.MethodPost, "/customers/co-http-a/company-relations", `{"company_id":64001}`)
	c.Params = gin.Params{{Key: "id", Value: "co-http-a"}}
	ctrl.UpsertCustomerRelation(c)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("空授权写关系应 403，实际 %d body=%s", rec.Code, rec.Body.String())
	}
}
