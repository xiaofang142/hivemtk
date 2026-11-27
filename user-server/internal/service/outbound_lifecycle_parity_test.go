package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func newOutboundParityService(t *testing.T, db *gorm.DB) *WebhookService {
	t.Helper()
	return &WebhookService{
		messageHubRepo: repository.NewMessageHubRepositoryWithDB(db),
		delayedRepo:    repository.NewDelayedOutboundRepositoryWithDB(db),
		ingressSvc:     NewInboxIngressService(),
		senders:        map[WebhookChannel]channelSender{},
	}
}

func outboundParityDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.Channel{}, &model.ChannelAccount{}, &model.MessageHub{}, &model.SessionMessage{},
		&model.InboxConversation{}, &model.UnifiedMessage{}, &model.WebhookEvent{},
		&model.IntegrationAccount{}, &DelayedOutboundReply{}, &model.CustomerSession{},
	)
}

func outboundParitySource(tenantID uint, channel WebhookChannel, accountID, conversationID string) *model.MessageHub {
	return &model.MessageHub{
		TenantID:           tenantID,
		Platform:           string(channel),
		AccountID:          accountID,
		MsgID:              "in-" + conversationID,
		Direction:          "inbound",
		MsgType:            "text",
		SenderID:           "customer-" + conversationID,
		Content:            "question",
		ConversationID:     conversationID,
		IsGroup:            true,
		GroupID:            "group-" + conversationID,
		Visibility:         model.VisibilityMember,
		AudienceJSON:       model.JSONStrings{"customer-" + conversationID, "agent"},
		VisibilityEvidence: model.JSONMap{"source": "parity-fixture"},
		Extra:              model.JSONMap{"custom_extra": "preserve", "thread_ts": "thread-1"},
		SentAt:             time.Now().Add(-time.Minute),
	}
}

func outboundParityContext(t *testing.T, tenantID uint, channel WebhookChannel, accountID string) context.Context {
	t.Helper()
	ctx := tenant.WithTenantID(context.Background(), tenantID)
	if bridgeOutboxCapable(channel) {
		return localBridgeSubmitContext(t, ctx, string(channel), accountID, "text")
	}
	return ctx
}

func readOutboundParityRow(t *testing.T, db *gorm.DB, channel WebhookChannel, correlationID string) *model.MessageHub {
	t.Helper()
	var row model.MessageHub
	if err := db.Where("platform = ? AND msg_id = ? AND direction = ?", string(channel), outboundHubMsgID(channel, correlationID), "outbound").First(&row).Error; err != nil {
		t.Fatalf("读取 canonical 出站行失败: %v", err)
	}
	return &row
}

func readOutboundParityReceipts(t *testing.T, row *model.MessageHub) []OutboundArtifactReceipt {
	t.Helper()
	raw, err := json.Marshal(row.Extra["receipts"])
	if err != nil {
		t.Fatalf("序列化回执失败: %v", err)
	}
	receipts := []OutboundArtifactReceipt{}
	if err := json.Unmarshal(raw, &receipts); err != nil {
		t.Fatalf("读取回执失败: %v", err)
	}
	return receipts
}

