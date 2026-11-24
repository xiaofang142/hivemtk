package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/channelbot/wecombot"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

// faWecombotTestKey 生成合法的 43 字节 EncodingAESKey（base64 去 =）。
func faWecombotTestKey() string {
	raw := bytes.Repeat([]byte{7}, 32)
	return strings.TrimSuffix(base64.StdEncoding.EncodeToString(raw), "=")
}

// faWecombotSetup 建隔离库 + 一个启用的企微机器人账号 + 完整装配的 WebhookService。
func faWecombotSetup(t *testing.T) (*WebhookService, string, string) {
	t.Helper()
	database := testutil.NewTestDBOrSkip(t,
		&model.MessageHub{}, &model.ChannelInteraction{}, &model.ChannelEventAudit{},
		&model.WeCombBotAccount{},
	)
	db.SetTestDB(database)
	key := faWecombotTestKey()
	acc := &model.WeCombBotAccount{AccountName: "fa-wecombot", AESKey: key, Status: 1}
	if err := database.Create(acc).Error; err != nil {
		t.Fatalf("create wecombot account: %v", err)
	}
	svc := NewWebhookService()
	memCache := cache.NewMemoryCache()
	t.Cleanup(func() { memCache.Close() })
	svc.ingressSvc.cache = memCache
	return svc, strconv.FormatUint(uint64(acc.ID), 10), key
}

// faWecombotCardRaw 构造一个加密的 template_card_event 回调体。
func faWecombotCardRaw(t *testing.T, key, eventType, eventKey, taskID, msgID, userID string) []byte {
	t.Helper()
	plain := fmt.Sprintf(`{"msgid":%q,"chatid":"chat-fa","from":{"userid":%q,"name":"张三"},`+
		`"msgtype":"event","event":{"eventtype":%q,"template_card_event":{"event_key":%q,"task_id":%q}},`+
		`"create_time":%d}`, msgID, userID, eventType, eventKey, taskID, time.Now().Unix())
	enc, err := wecombot.Encrypt(key, "", []byte(plain))
	if err != nil {
		t.Fatalf("encrypt card callback: %v", err)
	}
	raw, err := json.Marshal(map[string]string{"encrypt": enc})
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	return raw
}

func faWecombotInteractionID(accountID, msgID, eventKey string) string {
	return "wecombot_cb_" + accountID + "_" + msgID + "_" + eventKey
}

// TestFAChannel05_CardClickBecomesBusinessInteraction 卡片点击必须落 channel_interactions
// （操作者=点击人、动作=event_key），不落 message_hub、不触发 AI；重投幂等只一行。
func TestFAChannel05_CardClickBecomesBusinessInteraction(t *testing.T) {
	svc, accountID, key := faWecombotSetup(t)
	ctx := context.Background()
	raw := faWecombotCardRaw(t, key, "template_card_event", "approve:42", "task-9", "card-1", "user-a")

	hub, err := svc.dispatchWeCombBot(ctx, accountID, &ParsedPayload{}, raw)
	if err != nil {
		t.Fatalf("dispatch card callback: %v", err)
	}
	if hub != nil {
		t.Fatal("交互回调不得返回消息 hub（不进消息链路/不触发 AI）")
	}

	iid := faWecombotInteractionID(accountID, "card-1", "approve:42")
	got, err := svc.cardInteractionRepo.GetByInteractionID(ctx, model.ChannelWeCombBot, iid)
	if err != nil || got == nil {
		t.Fatalf("交互行未落库: %v / %+v", err, got)
	}
	row := *got
	if row.Action != "approve:42" || row.SenderID != "user-a" || row.AccountID != accountID {
		t.Fatalf("交互行字段丢失（操作者/动作/账号）：%+v", row)
	}
	if row.PlatformMsgID != "card-1" {
		t.Fatalf("platform_msg_id 丢失：%+v", row)
	}

	var hubCount int64
	db.GetDB().Model(&model.MessageHub{}).Where("account_id=?", accountID).Count(&hubCount)
	if hubCount != 0 {
		t.Fatalf("交互回调不得产生 message_hub 行，实际 %d", hubCount)
	}

	// 重投：确定性幂等键，仍只一行。
	if _, err := svc.dispatchWeCombBot(ctx, accountID, &ParsedPayload{}, raw); err != nil {
		t.Fatalf("replay dispatch: %v", err)
	}
	var n int64
	db.GetDB().Model(&model.ChannelInteraction{}).Where("interaction_id=?", iid).Count(&n)
	if n != 1 {
		t.Fatalf("同键重投应只 1 行，实际 %d", n)
	}
}

