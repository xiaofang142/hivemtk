package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
)

func TestNormalizeEvent_DefaultEventType(t *testing.T) {
	svc := &InboxIngressService{}
	ev := &model.MessageEvent{Channel: model.ChannelTelegram, SenderID: "u1", Content: "hi"}
	if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if ev.EventType != model.EventTypeMessageReceived {
		t.Fatalf("空事件类型应归一为 message.received，实际 %q", ev.EventType)
	}
}

func TestNormalizeEvent_KeepsExplicitEventType(t *testing.T) {
	svc := &InboxIngressService{}
	ev := &model.MessageEvent{Channel: model.ChannelTelegram, SenderID: "u1", EventType: model.EventTypeMessageEdited}
	if err := svc.NormalizeEvent(context.Background(), ev); err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if ev.EventType != model.EventTypeMessageEdited {
		t.Fatalf("显式事件类型不应被改写，实际 %q", ev.EventType)
	}
}

func TestHandleIngress_EditedEventWithoutRepo_IsIsolated(t *testing.T) {
	svc := &InboxIngressService{}
	ev := &model.MessageEvent{
		EventID: "m-1", Channel: model.ChannelTelegram, SenderID: "u1",
		EventType: model.EventTypeMessageEdited, Content: "edited",
	}
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Reason != "edited_no_repo" {
		t.Fatalf("编辑事件应走独立分支，实际 reason=%q", res.Reason)
	}
	if res.QueuedForAI {
		t.Fatal("编辑事件不得触发 AI")
	}
}

func TestHandleIngress_MemberEvent_AuditReason(t *testing.T) {
	svc := &InboxIngressService{}
	ev := &model.MessageEvent{
		EventID: "evt-2", Channel: model.ChannelWeCom, SenderID: "u2",
		EventType: model.EventTypeGroupMemberJoined,
		Extra:     map[string]any{"account_id": "7"},
	}
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !res.Accepted || res.Reason != model.EventTypeGroupMemberJoined {
		t.Fatalf("成员事件应被接受并留痕，实际 accepted=%v reason=%q", res.Accepted, res.Reason)
	}
	if res.QueuedForAI {
		t.Fatal("成员事件不得触发 AI")
	}
}

func TestHandleIngress_InteractionCallback_SetsInteractionID(t *testing.T) {
	svc := &InboxIngressService{}
	ev := &model.MessageEvent{
		EventID: "cb-1", Channel: model.ChannelTelegram, SenderID: "u3",
		EventType: model.EventTypeInteractionCallback,
		Extra:     map[string]any{"interaction_id": "cb-abc"},
	}
	res, err := svc.HandleIngressMessage(context.Background(), ev)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.InteractionID != "cb-abc" {
		t.Fatalf("交互回调应带出 interaction_id，实际 %q", res.InteractionID)
	}
	if res.QueuedForAI {
		t.Fatal("交互回调不得触发 AI")
	}
}
