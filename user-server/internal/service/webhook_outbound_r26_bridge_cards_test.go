package service

// R26：没有富卡载体的通道必须以完整文字替代，而不是丢掉事实仍标成功。
// 统一入口单次记录替代计数，桥接出站行同样留痕；飞书/TG保留原富卡能力。

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils/logger"
)

func bridgeCardsForTest(n int) []model.RichCard {
	cards := make([]model.RichCard, 0, n)
	for i := 0; i < n; i++ {
		cards = append(cards, model.RichCard{
			Type:  model.CardTypeProduct,
			Title: "卡片标题",
		})
	}
	return cards
}

func bridgeCardsTestHub(platform, account, conv, sender string) *model.MessageHub {
	return &model.MessageHub{
		MsgID:          "mh:r26-in-" + conv,
		Platform:       platform,
		AccountID:      account,
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       sender,
		Content:        "有推荐吗",
		ConversationID: conv,
		SentAt:         time.Now(),
	}
}

// TestSendOutbound_Bridge_CardsFallbackRecordedOnRow 验证实际载荷与替代计数一致。
func TestSendOutbound_Bridge_CardsFallbackRecordedOnRow(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)() // DISABLE_AI_QUIET_HOURS env 已退役（B8），测试装配口顶掉勿扰窗
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("douyin", "acct-r26-drop", "conv-r26-drop", "cust-r26-drop")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-drop")
	p := &ParsedPayload{EventID: "evt-r26-drop", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}

	useLegacyBridgeFixture(t)

	ctx := localBridgeSubmitContext(t, context.Background(), hub.Platform, hub.AccountID, "text")
	if _, err := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "给你推荐两款", hub, bridgeCardsForTest(2)); err != nil {
		t.Fatalf("正常出库不应报错: %v", err)
	}

	var out model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", hub.ConversationID).
		First(&out).Error; err != nil {
		t.Fatalf("读回出站行: %v", err)
	}
	if out.Extra == nil {
		t.Fatal("出站行 Extra 为空，文字替代计数无处可查")
	}
	raw, ok := out.Extra["cards_fallback_count"]
	if !ok {
		t.Fatalf("文字替代必须写进 Extra[\"cards_fallback_count\"]，实际键集合 %v", keysOf(out.Extra))
	}
	if n := asCount(raw); n != 2 {
		t.Errorf("cards_fallback_count = %v，期望 2", raw)
	}
	if out.MsgType != "text" {
		t.Errorf("桥接出站行应为包含完整事实的 text，实际 %q", out.MsgType)
	}
	if !strings.Contains(out.Content, "卡片标题") || out.Extra["cards_dropped"] != nil {
		t.Fatalf("替代应有可读事实，不标成丢弃：content=%q extra=%v", out.Content, out.Extra)
	}
}

// TestSendOutbound_Bridge_NoCardsNoDroppedKey 没有卡片时不得凭空写键：
// 否则"每单都丢了卡"与"这单本来没卡"在同一个字段上分不开。
func TestSendOutbound_Bridge_NoCardsNoDroppedKey(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)() // DISABLE_AI_QUIET_HOURS env 已退役（B8），测试装配口顶掉勿扰窗
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("douyin", "acct-r26-none", "conv-r26-none", "cust-r26-none")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-none")
	p := &ParsedPayload{EventID: "evt-r26-none", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}

	useLegacyBridgeFixture(t)

	ctx := localBridgeSubmitContext(t, context.Background(), hub.Platform, hub.AccountID, "text")
	if _, err := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "稍等", hub, nil); err != nil {
		t.Fatalf("正常出库不应报错: %v", err)
	}

	var out model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", hub.ConversationID).
		First(&out).Error; err != nil {
		t.Fatalf("读回出站行: %v", err)
	}
	if _, ok := out.Extra["cards_dropped"]; ok {
		t.Errorf("无卡片的出站不该出现 cards_dropped，实际 %v", out.Extra["cards_dropped"])
	}
	if _, ok := out.Extra["cards_fallback_count"]; ok {
		t.Errorf("无卡片不能凭空出现文字替代计数：%v", out.Extra)
	}
}

