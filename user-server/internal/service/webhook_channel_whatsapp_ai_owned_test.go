package service

import (
	"context"
	"strconv"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestDispatchWhatsApp_IngressDoesNotTriggerAI 锁住 WhatsApp 入站的单一 AI 触发归属。
//
// 与 2026-09-19 TG 实测到的双触发同形：dispatchWhatsApp 之后 handleJob 会按账号
// AI 开关 triggerSalesEngine，若中台 Ingress 也触发一次，同一条 wamid 就会跑出两份
// 回复并各自真实投递（两条路径的去重键不同，互相挡不住）。
func TestDispatchWhatsApp_IngressDoesNotTriggerAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	mc := newIsolatedCacheForTest(t)
	ing := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	ing.SetAITrigger(tr)
	svc.SetIngressSvc(ing)

	// 时间戳用"刚刚"：入站超过 5 分钟会被中台判定为历史消息而不触发 AI，
	// 那样本用例的"没触发"就分不清是标记生效还是窗口拦截，反向对照也会失真。
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	body := waBatchBody(
		`{"from":"+8613800000009","id":"wamid.OWN1","timestamp":"`+ts+`","type":"text","text":{"body":"这条只该触发一次"}}`,
		`{"profile":{"name":"Bob"},"wa_id":"+8613800000009"}`,
	)
	hub, err := svc.dispatchWhatsApp(context.Background(), "1", &ParsedPayload{EventID: "evt-wa-owned-1"}, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil || hub.ConversationID == "" {
		t.Fatalf("应返回入站 hub，got %+v", hub)
	}

	var cnt int64
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", "wamid.OWN1").Count(&cnt).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if cnt != 1 {
		t.Fatalf("抑制 AI 触发不得影响落库，期望 1 行实际 %d", cnt)
	}

	// 触发链可能落在异步收尾（防抖窗口 / goroutine），先给窗口再断言"没触发"。
	time.Sleep(300 * time.Millisecond)
	if tr.called != 0 {
		t.Fatalf("WhatsApp 入站不应由中台 Ingress 触发 AI（渠道 dispatch 才是唯一归属），实际 %d 次", tr.called)
	}
	// 不触发 AI 的同时必须留下「AI 处理中」标记，否则并发入站会被 recheck 当孤儿补触发。
	if exists, _ := mc.Exists(context.Background(), InboxAIProcessingKey+hub.ConversationID); !exists {
		t.Fatal("渠道自管分支应设置 ai_processing 标记，实际未设置")
	}

	// 反向对照：同一条消息若不带标记（桥接/其他入口），必须照常触发 AI，
	// 否则本用例等于把触发权整体掐掉而不是移交。换一个新会话：上一条已留下 ai_processing
	// 排他标记，同会话会被"AI 已在进行中"挡下，测不到标记本身的作用。
	if _, err := ing.HandleIngressMessage(context.Background(), &model.MessageEvent{
		EventID:        "wamid.OWN2",
		Channel:        "whatsapp",
		ConversationID: "+8613800000010",
		SenderID:       "+8613800000010",
		Content:        "无标记时应照常触发",
		Timestamp:      time.Now(),
	}); err != nil {
		t.Fatalf("HandleIngressMessage: %v", err)
	}
	for i := 0; i < 40 && tr.called == 0; i++ {
		time.Sleep(25 * time.Millisecond)
	}
	if tr.called != 1 {
		t.Fatalf("无标记入站应触发 AI 1 次，实际 %d 次", tr.called)
	}
}
