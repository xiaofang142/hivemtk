package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/event"
	"hivemtk-user/internal/model"
)

// 批K：QQ 入站的 AI 触发归属只有一处——dispatchQQ → 中台 Ingress（aiTrigger=TriggerInboundAI），
// handleJob 里 `if triggerAI && channel != ChannelQQ` 是「同一条消息只回一次」的承重守卫。
//
// 它此前零生产路径用例：唯一相关的一条 TestQQ_TriggerSalesEngineGuard 打的是生产里没人调的
// triggerQQSalesEngine，两次调用都没有任何断言。本用例把守卫两侧各钉一行：
//   - 中台那一臂必须恰好 1 次（有人把守卫扩成「QQ 连 Ingress 也不触发」就红：消息没人回）
//   - handleJob 那一臂 QQ 必须 0 次（删掉或反向 channel != ChannelQQ 就红：同一条消息两份回复）
//   - 同一臂的非 QQ 渠道（抖音）必须恰好 1 次（整段触发被删/写反就红：那才是"其余渠道零回复"）
//
// 正文为什么故意留空（K-m1 实测逼出来的两条前提，少一条用例就是假绿）：
//  1. shouldTriggerAI 的第一句是 `if s.salesEngine == nil { return false }`。不注入销售引擎，
//     triggerAI 恒 false，守卫那半句删掉也照样全绿——变异体第一次就是这么活下来的。
//  2. 注入引擎之后，handleJob 若真进了 triggerSalesEngine（＝变异态），空正文会让销售引擎
//     在它自己的第一道校验（sales_engine.go 的 `user_message is empty`）就地返回错误，
//     用例因此不会去敲真实 LLM；总线计数仍然记得到那一跳。

// batchKSalesEngineEntryCounter 挂上全局事件总线并订阅 TopicCustomerMessageReceived，返回读计数的函数。
//
// 判据为什么打在总线上：triggerSalesEngine 的第一句就是 PublishCustomerMessage，位置在
// smartOrchestrator / salesEngine 两处判空**之前** ⇒「总线收到一条」等价于「handleJob 真进过
// 那个触发分支」，不必往用例里塞真的推理链。中台 Ingress 那条路不调这个函数，
// 两侧计数因此互不串味（这也是本用例能分辨「是谁触发的」的原因）。
func batchKSalesEngineEntryCounter(t *testing.T) func() int {
	t.Helper()
	var mu sync.Mutex
	count := 0
	bus := event.New(1, 64)
	bus.Subscribe(event.TopicCustomerMessageReceived, func(evt event.Event) error {
		if _, ok := evt.Payload.(event.CustomerMessagePayload); !ok {
			return nil
		}
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})
	event.SetGlobalBus(bus)
	t.Cleanup(event.StopGlobal)
	return func() int {
		mu.Lock()
		defer mu.Unlock()
		return count
	}
}

// batchKQQGroupEvent 拼一条群 @ 事件；会话与发言人按 evtID 取唯一值，免得与同包其它 QQ 用例
// 共用会话而被 ai_processing 排他标记挡下触发。官方口径里 content 是剥掉 @ 之后的正文，
// 纯 @机器人不带字的事件该字段就是空串。
func batchKQQGroupEvent(evtID, content string) []byte {
	return []byte(`{"id":"` + evtID + `","op":0,"t":"GROUP_AT_MESSAGE_CREATE","d":{` +
		`"id":"ROBOT1.0_` + evtID + `",` +
		`"group_openid":"G-k-` + evtID + `",` +
		`"content":"` + content + `",` +
		`"author":{"member_openid":"M-k-` + evtID + `"},` +
		`"timestamp":"` + time.Now().Format(time.RFC3339) + `"}}`)
}