// TestFAChannel05_UnsupportedEventRejected 无 event_key / 非 template_card_event 明确拒绝，
// 零业务副作用（不建交互行、不进消息链路）。
func TestFAChannel05_UnsupportedEventRejected(t *testing.T) {
	svc, accountID, key := faWecombotSetup(t)
	ctx := context.Background()
	for _, tc := range []struct{ et, ek, msgID string }{
		{"enter_chat", "", "enter-1"},
		{"template_card_event", "", "card-nokey"},
		{"feedback", "like", "fb-1"},
	} {
		raw := faWecombotCardRaw(t, key, tc.et, tc.ek, "", tc.msgID, "user-a")
		_, err := svc.dispatchWeCombBot(ctx, accountID, &ParsedPayload{}, raw)
		if !errors.Is(err, ErrWeCombBotUnsupportedCardAction) {
			t.Fatalf("event %q key %q 应明确拒绝，实际 err=%v", tc.et, tc.ek, err)
		}
	}
	var n int64
	db.GetDB().Model(&model.ChannelInteraction{}).Where("account_id=?", accountID).Count(&n)
	if n != 0 {
		t.Fatalf("不支持事件零业务副作用，实际交互行 %d", n)
	}
}

// TestFAChannel05_ExpiredAndUnauthorizedZeroSideEffects 过期/越权交互被拒绝，
// 不改动已有权威行、不新增行。
func TestFAChannel05_ExpiredAndUnauthorizedZeroSideEffects(t *testing.T) {
	svc, accountID, key := faWecombotSetup(t)
	ctx := context.Background()

	// 过期：预置一条 expired 的权威行。
	iidExpired := faWecombotInteractionID(accountID, "card-expired", "approve:1")
	expired := &model.ChannelInteraction{
		Channel: model.ChannelWeCombBot, AccountID: accountID, InteractionID: iidExpired,
		SenderID: "owner", Action: "approve:1", Status: model.ChannelInteractionExpired,
	}
	if err := db.GetDB().Create(expired).Error; err != nil {
		t.Fatalf("seed expired: %v", err)
	}
	rawExpired := faWecombotCardRaw(t, key, "template_card_event", "approve:1", "", "card-expired", "owner")
	if _, err := svc.dispatchWeCombBot(ctx, accountID, &ParsedPayload{}, rawExpired); err != nil {
		t.Fatalf("expired dispatch: %v", err)
	}

	// 越权：预置一条 pending 行，操作者是 owner，但点击来自 attacker。
	iidOwned := faWecombotInteractionID(accountID, "card-owned", "approve:2")
	owned := &model.ChannelInteraction{
		Channel: model.ChannelWeCombBot, AccountID: accountID, InteractionID: iidOwned,
		SenderID: "owner", Action: "approve:2", Status: model.ChannelInteractionPending,
	}
	if err := db.GetDB().Create(owned).Error; err != nil {
		t.Fatalf("seed owned: %v", err)
	}
	rawBad := faWecombotCardRaw(t, key, "template_card_event", "approve:2", "", "card-owned", "attacker")
	if _, err := svc.dispatchWeCombBot(ctx, accountID, &ParsedPayload{}, rawBad); err != nil {
		t.Fatalf("unauthorized dispatch: %v", err)
	}

	var rows []model.ChannelInteraction
	if err := db.GetDB().Where("account_id=?", accountID).Find(&rows).Error; err != nil {
		t.Fatalf("list interactions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("过期/越权不得新增交互行，实际 %d", len(rows))
	}
	for _, r := range rows {
		if r.SenderID != "owner" {
			t.Fatalf("权威行操作者被越权改写：%+v", r)
		}
	}
}
