package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

// M-02：Meta 一次推送里带多条 message 时，只有第一条驱动 AI。
//
// 链路事实：handleJob 拿 dispatchWhatsApp 返回的 hub + 同一个 *ParsedPayload 去
// triggerSalesEngine，而 UserMessage 取的就是 p.Content（webhook_ai.go:144、:177）。
// 三条消息都各自落了 message_hub（Ingress 逐条入库），但推理输入里只有第一条，
// 客户连发"多少钱 / 有现货吗 / 能开发票吗"只会得到对"多少钱"的回答。
//
// 期望：同一条推送内的多条消息按出现顺序合成一份 AI 输入（中台批量入口
// HandleIngressBatch 已是同一口径："N messages merged, 1 AI trigger"），
// 一次推送一次回复，而不是丢条或逐条各回一次。

const (
	m02AccountID = "90114"
	m02WAID      = "+8613900000016"
)

func m02Body(nonce string, ids []string, bodies []string) []byte {
	msgs := make([]string, 0, len(ids))
	for i, id := range ids {
		msgs = append(msgs, `{"from":"`+m02WAID+`","id":"`+id+`","timestamp":"170000020`+fmt.Sprint(i)+
			`","type":"text","text":{"body":"`+bodies[i]+nonce+`"}}`)
	}
	return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"W","changes":[{"value":{"messages":[` +
		strings.Join(msgs, ",") +
		`],"contacts":[{"profile":{"name":"Dave"},"wa_id":"` + m02WAID + `"}]},"field":"messages"}]}]}`)
}

func TestM02_WhatsAppMultiMessageBatchFeedsEveryMessageToAI(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	nonce := fmt.Sprintf("-m02-%d", time.Now().UnixNano())
	ids := []string{"wamid-m02-1", "wamid-m02-2", "wamid-m02-3"}
	bodies := []string{"多少钱", "有现货吗", "能开发票吗"}

	p := &ParsedPayload{EventID: "evt-m02"}
	hub, err := svc.dispatchWhatsApp(context.Background(), m02AccountID, p, m02Body(nonce, ids, bodies))
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if hub == nil {
		t.Fatal("expected first hub")
	}

	for i, id := range ids {
		var got model.MessageHub
		if err := db.Where("platform = ? AND account_id = ? AND msg_id = ?", "whatsapp", m02AccountID, id).
			First(&got).Error; err != nil {
			t.Fatalf("hub %s 应入库: %v", id, err)
		}
		if got.Content != bodies[i]+nonce {
			t.Errorf("hub[%d].Content = %q, want %q", i, got.Content, bodies[i]+nonce)
		}
	}

	// AI 的推理输入：三条都要在，且保持客户发言顺序
	want := strings.Join([]string{bodies[0] + nonce, bodies[1] + nonce, bodies[2] + nonce}, "\n")
	if p.Content != want {
		t.Errorf("M-02 未达成：驱动 AI 的 payload.Content = %q，want %q（同推送内的后几条没进推理输入）",
			p.Content, want)
	}
	if p.ChatID != m02WAID || p.Sender != m02WAID {
		t.Errorf("payload 路由字段应仍是本条推送的对话方，got chat=%q sender=%q", p.ChatID, p.Sender)
	}
}

// TestM02_SingleMessagePayloadUnchanged 反向边界：单条推送不得被合成逻辑加进多余换行。
func TestM02_SingleMessagePayloadUnchanged(t *testing.T) {
	db := testutil.NewTestDB(t,
		&model.WhatsAppCloudAccount{}, &model.MessageHub{}, &model.InboxConversation{},
		&model.UnifiedMessage{}, &model.WebhookEvent{}, &model.IntegrationAccount{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	nonce := fmt.Sprintf("-m02s-%d", time.Now().UnixNano())
	p := &ParsedPayload{EventID: "evt-m02-single"}
	if _, err := svc.dispatchWhatsApp(context.Background(), m02AccountID, p,
		m02Body(nonce, []string{"wamid-m02-s1"}, []string{"只有一条"})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if want := "只有一条" + nonce; p.Content != want {
		t.Errorf("单条推送 payload.Content = %q，want %q（不得掺入分隔符或别的消息）", p.Content, want)
	}
}
