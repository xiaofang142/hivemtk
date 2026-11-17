package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type fakeAITrigger struct {
	called      int
	lastChannel string
	lastAccount string
	lastConv    string
	lastCust    string
	lastContent string
	lastEventID string
	lastMeta    *TriggerInboundMeta
}

func (f *fakeAITrigger) TriggerInboundAI(ctx context.Context, channel, accountID, conversationID, customerID, content, eventID string, opts ...TriggerInboundOption) {
	f.called++
	f.lastChannel = channel
	f.lastAccount = accountID
	f.lastConv = conversationID
	f.lastCust = customerID
	f.lastContent = content
	f.lastEventID = eventID
	meta := &TriggerInboundMeta{}
	for _, opt := range opts {
		if opt != nil {
			opt(meta)
		}
	}
	f.lastMeta = meta
}

// TestInbox_NewMessageTriggersAI_HistoryDoesNot 验证两种实际需求的核心语义：
//  1. 实时新消息（inbound）必须触发 AI 客服
//  2. 历史消息（history）必须仅落库、不触发 AI
//
// 使用 nil DB（hubRepo==nil 时持久化 no-op），无需真实 Postgres 即可密封验证。
func TestInbox_NewMessageTriggersAI_HistoryDoesNot(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	inEv := &model.MessageEvent{
		EventID:        "e1",
		Channel:        "douyin",
		ConversationID: "conv1",
		SenderID:       "cust1",
		Content:        "你好，我要咨询",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), inEv); err != nil {
		t.Fatalf("HandleIngressMessage(inbound) error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("期望新消息触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastContent != "你好，我要咨询" || tr.lastConv != "conv1" || tr.lastAccount != "acc1" || tr.lastEventID != "e1" {
		t.Fatalf("AI 触发参数错误: %+v", tr)
	}

	histEv := &model.MessageEvent{
		EventID:        "e2",
		Channel:        "douyin",
		ConversationID: "conv1",
		SenderID:       "cust1",
		Content:        "历史记录一条",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if err := svc.PersistBridgeHistory(context.Background(), histEv, "inbound"); err != nil {
		t.Fatalf("PersistBridgeHistory error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("历史消息不应触发 AI；期望调用次数仍为 1，实际 %d", tr.called)
	}
}

// TestInbox_GroupMessage_TriggersAI_WithMeta 验证群聊消息触发 AI 时透传 sender_name/群元数据
// （需求3：群聊成员身份不得在 AI 编排路径丢失）。
func TestInbox_GroupMessage_TriggersAI_WithMeta(t *testing.T) {
	t.Setenv("AI_DEBOUNCE_SECONDS", "0")
	svc := NewInboxIngressService()
	tr := &fakeAITrigger{}
	svc.SetAITrigger(tr)

	groupEv := &model.MessageEvent{
		EventID:        "g1",
		Channel:        "xiaohongshu",
		ConversationID: "group-1",
		SenderID:       "group-1",
		SenderName:     "张三",
		Content:        "@客服 帮我查一下订单",
		IsGroup:        true,
		GroupID:        "group-1",
		Extra:          map[string]any{"account_id": "acc1", "group_name": "产品交流群", "sender_type": "customer"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), groupEv); err != nil {
		t.Fatalf("HandleIngressMessage(group) error: %v", err)
	}
	if tr.called != 1 {
		t.Fatalf("期望群聊消息触发 AI 1 次，实际 %d", tr.called)
	}
	if tr.lastMeta == nil {
		t.Fatal("群聊消息应透传 TriggerInboundMeta（非 nil）")
	}
	if tr.lastMeta.SenderName != "张三" {
		t.Fatalf("SenderName 透传错误: %q", tr.lastMeta.SenderName)
	}
	if !tr.lastMeta.IsGroup {
		t.Fatal("IsGroup 应透传为 true")
	}
	if tr.lastMeta.GroupID != "group-1" || tr.lastMeta.GroupName != "产品交流群" {
		t.Fatalf("群元数据透传错误: %+v", tr.lastMeta)
	}
	tr.called = 0
	tr.lastMeta = nil
	oneEv := &model.MessageEvent{
		EventID:        "o1",
		Channel:        "douyin",
		ConversationID: "cust-1",
		SenderID:       "cust-1",
		Content:        "在吗",
		Extra:          map[string]any{"account_id": "acc1"},
		Timestamp:      time.Now(),
	}
	if _, err := svc.HandleIngressMessage(context.Background(), oneEv); err != nil {
		t.Fatalf("HandleIngressMessage(1:1) error: %v", err)
	}
	if tr.lastMeta != nil && (tr.lastMeta.IsGroup || tr.lastMeta.GroupID != "") {
		t.Fatalf("1:1 消息不应带群元数据: %+v", tr.lastMeta)
	}
}

// TestIsDuplicateKey 验证唯一键冲突幂等判定（重扫/断线重发同一 event_id 时应视为已落库）。
func TestIsDuplicateKey(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{name: "Postgres duplicate key", err: errors.New(`ERROR: duplicate key value violates unique constraint "message_hub_msg_id_key" (SQLSTATE 23505)`), want: true},
		{name: "Postgres unique constraint 中文/通用", err: errors.New(`pq: duplicate key value violates unique constraint "uk"`), want: true},
		{name: "gorm ErrDuplicatedKey", err: gorm.ErrDuplicatedKey, want: true},
		{name: "其他数据库错误", err: errors.New("connection refused"), want: false},
		{name: "nil 错误", err: nil, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDuplicateKey(tc.err); got != tc.want {
				t.Fatalf("isDuplicateKey(%v) = %v, 期望 %v", tc.err, got, tc.want)
			}
		})
	}
}
