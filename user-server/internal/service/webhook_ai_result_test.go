package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

type resultExitIntent struct {
	calls int
	run   func(context.Context, string) (*dto.RecognizeResult, error)
}

type resultExitLockValueCache struct {
	cache.Cache
	value string
	err   error
}

func (c *resultExitLockValueCache) Get(context.Context, string) (string, error) {
	return c.value, c.err
}

func TestWebhookAIResult_MissingLockScopeMustNotSend(t *testing.T) {
	for _, missing := range []string{"service", "session"} {
		t.Run(missing, func(t *testing.T) {
			ws, baseCtx := newBridgeSentTestService(t)
			t.Cleanup(func() { ws.Stop(baseCtx) })
			ctx := tenant.WithTenantID(baseCtx, c01TenantA)
			result := &HandleResult{SessionID: "missing-scope-session", AIReplied: true, Reply: "合成正常答案"}
			if missing == "service" {
				ws.ingressSvc = nil
			} else {
				result.SessionID = ""
			}
			hub := bridgeInboundHub("douyin", "missing-scope-account", "missing-scope-conversation", "missing-scope-customer")
			hub.TenantID = c01TenantA
			eventID := "missing-lock-scope-" + missing
			releaseReplyClaimForTest(t, eventID)
			ws.deliverAIResult(ctx, webhookReplyRequest{channel: ChannelDouyin, accountID: hub.AccountID, hub: hub,
				payload: &ParsedPayload{EventID: eventID, Sender: hub.SenderID, ChatID: hub.ConversationID},
			}, result, nil)
			var count int64
			if err := ws.eventRepo.GetDB().Model(&model.MessageHub{}).Where("direction = ? AND status = ?", "outbound", "pending").Count(&count).Error; err != nil {
				t.Fatalf("查询实际投递失败：%v", err)
			}
			if count != 0 {
				t.Fatalf("锁状态缺可靠前置不能外发，实际=%d", count)
			}
		})
	}
}

func TestWebhookAIResult_PresendLockMustBeConfirmed(t *testing.T) {
	cases := []struct {
		name        string
		lockCache   cache.Cache
		wantPending int64
	}{
		{name: "读锁故障不能抢答", lockCache: &resultExitLockValueCache{err: errors.New("合成读锁故障")}},
		{name: "缓存缺失不能确认锁", lockCache: nil},
		{name: "未知锁值不能当未接管", lockCache: &resultExitLockValueCache{value: "unconfirmed"}},
		{name: "明确已接管拒发", lockCache: &resultExitLockValueCache{value: "true"}},
		{name: "明确未接管仍正常发送", lockCache: &resultExitLockValueCache{value: "false"}, wantPending: 1},
		{name: "明确未命中不是故障", lockCache: &resultExitLockValueCache{err: cache.ErrCacheMiss}, wantPending: 1},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, baseCtx := newBridgeSentTestService(t)
			t.Cleanup(func() { ws.Stop(baseCtx) })
			ctx := tenant.WithTenantID(baseCtx, c01TenantA)
			// Get 是故障边界；其他真实缓存方法仍可用于发送收尾，避免 nil 内嵌接口造成假红。
			if fixture, ok := tc.lockCache.(*resultExitLockValueCache); ok {
				memory := cache.NewMemoryCache()
				t.Cleanup(memory.Close)
				fixture.Cache = memory
			}
			ws.ingressSvc.cache = tc.lockCache
			hub := bridgeInboundHub("douyin", "presend-account", "presend-conversation", "presend-customer")
			hub.TenantID = c01TenantA
			eventID := fmt.Sprintf("presend-confirm-%d", index)
			releaseReplyClaimForTest(t, eventID)
			ws.deliverAIResult(ctx, webhookReplyRequest{
				channel: ChannelDouyin, accountID: hub.AccountID, hub: hub,
				payload: &ParsedPayload{EventID: eventID, Sender: hub.SenderID, ChatID: hub.ConversationID},
			}, &HandleResult{SessionID: "presend-session", AIReplied: true, Reply: "针对合成问题的正常答案"}, nil)
			var count int64
			if err := ws.eventRepo.GetDB().Model(&model.MessageHub{}).Where("direction = ? AND status = ?", "outbound", "pending").Count(&count).Error; err != nil {
				t.Fatalf("查询可投递答案失败：%v", err)
			}
			if count != tc.wantPending {
				t.Fatalf("只有锁状态可确认才允许正常答案：期望=%d 实际=%d", tc.wantPending, count)
			}
		})
	}
}

