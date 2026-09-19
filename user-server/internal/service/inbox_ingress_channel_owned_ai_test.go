package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// TestIngress_WithChannelOwnedAITrigger_PersistsButSkipsAI 验证：
// webhook 原生 dispatch 已自行负责 AI 触发（含群门控）时，中台入站只落库不再二次触发，
// 否则同一条 TG 消息会被两条路径各生成一次回复（2026-09-19 实测：1 条消息发出 3 条回复）。
func TestIngress_WithChannelOwnedAITrigger_PersistsButSkipsAI(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	mc := cache.NewMemoryCache()
	defer mc.Close()

	svc := NewInboxIngressServiceWithDB(db, mc)
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	ev := &model.MessageEvent{
		EventID:        "tg_upd_suppress_1",
		Channel:        "telegram",
		ConversationID: "8608488936",
		SenderID:       "8608488936",
		Content:        "渠道门控自管触发的消息",
		Extra:          map[string]any{"account_id": "5"},
		Timestamp:      time.Now(),
	}
	res, err := svc.HandleIngressMessage(WithChannelOwnedAITrigger(context.Background()), ev)
	if err != nil {
		t.Fatalf("HandleIngressMessage error: %v", err)
	}
	if tr.called != 0 {
		t.Fatalf("通道自管触发时中台不应再触发 AI，实际 %d 次", tr.called)
	}
	if !res.Accepted || res.QueuedForAI {
		t.Fatalf("应accepted=true queued=false，实际 %+v", res)
	}

	var count int64
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", ev.EventID).Count(&count).Error; err != nil {
		t.Fatalf("统计落库行失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("抑制 AI 触发不能影响落库，期望 message_hub 1 行，实际 %d", count)
	}

	// 抑制 AI 的同时必须保留「AI 处理中」排他标记：渠道 dispatch 随即开始推理，
	// 标记缺失会让并发消息被 recheck 当成孤儿补触发。
	aiKey := InboxAIProcessingKey + ev.ConversationID
	if exists, _ := mc.Exists(context.Background(), aiKey); !exists {
		t.Fatal("渠道自管触发分支应设置 ai_processing 标记，实际未设置")
	}
}

// TestIngress_WithoutMarker_StillTriggersAI 反向对照：未打标记的入站（桥接/其他渠道）行为不变。
func TestIngress_WithoutMarker_StillTriggersAI(t *testing.T) {
	svc := NewInboxIngressServiceWithDB(nil, newIsolatedCacheForTest(t))
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	ev := &model.MessageEvent{
		EventID:        "bridge_no_marker_1",
		Channel:        "telegram",
		ConversationID: "conv-no-marker",
		SenderID:       "cust1",
		Content:        "桥接消息应照常触发 AI",
		Extra:          map[string]any{"account_id": "5"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), ev); err != nil {
		t.Fatalf("HandleIngressMessage error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("无标记时应触发 AI 1 次，实际 %d", tr.called)
	}
}