// 每个已登记渠道的 AI/human 成功路径都必须在真实库中留下一条 canonical correlated 行。
func TestOutboundLifecycleParity_EveryChannelAIAndHuman(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	useLegacyBridgeFixture(t)
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	channels := allDeclaredWebhookChannels()
	for _, channel := range channels {
		for _, origin := range []OutboundOrigin{OutboundOriginAI, OutboundOriginHuman} {
			t.Run(string(channel)+"/"+string(origin), func(t *testing.T) {
				accountID := seedCanonicalOutboundAccount(t, db, 501, string(channel), fmt.Sprintf("parity-%s-%s", channel, origin))
				conversationID := fmt.Sprintf("conv-%s-%s", channel, origin)
				correlationID := fmt.Sprintf("corr-%s-%s", channel, origin)
				eventID := "evt-" + correlationID
				releaseReplyClaimForTest(t, eventID)
				sender := &lifecycleSenderStub{result: &OutboundResult{
					Sent:          true,
					PlatformMsgID: "platform-" + correlationID,
					Receipts:      []OutboundArtifactReceipt{{ArtifactID: "text", PlatformMsgID: "platform-" + correlationID, Status: OutboundArtifactSent}},
				}}
				svc.senders[channel] = sender
				source := outboundParitySource(501, channel, accountID, conversationID)
				sessionID := "session-" + correlationID
				senderType := "ai"
				if origin == OutboundOriginHuman {
					senderType = "agent"
				}
				if err := db.Create(&model.SessionMessage{
					TenantID: 501, SessionID: sessionID, CorrelationID: correlationID,
					Content: "**raw**", ContentType: model.MessageTypeText,
					SenderType: senderType, SenderID: "legacy-sender",
				}).Error; err != nil {
					t.Fatalf("写入 SessionMessage 夹具失败: %v", err)
				}
				ctx := outboundParityContext(t, 501, channel, accountID)
				ctx = OutboundCorrelationToContext(ctx, correlationID)
				ctx = OutboundOriginToContext(ctx, origin)
				ctx = OutboundSessionIDToContext(ctx, sessionID)

				sent, sendErr := svc.sendOutbound(ctx, channel, accountID, &ParsedPayload{EventID: eventID, Sender: source.SenderID}, "**raw**", source, nil)
				if !sent || sendErr != nil {
					t.Fatalf("渠道 %s origin=%s 应成功，got sent=%v err=%v", channel, origin, sent, sendErr)
				}
				row := readOutboundParityRow(t, db, channel, correlationID)
				if row.Status != OutboundStatusSent || row.PlatformMsgID != "platform-"+correlationID {
					t.Fatalf("canonical 结算或回执不符: %+v", row)
				}
				if row.Extra["origin"] != string(origin) || row.Extra["custom_extra"] != "preserve" {
					t.Fatalf("origin 或原 Extra 丢失: %+v", row.Extra)
				}
				if row.IsAIReply != (origin == OutboundOriginAI) {
					t.Fatalf("IsAIReply 与 origin 不一致: %+v", row)
				}
				if !row.IsGroup || row.GroupID != source.GroupID || row.Visibility != model.VisibilityMember || len(row.AudienceJSON) != 2 {
					t.Fatalf("群聊 scope/visibility/audience 丢失: %+v", row)
				}
				if row.VisibilityEvidence["source"] != "parity-fixture" {
					t.Fatalf("visibility evidence 丢失: %+v", row.VisibilityEvidence)
				}
				receipts := readOutboundParityReceipts(t, row)
				if len(receipts) != 1 || receipts[0].PlatformMsgID != row.PlatformMsgID {
					t.Fatalf("回执未落 canonical 行: %+v", receipts)
				}
				var sessionRow model.SessionMessage
				if err := db.Where("tenant_id = ? AND correlation_id = ?", 501, correlationID).First(&sessionRow).Error; err != nil {
					t.Fatalf("读取会话补充行失败: %v", err)
				}
				if sessionRow.Content != row.Content || sessionRow.SenderType != senderType || sessionRow.DeliveredAt == nil {
					t.Fatalf("SessionMessage 未对齐 canonical 内容/来源/回执: session=%+v hub=%+v", sessionRow, row)
				}
				var count int64
				if err := db.Model(&model.MessageHub{}).Where("platform = ? AND msg_id = ?", string(channel), row.MsgID).Count(&count).Error; err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Fatalf("同一逻辑回复只能有一行，got %d", count)
				}
			})
		}
	}
}