// TestSendOutbound_Bridge_CardsDropWarns 丢弃必须出声，且**只出声一次**：入口那道统一门
// 是唯一的日志点（桥接族自己不再打一条），否则同一事件两行日志，按行数计数的告警会翻倍。
// 断言按"整行"匹配而不是"整段日志里出现过某子串"：后者的话计数与渠道可以分别来自两条不同
// 的行，一条真丢弃都没发生过也能拼出绿。
func TestSendOutbound_Bridge_CardsFallbackLogsOnce(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)() // DISABLE_AI_QUIET_HOURS env 已退役（B8），测试装配口顶掉勿扰窗
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("douyin", "acct-r26-warn", "conv-r26-warn", "cust-r26-warn")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-warn")
	p := &ParsedPayload{EventID: "evt-r26-warn", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}
	useLegacyBridgeFixture(t)
	ctx := localBridgeSubmitContext(t, context.Background(), hub.Platform, hub.AccountID, "text")

	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	// 正对照：只换 os.Stdout 不重建日志器会一行也抓不到（GetLogger 缓存实例），
	// 缺这行探针的话"没抓到"就分不清是接缝坏了还是代码没出声。
	logger.GetLogger().Info().Msg("r26-capture-probe")
	// 并发排空读端：正文执行期间全局 logger 仍会被本用例与他用例泄漏的后台协程持续写，
	// 无人读则管道缓冲写满 ⇒ 写端永久阻塞（2026-11-10 全包挂死实录：zerolog→os.File.Write）。
	var capBuf bytes.Buffer
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); io.Copy(&capBuf, r) }()
	t.Cleanup(func() {
		_ = w.Close() // 兜底：正文 t.Fatalf/Goexit 时也让排空协程见到 EOF
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
		<-drainDone
	})

	if _, sendErr := svc.sendOutbound(ctx, ChannelDouyin, hub.AccountID, p, "看下这款", hub, bridgeCardsForTest(3)); sendErr != nil {
		os.Stdout = oldOut
		_ = w.Close()
		t.Fatalf("正常出库不应报错: %v", sendErr)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	<-drainDone
	logged := capBuf.String()
	lines := linesWith(logged, "outbound rich cards rendered as complete text")
	if len(lines) != 1 {
		t.Fatalf("一次带卡出库应当且仅当打一条替代日志，实际 %d 条，捕获到的日志：\n%s", len(lines), logged)
	}
	line := lines[0]
	for _, want := range []string{
		`"channel":"douyin"`,
		`"cards_fallback_count":3`,
		`"conversation_id":"conv-r26-warn"`,
		`"account_id":"acct-r26-warn"`,
	} {
		if !strings.Contains(line, want) {
			t.Errorf("丢弃日志缺字段 %s，实际该行：\n%s", want, line)
		}
	}
}

// TestSendOutbound_CardlessChannelWarns 丢弃不止桥接五族：sendOutbound 的分支表里只有
// 飞书与 TG 真下发卡片（各自一个 `for _, card := range cards`），企微/QQ/WhatsApp/钉钉/公众号
// 五族同样整批丢掉。出声口径必须覆盖"没有卡片载体"的全部渠道，不能只修被点名的那一族。
func TestSendOutbound_CardlessChannelFallbackLogsOnce(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)() // DISABLE_AI_QUIET_HOURS env 已退役（B8），测试装配口顶掉勿扰窗
	testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService()
	defer svc.Stop(context.Background())

	releaseReplyClaimForTest(t, "evt-r26-wecom")
	p := &ParsedPayload{EventID: "evt-r26-wecom", Sender: "cust-r26-wecom", Content: "有推荐吗", ChatID: "conv-r26-wecom"}

	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	logger.GetLogger().Info().Msg("r26-capture-probe")
	// 并发排空读端：同上（管道缓冲写满 ⇒ 写端阻塞 ⇒ 全包挂死实录）。
	var capBuf bytes.Buffer
	drainDone := make(chan struct{})
	go func() { defer close(drainDone); io.Copy(&capBuf, r) }()
	t.Cleanup(func() {
		_ = w.Close() // 兜底：正文 t.Fatalf/Goexit 时也让排空协程见到 EOF
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
		<-drainDone
	})

	// 出站成功与否不在本用例的判据里（企微没有可用账号必然失败），只判"丢弃有没有出声"。
	_, _ = svc.sendOutbound(context.Background(), ChannelWeCom, "acct-r26-wecom", p, "看下这款", nil, bridgeCardsForTest(2))

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	<-drainDone
	logged := capBuf.String()
	// 判据按整行匹配（行里含 "message" 键那句文案）：只在整个日志块里找子串的话，
	// 计数与渠道可以来自两条不同的行，一次真丢弃都没发生过也能拼出绿。
	lines := linesWith(logged, "outbound rich cards rendered as complete text")
	if len(lines) != 1 {
		t.Fatalf("wecom 文字替代 2 张卡应当且仅当打一条日志，实际 %d 条，捕获到的日志：\n%s", len(lines), logged)
	}
	line := lines[0]
	if !strings.Contains(line, `"channel":"wecom"`) || !strings.Contains(line, `"cards_fallback_count":2`) {
		t.Errorf("丢弃日志必须在同一行带上渠道与张数才可能归因，实际该行：\n%s", line)
	}
	// hubMsg 为 nil 时该行不得出现会话号：判的是"统一门不许假设轨迹行一定在"。
	// 摘掉那句 nil 判断的话这里不是红而是 panic（会连带整个测试二进制一起倒）。
	if strings.Contains(line, `"conversation_id"`) {
		t.Errorf("hubMsg 为 nil 时该行不得带 conversation_id，实际该行：\n%s", line)
	}
}

