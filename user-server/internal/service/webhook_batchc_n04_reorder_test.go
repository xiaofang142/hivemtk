package service

// 审计 N-04：WhatsApp 乱序缓冲的 delayed 分支不可达（reorder_buffer.go:Offer 只在
// 缓冲区已有 >=2 条时才建定时器，而每条消息进来时缓冲区都是刚被上一次 flush 删空的
// 新建空表 ⇒ 长度恒为 1 ⇒ 立即 flush）。本用例把"乱序到达"这件事钉成可观测事实：
// 三条时间戳**倒序**的消息各自立刻入库、无一条被滞留、顺序即到达顺序。
// 删除缓冲前后跑同一条用例，输出必须一致——这就是"删除是行为等价"的正证。

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func waSingleBody(id, ts string) []byte {
	return waBatchBody(`{"from":"+8613900000002","id":"`+id+`","timestamp":"`+ts+`","type":"text","text":{"body":"乱序-`+id+`"}}`,
		`{"profile":{"name":"Bob"},"wa_id":"+8613900000002"}`)
}

func TestDispatchWhatsApp_OutOfOrderArrivalsAreNotHeld(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	// 时间戳严格递减：后两条相对已投递的消息都是"迟到者"。
	arrival := []struct {
		id string
		ts string
	}{
		{"wamid.N04C", "1700000300"},
		{"wamid.N04B", "1700000200"},
		{"wamid.N04A", "1700000100"},
	}
	for _, m := range arrival {
		hub, err := svc.dispatchWhatsApp(context.Background(), "1", &ParsedPayload{EventID: "evt-n04-" + m.id}, waSingleBody(m.id, m.ts))
		if err != nil {
			t.Fatalf("dispatch %s: %v", m.id, err)
		}
		if hub == nil {
			t.Errorf("%s 被滞留（返回 nil 即「交给 FlushHandler 稍后投递」）——缓冲的 delayed 分支被激活，删除它的等价性前提失效", m.id)
			continue
		}
		if hub.MsgID != m.id {
			t.Errorf("%s 返回值应就是本条：got %s", m.id, hub.MsgID)
		}
	}

	var hubs []model.MessageHub
	if err := db.Where("platform = ? AND direction = ? AND sender_id = ?", "whatsapp", "inbound", "+8613900000002").
		Order("id ASC").Find(&hubs).Error; err != nil {
		t.Fatalf("query hubs: %v", err)
	}
	if len(hubs) != len(arrival) {
		t.Fatalf("三条乱序消息应全部入库，实际 %d 条", len(hubs))
	}
	for i, h := range hubs {
		if h.MsgID != arrival[i].id {
			t.Errorf("hub[%d] 应按到达顺序落库（乱序重排并未生效）：want %s got %s", i, arrival[i].id, h.MsgID)
		}
	}
}
