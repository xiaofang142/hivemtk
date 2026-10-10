package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// 这组用例守 reach.account.list / reach.health 两条只读工具链：它们此前恒返回
// not implemented，工具在 AI 的清单里可见却永远调不通。现在读真实账号表，
// 于是「读哪张表、按什么口径判在线、哪些渠道根本没有账号这一层」必须可断言。

func newReachAccountDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.BridgeAccount{},
		&model.TelegramAccount{},
		&model.WhatsAppCloudAccount{},
		&model.FeishuAccount{},
		&model.DingTalkAppAccount{},
		&model.WechatAccount{},
		&model.WeComAccount{},
		&model.WeComAccountHealth{},
	)
}

// readAccountStatusRow 读回 telegram 账号行里的 status 列。
// 夹具是否真的把账号置成停用，只认库里这一行的值，不认结构体字段。
func readAccountStatusRow(t *testing.T, db *gorm.DB, id uint) int {
	t.Helper()
	var got int
	if err := db.Model(&model.TelegramAccount{}).Where("id = ?", id).Select("status").Scan(&got).Error; err != nil {
		t.Fatalf("读回 telegram.status 失败: %v", err)
	}
	return got
}

func TestListAccounts_BridgeChannelJudgesOnlineByLastSyncNotStickyColumn(t *testing.T) {
	db := newReachAccountDB(t)
	now := time.Now()
	stale := now.Add(-10 * time.Minute)

	// 「online + 刚心跳」在线；「online + 十分钟没心跳」离线——status 列只在 SSE 正常收尾时
	// 被改回 offline，扩展崩溃与断网都留不下这一笔，按列数会读出一屏假在线。
	// 「offline + 刚心跳」仍离线：显式离线优先于心跳。
	rows := []model.BridgeAccount{
		{UserID: 1, Channel: "douyin", AccountID: "dy-live", AccountName: "在线号", Status: "online", LastSyncAt: &now},
		{UserID: 1, Channel: "douyin", AccountID: "dy-stale", AccountName: "粘住列", Status: "online", LastSyncAt: &stale},
		{UserID: 1, Channel: "douyin", AccountID: "dy-off", AccountName: "显式离线", Status: "offline", LastSyncAt: &now},
		{UserID: 1, Channel: "kuaishou", AccountID: "ks-1", AccountName: "别的渠道", Status: "online", LastSyncAt: &now},
	}
	if err := db.Create(&rows).Error; err != nil {
		t.Fatalf("夹具写入失败: %v", err)
	}

	a := NewIntegrationReachAdapterFromDB(db)
	list, err := a.ListAccounts(context.Background(), "douyin")
	if err != nil {
		t.Fatalf("ListAccounts(douyin) 报错: %v", err)
	}
	if len(list) != 3 {
		t.Fatalf("douyin 账号数 = %d, want 3（其余渠道的行不得混进来）：%+v", len(list), list)
	}
	healthy := map[string]bool{}
	for _, acc := range list {
		if acc.Channel != "douyin" {
			t.Errorf("账号 %s 的 Channel = %q, want douyin", acc.AccountID, acc.Channel)
		}
		healthy[acc.AccountID] = acc.IsHealthy
	}
	if !healthy["dy-live"] {
		t.Error("刚心跳的行必须判在线")
	}
	if healthy["dy-stale"] {
		t.Error("status 仍标 online 但超过宽限窗的行必须判离线（粘列陷阱）")
	}
	if healthy["dy-off"] {
		t.Error("显式 offline 的行不得判在线")
	}
}

func TestListAccounts_ChannelAliasReadsSameTable(t *testing.T) {
	db := newReachAccountDB(t)
	now := time.Now()
	if err := db.Create(&model.BridgeAccount{
		Channel: "xiaohongshu", AccountID: "xhs-1", AccountName: "小红书", Status: "online", LastSyncAt: &now,
	}).Error; err != nil {
		t.Fatalf("夹具写入失败: %v", err)
	}
	a := NewIntegrationReachAdapterFromDB(db)
	for _, name := range []string{"xiaohongshu", "xhs", "xiaohongshu_web"} {
		list, err := a.ListAccounts(context.Background(), name)
		if err != nil {
			t.Fatalf("ListAccounts(%s) 报错: %v", name, err)
		}
		if len(list) != 1 {
			t.Errorf("别名 %s 读到 %d 条, want 1", name, len(list))
		}
	}
}

