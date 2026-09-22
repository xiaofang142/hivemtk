package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func waBatchBody(msgsJSON, contactsJSON string) []byte {
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[` +
		msgsJSON + `],"contacts":[` + contactsJSON + `]},"field":"messages"}]}]}`)
}

// TestDispatchWhatsApp_MultiMessageBatch 全部消息逐条入库
func TestDispatchWhatsApp_MultiMessageBatch(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	body := waBatchBody(
		`{"from":"+8613800000001","id":"wamid.A","timestamp":"1700000001","type":"text","text":{"body":"第一条"}},
		 {"from":"+8613800000001","id":"wamid.B","timestamp":"1700000002","type":"text","text":{"body":"第二条"}},
		 {"from":"+8613800000001","id":"wamid.C","timestamp":"1700000003","type":"image"}`,
		`{"profile":{"name":"Alice"},"wa_id":"+8613800000001"}`,
	)

	p := &ParsedPayload{EventID: "evt-wa-batch"}
	hub, err := svc.dispatchWhatsApp(context.Background(), "1", p, body)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("expected first hub")
	}
	if hub.MsgID != "wamid.A" {
		t.Errorf("expected first message hub returned, got MsgID=%s", hub.MsgID)
	}
	// 审计 M-02：同一条推送里的多条消息合成一份 AI 输入（原先只带首条，后两条客户
	// 消息在推理输入里根本不存在）。返回的 hub 仍是首条，路由字段取自本条推送。
	if p.Content != "第一条\n第二条\n[图片]" || p.Sender != "+8613800000001" {
		t.Errorf("payload.Content 应为本推送内三条消息的合成本文、Sender 为首条发言人，got content=%q sender=%q",
			p.Content, p.Sender)
	}

	var hubs []model.MessageHub
	if err := db.Where("platform = ? AND direction = ?", "whatsapp", "inbound").
		Order("id ASC").Find(&hubs).Error; err != nil {
		t.Fatalf("query hubs: %v", err)
	}
	if len(hubs) != 3 {
		t.Fatalf("W-4 未达成：期望 3 条消息全部入库，实际 %d 条", len(hubs))
	}
	wantIDs := []string{"wamid.A", "wamid.B", "wamid.C"}
	wantContents := []string{"第一条", "第二条", "[图片]"}
	for i, h := range hubs {
		if h.MsgID != wantIDs[i] {
			t.Errorf("hub[%d].MsgID expected %s, got %s", i, wantIDs[i], h.MsgID)
		}
		if h.Content != wantContents[i] {
			t.Errorf("hub[%d].Content expected %s, got %s", i, wantContents[i], h.Content)
		}
		if h.SenderID != "+8613800000001" {
			t.Errorf("hub[%d].SenderID expected +8613800000001, got %s", i, h.SenderID)
		}
	}
}

// TestDispatchWhatsApp_EmptyBatch 无消息事件返回 nil 不报错
func TestDispatchWhatsApp_EmptyBatch(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub, err := svc.dispatchWhatsApp(context.Background(), "1", &ParsedPayload{EventID: "evt-wa-empty"},
		[]byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{},"field":"messages"}]}]}`))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub != nil {
		t.Errorf("expected nil hub for empty batch, got %+v", hub)
	}
}

// TestWaMessageContent 媒体类型占位符映射。
//
// sticker 的期望值从 "[sticker]" 改成了 "[表情]"（批F-4，审计 N-17）：原用例钉的正是
// 缺陷本身 —— 客户发的贴纸，客服与 AI 看到的是一串英文类型名。改期望而不是改实现，
// 是因为实现方向与其余 8 个类型一致（全中文占位符），且库里那一行本就由同一张表写。
func TestWaMessageContent(t *testing.T) {
	cases := map[string]string{
		"text":        "",
		"image":       "[图片]",
		"audio":       "[语音]",
		"video":       "[视频]",
		"document":    "[文件]",
		"sticker":     "[表情]",
		"location":    "[位置]",
		"contacts":    "[联系人]",
		"unsupported": "[暂不支持的消息]",
		"unknown":     "[unknown]",
	}
	for typ, want := range cases {
		got := waMessageContent(typ, "正文")
		if typ == "text" {
			if got != "正文" {
				t.Errorf("text: expected 正文, got %s", got)
			}
			continue
		}
		if got != want {
			t.Errorf("%s: expected %s, got %s", typ, want, got)
		}
	}
}