// raw/rendered 同 correlation 必须被对齐，统一读者只能返回一条且不能 ambiguous。
func TestOutboundLifecycleParity_RawRenderedNotAmbiguous(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-rendered")
	correlationID := "corr-rendered"
	const sessionID = "session-rendered"
	if err := db.Create(&model.SessionMessage{
		TenantID: 501, SessionID: sessionID, CorrelationID: correlationID,
		Content: "**raw**", ContentType: model.MessageTypeText, SenderType: "ai", SenderID: "ai_assistant",
	}).Error; err != nil {
		t.Fatal(err)
	}
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true, PlatformMsgID: "p-rendered"}}
	svc.senders[ChannelCustom] = sender
	ctx := tenant.WithTenantID(context.Background(), 501)
	ctx = OutboundCorrelationToContext(ctx, correlationID)
	ctx = OutboundSessionIDToContext(ctx, sessionID)
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-rendered")
	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-rendered", Sender: source.SenderID}, "**raw**", source, nil)
	if !sent || sendErr != nil {
		t.Fatalf("发送失败: sent=%v err=%v", sent, sendErr)
	}
	reader := NewUnifiedMessageReader(repository.NewMessageHubRepositoryWithDB(db), repository.NewSessionMessageRepositoryWithDB(db))
	messages, err := reader.List(ctx, UnifiedMessageQuery{
		TenantID: 501, Platform: "custom", AccountIDs: []string{accountID},
		ConversationID: source.ConversationID, SessionID: sessionID, SessionAccountID: accountID, Limit: 10,
	})
	if err != nil {
		t.Fatalf("统一读者不得报 ambiguous duplicate: %v", err)
	}
	if len(messages) != 1 || messages[0].Content != "raw" {
		t.Fatalf("同一逻辑回复应只剩一条 canonical rendered 消息，got %+v", messages)
	}
}

// 可重试失败与延迟重放必须复用 correlation、scope、Extra 和同一 canonical 行。
func TestOutboundLifecycleParity_RetryAndReplayKeepIdentity(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-retry")
	correlationID := "corr-retry-replay"
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-retry-replay")
	sender := &lifecycleSenderStub{err: errors.New("connection refused")}
	svc.senders[ChannelCustom] = sender
	ctx := tenant.WithTenantID(context.Background(), 501)
	ctx = OutboundCorrelationToContext(ctx, correlationID)

	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-retry-replay", Sender: source.SenderID}, "retry body", source, nil)
	if sent || sendErr == nil {
		t.Fatalf("首次失败应如实返回: sent=%v err=%v", sent, sendErr)
	}
	row := readOutboundParityRow(t, db, ChannelCustom, correlationID)
	if row.Status != OutboundStatusPending {
		t.Fatalf("可重试失败应保持 pending，got %q", row.Status)
	}
	var rec DelayedOutboundReply
	if err := db.Where("correlation_id = ?", correlationID).First(&rec).Error; err != nil {
		t.Fatalf("重试队列应保存 correlation: %v", err)
	}
	if rec.CorrelationID != correlationID || rec.Origin != string(OutboundOriginAI) || !rec.IsGroup || rec.GroupID != source.GroupID || rec.HubExtra["custom_extra"] != "preserve" {
		t.Fatalf("重试记录丢失身份/scope/Extra: %+v", rec)
	}
	seedReplayAISession(t, db, ctx, &rec)
	rec.SendAt = time.Now().Add(-time.Second)
	if err := db.Model(&rec).Update("send_at", rec.SendAt).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.delayedRepo.ClaimForReplay(ctx, rec.ID, rec.Platform, rec.AccountID)
	if err != nil || claimed == nil {
		t.Fatalf("认领重放行失败: rec=%+v err=%v", claimed, err)
	}
	sender.err = nil
	sender.result = &OutboundResult{Sent: true, PlatformMsgID: "p-replayed"}
	svc.replayDelayedOutbound(ctx, claimed)

	after := readOutboundParityRow(t, db, ChannelCustom, correlationID)
	if after.ID != row.ID || after.Status != OutboundStatusSent || after.PlatformMsgID != "p-replayed" {
		t.Fatalf("重放应结算同一行: before=%+v after=%+v", row, after)
	}
	var afterRec DelayedOutboundReply
	if err := db.First(&afterRec, rec.ID).Error; err != nil {
		t.Fatal(err)
	}
	if afterRec.Status != model.DelayedStatusSent || afterRec.CorrelationID != correlationID {
		t.Fatalf("重放队列未同身份收口: %+v", afterRec)
	}
	var count int64
	if err := db.Model(&model.MessageHub{}).Where("platform = ? AND conversation_id = ? AND direction = ?", "custom", source.ConversationID, "outbound").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("重放不得新建 canonical 行，got %d", count)
	}
}

