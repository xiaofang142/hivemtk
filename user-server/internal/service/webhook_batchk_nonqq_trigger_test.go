package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

// 这条用例钉的不变量：渠道账号级 AI 开关（AIAgentEnabled）必须真的接到 handleJob 的触发判定上，
// 且一条入站消息的 AI 回复只能有一个归属人。
//
// 为什么函数级判据不够：TestE2E_WebhookService_ShouldTriggerAI_FourChannels
// （channel_fullchain_e2e_test.go）绿只能证明「shouldTriggerAI 会读开关」，
// 证不出「handleJob 真去调了它、并且按它的结果触发」。此前跨账号表的端到端臂只有抖音一家，
// 而它走 shouldTriggerAI 的 default 分支（无账号级开关 ⇒ 直接放行），
// 所以「有账号级开关的渠道」在 handleJob 层面是裸的。
//
// 本用例对企微 / 飞书 / WhatsApp 各铺一条 handleJob 端到端腿，每家钉四格：
//  1. 家账号开关为真 ⇒ 总线计数恰好 1（触发归属在 handleJob；0 ＝ 整段触发被删/条件写反，客户收不到回复）
//  2. 中台 aiTrigger 计数必须 0（同一条消息的第二归属人；QQ 踩过的正是这一格——
//     dispatch 里补了中台触发、又忘了给 webhook.go 的守卫加例外 ⇒ 同一条消息两份回复）
//  3. unified_messages 有行 ⇒ handleJob 真走到了触发判定那一行
//  4. hub 行有行 ⇒ 证明 triggerAI 的前置 hubMsg != nil 成立。缺这一格，"0 次触发"
//     既可能是守卫被删，也可能只是 dispatch 没产出 hub 行（非消息事件 / 幂等收敛），
//     两种红因完全不同，判据必须能分辨。
//
// WhatsApp 那一家的第 2 格另有牙齿：它的 dispatch 真的会调中台
// （webhook_channel_whatsapp.go 的 Ingress(WithChannelOwnedAITrigger(ctx), …)），
// 那个 option 就是把 AI 归属让给 handleJob 的；把 option 摘掉 ⇒ aiTrigger 记 1 次 ⇒ 本腿红。
//
// 为什么这里不能像 QQ 那条用例一样用空正文：三家 dispatch 都会把正文写回 payload
// （wecom/feishu 在空正文时还要落成 "[text]"/"[文本]" 占位符），所以销售引擎一定会往下走。
// 走到底就会尝试真实出站 HTTP ⇒ 整条腿必须套 withCaptureHTTPClient（它把 httpclient 的
// 全局 Transport 换成测试内捕获服务，配对还原）。引擎侧的可注入子服务里
// customerLookup/memory/sop/ragSearcher/scriptLookup/dispatcher/feedbackLearner 都有判空，
// generateCandidate 在 dispatcher 为 nil 时直接返回错误 ⇒ 不敲真实 LLM；
// 唯一无条件解引用的 polisher 按实测必须给一个真的（见下文的注引擎那一行）。

// batchKNonQQAccount 按渠道 seed 一家 AI 开关为真的账号，返回 handleJob 与 shouldTriggerAI
// 共用的 accountID。ID 必须现取、不许写死 "1"：同包其它用例往同一颗进程级测试库里也 seed 过这几家账号。
func batchKNonQQAccount(t *testing.T, db *gorm.DB, channel WebhookChannel, tag string) string {
	t.Helper()
	var id uint
	switch channel {
	case ChannelWeCom:
		acc := &model.WeComAccount{
			CorpID: "corp-" + tag, CorpSecret: "sec-" + tag,
			AgentID: 1, CallbackToken: "tok-" + tag,
			EncodingAESKey: "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789ABCDEFG",
			WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
		}
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("seed wecom account: %v", err)
		}
		id = acc.ID
	case ChannelFeishu:
		acc := &model.FeishuAccount{
			AccountName: "飞书-" + tag, AppID: "app-" + tag, AppSecret: "sec-" + tag,
			WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
		}
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("seed feishu account: %v", err)
		}
		id = acc.ID
	case ChannelWhatsapp:
		acc := &model.WhatsAppCloudAccount{
			AccountName: "WhatsApp-" + tag, PhoneNumberID: "pn-" + tag,
			WhatsAppBusinessID: "waba-" + tag, AccessToken: "tk-" + tag,
			WebhookEnabled: true, AIAgentEnabled: true, Status: 1,
		}
		if err := db.Create(acc).Error; err != nil {
			t.Fatalf("seed whatsapp account: %v", err)
		}
		id = acc.ID
	default:
		t.Fatalf("batchKNonQQAccount 不支持渠道 %s", channel)
	}
	return fmt.Sprintf("%d", id)
}

