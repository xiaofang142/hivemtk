package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 这条用例钉的不变量：/start 网关把一条私聊消息消费掉之后，同一条消息不得再进销售触发
// （webhook.go 里 `tgExtra.GateHandled` 那一臂）。判定顺序是「先网关、后触发」，网关已经
// 回了欢迎/提示语；守卫一旦失效，客户会在网关回复之后再收到一份 AI 销售回复。
//
// 两格互为对照，缺一就没有判据力：
//   - 正控制（普通私聊）⇒ 恰好 1 次：证明这套夹具确实会走到触发分支。没有它，下面那条
//     「0 次」可以由任何一处提前 return 凑出来（dispatch 没产出 hub、账号开关没放行、
//     正文被判空……），什么也证不了。
//   - /start ⇒ 恰好 0 次：私聊 hub 的 IsGroup 为假 ⇒ 走到 `triggerAI && channel != ChannelQQ`
//     就直接进 triggerSalesEngine。把 GateHandled 那块守卫删掉，这一格必须红。
//
// 出站面：/start 那一臂的网关会经 httpclient.Client 发 sendMessage（欢迎/提示语），
// 所以整条腿必须套 withCaptureHTTPClient，否则打到的是 api.telegram.org。

func tgGateTestAccount(t *testing.T, db *gorm.DB, tag string) string {
	t.Helper()
	acc := &model.TelegramAccount{
		AccountName:    "网关对照-" + tag,
		BotToken:       "715000000:" + tag,
		BotUsername:    "hivemtk_gate_test_bot",
		WebhookEnabled: true,
		AIAgentEnabled: true,
		Status:         1,
	}
	if err := db.Create(acc).Error; err != nil {
		t.Fatalf("seed telegram account: %v", err)
	}
	return fmt.Sprintf("%d", acc.ID)
}

func TestTGGateHandledSuppressesSalesTrigger(t *testing.T) {
	cases := []struct {
		name string
		text string
		want int
	}{
		{"PlainPrivateTriggers", "你好，我想了解一下价格", 1},
		{"StartGateSuppresses", "/start", 0},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, restoreHTTP := withCaptureHTTPClient(t)
			defer restoreHTTP()

			db := setupChannelFullDB(t)
			tag := fmt.Sprintf("tggate-%d", time.Now().UnixNano())
			accountID := tgGateTestAccount(t, db, tag)
			salesEntry := batchKSalesEngineEntryCounter(t)

			ws := NewWebhookService(db)
			t.Cleanup(func() { ws.Stop(context.Background()) })

			tr := &fakeAITrigger{}
			ing := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
			ing.SetAITrigger(tr)
			ws.SetIngressSvc(ing)

			// 前置 1：不注入引擎，triggerSalesEngine 第一句就 return false，两格都会读成 0。
			// 私聊正文非空 ⇒ 一定走到 e.polisher.Polish（唯一无条件解引用的注入位），要给真的。
			ws.SetSalesEngine(context.Background(), &SalesEngine{polisher: NewHumanizePolisher()})
			// 前置 2：这一家的账号级开关必须真放行，否则「0 次」的红因落在开关而不是守卫。
			if !ws.shouldTriggerAI(context.Background(), ChannelTelegram, accountID) {
				t.Fatalf("前置不成立：Telegram 账号 %s 的 AIAgentEnabled 没让 shouldTriggerAI 放行", accountID)
			}

			raw := []byte(`{"update_id":1,"message":{"message_id":100,"from":{"id":67890,` +
				`"first_name":"Gate"},"chat":{"id":12345,"type":"private","title":""},` +
				`"date":1700000000,"text":"` + c.text + `"}}`)
			payload, err := ws.ParsePayload(context.Background(), ChannelTelegram, raw)
			if err != nil {
				t.Fatalf("telegram ParsePayload: %v", err)
			}
			evt := &model.WebhookEvent{
				Platform: string(ChannelTelegram), EventID: payload.EventID, EventType: "message",
				AccountID: accountID, RawData: string(raw), Processed: false,
			}
			if err := db.Create(evt).Error; err != nil {
				t.Fatalf("seed event: %v", err)
			}

			base := salesEntry()
			ws.handleJob(context.Background(), &webhookJob{
				event: evt, raw: raw, channel: ChannelTelegram, account: accountID, payload: payload,
			})

			// 走到触发判定的证据：unified_messages 写在判定之前、hub 行是 triggerAI 的前置。
			var unified int64
			if err := db.Model(&model.UnifiedMessage{}).
				Where("platform = ? AND account_id = ?", model.Platform(ChannelTelegram), accountID).
				Count(&unified).Error; err != nil {
				t.Fatalf("count unified: %v", err)
			}
			if unified == 0 {
				t.Fatal("handleJob 没走到触发判定那一行（telegram 的 unified_messages 零行）——计数断言无效")
			}
			var hub int64
			if err := db.Model(&model.MessageHub{}).
				Where("platform = ? AND account_id = ? AND direction = ?", "telegram", accountID, "inbound").
				Count(&hub).Error; err != nil {
				t.Fatalf("count hub: %v", err)
			}
			if hub == 0 {
				t.Fatal("dispatch 没产出 hub 行（triggerAI 的前置 hubMsg != nil 不成立）——计数断言无效")
			}
			// 归属只在 handleJob：TG 的 dispatch 带 WithChannelOwnedAITrigger，中台那一臂必须让位。
			if tr.called != 0 {
				t.Fatalf("TG 入站同时被中台触发过 AI（%d 次，channel=%q event_id=%q）⇒ 同一条消息两份回复",
					tr.called, tr.lastChannel, tr.lastEventID)
			}

			if c.want == 1 {
				deadline := time.Now().Add(2 * time.Second)
				for time.Now().Before(deadline) && salesEntry()-base < 1 {
					time.Sleep(20 * time.Millisecond)
				}
				time.Sleep(300 * time.Millisecond)
			} else {
				// 给异步投递留一个排空窗口再断「仍然 0」，否则这一格只是在读队列还没跑完的空档。
				time.Sleep(800 * time.Millisecond)
			}
			if got := salesEntry() - base; got != c.want {
				t.Fatalf("%s：期望恰好 %d 次销售触发，实际 %d 次（正文 %q）"+
					"——0 变 1＝GateHandled 守卫失效，客户收到网关回复后又收到一份 AI 回复；1 变 0＝触发臂本身没接上",
					c.name, c.want, got, c.text)
			}
		})
	}
}