// TestSendOutbound_CardCapableChannelStaysQuiet 出声门必须只报真丢了的：分支表里只有
// 飞书与 TG 有卡片载体（webhook_outbound.go:476 / :525 两个 for range cards），这两族
// 走到 guard 处一条 warn 都不该有。否则"每次正常发卡"都会刷成丢弃告警，
// 这条日志在运维侧当场作废，桥接族那条真丢弃也被淹掉。
func TestSendOutbound_CardCapableChannelStaysQuiet(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)() // DISABLE_AI_QUIET_HOURS env 已退役（B8），测试装配口顶掉勿扰窗

	cases := []struct {
		name    string
		channel WebhookChannel
	}{
		{name: "feishu", channel: ChannelFeishu},
		{name: "telegram", channel: ChannelTelegram},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			testutil.NewTestDB(t,
				&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
				&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
			)
			svc := NewWebhookService()
			defer svc.Stop(context.Background())

			// 事件号必须新且未被 claim：claim 失败会在 guard 之前 return，
			// 负断言就成了"走不到判据"的空绿（电池 C8/C9 摘掉排除臂必须把它打死）。
			eventID := "evt-r26-quiet-" + tc.name
			releaseReplyClaimForTest(t, eventID)
			p := &ParsedPayload{EventID: eventID, Sender: "cust-r26-quiet", Content: "有推荐吗", ChatID: "conv-r26-quiet-" + tc.name}

			oldOut := os.Stdout
			r, w, err := os.Pipe()
			if err != nil {
				t.Fatalf("管道创建失败：%v", err)
			}
			os.Stdout = w
			logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
			logger.GetLogger().Info().Msg("r26-capture-probe")
			// 并发排空读端：同上（管道缓冲写满 ⇒ 写端阻塞 ⇒ 全包挂死实录）。
			var capBuf bytes.Buffer
			drainDone := make(chan struct{})
			go func() { defer close(drainDone); io.Copy(&capBuf, r) }()
			t.Cleanup(func() {
				_ = w.Close() // 兜底：正文 t.Fatalf/Goexit 时也让排空协程见到 EOF
				os.Stdout = oldOut
				logger.InitLogger(logger.DefaultConfig())
				<-drainDone
			})

			// 下发成败不在本用例判据里（没有可用账号必然失败），只判"有没有把这批卡报成丢弃"。
			_, _ = svc.sendOutbound(context.Background(), tc.channel, "acct-r26-quiet", p, "看下这款", nil, bridgeCardsForTest(2))

			if closeErr := w.Close(); closeErr != nil {
				t.Fatalf("关闭写端失败：%v", closeErr)
			}
			os.Stdout = oldOut
			<-drainDone
			logged := capBuf.String()
			if !strings.Contains(logged, "r26-capture-probe") {
				t.Fatalf("探针行都没抓到，日志接缝已坏，本用例的负断言不作证据：\n%s", logged)
			}
			if strings.Contains(logged, "outbound rich cards rendered as complete text") || strings.Contains(logged, "rich cards dropped") {
				t.Errorf("%s 有卡片载体，正常下发不得报成丢弃，捕获到的日志：\n%s", tc.name, logged)
			}
		})
	}
}

// linesWith 取出捕获日志里含某子串的整行。丢弃类判据必须落到"行"这一层：
// 一次丢弃 = 一行，行数即次数，行内即该次的全部字段。
func linesWith(logged, substr string) []string {
	var hits []string
	for _, line := range strings.Split(logged, "\n") {
		if strings.Contains(line, substr) {
			hits = append(hits, line)
		}
	}
	return hits
}

func asCount(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	default:
		return -1
	}
}