func TestBatchK_NonQQHomeChannelsTriggerAIExactlyOnce(t *testing.T) {
	cases := []struct {
		name      string
		channel   WebhookChannel
		platform  string
		eventType string
		body      func(tag string) []byte
	}{
		{
			name:      "WeCom",
			channel:   ChannelWeCom,
			platform:  "wecom",
			eventType: "message",
			// 明文外壳：parseWeComPlain 取不到 encrypt 字段时按明文直接使用。
			body: func(tag string) []byte {
				return []byte(`{"ToUserName":"gh-` + tag + `","FromUserName":"wm-user-` + tag +
					`","CreateTime":1700000000,"MsgType":"text","Content":"企微正文",` +
					`"MsgId":"m-k-` + tag + `"}`)
			},
		},
		{
			name:      "Feishu",
			channel:   ChannelFeishu,
			platform:  "feishu",
			eventType: "im.message.receive_v1",
			body: func(tag string) []byte {
				return []byte(`{"schema":"2.0","header":{"event_type":"im.message.receive_v1",` +
					`"app_id":"a","event_id":"e-k-` + tag + `","token":"v"},"event":{` +
					`"sender":{"sender_id":{"open_id":"ou-k-` + tag + `"}},` +
					`"message":{"message_id":"om-k-` + tag + `","chat_id":"oc-k-` + tag +
					`","chat_type":"p2p","message_type":"text","content":"{\"text\":\"飞书正文\"}"}}}`)
			},
		},
		{
			name:      "WhatsApp",
			channel:   ChannelWhatsapp,
			platform:  "whatsapp",
			eventType: "messages",
			body: func(tag string) []byte {
				return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"waba-` + tag +
					`","changes":[{"value":{"messaging_product":"whatsapp","messages":[{` +
					`"from":"` + tag + `","id":"wamid.k-` + tag +
					`","timestamp":"1700000000","type":"text","text":{"body":"WhatsApp正文"}}]},` +
					`"field":"messages"}]}]}`)
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// 出站面：三家的触发链路最终会打真实网关，捕获传输层是这条腿的前提，
			// 不是可选加固（withCaptureHTTPClient 自带 restore + srv.Close）。
			_, _, restoreHTTP := withCaptureHTTPClient(t)
			defer restoreHTTP()

			db := setupChannelFullDB(t)
			tag := fmt.Sprintf("k-nonqq-%d", time.Now().UnixNano())
			accountID := batchKNonQQAccount(t, db, c.channel, tag)

			salesEntry := batchKSalesEngineEntryCounter(t)

			ws := NewWebhookService(db)
			t.Cleanup(func() { ws.Stop(context.Background()) })

			tr := &fakeAITrigger{}
			ing := NewInboxIngressServiceWithDB(db, newIsolatedCacheForTest(t))
			ing.SetAITrigger(tr)
			ws.SetIngressSvc(ing)

			// 前置 1：shouldTriggerAI 的第一句是 `if s.salesEngine == nil { return false }`。
			// 不注入引擎，下面的"恰好 1 次"永远读成 0，判据就变成在测注入装配而不是测触发归属。
			//
			// polisher 不是可有可无的装饰：Handle 顺序链路里 6 个可注入子服务只有它是
			// **无条件解引用**的（`req.Config.EnableHumanizePolish` 在默认配置里为真，
			// sales_engine.go:403-404），而本用例的正文非空 ⇒ 一定会走到那一步。
			// 只给零值引擎实测就是在这一行 SIGSEGV。HumanizePolisher 是纯文本处理
			// （去 AI 痕迹/符号/截断/个性化），不碰 LLM、不碰网络。
			ws.SetSalesEngine(context.Background(), &SalesEngine{polisher: NewHumanizePolisher()})
			// 前置 2：这一家的账号级开关必须真放行，否则红因落在开关而不是触发归属。
			if !ws.shouldTriggerAI(context.Background(), c.channel, accountID) {
				t.Fatalf("前置不成立：%s 账号 %s 的 AIAgentEnabled 没让 shouldTriggerAI 放行，"+
					"后面的\"恰好 1 次\"没有意义（这一格测的是开关接线，不是触发归属）", c.channel, accountID)
			}

			raw := c.body(tag)
			payload, err := ws.ParsePayload(context.Background(), c.channel, raw)
			if err != nil {
				t.Fatalf("%s ParsePayload: %v", c.channel, err)
			}
			evt := &model.WebhookEvent{
				Platform: string(c.channel), EventID: payload.EventID, EventType: c.eventType,
				AccountID: accountID, RawData: string(raw), Processed: false,
			}
			if err := db.Create(evt).Error; err != nil {
				t.Fatalf("seed event: %v", err)
			}

			// base 必须在 handleJob 之前取：总线是这一趟刚挂上去的，
			// handleJob 里那一跳就发生在这之后，事后再取基线会把唯一那次触发算成"增量 0"。
			base := salesEntry()

			// 生产入口是 Receive → 队列 → handleJob；这里直调 handleJob，被测判据就在该函数体内。
			ws.handleJob(context.Background(), &webhookJob{
				event: evt, raw: raw, channel: c.channel, account: accountID, payload: payload,
			})

			// 第 3 格：unified_messages 写在 AI 触发判定之前。缺它，"0 次"也可能只是
			// handleJob 中途 return（dispatch 报错走重试、或非消息事件被跳过）。
			var unified int64
			if err := db.Model(&model.UnifiedMessage{}).
				Where("platform = ? AND account_id = ?", model.Platform(c.platform), accountID).
				Count(&unified).Error; err != nil {
				t.Fatalf("count unified: %v", err)
			}
			if unified == 0 {
				t.Fatalf("handleJob 没走到触发判定那一行（platform=%s 的 unified_messages 零行）——后面的计数无效", c.platform)
			}
			// 第 4 格：hub 行 = triggerAI 的前置 hubMsg != nil。单独钉住它，才能把
			// "守卫被删 ⇒ 不触发" 与 "dispatch 没产出 hub ⇒ 本就不该触发" 分成两种红。
			var hub int64
			if err := db.Model(&model.MessageHub{}).
				Where("platform = ? AND account_id = ? AND direction = ?", c.platform, accountID, "inbound").
				Count(&hub).Error; err != nil {
				t.Fatalf("count hub: %v", err)
			}
			if hub == 0 {
				t.Fatalf("%s 那一臂没产出 hub 行（triggerAI 的前置 hubMsg != nil 不成立）——下面的 1 次断言无效", c.platform)
			}

			// 第 2 格：第二归属人必须是 0 —— 这条消息的 AI 回复只能由 handleJob 出一次。
			if tr.called != 0 {
				t.Fatalf("%s 入站同时被中台触发过 AI（%d 次，channel=%q event_id=%q）⇒ 同一条消息两份回复；"+
					"归属只在 handleJob，中台那一臂必须由 WithChannelOwnedAITrigger 让位",
					c.channel, tr.called, tr.lastChannel, tr.lastEventID)
			}

			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) && salesEntry()-base < 1 {
				time.Sleep(20 * time.Millisecond)
			}
			// 总线异步投递：判下界后再给一个排空窗口，否则"2 次"可能只读到 1 次。
			time.Sleep(300 * time.Millisecond)
			if got := salesEntry() - base; got != 1 {
				t.Fatalf("%s 的 AI 触发归属在 handleJob，期望恰好 1 次，实际 %d 次"+
					"（0＝整段触发被删/条件写反，客户收不到回复；≥2＝同一条消息重复触发）", c.channel, got)
			}
		})
	}
}
