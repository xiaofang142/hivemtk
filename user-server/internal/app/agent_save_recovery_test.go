package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func TestAgentSaveRecoveryIdentity(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIAgent{})
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	ctx := tenant.WithTenantID(context.Background(), 1)
	repo := repository.NewAIAgentRepositoryWithDB(database)
	svc := service.NewAIAgentServiceWithRepo(repo)
	code := "recover/exact ?"
	agent := &model.AIAgent{AgentCode: code, Name: "恢复身份", Persona: "本地测试", AgentMode: "passive", Status: 1}
	if err := svc.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(func(c *gin.Context) {
		requestContext := c.Request.Context()
		if c.GetHeader("X-Test-Tenant") != "missing" {
			id := uint(1)
			if c.GetHeader("X-Test-Tenant") == "other" {
				id = 2
			}
			requestContext = tenant.WithTenantID(requestContext, id)
		}
		c.Request = c.Request.WithContext(requestContext)
		c.Next()
	})
	controller.NewAIAgentControllerWithService(svc).RegisterRoutes(router.Group("/api"))
	request := func(code, mode string, status int) any {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/ai-agents/by-code?agent_code="+url.QueryEscape(code), nil)
		req.Header.Set("X-Test-Tenant", mode)
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != status {
			t.Fatalf("status=%d want=%d body=%s", out.Code, status, out.Body.String())
		}
		var envelope map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if status == http.StatusOK && envelope["code"] != float64(0) {
			t.Fatalf("不是成功响应: %v", envelope)
		}
		return envelope["data"]
	}
	got, ok := request(code, "own", http.StatusOK).(map[string]any)
	if !ok || got["id"] != float64(agent.ID) || got["agent_code"] != code {
		t.Fatalf("错误身份: %v", got)
	}
	for _, missing := range []string{"recover", code + "-other"} {
		if got := request(missing, "own", http.StatusOK); got != nil {
			t.Fatalf("必须精确匹配: %v", got)
		}
	}
	if got := request(code, "other", http.StatusOK); got != nil {
		t.Fatalf("泄露其它租户: %v", got)
	}
	// 缺失可信租户是装配错误；隔离 callback 必须拒绝，而不能伪装为未找到。
	request(code, "missing", http.StatusInternalServerError)
	request("", "own", http.StatusBadRequest)
	duplicate := &model.AIAgent{AgentCode: code, Name: "不能重复", Persona: "测试", AgentMode: "active"}
	if err := repo.Create(ctx, duplicate); err == nil {
		t.Fatal("唯一编码不能生成第二条主记录")
	}
	t.Run("editor payload survives create update and readback", func(t *testing.T) {
		payload := map[string]any{
			"agent_code": "editor-round-trip", "name": "保存回读", "description": "", "avatar": "",
			"agent_type": "sales", "agent_mode": "passive", "persona": "本地测试", "system_prompt": "", "greeting": "",
			"asset_bundle_id": "", "internal_language": "zh", "target_language": "",
			"rag_product_ids": []any{}, "faq_entry_ids": []any{}, "sop_template_ids": []any{}, "sop_ids": []any{}, "script_library_ids": []any{},
			"llm_model": "gpt-4o-mini", "temperature": 0.7, "max_tokens": float64(2048), "top_p": 0.9,
			"frequency_penalty": 0.5, "presence_penalty": 0.5, "enable_rag": true, "enable_script_match": true,
			"enable_humanize_polish": true, "enable_content_audit": true, "enable_playbook": true,
			"rag_top_k": float64(3), "confidence_threshold": 0.7, "max_ai_consecutive": float64(5), "status": float64(1),
		}
		call := func(method, path string, data any) map[string]any {
			t.Helper()
			body, err := json.Marshal(data)
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(method, path, bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			out := httptest.NewRecorder()
			router.ServeHTTP(out, req)
			if out.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			var envelope struct {
				Code int            `json:"code"`
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(out.Body.Bytes(), &envelope); err != nil || envelope.Code != 0 || envelope.Data == nil {
				t.Fatalf("无效回读: %v %s", err, out.Body.String())
			}
			return envelope.Data
		}
		created := call(http.MethodPost, "/api/ai-agents", payload)
		path := fmt.Sprintf("/api/ai-agents/%.0f", created["id"])
		assertPayload := func(record map[string]any) {
			t.Helper()
			for key, expected := range payload {
				actual := record[key]
				if values, ok := expected.([]any); ok && len(values) == 0 && actual == nil {
					continue // 编辑器按空集合归一化 PostgreSQL 空数组。
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Errorf("字段 %s 回读=%#v 提交=%#v", key, actual, expected)
				}
			}
		}
		assertPayload(call(http.MethodGet, path, nil))
		payload["name"] = "更新后回读"
		call(http.MethodPut, path, payload)
		assertPayload(call(http.MethodGet, path, nil))
	})
	const callback = "agent-save-recovery-read-failure"
	if err := database.Callback().Query().Before("gorm:query").Register(callback, func(tx *gorm.DB) { tx.AddError(errors.New("受控查询故障")) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Callback().Query().Remove(callback) })
	request(code, "own", http.StatusInternalServerError)
}
