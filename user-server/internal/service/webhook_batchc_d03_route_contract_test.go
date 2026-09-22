package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// 审计 D-03：通用 webhook 路由（POST /api/webhook/:channel/:account_id）此前对
// kuaishou/xiaohongshu/xianyu/custom 一律"验签通过 → 事件落库 → 回 200"，
// 然后 dispatchToChannel 落空 default 静默丢弃：消息不进收件箱、不触发 AI、不能出站。
// 本文件锁住三件事：能力表 = 路由分支、拒绝必须早于落库、漏配渠道不得伪装成已收到。

// declaredWebhookChannels 对外声明的全部渠道枚举（与 webhook_channel_telegram.go 的常量块一一对应）。
var declaredWebhookChannels = []WebhookChannel{
	ChannelDouyin, ChannelKuaishou, ChannelXiaohongshu, ChannelXianyu, ChannelTiktok,
	ChannelWechat, ChannelWeCom, ChannelDingTalk, ChannelWhatsapp, ChannelTelegram,
	ChannelFeishu, ChannelQQ, ChannelCustom,
}

// init 夹具自检：入站管线用例（webhook_test.go / webhook_service_e2e_test.go）都挂在
// douyin 上跑，前提是它在能力表里。哪天 douyin 被移出表，那一整批用例会因为
// "第一道就被拒"而绿着什么也不测。放在 init 而不是某条用例里：用例执行顺序会变，init 不会。
func init() {
	if !webhookInboundCapable(ChannelDouyin) {
		panic("webhook 入站管线夹具失效：用例挂在 douyin 上，它必须在 webhookInboundCapable 里")
	}
}

func TestWebhookInbound_CapabilityMatchesDispatchSwitch(t *testing.T) {
	// 两个方向的漂移都要红：
	//   能力表说支持、dispatchToChannel 却没分支 = 事件被收下后静默丢弃（D-03 原缺陷）；
	//   dispatchToChannel 有分支、能力表却拒绝 = 适配器成了永远进不去的死代码。
	svc := NewWebhookService(newD03DispatchDB(t))
	defer svc.Stop(context.Background())

	for _, ch := range declaredWebhookChannels {
		notWired := errors.Is(dispatchProbe(t, svc, ch), ErrWebhookInboundNotWired)
		if webhookInboundCapable(ch) == notWired {
			t.Errorf("渠道 %s 能力表与 dispatchToChannel 分支不一致：capable=%v notWired=%v", ch, webhookInboundCapable(ch), notWired)
		}
	}
}

// dispatchProbe 探测某渠道在 dispatchToChannel 里有没有分支。
// 适配器可能因为夹具报文畸形而 panic，这里只关心"是不是漏配渠道"，故一并兜住。
func dispatchProbe(t *testing.T, svc *WebhookService, channel WebhookChannel) (err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			err = nil
		}
	}()
	_, _, err = svc.dispatchToChannel(context.Background(), channel, "d03-probe",
		&ParsedPayload{EventType: "message"}, []byte(`{}`), nil)
	return err
}

func TestWebhookInbound_RejectHintsStayConsistentWithCapability(t *testing.T) {
	for ch := range webhookInboundRejectHints {
		if webhookInboundCapable(ch) {
			t.Errorf("渠道 %s 在 dispatchToChannel 已有适配器，却仍挂着拒绝文案：能力表要同步摘除", ch)
		}
	}
	for _, ch := range declaredWebhookChannels {
		if webhookInboundCapable(ch) {
			continue
		}
		if _, ok := webhookInboundRejectHints[ch]; !ok {
			t.Errorf("渠道 %s 无适配器却没有拒绝理由，运维只会看到一句泛化报错", ch)
		}
	}
}

func TestReceive_CapableChannelsPassTheGate(t *testing.T) {
	// 反向闸：能力表不能把有适配器的渠道一起拒掉，否则就造出了新的死路。
	// 这里不要求验签通过（账号没配密钥），只要求拒绝理由不是"渠道不支持"。
	t.Setenv("ALLOW_INSECURE_WEBHOOK", "true")
	svc := NewWebhookService(newD03DispatchDB(t))
	defer svc.Stop(context.Background())

	for _, ch := range declaredWebhookChannels {
		if !webhookInboundCapable(ch) {
			continue
		}
		res, err := svc.Receive(context.Background(), &ReceiveRequest{
			Channel: ch, AccountID: "d03-capable", Body: []byte(`{"event_id":"d03-cap"}`),
		})
		if err != nil {
			t.Fatalf("%s Receive: %v", ch, err)
		}
		if strings.Contains(res.Reason, "无入站适配器") || strings.Contains(res.Reason, "不支持通用 webhook 入站") {
			t.Errorf("渠道 %s 有入站适配器却被能力闸拒了：%s", ch, res.Reason)
		}
	}
}