func TestWebhookAIResult_SafeExitBoundaries(t *testing.T) {
	cases := []struct {
		name        string
		result      *HandleResult
		recreated   *HandleResult
		generateErr error
		duplicate   bool
		wantReply   string
		wantCalls   int
	}{
		{name: "空结果不投递"},
		{name: "已在人工手里无新公告不重复通知", result: &HandleResult{Transferred: true, HandlerType: model.HandlerTypeHuman}},
		{name: "人工持有不能把内部答复当公告", result: &HandleResult{HandlerType: model.HandlerTypeHuman, AIReplied: true, Reply: "内部处置理由"}},
		{name: "新转接仅投递对客公告", result: &HandleResult{Transferred: true, TransferNotice: agent_runtime.HandoffQueueNotice, Reply: "内部投诉否决理由"}, wantReply: agent_runtime.HandoffQueueNotice},
		{name: "控制字忽略大小写及空白", result: &HandleResult{AIReplied: true, Reply: "  no_reply \n"}},
		{name: "没有获准回答不投递", result: &HandleResult{Reply: "未获准的候选答案"}},
		{name: "空回答不投递", result: &HandleResult{AIReplied: true}},
		{name: "第二次仍重复只保留审计", result: &HandleResult{AIReplied: true, Reply: "已经送达的回答"}, recreated: &HandleResult{AIReplied: true, Reply: "已经送达的回答"}, duplicate: true, wantCalls: 1},
		{name: "重生成错误不误投原回答", result: &HandleResult{AIReplied: true, Reply: "已经送达的回答"}, generateErr: errors.New("合成生成错误"), duplicate: true, wantCalls: 1},
		{name: "重生成返回空结果不误投", result: &HandleResult{AIReplied: true, Reply: "已经送达的回答"}, duplicate: true, wantCalls: 1},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, baseCtx := newBridgeSentTestService(t)
			t.Cleanup(func() { ws.Stop(baseCtx) })
			ctx := tenant.WithTenantID(baseCtx, c01TenantA)
			database := ws.eventRepo.GetDB()
			// 正常结果边界具有明确会话；缺身份的拒发另有目标回归。
			if tc.result != nil {
				tc.result.SessionID = fmt.Sprintf("result-boundary-session-%d", index)
			}
			if tc.recreated != nil {
				tc.recreated.SessionID = fmt.Sprintf("result-boundary-session-%d", index)
			}
			account, conv, eventID := "result-boundary-account", "result-boundary-conversation", fmt.Sprintf("result-boundary-%d", index)
			hub := bridgeInboundHub("douyin", account, conv, "result-boundary-customer")
			hub.TenantID = c01TenantA
			if tc.duplicate {
				if err := database.Create(&model.MessageHub{
					TenantID: c01TenantA, Platform: "douyin", AccountID: account, ConversationID: conv,
					Direction: "outbound", Status: "delivered", MsgType: "text", Content: tc.result.Reply,
					MsgID: ContentHashMsgID("douyin", conv, tc.result.Reply), SentAt: time.Now(),
				}).Error; err != nil {
					t.Fatalf("准备重复回答失败：%v", err)
				}
			}
			releaseReplyClaimForTest(t, eventID)
			calls := 0
			ws.deliverAIResult(ctx, webhookReplyRequest{
				channel: ChannelDouyin, accountID: account, hub: hub,
				payload:  &ParsedPayload{EventID: eventID, ChatID: conv, Sender: hub.SenderID, Content: "合成客户问题"},
				incoming: &IncomingContext{Platform: "douyin", MessageID: eventID},
			}, tc.result, func(_ context.Context, incoming *IncomingContext, _ *AgentContext) (*HandleResult, error) {
				calls++
				if !incoming.NoFastpath || incoming.MessageID != eventID {
					t.Fatal("重生成必须关闭快答并保持入站身份")
				}
				return tc.recreated, tc.generateErr
			})
			if calls != tc.wantCalls {
				t.Fatalf("生成次数必须有界：期望=%d 实际=%d", tc.wantCalls, calls)
			}
			var rows []model.MessageHub
			if err := database.Where("conversation_id = ? AND direction = ? AND status = ?", conv, "outbound", "pending").Find(&rows).Error; err != nil {
				t.Fatalf("查询可投递结果失败：%v", err)
			}
			if tc.wantReply == "" {
				if len(rows) != 0 {
					t.Fatalf("不应有可投递结果：%+v", rows)
				}
			} else if len(rows) != 1 || rows[0].Content != tc.wantReply {
				t.Fatalf("只允许正确对客公告入队：%+v", rows)
			}
		})
	}
}

