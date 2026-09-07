package service

// QQ 渠道二次全流程检查测试（全自动化，模拟信号驱动）。
//
// 测试策略（脑暴结论 A+F 方案）：
//   - SimQQPlatform：内存模拟 QQ 开放平台，用真实 Ed25519 密钥派生签名，
//     把 webhook 事件 POST 到真实 gin HTTP 入口（/api/webhook/qq/{account_id}），
//     断言 HTTP 状态码与应答体（覆盖 controller 层，不只测 service 内部函数）。
//   - 模拟 AI 信号：FAQ 高分命中走 LayerRouter SkipLLM 分支（官方为 LLM 留的
//     零依赖旁路），SmartCSOrchestrator 真实执行（建会话/存消息/取回复），
//     无需 LLM key、无需外网。
//   - 出站模拟：qq.Client 经 WithBaseURL 指向 httptest 模拟平台，
//     断言 Authorization: QQBot 头、msg_type/msg_id/msg_seq 协议字段。
//
// 覆盖链路：
//   HTTP 入口 → 验签(Ed25519) → 入队 → dispatchQQ → Ingress(幂等) → message_hub
//   → AI 触发 → orchestrator(会话/消息/回复) → sendOutbound → qq.Client → 模拟平台。

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/channelbot/qq"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	dbutil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/featureflag"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// SimQQPlatform：模拟 QQ 开放平台
// ---------------------------------------------------------------------------

// simSentMessage 模拟平台收到的出站消息
type simSentMessage struct {
	GroupOpenID string
	UserOpenID  string
	Content     string
	MsgID       string // 被动回复关联的原消息 ID（空=主动消息）
	MsgSeq      int
	AuthHeader  string
	ReceivedAt  time.Time
}

// SimQQPlatform 内存模拟 QQ 开放平台
//
// 职责：
//  1. 持有 BotSecret，用真实 Ed25519 派生密钥为推送事件签名（模拟官方签名算法）
//  2. 记录机器人发出的消息（供协议字段断言）
//  3. PushGroupAtMessage 产出可投递的 webhook 请求（模拟群 @机器人 事件）
type SimQQPlatform struct {
	t           *testing.T
	Secret      string
	mu          sync.Mutex
	SentMsgs    []simSentMessage
	tokenServed int
}

func NewSimQQPlatform(t *testing.T, secret string) *SimQQPlatform {
	return &SimQQPlatform{t: t, Secret: secret}
}

// sign 模拟官方签名：timestamp + body
func (s *SimQQPlatform) sign(timestamp string, body []byte) string {
	priv := qq.DerivePrivateKey(s.Secret)
	return hex.EncodeToString(ed25519.Sign(priv, append([]byte(timestamp), body...)))
}

// webhookHeaders 产出官方推送应带的请求头
func (s *SimQQPlatform) webhookHeaders(timestamp string, body []byte) map[string]string {
	return map[string]string{
		"Content-Type":          "application/json",
		"X-Signature-Ed25519":   s.sign(timestamp, body),
		"X-Signature-Timestamp": timestamp,
		"X-Bot-Appid":           "sim-appid",
		"User-Agent":            "QQBot-Callback",
	}
}

// GroupAtMessageBody 构造群 @机器人 事件 body
func (s *SimQQPlatform) GroupAtMessageBody(eventID, groupOpenID, memberOpenID, content string) []byte {
	d := map[string]any{
		"id":           eventID,
		"group_openid": groupOpenID,
		"content":      content,
		"author":       map[string]any{"member_openid": memberOpenID},
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	raw, _ := json.Marshal(map[string]any{
		"id": "evt-" + eventID,
		"op": 0,
		"t":  qq.EventGroupAtMessage,
		"s":  time.Now().UnixNano(),
		"d":  d,
	})
	return raw
}

// C2CAtMessageBody 构造单聊事件 body
func (s *SimQQPlatform) C2CAtMessageBody(eventID, userOpenID, content string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"id": "evt-" + eventID,
		"op": 0,
		"t":  qq.EventC2CAtMessage,
		"d": map[string]any{
			"id":          eventID,
			"user_openid": userOpenID,
			"content":     content,
			"timestamp":   time.Now().Format(time.RFC3339),
		},
	})
	return raw
}