func TestBatchK_QQHandleJobDoesNotDoubleTriggerAI(t *testing.T) {
	db := setupQQDB(t)
	if err := db.Create(&model.QQAccount{
		AccountName: "批K双触发号", AppID: "app-k-dup", AppSecret: "sec-k-dup",
		WebhookSecret: "wh-k-dup", Status: 1, WebhookEnabled: true, AIAgentEnabled: true,
	}).Error; err != nil {
		t.Fatalf("seed qq account: %v", err)
	}
	ws := NewWebhookService(db)
	t.Cleanup(func() { ws.Stop(context.Background()) })

	tr := &fakeAITrigger{}
	ing := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
	ing.SetAITrigger(tr)
	ws.SetIngressSvc(ing)

	salesEntry := batchKSalesEngineEntryCounter(t)

	// 前提 1：守卫只有在 triggerAI 本应为 true 时才有牙齿，所以必须先把它成立的条件钉住。
	ws.SetSalesEngine(context.Background(), &SalesEngine{})
	if !ws.shouldTriggerAI(context.Background(), ChannelQQ, "1") {
		t.Fatal("前置不成立：QQ 账号的 AI 开关没生效，守卫这一格无从判定（后面的 0 次断言没有意义）")
	}

	// 正例腿（先跑）：证明总线接缝是活的，否则下面的 0 次分不清是守卫挡下了还是根本没接上。
	// 空正文 ⇒ 引擎在 `user_message is empty` 处返回，不碰 LLM。
	ws.triggerSalesEngine(context.Background(), ChannelQQ, "1",
		&ParsedPayload{EventID: "k-control"},
		&model.MessageHub{Platform: "qq", ConversationID: "k-control-conv"})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && salesEntry() == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := salesEntry(); got != 1 {
		t.Fatalf("triggerSalesEngine 没在总线上留下计数（got %d）——计数器是哑的，本用例的 0 次断言不成立", got)
	}
	base := salesEntry()

	evtID := fmt.Sprintf("k-qq-dup-%d", time.Now().UnixNano())
	raw := batchKQQGroupEvent(evtID, "")
	evt := &model.WebhookEvent{
		Platform: string(ChannelQQ), EventID: evtID, EventType: "message",
		AccountID: "1", RawData: string(raw), Processed: false,
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("seed event: %v", err)
	}

	// 生产入口是 Receive → 队列 → handleJob；这里直调 handleJob，被测判据就在该函数体内。
	ws.handleJob(context.Background(), &webhookJob{event: evt, raw: raw, channel: ChannelQQ, account: "1"})

	// 到达见证：unified_messages 落在 dispatch 之后、AI 触发判定之前。缺了这一步，
	// 「0 次触发」也可能只是 handleJob 中途 return（非消息事件 / dispatch 失败）。
	var unified int64
	if err := db.Model(&model.UnifiedMessage{}).Count(&unified).Error; err != nil {
		t.Fatalf("count unified: %v", err)
	}
	if unified == 0 {
		t.Fatal("handleJob 没走到守卫那一行（unified_messages 零行）——后面的触发计数无效")
	}

	if tr.called != 1 {
		t.Fatalf("QQ 入站的 AI 触发归属在中台 Ingress，期望恰好 1 次，实际 %d 次", tr.called)
	}
	if tr.lastChannel != "qq" || tr.lastEventID != "qq_evt_"+evtID {
		t.Fatalf("中台触发的不是这条 QQ 消息：channel=%q event_id=%q want qq / qq_evt_%s",
			tr.lastChannel, tr.lastEventID, evtID)
	}

	// 总线异步投递，给一个排空窗口再判 0。
	time.Sleep(300 * time.Millisecond)
	if got := salesEntry() - base; got != 0 {
		t.Fatalf("同一条 QQ 消息在 handleJob 里被二次触发 AI（就是双重回复那个老病），多打 %d 次；承重守卫是 webhook.go 的 channel != ChannelQQ", got)
	}

	// 反向一臂：守卫的 else 侧（非 QQ 渠道）必须在 handleJob 里恰好触发一次。
	// 只钉 QQ 那一臂，等于允许有人把整段触发删掉或把条件写反而全绿——那时 QQ 仍能经中台收到回复，
	// 抖音/企微/飞书却一条都收不到（§17.7 处置② 要求的"其余渠道恰好一次"就是这一格）。
	// 抖音走 default 分支（无账号级 AI 开关，shouldTriggerAI 直接放行），不碰任何家账号表。
	dyEvtID := fmt.Sprintf("k-dy-%d", time.Now().UnixNano())
	dyBase := salesEntry()
	dyEvt := &model.WebhookEvent{
		Platform: string(ChannelDouyin), EventID: dyEvtID, EventType: "message",
		AccountID: "1", RawData: `{"event_id":"` + dyEvtID + `"}`, Processed: false,
	}
	if err := db.Create(dyEvt).Error; err != nil {
		t.Fatalf("seed douyin event: %v", err)
	}
	ws.handleJob(context.Background(), &webhookJob{
		event: dyEvt, raw: []byte(`{"event_id":"` + dyEvtID + `"}`), channel: ChannelDouyin, account: "1",
		payload: &ParsedPayload{EventID: dyEvtID, EventType: "message", Sender: "u-k-dy", ChatID: "c-k-dy"},
	})
	var dyHub int64
	if err := db.Model(&model.MessageHub{}).Where("account_id = ? AND sender_id = ?", "1", "u-k-dy").Count(&dyHub).Error; err != nil {
		t.Fatalf("count douyin hub: %v", err)
	}
	if dyHub == 0 {
		t.Fatal("抖音那一臂没产出 hub 行（triggerAI 的前置 hubMsg != nil 不成立）——下面的 1 次断言无效")
	}
	dyDeadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(dyDeadline) && salesEntry()-dyBase < 1 {
		time.Sleep(20 * time.Millisecond)
	}
	if got := salesEntry() - dyBase; got != 1 {
		t.Fatalf("非 QQ 渠道的 AI 触发归属在 handleJob，期望恰好 1 次，实际 %d 次（0＝触发被删掉/条件写反，客户收不到回复）", got)
	}
}