func (f *resultExitIntent) Recognize(ctx context.Context, sessionID, customerID, text string) (*dto.RecognizeResult, error) {
	f.calls++
	return f.run(ctx, sessionID)
}

func TestWebhookAIResult_RecreatedAnswerUsesSafeExit(t *testing.T) {
	cases := []struct {
		name       string
		intent     string
		modelReply string
		takeover   bool
		wantReply  string
	}{
		{name: "重跑投诉否决只发送真实排队公告", intent: IntentComplaint, wantReply: agent_runtime.HandoffQueueNotice},
		{name: "重跑NO_REPLY不发送控制字", intent: IntentGreeting, modelReply: "NO_REPLY"},
		{name: "重跑期间接管不抢答", intent: IntentGreeting, modelReply: "这是一条针对追问的新答案。", takeover: true},
		{name: "未接管的新答案仍正常入队", intent: IntentGreeting, modelReply: "这是一条针对追问的新答案。", wantReply: "这是一条针对追问的新答案。"},
	}
	for index, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			intent := &resultExitIntent{}
			ws, orch, database := c01SetupAI(t, &fakeRAGSearcher{}, intent)
			// 单机夹具的会话/消息由默认租户写入；上下文与行身份必须一致。
			fixtureTenant := tenant.DefaultTenantID
			ctx := tenant.WithTenantID(context.Background(), fixtureTenant)
			conv := "result-exit-conversation"
			sender := "result-exit-customer"
			// 模型缓存按 prompt 共享；合成问题区分场景，避免 NO_REPLY 缓存污染正常对照。
			question := fmt.Sprintf("标准版部署配置场景%d是什么？", index)
			if !faqFastQuestionSafe(question) {
				t.Fatal("首次快答对照必须具有安全的完整标准问题")
			}
			firstReply := "已投递过的标准硬件介绍。"
			eventID := "result-exit-event"
			// 每个子用例重建专用测试库；账号区分以免共享确认缓存影响其他用例。
			accountID := []string{"result-exit-handoff", "result-exit-no-reply", "result-exit-lock", "result-exit-normal"}[index]
			hub := c01InboundHub(eventID, conv, sender, accountID, question)
			hub.TenantID = fixtureTenant
			p := &ParsedPayload{EventID: eventID, Sender: sender, Content: question, ChatID: conv}
			original := &model.MessageHub{
				TenantID: fixtureTenant, Platform: "douyin", AccountID: accountID,
				ConversationID: conv, Direction: "outbound", Status: "delivered", MsgType: "text",
				MsgID: ContentHashMsgID("douyin", conv, firstReply), Content: firstReply, SentAt: time.Now(),
			}
			if err := database.Create(original).Error; err != nil {
				t.Fatalf("准备已经送达的标准答案失败：%v", err)
			}
			enabled, agentID := true, uint(99)
			orch.engine.SetFAQService(newFAQServiceForFastPath([]model.FAQEntry{{
				ID: 99, AgentID: &agentID, Enabled: &enabled, Question: question, Answer: firstReply,
				Keywords: []string{"标准版", "部署", "配置"}, Confidence: 0.99, Intent: IntentGreeting,
			}}))
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if err := json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{
					"message": map[string]any{"role": "assistant", "content": tc.modelReply},
				}}}); err != nil {
					t.Errorf("写入合成模型响应失败：%v", err)
				}
			}))
			t.Cleanup(srv.Close)
			orch.engine.dispatcher = c01DispatcherAt(t, srv.URL, "result-exit-model")
			lockedSession := ""
			intent.run = func(callCtx context.Context, sessionID string) (*dto.RecognizeResult, error) {
				if tc.takeover {
					if err := ws.ingressSvc.LockSessionForHuman(callCtx, sessionID, "结果出口回归：人工接管"); err != nil {
						t.Fatalf("合成接管窗口无法建立人工锁：%v", err)
					}
					lockedSession = sessionID
				}
				return &dto.RecognizeResult{IntentType: tc.intent, Confidence: 0.99}, nil
			}
			t.Cleanup(func() {
				if lockedSession != "" {
					if err := ws.ingressSvc.UnlockSessionForHuman(ctx, lockedSession); err != nil {
						t.Errorf("清理本用例人工锁失败：%v", err)
					}
				}
			})
			agent := &AgentContext{AgentID: agentID, MaxTokens: 512, ConfidenceThreshold: 0.7}
			incoming := &IncomingContext{
				Platform: "douyin", AccountID: accountID, SenderID: sender, OneID: sender,
				Content: question, MessageID: eventID,
			}
			session, err := orch.findOrCreateSession(ctx, incoming)
			if err != nil || orch.faqHasPriorAnswer(ctx, incoming, session) {
				t.Fatalf("首次快答必须具有同租户权威新会话：%v", err)
			}
			incoming.SessionID = session.SessionID
			releaseReplyClaimForTest(t, eventID)
			ws.runAIGeneration(ctx, ChannelDouyin, accountID, p, hub, incoming, agent)
			if intent.calls != 1 {
				t.Fatalf("标准答案先命中且只允许一次完整重生成：意图调用=%d", intent.calls)
			}
			var audit model.MessageHub
			if err := database.Where("tenant_id = ? AND account_id = ? AND msg_id = ? AND direction = ?",
				fixtureTenant, accountID, original.MsgID+":dup", "outbound").First(&audit).Error; err != nil {
				t.Fatalf("必须真的经历首次快答重复再重生成，不能把首次完整生成伪装为重跑：%v", err)
			}
			if audit.Status != "failed" || audit.PushAttempts != 0 {
				t.Fatal("首次重复审计不得变成可投递消息")
			}
			var pending []model.MessageHub
			if err := database.Where("conversation_id = ? AND direction = ? AND status = ?", conv, "outbound", "pending").Find(&pending).Error; err != nil {
				t.Fatalf("查询可投递答案失败：%v", err)
			}
			if tc.wantReply == "" {
				if len(pending) != 0 {
					t.Fatalf("安全出口应无可投递答案：实际=%d 首条=%q", len(pending), pending[0].Content)
				}
			} else if len(pending) != 1 || pending[0].Content != tc.wantReply {
				t.Fatalf("安全出口应只入队正确答案/公告：期望=%q 实际=%+v", tc.wantReply, pending)
			}
			if lockedSession != "" {
				locked, err := ws.ingressSvc.IsSessionHumanLocked(ctx, lockedSession)
				if err != nil || !locked {
					t.Fatalf("结果出口不得解除人工锁：持有=%v 错误=%v", locked, err)
				}
			}
		})
	}
}
