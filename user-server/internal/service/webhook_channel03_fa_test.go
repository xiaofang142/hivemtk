package service

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"
)

// FA-CHANNEL-03：统一 webhook 分发持久化失败不得降级后标处理成功。
//
// 原缺陷：handleJob 在 dispatchToChannel 返回错误（适配器/入站仓储失败）时只记日志，
// 仍写一条 legacy unified_message 留痕，随后照常 markProcessed —— 事件进入"已处理"，
// message_hub/AI 链却没完成，恢复扫描器不再重投，客户消息永久丢失。
//
// 关闭判据：分发失败保持可恢复（processed=false）；恢复重投后权威消息/会话/AI 派工
// 各恰好一次，不能仅检查 legacy 行。本用例用 dispatchChannelFn 假分发 + newD03DispatchDB
// 隔离库注入失败，不连任何真实渠道。
func TestFAChannel03_DispatchFailureStaysRecoverableThenRecovers(t *testing.T) {
	db := newD03DispatchDB(t)
	svc := NewWebhookService()
	t.Cleanup(func() { svc.Stop(context.Background()) })

	// AI 派工计数钉在全局总线上：triggerSalesEngine 第一句就是 PublishCustomerMessage，
	// 位置在 salesEngine 判空之前 ⇒「总线收到一条」等价于「handleJob 真进了触发分支」。
	aiEntry := batchKSalesEngineEntryCounter(t)
	svc.SetSalesEngine(context.Background(), &SalesEngine{})

	eventID := fmt.Sprintf("fa-c03-%d", time.Now().UnixNano())
	payload := &ParsedPayload{EventID: eventID, EventType: "message", Content: "", Sender: "fa-c03-sender"}

	// 假分发：fail=true 模拟入站仓储失败（不写权威 hub）；fail=false 模拟恢复后
	// 权威入站成功（写一行 message_hub，作为权威入站耐久凭据）并返回 hubMsg。
	var fail = true
	svc.dispatchChannelFn = func(ctx context.Context, channel WebhookChannel, accountID string, p *ParsedPayload, raw []byte, headers map[string]string) (*model.MessageHub, *tgDispatchExtra, error) {
		if fail {
			return nil, nil, errors.New("injected inbound repo failure")
		}
		hub := &model.MessageHub{
			Platform:       string(channel),
			MsgID:          eventID + ":hub",
			Direction:      "inbound",
			AccountID:      accountID,
			MsgType:        "text",
			SenderID:       p.Sender,
			Content:        p.Content,
			ConversationID: eventID + ":conv",
			SentAt:         time.Now(),
		}
		if err := db.Create(hub).Error; err != nil {
			return nil, nil, err
		}
		return hub, nil, nil
	}

	evt := &model.WebhookEvent{
		Platform: string(ChannelCustom), EventID: eventID, EventType: "message",
		AccountID: "1", RawData: "{}", Processed: false,
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("seed event: %v", err)
	}
	job := &webhookJob{
		event:   evt,
		raw:     []byte("{}"),
		channel: ChannelCustom,
		account: "1",
		payload: payload,
	}

	// ---- 阶段 1：分发失败 ----
	svc.handleJob(context.Background(), job)

	// 必须保持未处理（可恢复），留给恢复扫描器重投。
	fresh := model.WebhookEvent{}
	if err := db.Where("event_id = ?", eventID).First(&fresh).Error; err != nil {
		t.Fatalf("读回事件: %v", err)
	}
	if fresh.Processed {
		t.Fatal("分发失败不得 markProcessed：事件进入已处理后 message_hub/AI 链未完成，恢复扫描不再重投")
	}
	// 不得触发 AI 派工。
	if got := aiEntry(); got != 0 {
		t.Fatalf("分发失败不得触发 AI 派工，实际 %d 次", got)
	}
	// 权威消息不许存在（fake 失败时未写）——legacy 行不能冒充权威入站。
	var hubs int64
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", eventID+":hub").Count(&hubs).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if hubs != 0 {
		t.Fatalf("分发失败不应有权威 hub 行，实际 %d", hubs)
	}
	// legacy 留痕允许存在，但它不是完成凭据（上面已断言 processed=false）。
	// 留痕按 sender_id 归属；它存在也不改变"未处理"结论。
	var legacy int64
	if err := db.Model(&model.UnifiedMessage{}).Where("sender_id = ?", payload.Sender).Count(&legacy).Error; err != nil {
		t.Fatalf("count legacy: %v", err)
	}

	// ---- 阶段 2：恢复重投，分发成功 ----
	fail = false
	svc.handleJob(context.Background(), &webhookJob{
		event:   &fresh,
		raw:     []byte("{}"),
		channel: ChannelCustom,
		account: "1",
		payload: payload,
	})

	if err := db.Where("event_id = ?", eventID).First(&fresh).Error; err != nil {
		t.Fatalf("读回事件(恢复后): %v", err)
	}
	if !fresh.Processed {
		t.Fatal("恢复重投成功后必须 markProcessed")
	}
	// 权威消息恰好一次。
	if err := db.Model(&model.MessageHub{}).Where("msg_id = ?", eventID+":hub").Count(&hubs).Error; err != nil {
		t.Fatalf("count hub(恢复后): %v", err)
	}
	if hubs != 1 {
		t.Fatalf("恢复后权威 hub 应恰好 1 行，实际 %d", hubs)
	}
	// AI 派工恰好一次（等总线异步投递）。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && aiEntry() < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := aiEntry(); got != 1 {
		t.Fatalf("恢复后 AI 派工应恰好 1 次，实际 %d 次", got)
	}
}
