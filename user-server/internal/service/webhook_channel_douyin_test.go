package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func newDouyinGenericTestService(t *testing.T) (*WebhookService, context.Context) {
	t.Helper()
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	return NewWebhookService(db), context.Background()
}

// TestDispatchDouyinGeneric_MsgIDStable 相同内容重推 → MsgID 稳定（幂等键生效）
func TestDispatchDouyinGeneric_MsgIDStable(t *testing.T) {
	svc, ctx := newDouyinGenericTestService(t)
	defer svc.Stop(ctx)

	p1 := &ParsedPayload{EventID: "evt-dy-g1", Sender: "user_001", Content: "你们产品多少钱"}
	hub1, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p1, []byte("{not-json"))
	if err != nil {
		t.Fatalf("dispatch1: %v", err)
	}
	if hub1 == nil {
		t.Fatal("expected hub from generic branch")
	}

	p2 := &ParsedPayload{EventID: "evt-dy-g2", Sender: "user_001", Content: "你们产品多少钱"}
	hub2, _, err := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p2, []byte("{not-json"))
	if err != nil {
		t.Fatalf("dispatch2: %v", err)
	}
	if hub2 == nil {
		t.Fatal("expected second hub")
	}

	if hub1.MsgID != hub2.MsgID {
		t.Errorf("W-5 未达成：相同内容两次投递 MsgID 不一致 %q vs %q（时间戳残留）", hub1.MsgID, hub2.MsgID)
	}
	if !strings.HasPrefix(hub1.MsgID, "dy_7_generic_") || !strings.Contains(hub1.MsgID, "mh:") {
		t.Errorf("MsgID 应为「平台_账号_generic_内容哈希」形态 dy_7_generic_mh:*，实际 %q", hub1.MsgID)
	}

	wantSuffix := ContentHashMsgID("douyin", "user_001", "你们产品多少钱")
	if hub1.MsgID != "dy_7_generic_"+wantSuffix {
		t.Errorf("MsgID expected dy_7_generic_%s, got %s", wantSuffix, hub1.MsgID)
	}
}

// TestDispatchDouyinGeneric_DifferentContentDifferentID 不同内容 → 不同 ID
func TestDispatchDouyinGeneric_DifferentContentDifferentID(t *testing.T) {
	svc, ctx := newDouyinGenericTestService(t)
	defer svc.Stop(ctx)

	p1 := &ParsedPayload{EventID: "evt-dy-c1", Sender: "user_002", Content: "内容甲"}
	h1, _, _ := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p1, []byte("{not-json"))
	p2 := &ParsedPayload{EventID: "evt-dy-c2", Sender: "user_002", Content: "内容乙"}
	h2, _, _ := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p2, []byte("{not-json"))

	if h1 == nil || h2 == nil {
		t.Fatal("expected both hubs")
	}
	if h1.MsgID == h2.MsgID {
		t.Errorf("不同内容不应共享 MsgID: %s", h1.MsgID)
	}
}

// TestDispatchDouyinGeneric_EmptyContentStable 空内容兜底文案也应产生稳定 ID
func TestDispatchDouyinGeneric_EmptyContentStable(t *testing.T) {
	svc, ctx := newDouyinGenericTestService(t)
	defer svc.Stop(ctx)

	p1 := &ParsedPayload{EventID: "evt-dy-e1", Sender: "user_003"}
	h1, _, _ := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p1, []byte("{not-json"))
	p2 := &ParsedPayload{EventID: "evt-dy-e2", Sender: "user_003"}
	h2, _, _ := svc.dispatchDouyin(ctx, ChannelDouyin, "7", p2, []byte("{not-json"))

	if h1 == nil || h2 == nil {
		t.Fatal("expected both hubs")
	}
	if h1.Content != "[douyin generic event]" {
		t.Errorf("expected fallback content, got %q", h1.Content)
	}
	if h1.MsgID != h2.MsgID {
		t.Errorf("空内容兜底 MsgID 仍应稳定: %q vs %q", h1.MsgID, h2.MsgID)
	}
}

// TestDispatchDouyinStructured_MissingMessageIDStable /
// TestDispatchDouyinStructured_ExplicitMessageIDUnchanged 已随批G 删除：两条的夹具是
// 飞书式外壳 {"event_type":"im.message.receive_v1","data":{...}} —— 抖音官方报文里
// 根本没有这些键（审计 §16.1），它们 certifies 的是「解不出的结构化分支退化成内容哈希」
// 与「平台 message_id 原样进 MsgID」两条旧契约。后者已被批G 有意替换：官方
// server_message_id 是 88 字符 base64，原样拼会顶破 msg_id varchar(100)（插入失败还会被吞掉），
// 现在键取 sha1 前缀、原文进 Extra.server_message_id。两条新契约的正向断言在
// webhook_batchg_douyin_test.go 的 _MsgIDDeterministicAndWithinColumnLimit 与
// _MissingServerMessageIDFallsBackToContentKey。
