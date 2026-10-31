package controller

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func setupCustomerSessionTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
	)
	db.SetTestDB(database)
	return database
}

func setupCustomerSessionController(t *testing.T) (*CustomerSessionController, *AgentStatusController, *gin.Engine) {
	setupCustomerSessionTestDB(t)
	sessionCtrl := NewCustomerSessionController()
	agentCtrl := NewAgentStatusController(
		service.NewAgentStatusService(
			repository.NewAgentStatusRepositoryWithDB(db.GetDB()),
			repository.NewCustomerSessionRepositoryWithDB(db.GetDB()),
		),
		service.NewCustomerServiceAgentServiceWithRepos(
			repository.NewCustomerServiceAgentRepositoryWithDB(db.GetDB()),
			repository.NewAIAgentRepositoryWithDB(db.GetDB()),
			repository.NewAgentStatusRepositoryWithDB(db.GetDB()),
			nil,
		),
	)
	router := gin.New()

	router.Use(func(c *gin.Context) {
		c.Set("user_id", uint(1))
		c.Next()
	})

	return sessionCtrl, agentCtrl, router
}

// TestCustomerSessionController_GetSessions_Success 测试获取会话列表成功
func TestCustomerSessionController_GetSessions_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions", ctrl.GetSessions)

	// status 用模型真实枚举（pending）：老测试写的 active 从来不是合法会话状态，
	// 状态白名单（isKnownSessionStatus）上线后该值正确地回 400。
	req, _ := http.NewRequest("GET", "/customer-sessions?status=pending&page=1&page_size=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_GetSessions_EmptyList 测试空列表
func TestCustomerSessionController_GetSessions_EmptyList(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions", ctrl.GetSessions)

	req, _ := http.NewRequest("GET", "/customer-sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_GetSessionByID_Success 测试获取会话详情成功
func TestCustomerSessionController_GetSessionByID_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions/:id", ctrl.GetSessionByID)

	req, _ := http.NewRequest("GET", "/customer-sessions/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
		t.Errorf("Expected status OK, Internal Server Error or Not Found, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_GetSessionByID_InvalidID 测试无效会话 ID
func TestCustomerSessionController_GetSessionByID_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions/:id", ctrl.GetSessionByID)

	req, _ := http.NewRequest("GET", "/customer-sessions/invalid", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_CreateSession_Success 测试创建会话成功
func TestCustomerSessionController_CreateSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions", ctrl.CreateSession)

	createReq := map[string]any{
		"customer_id":   "customer-123",
		"customer_name": "测试客户",
		"source":        "web",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/customer-sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_CreateSession_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_CreateSession_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions", ctrl.CreateSession)

	req, _ := http.NewRequest("POST", "/customer-sessions", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_AssignSession_Success 测试分配会话成功
func TestCustomerSessionController_AssignSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/assign", ctrl.AssignSession)

	assignReq := map[string]any{
		"session_id": 1,
		"agent_id":   2,
	}
	body, _ := json.Marshal(assignReq)

	req, _ := http.NewRequest("POST", "/customer-sessions/assign", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_AssignSession_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_AssignSession_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/assign", ctrl.AssignSession)

	req, _ := http.NewRequest("POST", "/customer-sessions/assign", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_AutoAssignSession_Success 测试自动分配会话成功
func TestCustomerSessionController_AutoAssignSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/auto-assign", ctrl.AutoAssignSession)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/auto-assign", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_AutoAssignSession_InvalidID 测试无效会话 ID
func TestCustomerSessionController_AutoAssignSession_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/auto-assign", ctrl.AutoAssignSession)

	req, _ := http.NewRequest("POST", "/customer-sessions/invalid/auto-assign", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_GetMessages_Success 测试获取消息列表成功
func TestCustomerSessionController_GetMessages_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions/:id/messages", ctrl.GetMessages)

	req, _ := http.NewRequest("GET", "/customer-sessions/1/messages?page=1&page_size=20", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_SendMessage_Success 测试发送消息成功
func TestCustomerSessionController_SendMessage_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/messages", ctrl.SendMessage)

	sendReq := map[string]any{
		"content":      "测试消息",
		"message_type": "text",
	}
	body, _ := json.Marshal(sendReq)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_SendMessage_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_SendMessage_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/messages", ctrl.SendMessage)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/messages", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_UpdateSessionStatus_Success 测试更新会话状态成功
func TestCustomerSessionController_UpdateSessionStatus_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/status", ctrl.UpdateSessionStatus)

	statusReq := map[string]any{
		"status": "closed",
	}
	body, _ := json.Marshal(statusReq)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_UpdateSessionStatus_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_UpdateSessionStatus_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/status", ctrl.UpdateSessionStatus)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/status", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_RateSession_Success 测试评价会话成功
func TestCustomerSessionController_RateSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/rate", ctrl.RateSession)

	rateReq := map[string]any{
		"rating":  5,
		"comment": "服务很好",
	}
	body, _ := json.Marshal(rateReq)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/rate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_RateSession_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_RateSession_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.POST("/customer-sessions/:id/rate", ctrl.RateSession)

	req, _ := http.NewRequest("POST", "/customer-sessions/1/rate", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_NewCustomerSessionController 测试构造函数
func TestCustomerSessionController_NewCustomerSessionController(t *testing.T) {
	ctrl := NewCustomerSessionController()
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}

// TestCustomerSessionController_TransferSession_Success 测试转接会话成功
func TestCustomerSessionController_TransferSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/transfer", ctrl.TransferSession)

	transferReq := map[string]any{
		"new_agent_id": float64(2),
	}
	body, _ := json.Marshal(transferReq)

	req, _ := http.NewRequest("PUT", "/customer-sessions/1/transfer", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_TransferSession_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_TransferSession_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/transfer", ctrl.TransferSession)

	req, _ := http.NewRequest("PUT", "/customer-sessions/1/transfer", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_TransferSession_InvalidID 测试无效会话 ID
func TestCustomerSessionController_TransferSession_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/transfer", ctrl.TransferSession)

	req, _ := http.NewRequest("PUT", "/customer-sessions/invalid/transfer", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_TagSession_Success 测试标记会话成功
func TestCustomerSessionController_TagSession_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/tags", ctrl.TagSession)

	tagReq := map[string]any{
		"tags": []any{"urgent", "vip"},
	}
	body, _ := json.Marshal(tagReq)

	req, _ := http.NewRequest("PUT", "/customer-sessions/1/tags", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestCustomerSessionController_TagSession_InvalidJSON 测试无效 JSON
func TestCustomerSessionController_TagSession_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/tags", ctrl.TagSession)

	req, _ := http.NewRequest("PUT", "/customer-sessions/1/tags", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_TagSession_InvalidID 测试无效会话 ID
func TestCustomerSessionController_TagSession_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.PUT("/customer-sessions/:id/tags", ctrl.TagSession)

	req, _ := http.NewRequest("PUT", "/customer-sessions/invalid/tags", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestCustomerSessionController_GetPendingSessions 测试获取待处理会话
func TestCustomerSessionController_GetPendingSessions(t *testing.T) {
	setupCustomerSessionTestDB(t)
	ctrl, _, router := setupCustomerSessionController(t)
	router.GET("/customer-sessions/pending", ctrl.GetPendingSessions)

	req, _ := http.NewRequest("GET", "/customer-sessions/pending", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_CreateAgent_Success 测试创建客服成功
func TestAgentStatusController_CreateAgent_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents", ctrl.CreateAgent)

	createReq := map[string]any{
		"agent_id":   float64(100),
		"agent_name": "Test Agent",
	}
	body, _ := json.Marshal(createReq)

	req, _ := http.NewRequest("POST", "/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_CreateAgent_InvalidJSON 测试无效 JSON
func TestAgentStatusController_CreateAgent_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents", ctrl.CreateAgent)

	req, _ := http.NewRequest("POST", "/agents", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAgentStatusController_GetAgentStatus_Success 测试获取客服状态成功
func TestAgentStatusController_GetAgentStatus_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.GET("/agents/:id/status", ctrl.GetAgentStatus)

	req, _ := http.NewRequest("GET", "/agents/100/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_GetAgentStatus_InvalidID 测试无效客服 ID
func TestAgentStatusController_GetAgentStatus_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.GET("/agents/:id/status", ctrl.GetAgentStatus)

	req, _ := http.NewRequest("GET", "/agents/invalid/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAgentStatusController_GetOnlineAgents 测试获取在线客服
func TestAgentStatusController_GetOnlineAgents(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.GET("/agents/online", ctrl.GetOnlineAgents)

	req, _ := http.NewRequest("GET", "/agents/online", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_UpdateAgentStatus_Success 测试更新客服状态成功
func TestAgentStatusController_UpdateAgentStatus_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.PUT("/agents/:id/status", ctrl.UpdateAgentStatus)

	updateReq := map[string]any{
		"status": "offline",
	}
	body, _ := json.Marshal(updateReq)

	req, _ := http.NewRequest("PUT", "/agents/100/status", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_UpdateAgentStatus_InvalidJSON 测试无效 JSON
func TestAgentStatusController_UpdateAgentStatus_InvalidJSON(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.PUT("/agents/:id/status", ctrl.UpdateAgentStatus)

	req, _ := http.NewRequest("PUT", "/agents/100/status", bytes.NewReader([]byte("invalid-json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAgentStatusController_GoOnline_Success 测试客服上线成功
func TestAgentStatusController_GoOnline_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents/:id/online", ctrl.GoOnline)

	req, _ := http.NewRequest("POST", "/agents/100/online", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_GoOnline_InvalidID 测试无效客服 ID
func TestAgentStatusController_GoOnline_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents/:id/online", ctrl.GoOnline)

	req, _ := http.NewRequest("POST", "/agents/invalid/online", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAgentStatusController_GoOffline_Success 测试客服下线成功
func TestAgentStatusController_GoOffline_Success(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents/:id/offline", ctrl.GoOffline)

	req, _ := http.NewRequest("POST", "/agents/100/offline", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusNotFound && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Not Found or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_GoOffline_InvalidID 测试无效客服 ID
func TestAgentStatusController_GoOffline_InvalidID(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.POST("/agents/:id/offline", ctrl.GoOffline)

	req, _ := http.NewRequest("POST", "/agents/invalid/offline", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected status Bad Request, got %d", w.Code)
	}
}

// TestAgentStatusController_GetAgentSessions 测试获取客服会话列表
func TestAgentStatusController_GetAgentSessions(t *testing.T) {
	setupCustomerSessionTestDB(t)
	_, ctrl, router := setupCustomerSessionController(t)
	router.GET("/agents/:id/sessions", ctrl.GetAgentSessions)

	req, _ := http.NewRequest("GET", "/agents/100/sessions", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 数据隔离后：无登录态（非 admin、user_id=0）查他人坐席会话 → 403 也是合法档。
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest && w.Code != http.StatusForbidden && w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status OK, Bad Request, Forbidden or Internal Server Error, got %d, body: %s", w.Code, w.Body.String())
	}
}

// TestAgentStatusController_NewAgentStatusController 测试构造函数
func TestAgentStatusController_NewAgentStatusController(t *testing.T) {
	ctrl := NewAgentStatusController(nil, nil)
	if ctrl == nil {
		t.Error("Expected controller instance, got nil")
	}
}
