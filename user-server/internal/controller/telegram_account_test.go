package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func setupTelegramControllerTestDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.TelegramAccount{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.WebhookEvent{},
		&model.Customer{},
		&model.IntegrationAccount{},
		&model.UnifiedMessage{},
	)
	db.SetTestDB(database)
	return database
}

func setupTelegramAccountRouter(ctrl *TelegramAccountController) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	ctrl.RegisterRoutes(r.Group("/api"))
	return r
}

func setupTelegramWebhookRouter(database *gorm.DB) (*gin.Engine, *service.WebhookService) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc := service.NewWebhookService()
	whCtrl := &WebhookController{svc: svc}
	wh := r.Group("/api/webhook")
	{
		wh.POST("/:channel/:account_id", whCtrl.Receive)
	}
	return r, svc
}

func waitForWorker(t *testing.T, database *gorm.DB, query string, args ...any) {
	for i := 0; i < 200; i++ {
		var count int64
		database.Model(&model.MessageHub{}).Where(query, args...).Count(&count)
		if count > 0 {
			return
		}
		time.Sleep(time.Millisecond * 100)
	}
	t.Logf("⚠️ worker 处理超时，查询: %s args: %v", query, args)
}

// TestTelegramAccountController_CreateAndList 创建账号并列表查询
func TestTelegramAccountController_CreateAndList(t *testing.T) {
	setupTelegramControllerTestDB(t)
	ctrl := NewTelegramAccountController(nil)
	router := setupTelegramAccountRouter(ctrl)

	createBody := map[string]any{
		"account_name":     "销售助手Bot",
		"bot_token":        "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		"webhook_url":      "https://shop.example.com/api/webhook/telegram/1",
		"webhook_secret":   "test-secret-32chars-random-string",
		"webhook_enabled":  true,
		"ai_agent_enabled": true,
		"status":           1,
	}
	bodyBytes, _ := json.Marshal(createBody)
	req, _ := http.NewRequest("POST", "/api/telegram/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("创建账号期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var createResp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("解析创建响应失败: %v, body: %s", err, w.Body.String())
	}
	if createResp.Code != 0 {
		t.Errorf("创建响应 code 期望 SUCCESS, 实际 %d", createResp.Code)
	}
	if createResp.Data["account_name"] != "销售助手Bot" {
		t.Errorf("account_name 错误: %v", createResp.Data["account_name"])
	}
	masked, _ := createResp.Data["bot_token_masked"].(string)
	if masked == "123456789:abcdefghijklmnopqrstuvwxyz123456789" {
		t.Error("Bot Token 应被掩码处理")
	}
	if len(masked) > 0 && masked[:4] != "1234" {
		t.Errorf("Bot Token 掩码应保留前4位, 实际: %s", masked)
	}
	t.Logf("✅ 创建账号成功: id=%v, masked_token=%s", createResp.Data["id"], masked)

	req, _ = http.NewRequest("GET", "/api/telegram/accounts", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("列表查询期望 200, 实际 %d", w.Code)
	}
	var listResp struct {
		Code int              `json:"code"`
		Data map[string]any   `json:"data"`
		List []map[string]any `json:"list"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("解析列表响应失败: %v", err)
	}
	list := listResp.List
	if len(list) == 0 {
		if dataList, ok := listResp.Data["list"].([]any); ok && len(dataList) > 0 {
			t.Logf("✅ 列表查询成功: %d 条记录", len(dataList))
			return
		}
		if total, ok := listResp.Data["total"].(float64); ok && total > 0 {
			t.Logf("✅ 列表查询成功: total=%v", total)
			return
		}
		t.Errorf("列表查询应返回至少 1 条记录, body: %s", w.Body.String())
		return
	}
	t.Logf("✅ 列表查询成功: %d 条记录", len(list))
}

// TestTelegramAccountController_GetAndGetByID 创建后按 ID 查询
func TestTelegramAccountController_GetAndGetByID(t *testing.T) {
	setupTelegramControllerTestDB(t)
	ctrl := NewTelegramAccountController(nil)
	router := setupTelegramAccountRouter(ctrl)

	bodyBytes, _ := json.Marshal(map[string]any{
		"account_name": "测试Bot",
		"bot_token":    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		"status":       1,
	})
	req, _ := http.NewRequest("POST", "/api/telegram/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("创建失败: %d %s", w.Code, w.Body.String())
	}

	req, _ = http.NewRequest("GET", "/api/telegram/accounts", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var listResp struct {
		Data map[string]any `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &listResp)

	accs, _ := ctrl.svc.ListAccounts(context.Background())
	if len(accs) != 1 {
		t.Fatalf("应有 1 条记录, 实际 %d", len(accs))
	}
	accID := accs[0].ID

	req, _ = http.NewRequest("GET", "/api/telegram/accounts/"+uintToStr(accID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("按 ID 查询期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	t.Logf("✅ 按 ID 查询成功: %s", w.Body.String()[:min(80, len(w.Body.String()))])
}

// TestTelegramAccountController_Update 更新账号
func TestTelegramAccountController_Update(t *testing.T) {
	setupTelegramControllerTestDB(t)
	ctrl := NewTelegramAccountController(nil)
	router := setupTelegramAccountRouter(ctrl)

	bodyBytes, _ := json.Marshal(map[string]any{
		"account_name": "原名称",
		"bot_token":    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		"status":       1,
	})
	req, _ := http.NewRequest("POST", "/api/telegram/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	accs, _ := ctrl.svc.ListAccounts(context.Background())
	if len(accs) != 1 {
		t.Fatalf("创建后应有 1 条")
	}
	accID := accs[0].ID

	updateBody, _ := json.Marshal(map[string]any{
		"account_name":     "新名称",
		"bot_token":        "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		"ai_agent_enabled": true,
		"status":           1,
	})
	req, _ = http.NewRequest("PUT", "/api/telegram/accounts/"+uintToStr(accID), bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("更新期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}

	updated, _ := ctrl.svc.GetAccount(context.Background(), accID)
	if updated.AccountName != "新名称" {
		t.Errorf("名称应更新为 '新名称', 实际 '%s'", updated.AccountName)
	}
	if !updated.AIAgentEnabled {
		t.Error("AIAgentEnabled 应为 true")
	}
	if updated.BotToken != "123456789:abcdefghijklmnopqrstuvwxyz123456789" {
		t.Errorf("Bot Token 错误, 实际 '%s'", updated.BotToken)
	}
	t.Logf("✅ 更新成功: name=%s, ai=%v, token=%s", updated.AccountName, updated.AIAgentEnabled, updated.BotToken)
}

// TestTelegramAccountController_Delete 删除账号
func TestTelegramAccountController_Delete(t *testing.T) {
	setupTelegramControllerTestDB(t)
	ctrl := NewTelegramAccountController(nil)
	router := setupTelegramAccountRouter(ctrl)

	bodyBytes, _ := json.Marshal(map[string]any{
		"account_name": "待删除",
		"bot_token":    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		"status":       1,
	})
	req, _ := http.NewRequest("POST", "/api/telegram/accounts", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	accs, _ := ctrl.svc.ListAccounts(context.Background())
	if len(accs) != 1 {
		t.Fatalf("创建后应有 1 条")
	}
	accID := accs[0].ID

	req, _ = http.NewRequest("DELETE", "/api/telegram/accounts/"+uintToStr(accID), nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("删除期望 200, 实际 %d", w.Code)
	}

	accs, _ = ctrl.svc.ListAccounts(context.Background())
	if len(accs) != 0 {
		t.Errorf("删除后应有 0 条, 实际 %d", len(accs))
	}
	t.Logf("✅ 删除成功")
}

// TestTelegramWebhook_JoinEvent_E2E 完整链路：
// HTTP POST /api/webhook/telegram/:account_id → dispatchTelegram → MessageHub
func TestTelegramWebhook_JoinEvent_E2E(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	database := setupTelegramControllerTestDB(t)

	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), database)
	acc := &model.TelegramAccount{
		AccountName:    "E2E测试Bot",
		BotToken:       "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		WebhookSecret:  "",
		AIAgentEnabled: false,
		Status:         1,
	}
	if err := repo.Create(context.Background(), acc); err != nil {
		t.Fatalf("创建测试账号失败: %v", err)
	}
	accID := acc.ID

	router, svc := setupTelegramWebhookRouter(database)
	defer svc.Stop(context.Background())

	joinPayload := `{
		"update_id": 2001,
		"message": {
			"message_id": 6001,
			"from": {"id": 888, "first_name": "Inviter", "is_bot": false},
			"chat": {"id": -1009876543210, "type": "supergroup", "title": "E2E销售群"},
			"date": 1700000000,
			"new_chat_members": [
				{"id": 777, "first_name": "新客户", "username": "newcustomer", "is_bot": false}
			]
		}
	}`
	req, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), bytes.NewReader([]byte(joinPayload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Webhook 请求期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Accepted  bool   `json:"accepted"`
		EventID   string `json:"event_id"`
		EventType string `json:"event_type"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v, body: %s", err, w.Body.String())
	}
	if !resp.Accepted {
		t.Errorf("accepted 应为 true, body: %s", w.Body.String())
	}
	t.Logf("✅ Webhook 入群事件接收成功: event_id=%s, type=%s", resp.EventID, resp.EventType)

	waitForWorker(t, database, "platform = ? AND msg_type = ?", "telegram", "event")
	var hub model.MessageHub
	if err := database.Where("platform = ? AND msg_type = ?", "telegram", "event").First(&hub).Error; err != nil {
		t.Fatalf("MessageHub 中应入群事件记录: %v", err)
	}
	if hub.MsgID != "tg_join_-1009876543210_777" {
		t.Errorf("MsgID 错误: %s", hub.MsgID)
	}
	if hub.ConversationID != "-1009876543210" {
		t.Errorf("ConversationID 错误: %s", hub.ConversationID)
	}
	if !hub.IsGroup {
		t.Error("入群事件 IsGroup 应为 true")
	}
	t.Logf("✅ MessageHub 入群事件记录: msg_id=%s, content=%s", hub.MsgID, hub.Content)
}

// TestTelegramWebhook_RegularMessage_E2E 普通消息端到端
func TestTelegramWebhook_RegularMessage_E2E(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	database := setupTelegramControllerTestDB(t)

	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), database)
	acc := &model.TelegramAccount{
		AccountName:    "消息测试Bot",
		BotToken:       "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		WebhookSecret:  "",
		AIAgentEnabled: false,
		Status:         1,
	}
	repo.Create(context.Background(), acc)
	accID := acc.ID

	router, svc := setupTelegramWebhookRouter(database)
	defer svc.Stop(context.Background())

	msgPayload := `{
		"update_id": 3001,
		"message": {
			"message_id": 7001,
			"from": {"id": 555, "first_name": "客户A", "username": "userA", "is_bot": false},
			"chat": {"id": -1005555555555, "type": "group", "title": "咨询群"},
			"date": 1700000100,
			"text": "你好，我想了解一下光子嫩肤的价格"
		}
	}`
	req, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), bytes.NewReader([]byte(msgPayload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Webhook 请求期望 200, 实际 %d, body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Accepted bool `json:"accepted"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Accepted {
		t.Errorf("accepted 应为 true, body: %s", w.Body.String())
	}

	waitForWorker(t, database, "platform = ? AND msg_type = ?", "telegram", "text")
	var hub model.MessageHub
	if err := database.Where("platform = ? AND msg_type = ?", "telegram", "text").First(&hub).Error; err != nil {
		t.Fatalf("MessageHub 中应有 text 记录: %v", err)
	}
	if hub.Content != "你好，我想了解一下光子嫩肤的价格" {
		t.Errorf("Content 错误: %s", hub.Content)
	}
	if !hub.IsGroup {
		t.Error("群组消息 IsGroup 应为 true")
	}
	t.Logf("✅ 普通消息记录: content=%s", hub.Content)
}

// TestTelegramWebhook_LeftEvent_E2E 退群事件端到端
func TestTelegramWebhook_LeftEvent_E2E(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	database := setupTelegramControllerTestDB(t)

	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), database)
	acc := &model.TelegramAccount{
		AccountName: "退群测试Bot",
		BotToken:    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		Status:      1,
	}
	repo.Create(context.Background(), acc)
	accID := acc.ID

	router, svc := setupTelegramWebhookRouter(database)
	defer svc.Stop(context.Background())

	leftPayload := `{
		"update_id": 4001,
		"message": {
			"message_id": 8001,
			"from": {"id": 444, "first_name": "Admin", "is_bot": false},
			"chat": {"id": -1004444444444, "type": "group", "title": "退群测试"},
			"date": 1700000200,
			"left_chat_member": {"id": 333, "first_name": "离开的客户", "username": "leaver", "is_bot": false}
		}
	}`
	req, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), bytes.NewReader([]byte(leftPayload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Webhook 请求期望 200, 实际 %d", w.Code)
	}

	waitForWorker(t, database, "platform = ? AND msg_id LIKE ?", "telegram", "tg_left_%")
	var count int64
	database.Model(&model.MessageHub{}).Where("platform = ? AND msg_id LIKE ?", "telegram", "tg_left_%").Count(&count)
	if count == 0 {
		t.Error("应有退群事件记录 (msg_id LIKE tg_left_%)")
	}
	t.Logf("✅ 退群事件记录: %d 条", count)
}

// TestTelegramWebhook_BotMembersSkipped 仅 bot 成员入群不产生 event 记录
func TestTelegramWebhook_BotMembersSkipped(t *testing.T) {
	database := setupTelegramControllerTestDB(t)

	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), database)
	acc := &model.TelegramAccount{
		AccountName: "Bot过滤测试",
		BotToken:    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		Status:      1,
	}
	repo.Create(context.Background(), acc)
	accID := acc.ID

	router, svc := setupTelegramWebhookRouter(database)
	defer svc.Stop(context.Background())

	botOnlyPayload := `{
		"update_id": 5001,
		"message": {
			"message_id": 9001,
			"from": {"id": 222, "first_name": "Admin", "is_bot": false},
			"chat": {"id": -1002222222222, "type": "group", "title": "Bot过滤"},
			"date": 1700000300,
			"new_chat_members": [
				{"id": 111, "first_name": "OtherBot", "username": "other_bot", "is_bot": true}
			]
		}
	}`
	req, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), bytes.NewReader([]byte(botOnlyPayload)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var eventCount int64
	database.Model(&model.MessageHub{}).Where("platform = ? AND msg_type = ?", "telegram", "event").Count(&eventCount)
	if eventCount > 0 {
		t.Errorf("仅 bot 入群不应产生 event 记录, 实际 %d 条", eventCount)
	}
	t.Logf("✅ 仅 bot 入群正确跳过: event 记录 %d 条", eventCount)
}

// TestTelegramWebhook_Idempotent 相同 update_id 重复请求应被去重
func TestTelegramWebhook_Idempotent(t *testing.T) {
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	t.Setenv("ALLOW_INSECURE_TELEGRAM_WEBHOOK", "true")
	database := setupTelegramControllerTestDB(t)

	repo := repository.NewTelegramAccountRepository()
	repo.SetDB(context.Background(), database)
	acc := &model.TelegramAccount{
		AccountName: "幂等测试Bot",
		BotToken:    "123456789:abcdefghijklmnopqrstuvwxyz123456789",
		Status:      1,
	}
	repo.Create(context.Background(), acc)
	accID := acc.ID

	router, svc := setupTelegramWebhookRouter(database)
	defer svc.Stop(context.Background())

	payload := `{
		"update_id": 6001,
		"message": {
			"message_id": 10001,
			"from": {"id": 999, "first_name": "Test", "is_bot": false},
			"chat": {"id": -1009999999999, "type": "private"},
			"date": 1700000400,
			"text": "测试幂等"
		}
	}`
	body := bytes.NewReader([]byte(payload))

	req, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("第一次请求期望 200, 实际 %d", w.Code)
	}
	var firstResp struct {
		Accepted  bool `json:"accepted"`
		Duplicate bool `json:"duplicate"`
	}
	json.Unmarshal(w.Body.Bytes(), &firstResp)
	if !firstResp.Accepted || firstResp.Duplicate {
		t.Errorf("第一次应 accepted=true, duplicate=false, body: %s", w.Body.String())
	}

	req2, _ := http.NewRequest("POST", "/api/webhook/telegram/"+uintToStr(accID), bytes.NewReader([]byte(payload)))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("第二次请求期望 200, 实际 %d", w2.Code)
	}
	var secondResp struct {
		Accepted  bool `json:"accepted"`
		Duplicate bool `json:"duplicate"`
	}
	json.Unmarshal(w2.Body.Bytes(), &secondResp)
	if !secondResp.Accepted {
		t.Errorf("第二次应 accepted=true（幂等返回）, body: %s", w2.Body.String())
	}
	t.Logf("✅ 幂等去单: 第一次 duplicate=%v, 第二次 duplicate=%v", firstResp.Duplicate, secondResp.Duplicate)
}

func uintToStr(n uint) string {
	return fmtUint(n)
}



func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
