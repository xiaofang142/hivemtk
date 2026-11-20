package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/aiagent/agent/tooluse"

	"github.com/gin-gonic/gin"
)

// 白名单入口必须操作注入的同一检查器，并保留 false 和绑定失败语义。
func TestAgentInferenceController_PermissionContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pc := tooluse.NewWhitelistPermissionChecker()
	ctrl := NewAgentInferenceController(func() *agent_runtime.CoreDataFlowOrchestrator { return nil }, func() *tooluse.WhitelistPermissionChecker { return pc })
	engine := gin.New()
	engine.PUT("/default", ctrl.SetPermissionDefault)
	engine.POST("/global", ctrl.AddGlobalWhitelist)
	engine.POST("/agents/:agent_id", ctrl.SetAgentWhitelist)
	engine.DELETE("/agents/:agent_id", ctrl.RemoveAgentWhitelist)
	request := func(method, path, body string, status int) {
		t.Helper()
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		engine.ServeHTTP(w, req)
		if w.Code != status {
			t.Fatalf("%s %s：HTTP %d，期望 %d：%s", method, path, w.Code, status, w.Body.String())
		}
		if status == http.StatusOK {
			var result struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Code != 0 {
				t.Fatalf("成功响应信封不兼容：%s", w.Body.String())
			}
		}
	}
	request(http.MethodPut, "/default", `{"default_allow":false}`, http.StatusOK)
	if pc.GetDefaultAllow() {
		t.Fatal("false 策略未写入注入的检查器")
	}
	request(http.MethodPost, "/global", `{}`, http.StatusBadRequest)
	request(http.MethodPost, "/global", `{"tools":["customer.get"]}`, http.StatusOK)
	if tools := pc.ListGlobalWhitelist(); len(tools) != 1 || tools[0] != "customer.get" {
		t.Fatalf("全局白名单写入错误：%v", tools)
	}
	request(http.MethodPost, "/agents/7", `{"tools":["rag.search"]}`, http.StatusOK)
	if tools := pc.ListAgentWhitelist("7"); len(tools) != 1 || tools[0] != "rag.search" {
		t.Fatalf("智能体白名单写入错误：%v", tools)
	}
	request(http.MethodDelete, "/agents/7", ``, http.StatusOK)
	if agents := pc.ListConfiguredAgents(); len(agents) != 0 {
		t.Fatalf("移除白名单后配置仍存在：%v", agents)
	}
}

func TestAgentInferenceController_UnavailableAndBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var orch *agent_runtime.CoreDataFlowOrchestrator
	ctrl := NewAgentInferenceController(func() *agent_runtime.CoreDataFlowOrchestrator { return orch }, func() *tooluse.WhitelistPermissionChecker { return nil })
	engine := gin.New()
	engine.POST("/run", ctrl.Run)
	engine.GET("/stats", ctrl.Stats)
	for _, path := range []string{"/run", "/stats"} {
		method := http.MethodGet
		if path == "/run" {
			method = http.MethodPost
		}
		w := httptest.NewRecorder()
		engine.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("未装配运行时应报告 503：%s %d", path, w.Code)
		}
	}
	orch = agent_runtime.NewCoreDataFlowOrchestrator(agent_runtime.NewInferenceCycle(), nil)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/run", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("缺必填字段应在绑定阶段拒绝：HTTP %d", w.Code)
	}
}