// quiet-hours 只入队不发送；到期重放保持同一逻辑身份。
func TestOutboundLifecycleParity_QuietHoursAndReplayKeepIdentity(t *testing.T) {
	orig := loadAIReplyQuietHoursFn()
	storeAIReplyQuietHoursFn(func(time.Time) bool { return true })
	defer func() { storeAIReplyQuietHoursFn(orig) }()
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-quiet")
	correlationID := "corr-quiet-replay"
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-quiet-replay")
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true, PlatformMsgID: "p-quiet"}}
	svc.senders[ChannelCustom] = sender
	ctx := tenant.WithTenantID(context.Background(), 501)
	ctx = OutboundCorrelationToContext(ctx, correlationID)

	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-quiet-replay", Sender: source.SenderID}, "quiet body", source, nil)
	if sent || sendErr != nil || sender.calls != 0 {
		t.Fatalf("quiet-hours 不得发送或伪报成功: sent=%v err=%v calls=%d", sent, sendErr, sender.calls)
	}
	row := readOutboundParityRow(t, db, ChannelCustom, correlationID)
	if row.Status != OutboundStatusPending {
		t.Fatalf("quiet-hours canonical 行应 pending，got %q", row.Status)
	}
	var rec DelayedOutboundReply
	if err := db.Where("correlation_id = ?", correlationID).First(&rec).Error; err != nil {
		t.Fatalf("quiet-hours 应保存 correlation: %v", err)
	}
	if rec.Kind != model.DelayedKindQuietHours || rec.CorrelationID != correlationID || rec.HubExtra["custom_extra"] != "preserve" {
		t.Fatalf("quiet-hours 记录丢失身份/Extra: %+v", rec)
	}
	seedReplayAISession(t, db, ctx, &rec)
	rec.SendAt = time.Now().Add(-time.Second)
	if err := db.Model(&rec).Update("send_at", rec.SendAt).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := svc.delayedRepo.ClaimForReplay(ctx, rec.ID, rec.Platform, rec.AccountID)
	if err != nil || claimed == nil {
		t.Fatalf("认领 quiet-hours 重放行失败: rec=%+v err=%v", claimed, err)
	}
	// 重放路径必须绕过 quiet-hours 判定。
	replayCtx := DelayedReplayToContext(ctx)
	svc.replayDelayedOutbound(replayCtx, claimed)
	after := readOutboundParityRow(t, db, ChannelCustom, correlationID)
	if after.ID != row.ID || after.Status != OutboundStatusSent || sender.calls != 1 {
		t.Fatalf("quiet-hours 重放应复用同一行且只发送一次: before=%+v after=%+v calls=%d", row, after, sender.calls)
	}
}

// unknown 独立落库，不得自动重试或伪报 sent。
func TestOutboundLifecycleParity_UnknownIsTerminalWithoutRetry(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-unknown")
	correlationID := "corr-unknown"
	sender := &lifecycleSenderStub{result: &OutboundResult{Unknown: true}}
	svc.senders[ChannelCustom] = sender
	ctx := tenant.WithTenantID(context.Background(), 501)
	ctx = OutboundCorrelationToContext(ctx, correlationID)
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-unknown")

	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-unknown", Sender: source.SenderID}, "unknown body", source, nil)
	if sent || sendErr == nil {
		t.Fatalf("unknown 不得伪报成功: sent=%v err=%v", sent, sendErr)
	}
	row := readOutboundParityRow(t, db, ChannelCustom, correlationID)
	if row.Status != OutboundStatusUnknown {
		t.Fatalf("unknown 应落同一 canonical 行，got %q", row.Status)
	}
	var retries int64
	if err := db.Model(&DelayedOutboundReply{}).Where("correlation_id = ?", correlationID).Count(&retries).Error; err != nil {
		t.Fatal(err)
	}
	if retries != 0 {
		t.Fatalf("unknown 不得自动重试，got %d 条重试", retries)
	}
}