func TestListAccounts_IntStatusChannelsMapEnabledFlag(t *testing.T) {
	db := newReachAccountDB(t)
	if err := db.Create(&model.TelegramAccount{AccountName: "开着的bot", Status: 1}).Error; err != nil {
		t.Fatalf("telegram 夹具失败: %v", err)
	}
	// status 列带 gorm:"default:1"：Create 时 Go 零值会被 GORM 跳过、由列默认值补成 1，
	// 所以"停用"这一行必须显式 Update 落 0，且要在落库那一行读回确认——否则夹具根本没停用
	// 任何账号，本用例断言的 enabled/disabled 就量错了对象。
	off := model.TelegramAccount{AccountName: "停用的bot", Status: 1}
	if err := db.Create(&off).Error; err != nil {
		t.Fatalf("telegram 夹具失败: %v", err)
	}
	if err := db.Model(&model.TelegramAccount{}).Where("id = ?", off.ID).Update("status", 0).Error; err != nil {
		t.Fatalf("telegram 停用夹具失败: %v", err)
	}
	if got := readAccountStatusRow(t, db, off.ID); got != 0 {
		t.Fatalf("夹具没把 status 落成 0（读回 %d）：default:1 的列不能用 Create 写零值", got)
	}
	a := NewIntegrationReachAdapterFromDB(db)
	list, err := a.ListAccounts(context.Background(), "telegram")
	if err != nil {
		t.Fatalf("ListAccounts(telegram) 报错: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("telegram 账号数 = %d, want 2", len(list))
	}
	var enabled, disabled int
	for _, acc := range list {
		switch acc.Status {
		case "enabled":
			enabled++
			if !acc.IsHealthy {
				t.Errorf("status=enabled 的行 IsHealthy 必须为 true：%+v", acc)
			}
		case "disabled":
			disabled++
			if acc.IsHealthy {
				t.Errorf("status=0 的行不得判健康：%+v", acc)
			}
		default:
			t.Errorf("未预期的状态值 %q", acc.Status)
		}
	}
	if enabled != 1 || disabled != 1 {
		t.Errorf("enabled/disabled = %d/%d, want 1/1", enabled, disabled)
	}
}

func TestListAccounts_EmptyChannelCoversEveryAccountBearingChannel(t *testing.T) {
	db := newReachAccountDB(t)
	now := time.Now()
	if err := db.Create(&model.TelegramAccount{AccountName: "tg", Status: 1}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	if err := db.Create(&model.WhatsAppCloudAccount{AccountName: "wa", Status: 1}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	if err := db.Create(&model.FeishuAccount{AccountName: "fs", Status: 1}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	if err := db.Create(&model.DingTalkAppAccount{AccountName: "dt", AppKey: "k", AppSecret: "s", Status: 1}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	if err := db.Create(&model.WechatAccount{AppID: "wx-appid", Status: "active"}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	if err := db.Create(&model.BridgeAccount{Channel: "douyin", AccountID: "dy-1", Status: "online", LastSyncAt: &now}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}

	a := NewIntegrationReachAdapterFromDB(db)
	list, err := a.ListAccounts(context.Background(), "")
	if err != nil {
		t.Fatalf(`ListAccounts("") 报错: %v`, err)
	}
	seen := map[string]int{}
	for _, acc := range list {
		seen[acc.Channel]++
	}
	// 每类只种了一行；少一类就是「空渠道 = 全量」这条路径漏了某张表。
	for _, ch := range []string{"telegram", "whatsapp", "feishu", "dingtalk", "weixin", "douyin"} {
		if seen[ch] != 1 {
			t.Errorf("渠道 %s 在全量清单里读到 %d 条, want 1（读侧缺这张表）", ch, seen[ch])
		}
	}
}

func TestListAccounts_NoAccountDimensionAndUnknownChannelAreDifferent(t *testing.T) {
	db := newReachAccountDB(t)
	a := NewIntegrationReachAdapterFromDB(db)

	// sms 真的没有账号这一层，而 messanger 是拼错。两者都回空列表的话，
	// 调用方只能把「问错了」排查成「没配」。
	for _, ch := range []string{"sms", "email", "card", "web"} {
		if _, err := a.ListAccounts(context.Background(), ch); !errors.Is(err, ErrChannelNoAccountDimension) {
			t.Errorf("ListAccounts(%s) err = %v, want ErrChannelNoAccountDimension", ch, err)
		}
	}
	for _, ch := range []string{"messanger", "twiter"} {
		if _, err := a.ListAccounts(context.Background(), ch); !errors.Is(err, ErrUnknownReachChannel) {
			t.Errorf("ListAccounts(%s) err = %v, want ErrUnknownReachChannel", ch, err)
		}
	}
}

func TestListAccounts_NilDBFailsLoud(t *testing.T) {
	a := NewIntegrationReachAdapterFromDB(nil)
	if _, err := a.ListAccounts(context.Background(), "telegram"); !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Fatalf("无库时必须报未装配, got %v", err)
	}
}

func TestAccountHealth_WecomCarriesQuotaAndRisk(t *testing.T) {
	db := newReachAccountDB(t)
	acc := model.WeComAccount{
		CorpID:        "corp-1",
		CorpSecret:    "secret",
		Status:        1,
		LoginState:    service.WeComLoginOnline,
		DailyMsgQuota: 500,
		DailyMsgUsed:  120,
		RiskLevel:     "normal",
	}
	if err := db.Create(&acc).Error; err != nil {
		t.Fatalf("企微账号夹具失败: %v", err)
	}
	if err := db.Create(&model.WeComAccountHealth{
		AccountID: acc.ID, RiskLevel: "warning", QuotaUsed: 400, QuotaTotal: 500,
		ReportedAt: time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("健康记录夹具失败: %v", err)
	}

	a := NewIntegrationReachAdapterFromDB(db)
	info, err := a.AccountHealth(context.Background(), "wecom", strconv.FormatUint(uint64(acc.ID), 10))
	if err != nil {
		t.Fatalf("AccountHealth(wecom) 报错: %v", err)
	}
	// 有健康记录时以最近一条为准：账号行上的 used 是同步快照，可能落后一次上报。
	if info.DailyQuota != 500 || info.DailyUsed != 400 || info.DailyRemain != 100 {
		t.Errorf("配额 = %d/%d 剩 %d, want 500/400 剩 100", info.DailyQuota, info.DailyUsed, info.DailyRemain)
	}
	if info.RiskLevel != "warning" {
		t.Errorf("RiskLevel = %q, want warning（取健康记录那一条）", info.RiskLevel)
	}
	if info.Status != service.WeComLoginOnline {
		t.Errorf("Status = %q, want %q", info.Status, service.WeComLoginOnline)
	}
	if info.LastCheckAt == "" {
		t.Error("LastCheckAt 必须带上报时间，否则读侧无法判断这份健康度多旧")
	}
}

func TestAccountHealth_RequiresBothChannelAndAccountID(t *testing.T) {
	db := newReachAccountDB(t)
	a := NewIntegrationReachAdapterFromDB(db)

	err := func() error {
		_, err := a.AccountHealth(context.Background(), "telegram", "")
		return err
	}()
	if err == nil {
		t.Fatal("account_id 为空必须报错：单账号健康没有「全部账号」这种答案")
	} else if !strings.Contains(err.Error(), "reach.account.list") {
		t.Errorf("空 account_id 的错误应指路到清单工具, got %v", err)
	}
	if _, err := a.AccountHealth(context.Background(), "", "1"); err == nil {
		t.Fatal(`channel 为空必须报错：空值在清单侧是「全部渠道」，在健康侧不得变成「取第一个」`)
	}
	if _, err := a.AccountHealth(context.Background(), "sms", "1"); !errors.Is(err, ErrChannelNoAccountDimension) {
		t.Errorf("AccountHealth(sms) err = %v, want ErrChannelNoAccountDimension", err)
	}
}

func TestAccountHealth_BridgeQuotaIsNotTracked(t *testing.T) {
	db := newReachAccountDB(t)
	now := time.Now()
	if err := db.Create(&model.BridgeAccount{
		Channel: "douyin", AccountID: "dy-1", AccountName: "抖音号", Status: "online", LastSyncAt: &now,
	}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	a := NewIntegrationReachAdapterFromDB(db)
	info, err := a.AccountHealth(context.Background(), "douyin", "dy-1")
	if err != nil {
		t.Fatalf("AccountHealth(douyin) 报错: %v", err)
	}
	if info.Status != "online" {
		t.Errorf("Status = %q, want online", info.Status)
	}
	// 桥接渠道没有日配额概念：回 0 并标 not_tracked，而不是把「没测过」写成「还剩 0 条」。
	if info.DailyQuota != 0 || info.DailyRemain != 0 || info.RiskLevel != reachRiskNotTracked {
		t.Errorf("无配额渠道应回 0/0/not_tracked, got %+v", info)
	}

	if _, err := a.AccountHealth(context.Background(), "douyin", "nope"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("查不到的账号应透传 not found, got %v", err)
	}
}

func TestAccountHealth_BridgeStaleRowReadsOffline(t *testing.T) {
	db := newReachAccountDB(t)
	stale := time.Now().Add(-bridge.OnlineGraceWindow - time.Minute)
	if err := db.Create(&model.BridgeAccount{
		Channel: "tiktok", AccountID: "tt-1", Status: "online", LastSyncAt: &stale,
	}).Error; err != nil {
		t.Fatalf("夹具失败: %v", err)
	}
	a := NewIntegrationReachAdapterFromDB(db)
	info, err := a.AccountHealth(context.Background(), "tiktok", "tt-1")
	if err != nil {
		t.Fatalf("AccountHealth(tiktok) 报错: %v", err)
	}
	if info.Status != "offline" {
		t.Errorf("超过宽限窗的行必须读成 offline，got %q", info.Status)
	}
}
