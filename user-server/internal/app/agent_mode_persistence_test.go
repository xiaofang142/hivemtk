package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/aiagent/agent/lifecycle"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 真实管理 HTTP -> PostgreSQL -> 上下文 -> 生产运行入口；只有生命周期出口计数。
func TestAgentModePersistence(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIAgent{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 1)
	repo := repository.NewAIAgentRepositoryWithDB(database)
	management := service.NewAIAgentServiceWithRepo(repo)
	// 生产装配的管理端与运行时各持有一个 service，不能借用同实例失效缓存。
	runtimeLoader := service.NewAIAgentServiceWithRepo(repo)
	runtime, passive, active := newTestRuntime(&lifecycleContextLoader{service: runtimeLoader})
	previous := GetAgentLifecycleRuntime()
	SetAgentLifecycleRuntime(runtime)
	t.Cleanup(func() { SetAgentLifecycleRuntime(previous) })
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Request = c.Request.WithContext(ctx); c.Next() })
	group := router.Group("/api")
	controller.NewAIAgentControllerWithService(management).RegisterRoutes(group)
	registerLifecycleTestRoute(group)
	request := func(t *testing.T, method, path string, data any, wantStatus int) map[string]any {
		t.Helper()
		body, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		router.ServeHTTP(out, req)
		if out.Code != wantStatus {
			t.Fatalf("%s %s status=%d want=%d body=%s", method, path, out.Code, wantStatus, out.Body.String())
		}
		var envelope map[string]any
		if err := json.Unmarshal(out.Body.Bytes(), &envelope); err != nil {
			t.Fatal(err)
		}
		if wantStatus == http.StatusOK && envelope["code"] != float64(0) {
			t.Fatalf("成功响应: %v", envelope)
		}
		if wantStatus != http.StatusOK && envelope["code"] == float64(0) {
			t.Fatalf("拒绝不能返回成功: %v", envelope)
		}
		dataMap, _ := envelope["data"].(map[string]any)
		return dataMap
	}
	for _, initial := range []string{"active", "passive"} {
		t.Run(initial, func(t *testing.T) {
			created := request(t, http.MethodPost, "/api/ai-agents", map[string]any{"agent_code": "mode-" + initial, "name": initial, "persona": "受控人设", "agent_mode": initial}, http.StatusOK)
			id := uint(created["id"].(float64))
			path := fmt.Sprintf("/api/ai-agents/%d", id)
			check := func(mode string) {
				t.Helper()
				if got := request(t, http.MethodGet, path, nil, http.StatusOK)["agent_mode"]; got != mode {
					t.Fatalf("重载 mode=%v want=%s", got, mode)
				}
				beforePassive, beforeActive := passive.calls, active.calls
				res := request(t, http.MethodPost, "/api/agent/lifecycle/run", map[string]any{"agent_id": id, "customer_id": "synthetic-mode-customer", "content": "受控消息"}, http.StatusOK)
				if res["mode"] != mode {
					t.Fatalf("实际运行=%v want=%s", res, mode)
				}
				wantPassive, wantActive := 0, 0
				if mode == "active" {
					wantActive = 1
				} else {
					wantPassive = 1
				}
				if passive.calls-beforePassive != wantPassive || active.calls-beforeActive != wantActive {
					t.Fatalf("生命周期实际调用 passive=%d active=%d", passive.calls-beforePassive, active.calls-beforeActive)
				}
				cold := service.NewAIAgentServiceWithRepo(repo)
				loaded, err := cold.LoadContext(ctx, id)
				if err != nil || loaded == nil || loaded.AgentMode != mode {
					t.Fatalf("冷服务重载=%+v err=%v", loaded, err)
				}
			}
			if _, err := runtimeLoader.LoadContext(ctx, id); err != nil {
				t.Fatal(err)
			}
			check(initial) // 另一 service 的旧模式上下文缓存已预热。
			next := "active"
			if initial == "active" {
				next = "passive"
			}
			request(t, http.MethodPut, path, map[string]any{"agent_mode": next}, http.StatusOK)
			check(next)
			request(t, http.MethodPut, path, map[string]any{"description": "省略模式保留"}, http.StatusOK)
			check(next)
			before, err := management.GetByID(ctx, id)
			if err != nil {
				t.Fatal(err)
			}
			for _, invalid := range []any{"hybrid", "Active", " active ", "", nil, 12, map[string]any{}} {
				request(t, http.MethodPut, path, map[string]any{"agent_mode": invalid}, http.StatusBadRequest)
				after, err := management.GetByID(ctx, id)
				if err != nil || after.AgentMode != next || after.Version != before.Version {
					t.Fatalf("拒绝后状态变化: %+v err=%v", after, err)
				}
			}
			for _, raw := range []string{"null", "[]", "{"} {
				out := httptest.NewRecorder()
				req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(raw))
				req.Header.Set("Content-Type", "application/json")
				router.ServeHTTP(out, req)
				if out.Code != http.StatusBadRequest {
					t.Fatalf("错误 JSON %q status=%d body=%s", raw, out.Code, out.Body.String())
				}
			}
			check(next)
		})
	}
	for index, invalid := range []any{"hybrid", "Active", " active ", 12, map[string]any{}} {
		request(t, http.MethodPost, "/api/ai-agents", map[string]any{"agent_code": fmt.Sprintf("invalid-mode-%d", index), "name": "invalid", "persona": "受控", "agent_mode": invalid}, http.StatusBadRequest)
	}
	legacy := &model.AIAgent{AgentCode: "legacy-mode", Name: "legacy", Persona: "受控", Status: 1}
	if err := management.Create(ctx, legacy); err != nil || legacy.AgentMode != "passive" {
		t.Fatalf("旧调用默认 mode=%s err=%v", legacy.AgentMode, err)
	}
	for _, operation := range []string{"create", "update"} {
		candidate := &model.AIAgent{AgentCode: "invalid-service-mode", Name: "invalid", Persona: "受控", AgentMode: "hybrid", Status: 1}
		var err error
		if operation == "create" {
			err = management.Create(ctx, candidate)
		} else {
			candidate.ID = legacy.ID
			err = management.Update(ctx, candidate)
		}
		if err == nil {
			t.Errorf("内部 %s 也必须拒绝未知模式", operation)
		}
	}
}