// 跨租户 source/ctx 必须 fail-closed，sender 不能被调用。
func TestOutboundLifecycleParity_CrossTenantBlocksTransport(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := outboundParityDB(t)
	svc := newOutboundParityService(t, db)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-cross-tenant")
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true}}
	svc.senders[ChannelCustom] = sender
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-cross-tenant")
	ctx := tenant.WithTenantID(context.Background(), 502)

	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-cross-tenant", Sender: source.SenderID}, "blocked", source, nil)
	if sent || sendErr == nil || sender.calls != 0 {
		t.Fatalf("跨租户必须零外发: sent=%v err=%v calls=%d", sent, sendErr, sender.calls)
	}
}

// OneBot 分块必须逐块返回平台 message_id，同时只保留一条逻辑回复行。
func TestOutboundSenderReceipts_OneBotChunks(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := testutil.NewTestDB(t,
		&model.Channel{}, &model.ChannelAccount{}, &model.MessageHub{}, &model.SessionMessage{},
		&model.OneBotAccount{}, &DelayedOutboundReply{},
	)
	var mu sync.Mutex
	calls := 0
	platform := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		id := calls
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"status":"ok","retcode":0,"data":{"message_id":%d}}`, id)
	}))
	defer platform.Close()
	accountID := seedCanonicalOutboundAccount(t, db, 501, "onebot", "receipt-onebot")
	id, err := strconv.ParseUint(accountID, 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.OneBotAccount{
		ID: uint(id), TenantID: 501, AccountName: "receipt-onebot", SelfID: "42",
		HTTPBaseURL: platform.URL, Status: 1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := newOutboundParityService(t, db)
	correlationID := "corr-onebot-chunks"
	ctx := tenant.WithTenantID(context.Background(), 501)
	ctx = OutboundCorrelationToContext(ctx, correlationID)
	source := outboundParitySource(501, ChannelOneBot, accountID, "conv-onebot-chunks")
	source.IsGroup = false
	source.GroupID = ""
	source.SenderID = "42"

	sent, sendErr := svc.sendOutbound(ctx, ChannelOneBot, accountID, &ParsedPayload{EventID: "evt-onebot-chunks", Sender: "42"}, strings.Repeat("x", 2500), source, nil)
	if !sent || sendErr != nil {
		t.Fatalf("OneBot 分块发送应成功: sent=%v err=%v", sent, sendErr)
	}
	mu.Lock()
	callCount := calls
	mu.Unlock()
	if callCount != 2 {
		t.Fatalf("2500 字应分两块，got %d 次", callCount)
	}
	row := readOutboundParityRow(t, db, ChannelOneBot, correlationID)
	receipts := readOutboundParityReceipts(t, row)
	if len(receipts) != 2 || receipts[0].PlatformMsgID != "1" || receipts[1].PlatformMsgID != "2" {
		t.Fatalf("应保留逐块平台回执，got %+v", receipts)
	}
}

// 真实写库失败时 sender 调用次数必须为零。
func TestOutboundLifecycleParity_PersistenceFailureBlocksTransport(t *testing.T) {
	defer DisableAIQuietHoursForTest(true)()
	db := outboundParityDB(t)
	accountID := seedCanonicalOutboundAccount(t, db, 501, "custom", "parity-write-failure")
	if err := db.Callback().Create().Before("gorm:create").Register("test:outbound_prepare_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == (model.MessageHub{}).TableName() {
			tx.AddError(errors.New("injected outbound persistence failure"))
		}
	}); err != nil {
		t.Fatal(err)
	}
	svc := newOutboundParityService(t, db)
	sender := &lifecycleSenderStub{result: &OutboundResult{Sent: true}}
	svc.senders[ChannelCustom] = sender
	ctx := tenant.WithTenantID(context.Background(), 501)
	source := outboundParitySource(501, ChannelCustom, accountID, "conv-write-failure")

	sent, sendErr := svc.sendOutbound(ctx, ChannelCustom, accountID, &ParsedPayload{EventID: "evt-write-failure", Sender: source.SenderID}, "blocked", source, nil)
	if sent || sendErr == nil || sender.calls != 0 {
		t.Fatalf("持久化失败必须零外发: sent=%v err=%v calls=%d", sent, sendErr, sender.calls)
	}
	if !strings.Contains(sendErr.Error(), "持久化失败") {
		t.Fatalf("错误应明确说明持久化失败，got %v", sendErr)
	}
}