// Op13Body 构造回调地址验证 body
func (s *SimQQPlatform) Op13Body(plainToken string) []byte {
	raw, _ := json.Marshal(map[string]any{
		"op":          qq.CallbackOpVerify,
		"plain_token": plainToken,
		"event_ts":    fmt.Sprintf("%d", time.Now().Unix()),
	})
	return raw
}

// StartSimBotAPI 启动模拟机器人 API（/app/getAppAccessToken + /v2/groups|users/*/messages），
// 返回 base URL（供 qq.Client WithBaseURL 指向）。
func (s *SimQQPlatform) StartSimBotAPI() *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/app/getAppAccessToken", func(w http.ResponseWriter, _ *http.Request) {
		s.mu.Lock()
		s.tokenServed++
		s.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]string{"access_token": "SIM-TOKEN-" + fmt.Sprint(s.tokenServed), "expires_in": "7200"})
	})
	mux.HandleFunc("/v2/groups/", func(w http.ResponseWriter, r *http.Request) {
		openid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/groups/"), "/messages")
		s.recordOutbound(r, func(m *simSentMessage) { m.GroupOpenID = openid })
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"msg_id": "sim-out-" + fmt.Sprint(time.Now().UnixNano())}})
	})
	mux.HandleFunc("/v2/users/", func(w http.ResponseWriter, r *http.Request) {
		openid := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v2/users/"), "/messages")
		s.recordOutbound(r, func(m *simSentMessage) { m.UserOpenID = openid })
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"msg_id": "sim-out-" + fmt.Sprint(time.Now().UnixNano())}})
	})
	return httptest.NewServer(mux)
}