func TestAgentModeRuntimeReadFailure(t *testing.T) {
	database := testutil.NewTestDB(t, &model.AIAgent{})
	previousMode := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previousMode) })
	ctx := tenant.WithTenantID(context.Background(), 1)
	loader := service.NewAIAgentServiceWithRepo(repository.NewAIAgentRepositoryWithDB(database))
	agent := &model.AIAgent{AgentCode: "mode-read-failure", Name: "受控", Persona: "受控", AgentMode: "active", Status: 1}
	if err := loader.Create(ctx, agent); err != nil {
		t.Fatal(err)
	}
	if _, err := loader.LoadContext(ctx, agent.ID); err != nil {
		t.Fatal(err)
	}
	runtime, passive, active := newTestRuntime(&lifecycleContextLoader{service: loader})
	for _, denied := range []context.Context{context.Background(), tenant.WithTenantID(context.Background(), 2)} {
		if _, err := runtime.Run(denied, agent.ID, &lifecycle.LifecycleRequest{}); err == nil {
			t.Fatal("缺失或错误租户不得使用已预热的执行配置")
		}
	}
	readFailure := errors.New("受控配置读取故障")
	if err := database.Callback().Query().Before("gorm:query").Register("agent-mode-read-failure", func(tx *gorm.DB) {
		tx.AddError(readFailure)
	}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Callback().Query().Remove("agent-mode-read-failure") })
	if _, err := runtime.Run(ctx, agent.ID, &lifecycle.LifecycleRequest{}); !errors.Is(err, readFailure) {
		t.Fatalf("读取失败必须上抛，不能用旧缓存执行: %v", err)
	}
	if passive.calls+active.calls != 0 {
		t.Fatalf("拒绝仍调用生命周期 passive=%d active=%d", passive.calls, active.calls)
	}
}