// newD03DispatchDB 覆盖 handleJob 一路能碰到的表（setupWebhookTestDB 不含 message_hub）。
func newD03DispatchDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.WebhookEvent{},
		&model.UnifiedMessage{},
		&model.IntegrationAccount{},
		&model.MessageHub{},
		&model.InboxConversation{},
	)
}

func TestHandleJob_UnwiredChannelLeavesNoUnifiedMessage(t *testing.T) {
	// 唯一还能走到 default 的路径是恢复扫描器重放改造前入库的旧事件行。
	// 这时既不能写一条 unified_message 冒充"已收到"，也不能留着 processed=false 让扫描器反复重投。
	db := newD03DispatchDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	eventID := fmt.Sprintf("d03-unwired-%d", time.Now().UnixNano())
	evt := &model.WebhookEvent{
		Platform: string(ChannelKuaishou), EventID: eventID, EventType: "message",
		AccountID: "d03-acc", RawData: `{"event_id":"x"}`, Processed: false,
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("seed event: %v", err)
	}

	svc.handleJob(context.Background(), &webhookJob{
		event: evt, raw: []byte(`{"event_id":"x"}`), channel: ChannelKuaishou,
		account: "d03-acc",
		payload: &ParsedPayload{EventID: eventID, EventType: "message", Content: "客户消息", Sender: "d03-sender"},
	})

	var unified int64
	if err := db.Model(&model.UnifiedMessage{}).Where("account_id = ?", "d03-acc").Count(&unified).Error; err != nil {
		t.Fatalf("count unified: %v", err)
	}
	if unified != 0 {
		t.Errorf("漏配渠道不得写 unified_messages（写进去就是告诉运营「收到了」，却永远不会有人回复），实际 %d 条", unified)
	}

	fresh := model.WebhookEvent{}
	if err := db.Where("event_id = ?", eventID).First(&fresh).Error; err != nil {
		t.Fatalf("read back event: %v", err)
	}
	if !fresh.Processed {
		t.Error("被判定为漏配的旧事件必须就地标记处理完，否则恢复扫描器会无限重投")
	}
}

func TestHandleJob_DouyinNonMessageEventWritesNoUnifiedMessage(t *testing.T) {
	// handleJob 的"非消息事件"白名单原先写死 5 个渠道，抖系加了解析器却没进清单：
	// 无 sender 的抖系事件不产 hub 行，却继续往下落一条空内容 unified_message。
	db := newD03DispatchDB(t)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	eventID := fmt.Sprintf("d03-dy-skip-%d", time.Now().UnixNano())
	evt := &model.WebhookEvent{
		Platform: string(ChannelDouyin), EventID: eventID, EventType: "message",
		AccountID: "d03-dy-acc", RawData: "not-json", Processed: false,
	}
	if err := db.Create(evt).Error; err != nil {
		t.Fatalf("seed event: %v", err)
	}

	svc.handleJob(context.Background(), &webhookJob{
		// raw 非 JSON ⇒ dispatchDouyin 退回通用解析；payload 没有 sender ⇒ 不产 hub 行。
		event: evt, raw: []byte("not-json"), channel: ChannelDouyin, account: "d03-dy-acc",
		payload: &ParsedPayload{EventID: eventID, EventType: "message", Content: "群聊里的询价"},
	})

	var hub int64
	if err := db.Model(&model.MessageHub{}).Where("account_id = ?", "d03-dy-acc").Count(&hub).Error; err != nil {
		t.Fatalf("count hub: %v", err)
	}
	if hub != 0 {
		t.Errorf("无 sender 的抖系事件不应产出 hub 行，实际 %d 条", hub)
	}
	var unified int64
	if err := db.Model(&model.UnifiedMessage{}).Where("account_id = ?", "d03-dy-acc").Count(&unified).Error; err != nil {
		t.Fatalf("count unified: %v", err)
	}
	if unified != 0 {
		t.Errorf("抖系非消息事件不得写 unified_messages，实际 %d 条", unified)
	}
}
