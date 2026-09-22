package service

// R26：sendOutbound 的分支表里只有飞书（webhook_outbound.go:476）与 TG（:525）两处
// `for _, card := range cards` 真把富卡下发；桥接五族（抖音/小红书/TikTok/闲鱼/快手，
// :707 起）与企微/QQ/WhatsApp/钉钉/公众号五族都不读 cards —— 富卡整批丢掉，而文本回复
// 照标 sent / 队列行照走 MarkSent，日志、轨迹、落库行三处零观测（比"标成失败"更坏：
// 它把丢失写成成功）。
// 本文件把"丢了必须留痕"钉成两格：入口统一门出声一次（全渠道，含重放路径），
// 桥接族额外把张数写进出站行 Extra["cards_dropped"]（管理端读的是那一行）。

import (
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

// TestSendOutbound_Bridge_CardsDroppedRecordedOnRow 丢弃的富卡数必须写进出站行：
// 管理端读的是这一行，只有日志的话"这一单丢了卡"在界面上永远看不出来。
func TestSendOutbound_Bridge_CardsDroppedRecordedOnRow(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("douyin", "acct-r26-drop", "conv-r26-drop", "cust-r26-drop")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-drop")
	p := &ParsedPayload{EventID: "evt-r26-drop", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}

	if _, err := svc.sendOutbound(context.Background(), ChannelDouyin, hub.AccountID, p, "给你推荐两款", hub, bridgeCardsForTest(2)); err != nil {
		t.Fatalf("正常出库不应报错: %v", err)
	}

	var out model.MessageHub
	if err := db.Where("direction = ? AND conversation_id = ?", "outbound", hub.ConversationID).
		First(&out).Error; err != nil {
		t.Fatalf("读回出站行: %v", err)
	}
	if out.Extra == nil {
		t.Fatal("出站行 Extra 为空，丢弃计数无处可查")
	}
	raw, ok := out.Extra["cards_dropped"]
	if !ok {
		t.Fatalf("丢弃的富卡必须写进 Extra[\"cards_dropped\"]，实际键集合 %v", keysOf(out.Extra))
	}
	if n := asCount(raw); n != 2 {
		t.Errorf("cards_dropped = %v，期望 2", raw)
	}
	if out.MsgType != "text" {
		t.Errorf("桥接出站行仍应是 text（卡片未降级渲染），got %q", out.MsgType)
	}
}

// TestSendOutbound_Bridge_NoCardsNoDroppedKey 没有卡片时不得凭空写键：
// 否则"每单都丢了卡"与"这单本来没卡"在同一个字段上分不开。
func TestSendOutbound_Bridge_NoCardsNoDroppedKey(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("kuaishou", "acct-r26-none", "conv-r26-none", "cust-r26-none")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-none")
	p := &ParsedPayload{EventID: "evt-r26-none", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}

	if _, err := svc.sendOutbound(context.Background(), ChannelKuaishou, hub.AccountID, p, "稍等", hub, nil); err != nil {
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
}

// TestSendOutbound_Bridge_CardsDropWarns 丢弃必须出声，且**只出声一次**：入口那道统一门
// 是唯一的日志点（桥接族自己不再打一条），否则同一事件两行日志，按行数计数的告警会翻倍。
// 断言按"整行"匹配而不是"整段日志里出现过某子串"：后者的话计数与渠道可以分别来自两条不同
// 的行，一条真丢弃都没发生过也能拼出绿。
func TestSendOutbound_Bridge_CardsDropWarns(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService(db)
	defer svc.Stop(context.Background())

	hub := bridgeCardsTestHub("xiaohongshu", "acct-r26-warn", "conv-r26-warn", "cust-r26-warn")
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed inbound: %v", err)
	}
	releaseReplyClaimForTest(t, "evt-r26-warn")
	p := &ParsedPayload{EventID: "evt-r26-warn", Sender: hub.SenderID, Content: "有推荐吗", ChatID: hub.ConversationID}

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
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	if _, sendErr := svc.sendOutbound(context.Background(), ChannelXiaohongshu, hub.AccountID, p, "看下这款", hub, bridgeCardsForTest(3)); sendErr != nil {
		os.Stdout = oldOut
		_ = w.Close()
		t.Fatalf("正常出库不应报错: %v", sendErr)
	}

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	logged := string(captured)
	lines := linesWith(logged, "no card transport, rich cards dropped")
	if len(lines) != 1 {
		t.Fatalf("一次带卡出库应当且仅当打一条丢弃日志，实际 %d 条，捕获到的日志：\n%s", len(lines), logged)
	}
	line := lines[0]
	for _, want := range []string{
		`"channel":"xiaohongshu"`,
		`"cards_dropped":3`,
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
func TestSendOutbound_CardlessChannelWarns(t *testing.T) {
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")
	db := testutil.NewTestDB(t,
		&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
		&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
	)
	svc := NewWebhookService(db)
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
	t.Cleanup(func() {
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	// 出站成功与否不在本用例的判据里（企微没有可用账号必然失败），只判"丢弃有没有出声"。
	_, _ = svc.sendOutbound(context.Background(), ChannelWeCom, "acct-r26-wecom", p, "看下这款", nil, bridgeCardsForTest(2))

	if closeErr := w.Close(); closeErr != nil {
		t.Fatalf("关闭写端失败：%v", closeErr)
	}
	os.Stdout = oldOut
	captured, readErr := io.ReadAll(r)
	if readErr != nil {
		t.Fatalf("读取日志失败：%v", readErr)
	}
	logged := string(captured)
	// 判据按整行匹配（行里含 "message" 键那句文案）：只在整个日志块里找子串的话，
	// 计数与渠道可以来自两条不同的行，一次真丢弃都没发生过也能拼出绿。
	lines := linesWith(logged, "no card transport, rich cards dropped")
	if len(lines) != 1 {
		t.Fatalf("wecom 丢弃 2 张卡应打且仅打一条丢弃日志，实际 %d 条，捕获到的日志：\n%s", len(lines), logged)
	}
	line := lines[0]
	if !strings.Contains(line, `"channel":"wecom"`) || !strings.Contains(line, `"cards_dropped":2`) {
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
	t.Setenv("DISABLE_AI_QUIET_HOURS", "1")

	cases := []struct {
		name    string
		channel WebhookChannel
	}{
		{name: "feishu", channel: ChannelFeishu},
		{name: "telegram", channel: ChannelTelegram},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := testutil.NewTestDB(t,
				&model.MessageHub{}, &model.InboxConversation{}, &model.UnifiedMessage{},
				&model.WebhookEvent{}, &model.IntegrationAccount{}, &DelayedOutboundReply{},
			)
			svc := NewWebhookService(db)
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
			t.Cleanup(func() {
				os.Stdout = oldOut
				logger.InitLogger(logger.DefaultConfig())
			})

			// 下发成败不在本用例判据里（没有可用账号必然失败），只判"有没有把这批卡报成丢弃"。
			_, _ = svc.sendOutbound(context.Background(), tc.channel, "acct-r26-quiet", p, "看下这款", nil, bridgeCardsForTest(2))

			if closeErr := w.Close(); closeErr != nil {
				t.Fatalf("关闭写端失败：%v", closeErr)
			}
			os.Stdout = oldOut
			captured, readErr := io.ReadAll(r)
			if readErr != nil {
				t.Fatalf("读取日志失败：%v", readErr)
			}
			logged := string(captured)
			if !strings.Contains(logged, "r26-capture-probe") {
				t.Fatalf("探针行都没抓到，日志接缝已坏，本用例的负断言不作证据：\n%s", logged)
			}
			if strings.Contains(logged, "no card transport, rich cards dropped") {
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
