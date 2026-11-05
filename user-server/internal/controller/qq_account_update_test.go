package controller

// 契约：更新 QQ 账号时，前端回填空串的 secret 必须被当作「保留原值」，
// 不能在绑定层被 required 拦下。
//
// 背景（2026-11-01 API 全量回归实测）：账号 VO 从不回显明文密钥，前端编辑表单
// 把 app_secret 回填为空串；Update 的注释语义也是「为空保留原值」。但上一版直接
// 复用创建结构 qqAccountCreateReq（AppSecret binding:"required"），空串在
// ShouldBindJSON 阶段就 400「AppSecret required」——"保留原值"分支永远走不到，
// 表现为「编辑 QQ 账号只要不重填密钥就存不了」。

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestQQAccountUpdateReqAllowsEmptySecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 前端真实载荷：account_name/app_id 回填，两个 secret 为空串
	body := map[string]any{
		"account_name":     "APITestQQ-改",
		"app_id":           "apitest-qq-1031",
		"app_secret":       "",
		"webhook_secret":   "",
		"webhook_url":      "",
		"webhook_enabled":  false,
		"ai_agent_enabled": true,
		"status":           1,
	}
	raw, _ := json.Marshal(body)

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPut, "/api/qq/accounts/1", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")

	var req qqAccountUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		t.Fatalf("空 secret 的更新载荷必须能绑定（否则「空=保留原值」分支不可达）：%v", err)
	}
	if req.AppSecret != "" || req.WebhookSecret != "" {
		t.Fatalf("空 secret 应保持空串传递给「保留原值」判定：%+v", req)
	}
	if req.AccountName != "APITestQQ-改" {
		t.Fatalf("account_name 应正常绑定：%+v", req)
	}
}

// 创建口仍必须强制密钥必填——本测试防止有人把创建也放宽。
func TestQQAccountCreateReqStillRequiresSecret(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := []byte(`{"account_name":"x","app_id":"y"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/api/qq/accounts", bytes.NewReader(raw))
	c.Request.Header.Set("Content-Type", "application/json")

	var req qqAccountCreateReq
	if err := c.ShouldBindJSON(&req); err == nil {
		t.Fatal("创建口缺少 app_secret 必须报错（密钥不能在创建时留空）")
	}
}