func (s *SimQQPlatform) recordOutbound(r *http.Request, fill func(*simSentMessage)) {
	var body struct {
		Content string `json:"content"`
		MsgID   string `json:"msg_id"`
		MsgSeq  int    `json:"msg_seq"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	m := simSentMessage{
		Content:    body.Content,
		MsgID:      body.MsgID,
		MsgSeq:     body.MsgSeq,
		AuthHeader: r.Header.Get("Authorization"),
		ReceivedAt: time.Now(),
	}
	fill(&m)
	s.mu.Lock()
	s.SentMsgs = append(s.SentMsgs, m)
	s.mu.Unlock()
}

func (s *SimQQPlatform) SentCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.SentMsgs)
}

func (s *SimQQPlatform) LastSent() simSentMessage {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.SentMsgs) == 0 {
		return simSentMessage{}
	}
	return s.SentMsgs[len(s.SentMsgs)-1]
}

// PostWebhook 把事件投递到被测 gin 引擎（模拟官方 HTTP 推送行为）
func (s *SimQQPlatform) PostWebhook(engine *gin.Engine, accountID string, body []byte) (*httptest.ResponseRecorder, map[string]any) {
	s.t.Helper()
	ts := fmt.Sprintf("%d", time.Now().Unix())
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/qq/"+accountID, bytes.NewReader(body))
	for k, v := range s.webhookHeaders(ts, body) {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, req)
	var resp map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	}
	return rec, resp
}

// ---------------------------------------------------------------------------
// 局部装配：真实 gin 入口 + 真实 webhook 服务 + FAQ 模拟 AI（免 router.Setup 全局副作用）
// ---------------------------------------------------------------------------

type qqFullchainEnv struct {
	db        *gorm.DB
	engine    *gin.Engine
	platform  *SimQQPlatform
	botAPI    *httptest.Server
	webhook   *WebhookService
	accountID string
	secret    string
}

func setupQQFullchain(t *testing.T) *qqFullchainEnv {
	t.Helper()

	database := testutil.NewTestDB(t,
		&model.QQAccount{},
		&model.MessageHub{},
		&model.InboxConversation{},
		&model.UnifiedMessage{},
		&model.WebhookEvent{},
		&model.IntegrationAccount{},
		&model.CustomerSession{},
		&model.SessionMessage{},
		&model.AgentStatus{},
		&model.AISuggestion{},
		&model.FAQEntry{},
		&model.SOPTemplate{},
		&model.Customer{},
		&model.AIAgent{},
		&model.ChannelAgentBinding{},
	)
	// orchestrator 内部 repositories 在构造时捕获全局 DB（_db.GetDB()），
	// 必须先 SetTestDB 再 NewSmartCSOrchestrator / NewWebhookService。
	dbutil.SetTestDB(database)
	t.Cleanup(func() { dbutil.SetTestDB(nil) })

	secret := "sim-qq-bot-secret"
	platform := NewSimQQPlatform(t, secret)
	botAPI := platform.StartSimBotAPI()
	t.Cleanup(botAPI.Close)
	// 出站模拟：QQIntegrationService 经 QQ_API_BASE_URL 指向模拟平台
	t.Setenv("QQ_API_BASE_URL", botAPI.URL)

	// 种子 QQ 账号（webhook 验签 secret = 模拟平台 BotSecret）
	acc := &model.QQAccount{
		AccountName: "全链路模拟号", AppID: "sim-appid", AppSecret: "sim-app-secret",
		WebhookSecret: secret, Status: 1, AIAgentEnabled: true,
	}
	if err := database.Create(acc).Error; err != nil {
		t.Fatalf("seed qq account: %v", err)
	}

	// SOP 模板模拟 AI：LayerRouter SOP 分支 SkipLLM（无需 LLM 的官方零依赖旁路）。
	// 触发条件：AgentID>0 + Intent(价格咨询) + 高置信 SOP 模板 → 模板渲染即回复。
	// 前置：seed AI 智能体 + 渠道绑定（loadAgentForChannel → req.AgentID）。
	enabled := true
	agent := model.AIAgent{
		AgentCode: "sim_agent", Name: "模拟销售智能体", AgentType: "sales",
		Persona: "你是 QQ 群里的销售助手", Status: 1,
	}
	if err := database.Create(&agent).Error; err != nil {
		t.Fatalf("seed ai agent: %v", err)
	}
	binding := model.ChannelAgentBinding{
		ChannelType: "qq", AccountID: fmt.Sprintf("%d", 1), AgentID: agent.ID,
		IsPrimary: true, Enabled: true,
	}
	if err := database.Create(&binding).Error; err != nil {
		t.Fatalf("seed binding: %v", err)
	}
	agentID := agent.ID
	sopTpl := model.SOPTemplate{
		Name: "价格咨询模拟", Intent: IntentPriceInquiry, Stage: "",
		Template:   "我们的旗舰套餐是 999 元/月，企业版可议价。",
		Confidence: 0.9, AgentID: &agentID, Enabled: &enabled,
	}
	if err := database.Create(&sopTpl).Error; err != nil {
		t.Fatalf("seed sop: %v", err)
	}

	// 引擎装配：dispatcher=nil（SOP 命中即回，不触 LLM）；intent=nil 走 fallback(unknown)
	// → SOP 分支要求 Intent != unknown，故注入规则意图识别 mock。
	engine := NewSalesEngine(database, nil, &simIntentRecognizer{}, nil, nil, nil, nil, nil)
	withLayer1Flag(t, "1")
	lr := &LayerRouter{
		faqRepo: nil,
		sopRepo: repository.NewSOPTemplateRepository(database),
		sopSvc:  NewSOPTemplateService(database, nil),
		logRepo: nil,
	}
	engine.SetLayerRouter(context.Background(), lr)
	orch := NewSmartCSOrchestrator(engine, DefaultOrchestratorConfig(), nil)

	// webhook 服务 + AI 注入
	// 生产装配（router.go:348,466-467）：bridgeIngressSvc 是独立 ingress，
	// 其 aiTrigger = webhookSvc（AITrigger 接口），webhookSvc.ingressSvc = bridgeIngressSvc。
	// 测试复刻该接线：入站消息 → dispatchQQ → webhookSvc.ingressSvc(=bridge ingress)
	// → persist → triggerAIForEvent → aiTrigger(=webhookSvc).TriggerInboundAI → AI。
	webhookSvc := NewWebhookService(database)
	bridgeIngress := NewInboxIngressServiceWithDB(database, nil)
	bridgeIngress.SetAITrigger(webhookSvc)
	bridgeIngress.SetInboxService(NewInboxServiceWithDB(database))
	webhookSvc.SetIngressSvc(bridgeIngress)
	webhookSvc.SetSalesEngine(context.Background(), engine)
	webhookSvc.SetSmartOrchestrator(context.Background(), orch)
	// 渠道绑定智能体（生产为全局单例构造；测试注入测试 DB）
	webhookSvc.SetAgentBindingService(context.Background(), NewChannelAgentBindingServiceWithDB(database, NewAIAgentServiceWithDB(database)))

	// 真 gin 入口：只挂 QQ webhook 路由（等价 controller.RegisterRoutes 的 QQ 分支）
	gin.SetMode(gin.TestMode)
	g := gin.New()
	g.POST("/api/webhook/qq/:account_id", func(c *gin.Context) {
		// 与 controller.Receive 相同的 QQ Op13 短路与 Receive 请求构造
		channel := ChannelQQ
		accountID := c.Param("account_id")
		body, err := readAllBody(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"accepted": false, "reason": "read body: " + err.Error()})
			return
		}
		if handled, payload := webhookSvc.HandleQQCallbackChallenge(c.Request.Context(), accountID, body); handled {
			c.JSON(http.StatusOK, payload)
			return
		}
		reqCtx := c.Request.Context()
		result, _ := webhookSvc.Receive(reqCtx, &ReceiveRequest{
			Channel: channel, AccountID: accountID, Body: body,
			Headers: extractGinHeaders(c), SourceIP: c.ClientIP(), Query: nil,
		})
		status := http.StatusOK
		if result != nil && !result.Accepted {
			status = http.StatusBadRequest
			if result.VerifyFail {
				status = http.StatusUnauthorized
			}
		}
		c.JSON(status, result)
	})

	return &qqFullchainEnv{
		db: database, engine: g, platform: platform, botAPI: botAPI,
		webhook: webhookSvc, accountID: fmt.Sprintf("%d", acc.ID), secret: secret,
	}
}

// simIntentRecognizer 意图识别模拟信号：把含"价格"的消息识别为价格咨询，
// 其余为 greeting（保证 SOP 分支 Intent != unknown 可命中）。
type simIntentRecognizer struct{}

func (m *simIntentRecognizer) Recognize(_ context.Context, _, _, text string) (*dto.RecognizeResult, error) {
	if strings.Contains(text, "价格") {
		return &dto.RecognizeResult{IntentType: IntentPriceInquiry, Confidence: 0.9, Method: "sim"}, nil
	}
	return &dto.RecognizeResult{IntentType: IntentGreeting, Confidence: 0.6, Method: "sim"}, nil
}

func readAllBody(c *gin.Context) ([]byte, error) {
	defer c.Request.Body.Close()
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(c.Request.Body); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func extractGinHeaders(c *gin.Context) map[string]string {
	out := map[string]string{}
	for k, v := range c.Request.Header {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

// waitCond 轮询等待异步条件（AI 出站是异步 goroutine）
func waitCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timeout waiting: %s", msg)
}

// ---------------------------------------------------------------------------
// 用例 1：正向全链路 — 群 @机器人 → 验签 → 中台 → AI(FAQ SkipLLM) → 出站到模拟平台
// ---------------------------------------------------------------------------

func TestQQFullchain_GroupAtMessage_SignToReply(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	body := env.platform.GroupAtMessageBody("m-full-1", "G0SIMGRP", "M0USER1", "价格是多少")
	rec, resp := env.platform.PostWebhook(env.engine, env.accountID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook HTTP status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if resp["accepted"] != true {
		t.Fatalf("expected accepted=true, got %v", resp)
	}

	// 1) message_hub 落库（幂等键 = qq_evt_evt-m-full-1：协议层 qq_evt_ + 平台事件 id evt-m-full-1）
	var hub model.MessageHub
	waitCond(t, 10*time.Second, func() bool {
		return env.db.Where("platform = ? AND msg_id = ?", "qq", "qq_evt_evt-m-full-1").First(&hub).Error == nil
	}, "message_hub should persist qq inbound message")
	if !hub.IsGroup || hub.GroupID != "G0SIMGRP" || hub.SenderID != "M0USER1" {
		t.Errorf("hub fields mismatch: %+v", hub)
	}

	// 2) inbox 会话建立
	var convCount int64
	env.db.Model(&model.InboxConversation{}).Where("platform = ?", "qq").Count(&convCount)
	if convCount != 1 {
		t.Errorf("expected 1 qq inbox conversation, got %d", convCount)
	}

	// 3) AI 异步处理 → FAQ SkipLLM 回复 → 出站到模拟平台
	waitCond(t, 30*time.Second, func() bool {
		return env.platform.SentCount() > 0
	}, "sim platform should receive outbound reply")

	sent := env.platform.LastSent()
	if sent.GroupOpenID != "G0SIMGRP" {
		t.Errorf("reply should target group openid G0SIMGRP, got %q", sent.GroupOpenID)
	}
	if !strings.Contains(sent.Content, "999") {
		t.Errorf("reply should be FAQ answer (含 999), got %q", sent.Content)
	}
	if !strings.HasPrefix(sent.AuthHeader, "QQBot SIM-TOKEN-") {
		t.Errorf("auth header should be QQBot SIM-TOKEN-*, got %q", sent.AuthHeader)
	}
	if sent.MsgID != "qq_m-full-1" {
		t.Errorf("passive reply should carry msg_id=qq_m-full-1, got %q", sent.MsgID)
	}

	// 4) 出站回复落 message_hub
	var outHub model.MessageHub
	waitCond(t, 10*time.Second, func() bool {
		return env.db.Where("platform = ? AND direction = ?", "qq", "outbound").First(&outHub).Error == nil
	}, "outbound reply should persist to message_hub")
}

// ---------------------------------------------------------------------------
// 用例 2：单聊全链路
// ---------------------------------------------------------------------------

func TestQQFullchain_C2CMessage_Reply(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	body := env.platform.C2CAtMessageBody("m-c2c-1", "C0SIMUSER", "价格是多少")
	rec, resp := env.platform.PostWebhook(env.engine, env.accountID, body)
	if rec.Code != http.StatusOK || resp["accepted"] != true {
		t.Fatalf("webhook rejected: status=%d body=%s", rec.Code, rec.Body.String())
	}

	waitCond(t, 30*time.Second, func() bool { return env.platform.SentCount() > 0 }, "c2c reply outbound")
	sent := env.platform.LastSent()
	if sent.UserOpenID != "C0SIMUSER" {
		t.Errorf("c2c reply target should be C0SIMUSER, got %q", sent.UserOpenID)
	}

	var hub model.MessageHub
	if err := env.db.Where("platform = ? AND msg_id = ?", "qq", "qq_evt_evt-m-c2c-1").First(&hub).Error; err != nil {
		t.Fatalf("c2c hub row: %v", err)
	}
	if hub.IsGroup {
		t.Error("c2c message should not be group")
	}
}

// ---------------------------------------------------------------------------
// 用例 3：负向信号 — 篡改签名/缺头/错误时间戳 → 401，且不落库不触发 AI
// ---------------------------------------------------------------------------

func TestQQFullchain_TamperedSignature_Rejected(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	body := env.platform.GroupAtMessageBody("m-bad-1", "G0X", "M0X", "价格是多少")

	cases := []struct {
		name    string
		headers map[string]string
	}{
		{"wrong signature", map[string]string{
			"Content-Type": "application/json",
			"X-Signature-Ed25519": func() string {
				other := NewSimQQPlatform(t, "other-secret")
				return other.sign("1728000000", body)
			}(),
			"X-Signature-Timestamp": "1728000000",
		}},
		{"missing headers", map[string]string{"Content-Type": "application/json"}},
		{"garbage hex", map[string]string{
			"Content-Type":          "application/json",
			"X-Signature-Ed25519":   "zz-not-hex",
			"X-Signature-Timestamp": "1",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := fmt.Sprintf("%d", time.Now().Unix())
			hdrs := map[string]string{}
			for k, v := range tc.headers {
				hdrs[k] = v
			}
			_ = ts
			req := httptest.NewRequest(http.MethodPost, "/api/webhook/qq/"+env.accountID, bytes.NewReader(body))
			for k, v := range hdrs {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			env.engine.ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	// 负向信号不得产生任何业务副作用
	waitCond(t, 3*time.Second, func() bool { return true }, "")
	var hubCount int64
	env.db.Model(&model.MessageHub{}).Where("platform = ?", "qq").Count(&hubCount)
	if hubCount != 0 {
		t.Errorf("tampered signals must not persist, got %d hub rows", hubCount)
	}
	if env.platform.SentCount() != 0 {
		t.Errorf("tampered signals must not trigger AI reply, got %d", env.platform.SentCount())
	}
}

// ---------------------------------------------------------------------------
// 用例 4：重放幂等 — 同一事件二次推送不重复落库、不重复回复
// ---------------------------------------------------------------------------

func TestQQFullchain_ReplayIdempotent(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	body := env.platform.GroupAtMessageBody("m-replay-1", "G0RP", "M0RP", "价格是多少")
	rec1, _ := env.platform.PostWebhook(env.engine, env.accountID, body)
	rec2, _ := env.platform.PostWebhook(env.engine, env.accountID, body)
	if rec1.Code != http.StatusOK || rec2.Code != http.StatusOK {
		t.Fatalf("both pushes should HTTP 200, got %d %d", rec1.Code, rec2.Code)
	}
	{
		var resp map[string]any
		_ = json.Unmarshal([]byte(rec2.Body.String()), &resp)
		if resp["duplicate"] != true {
			t.Errorf("second push should be marked duplicate, got %v (body=%s)", resp["duplicate"], rec2.Body.String())
		}
	}

	waitCond(t, 30*time.Second, func() bool { return env.platform.SentCount() > 0 }, "first push should reply")
	time.Sleep(500 * time.Millisecond) // 给可能的重复回复留窗口
	var hubCount int64
	env.db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "qq", "inbound").Count(&hubCount)
	if hubCount != 1 {
		t.Errorf("replay must not duplicate inbound, got %d", hubCount)
	}
	if env.platform.SentCount() != 1 {
		t.Errorf("replay must not duplicate AI reply, got %d outbound", env.platform.SentCount())
	}
}

// ---------------------------------------------------------------------------
// 用例 5：Op13 回调地址验证 — 签名可回验
// ---------------------------------------------------------------------------

func TestQQFullchain_Op13Challenge(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	body := env.platform.Op13Body("sim-plain-token")
	req := httptest.NewRequest(http.MethodPost, "/api/webhook/qq/"+env.accountID, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	env.engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("op13 should 200, got %d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("op13 response json: %v", err)
	}
	if resp["plain_token"] != "sim-plain-token" {
		t.Errorf("plain_token mismatch: %v", resp["plain_token"])
	}
	sig, _ := resp["signature"].(string)
	if len(sig) != 128 {
		t.Fatalf("signature hex len = %d, want 128", len(sig))
	}
	// 回验：用模拟平台的派生公钥验证
	pub := qq.DerivePrivateKey(env.secret).Public().(ed25519.PublicKey)
	raw, _ := hex.DecodeString(sig)
	eventTS := ""
	_ = json.Unmarshal(body, &map[string]any{})
	var op13 struct {
		EventTS string `json:"event_ts"`
	}
	_ = json.Unmarshal(body, &op13)
	eventTS = op13.EventTS
	if !ed25519.Verify(pub, []byte(eventTS+"sim-plain-token"), raw) {
		t.Error("op13 signature should verify against derived public key")
	}
}

// ---------------------------------------------------------------------------
// 用例 6：msg_seq 递增 — 同一 msg_id 多次被动回复 seq 单调
// ---------------------------------------------------------------------------

func TestQQFullchain_MsgSeqMonotonic(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	integration := NewQQIntegrationService(env.db)
	if seq := integration.nextMsgSeq("seq-msg"); seq != 1 {
		t.Errorf("first seq = %d", seq)
	}
	if seq := integration.nextMsgSeq("seq-msg"); seq != 2 {
		t.Errorf("second seq = %d", seq)
	}
	if seq := integration.nextMsgSeq("seq-msg-2"); seq != 1 {
		t.Errorf("new msg seq = %d", seq)
	}
}

// ---------------------------------------------------------------------------
// 用例 7：AI 开关关闭 — 消息落库但不触发 AI、不出站
// ---------------------------------------------------------------------------

func TestQQFullchain_AIDisabled_NoOutbound(t *testing.T) {
	env := setupQQFullchain(t)
	defer env.webhook.Stop(context.Background())

	// 关闭 AI 开关
	env.db.Model(&model.QQAccount{}).Where("id = ?", env.accountID).Update("ai_agent_enabled", false)

	body := env.platform.GroupAtMessageBody("m-noai-1", "G0NOAI", "M0NOAI", "价格是多少")
	rec, resp := env.platform.PostWebhook(env.engine, env.accountID, body)
	if rec.Code != http.StatusOK || resp["accepted"] != true {
		t.Fatalf("webhook should still accept, got %d", rec.Code)
	}

	var hub model.MessageHub
	waitCond(t, 10*time.Second, func() bool {
		return env.db.Where("platform = ? AND msg_id = ?", "qq", "qq_evt_evt-m-noai-1").First(&hub).Error == nil
	}, "message should persist even with AI disabled")

	time.Sleep(1 * time.Second)
	if env.platform.SentCount() != 0 {
		t.Errorf("AI disabled should not produce outbound, got %d", env.platform.SentCount())
	}
	var outCount int64
	env.db.Model(&model.MessageHub{}).Where("platform = ? AND direction = ?", "qq", "outbound").Count(&outCount)
	if outCount != 0 {
		t.Errorf("AI disabled should not persist outbound, got %d", outCount)
	}
}

// ---------------------------------------------------------------------------
// 用例 8：中台白名单 — "qq" 必须是合法平台（白名单接线的回归守卫）
// ---------------------------------------------------------------------------

func TestQQFullchain_PlatformWhitelist(t *testing.T) {
	if !ValidPlatform("qq") {
		t.Fatal("qq should be a valid platform in messageHubPlatforms")
	}
	if NormalizeChannelType("qq") != string(model.ChannelTypeQQ) {
		t.Errorf("NormalizeChannelType(qq) = %q", NormalizeChannelType("qq"))
	}
	_ = featureflag.Get("layer1") // 引用防 import 裁剪
}
