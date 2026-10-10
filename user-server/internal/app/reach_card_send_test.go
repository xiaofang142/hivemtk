package app

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

// 这组用例守 reach.card.send：它在工具清单里挂了两年，最后一段 SendCard 是 not implemented，
// 于是「模型挑了渠道、审批过了、限流过了，客户什么都没收到」。现在读真实的卡片模板表 +
// 短链表，把运营在后台复制的那条 /s/ 短链发进会话。
// 判据因此是三类：能不能发出去、发出去的是哪条链接、发不出去时当场响在哪一格。

func newReachCardDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t,
		&model.DouyinCard{},
		&model.KuaishouCard{},
		&model.XiaohongshuCard{},
		&model.TikTokCard{},
		&model.XianyuCard{},
		&model.ShortLink{},
		&model.MessageHub{},
	)
}

// createReachCard 在渠道自己的卡片表里种一行。
//
// is_active 列带 gorm:"default:true"：Create 时 false 是 Go 零值，会被 GORM 整个跳过、
// 由列默认值补成 true。所以"停用"这张卡片只能建完再显式 Update，并在落库那一行读回确认——
// 否则夹具里根本没有停用的卡片，下面那条停用判据就是在量一个不存在的前提。
func createReachCard(t *testing.T, db *gorm.DB, channel string, id uint, title string, shortLinkID uint, active bool) {
	t.Helper()
	var err error
	switch channel {
	case "douyin":
		err = db.Create(&model.DouyinCard{ID: id, Title: title, Description: "描述",
			RedirectURL: "https://target.example.test/p/1", ShortLinkID: shortLinkID, IsActive: true}).Error
	case "kuaishou":
		linkID := shortLinkID
		err = db.Create(&model.KuaishouCard{ID: id, Title: title, Description: "描述",
			RedirectURL: "https://target.example.test/p/1", ShortLinkID: &linkID, IsActive: true}).Error
	case "xiaohongshu":
		linkID := shortLinkID
		err = db.Create(&model.XiaohongshuCard{ID: id, Title: title, Description: "描述",
			RedirectURL: "https://target.example.test/p/1", ShortLinkID: &linkID, IsActive: true}).Error
	case "tiktok":
		err = db.Create(&model.TikTokCard{ID: id, Title: title, Description: "描述",
			RedirectURL: "https://target.example.test/p/1", ShortLinkID: shortLinkID, IsActive: true}).Error
	case "xianyu":
		err = db.Create(&model.XianyuCard{ID: id, Title: title, Description: "描述",
			RedirectURL: "https://target.example.test/p/1", ShortLinkID: shortLinkID, IsActive: true}).Error
	default:
		t.Fatalf("夹具不认识渠道 %s", channel)
	}
	if err != nil {
		t.Fatalf("%s 卡片夹具失败: %v", channel, err)
	}
	if active {
		return
	}
	if uerr := cardScope(db, channel, id).Update("is_active", false).Error; uerr != nil {
		t.Fatalf("%s 卡片停用夹具失败: %v", channel, uerr)
	}
	var got bool
	if qerr := cardScope(db, channel, id).Select("is_active").Scan(&got).Error; qerr != nil {
		t.Fatalf("读回 %s.is_active 失败: %v", channel, qerr)
	}
	if got {
		t.Fatalf("夹具没把 %s#%d 置成停用（读回 is_active=true）：default:true 的列不能用 Create 写零值", channel, id)
	}
}

// cardScope 按渠道+id 指回那一行，供 Update/读回用。
func cardScope(db *gorm.DB, channel string, id uint) *gorm.DB {
	var row any
	switch channel {
	case "douyin":
		row = &model.DouyinCard{}
	case "kuaishou":
		row = &model.KuaishouCard{}
	case "xiaohongshu":
		row = &model.XiaohongshuCard{}
	case "tiktok":
		row = &model.TikTokCard{}
	case "xianyu":
		row = &model.XianyuCard{}
	default:
		panic("cardScope: 未预期渠道 " + channel)
	}
	return db.Model(row).Where("id = ?", id)
}

func createReachShortLink(t *testing.T, db *gorm.DB, id uint, code string, status int, expire *time.Time) {
	t.Helper()
	link := model.ShortLink{ID: id, ShortCode: code, OriginalURL: "https://target.example.test/p/1",
		Title: "卡片", Status: status, ExpireTime: expire}
	if err := db.Create(&link).Error; err != nil {
		t.Fatalf("短链夹具失败: %v", err)
	}
}

// isolateBridgeOutbox 把桥接出站底座接到本用例的库上；包级全局必须成对还原。
func isolateBridgeOutbox(t *testing.T, db *gorm.DB) *service.InboxIngressService {
	t.Helper()
	mem := cache.NewMemoryCache()
	t.Cleanup(mem.Close)
	svc := service.NewInboxIngressServiceWithDB(db, mem)
	service.SetGlobalInboxIngressService(svc)
	t.Cleanup(func() { service.SetGlobalInboxIngressService(nil) })
	return svc
}

var reachCardChannels = []string{"douyin", "kuaishou", "xiaohongshu", "tiktok", "xianyu"}

// 五张卡片表各自独立，主键会撞：卡片 id 用 11..15，短链 id 用 21..25，一一对应。
func cardIDFor(channel string) uint { return uint(11 + indexOfCardChannel(channel)) }
func linkIDFor(channel string) uint { return uint(21 + indexOfCardChannel(channel)) }
func codeFor(channel string) string { return "code" + channel }
func indexOfCardChannel(channel string) int {
	for i, c := range reachCardChannels {
		if c == channel {
			return i
		}
	}
	return -1
}

func TestSendCard_DeliversCardShortLinkToBridgeOutbox(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://cards.example.test")
	db := newReachCardDB(t)
	svc := isolateBridgeOutbox(t, db)

	for _, channel := range reachCardChannels {
		createReachShortLink(t, db, linkIDFor(channel), codeFor(channel), 1, nil)
		createReachCard(t, db, channel, cardIDFor(channel), "标题"+channel, linkIDFor(channel), true)
	}

	a := NewIntegrationReachAdapterFromDB(db)
	ctx := context.Background()
	for _, channel := range reachCardChannels {
		cardID := strconv.Itoa(int(cardIDFor(channel)))
		msgID, err := a.SendCard(ctx, channel, "acc-1", "conv-1", cardID)
		if err != nil {
			t.Fatalf("SendCard(%s) 报错: %v", channel, err)
		}
		if want := "bridge:" + channel + ":acc-1:conv-1"; msgID != want {
			t.Errorf("SendCard(%s) msgID = %q, want %q", channel, msgID, want)
		}

		pending, err := svc.ListPendingOutbound(ctx, channel, "acc-1")
		if err != nil {
			t.Fatalf("读回 %s 出站队列失败: %v", channel, err)
		}
		if len(pending) != 1 {
			t.Fatalf("%s 出站队列 = %d 条, want 1", channel, len(pending))
		}
		row := pending[0]
		wantURL := "https://cards.example.test/s/" + codeFor(channel)
		if !strings.Contains(row.Content, wantURL) {
			t.Errorf("%s 出站内容没带绝对短链 %q，实际 %q", channel, wantURL, row.Content)
		}
		if !strings.Contains(row.Content, "标题"+channel) {
			t.Errorf("%s 出站内容没带卡片标题，实际 %q", channel, row.Content)
		}
		// 桥接客户端只认文本（现网 12 处出站全是 "text"）：写 "card" 等于替客户端决定怎么渲染。
		if row.MsgType != "text" {
			t.Errorf("%s 出站 msg_type = %q, want text", channel, row.MsgType)
		}
		if row.ReceiverID != "conv-1" {
			t.Errorf("%s 出站收件人 = %q, want conv-1", channel, row.ReceiverID)
		}
	}
}

// 渠道别名必须落回同一张表：工具侧可能传 xiaohongshu_web / xhs。
func TestSendCard_ChannelAliasReadsSameCardTable(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://cards.example.test")
	db := newReachCardDB(t)
	svc := isolateBridgeOutbox(t, db)

	createReachShortLink(t, db, linkIDFor("xiaohongshu"), codeFor("xiaohongshu"), 1, nil)
	createReachCard(t, db, "xiaohongshu", cardIDFor("xiaohongshu"), "小红书卡片", linkIDFor("xiaohongshu"), true)

	a := NewIntegrationReachAdapterFromDB(db)
	for _, alias := range []string{"xiaohongshu", "xiaohongshu_web", "xhs"} {
		if _, err := a.SendCard(context.Background(), alias, "acc-alias", "conv-"+alias,
			strconv.Itoa(int(cardIDFor("xiaohongshu")))); err != nil {
			t.Fatalf("SendCard(%s) 报错: %v", alias, err)
		}
	}
	// 三个名字必须落到同一张 xiaohongshu_cards 表、同一个 platform 标签上；
	// 归一失败的话，别名那两条会带着 xiaohongshu_web / xhs 的 platform 落库（或干脆报错）。
	pending, err := svc.ListPendingOutbound(context.Background(), "xiaohongshu", "acc-alias")
	if err != nil {
		t.Fatalf("读回别名渠道出站失败: %v", err)
	}
	if len(pending) != 3 {
		t.Fatalf("三个别名共写出 %d 条出站, want 3", len(pending))
	}
	wantURL := "https://cards.example.test/s/" + codeFor("xiaohongshu")
	for _, row := range pending {
		if row.Platform != "xiaohongshu" {
			t.Errorf("出站 platform = %q, want xiaohongshu（别名没归一）", row.Platform)
		}
		if !strings.Contains(row.Content, wantURL) {
			t.Errorf("出站内容没带短链 %q，实际 %q", wantURL, row.Content)
		}
	}
}

// 发不出去的四种具体原因必须各自当场响，而且一条都不能落到出站队列里。
func TestSendCard_UndeliverableCardsFailLoudAndSendNothing(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://cards.example.test")
	db := newReachCardDB(t)
	svc := isolateBridgeOutbox(t, db)

	// a) 卡片已停用
	createReachShortLink(t, db, 31, "c-inactive", 1, nil)
	createReachCard(t, db, "douyin", 41, "停用卡", 31, false)
	// b) 还没生成短链
	createReachCard(t, db, "douyin", 42, "没短链", 0, true)
	// c) 短链已停用（客户打开是 410）
	createReachShortLink(t, db, 33, "c-disabled", 2, nil)
	createReachCard(t, db, "douyin", 43, "短链停用", 33, true)
	// d) 短链已过期
	expired := time.Now().Add(-time.Hour)
	createReachShortLink(t, db, 34, "c-expired", 1, &expired)
	createReachCard(t, db, "douyin", 44, "短链过期", 34, true)

	a := NewIntegrationReachAdapterFromDB(db)
	cases := []struct {
		name   string
		cardID string
		expect string
	}{
		{"卡片停用", "41", "已停用"},
		{"无短链", "42", "还没生成"},
		{"短链停用", "43", "已停用"},
		{"短链过期", "44", "过期"},
	}
	for _, tc := range cases {
		_, err := a.SendCard(context.Background(), "douyin", "acc-bad", "conv-1", tc.cardID)
		if !errors.Is(err, ErrCardNotDeliverable) {
			t.Errorf("%s: err = %v, want ErrCardNotDeliverable", tc.name, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.expect) {
			t.Errorf("%s: 错误没说是哪一格（期望含 %q），实际 %v", tc.name, tc.expect, err)
		}
	}

	pending, err := svc.ListPendingOutbound(context.Background(), "douyin", "acc-bad")
	if err != nil {
		t.Fatalf("读出站队列失败: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("四张发不出的卡片共写出 %d 条出站，want 0（判据必须在投递之前）", len(pending))
	}
}

// 参数与渠道这一层的错不能和"卡片发不出去"混成一类：id 写错、渠道没有卡片表、
// 没装配库，各是各的处置动作。
func TestSendCard_BadArgumentsAndChannels(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "https://cards.example.test")
	db := newReachCardDB(t)
	svc := isolateBridgeOutbox(t, db)
	a := NewIntegrationReachAdapterFromDB(db)
	ctx := context.Background()

	if _, err := a.SendCard(ctx, "wecom", "acc-1", "conv-1", "11"); !errors.Is(err, ErrChannelNotImplemented) {
		t.Errorf("企微没有卡片模板表，应显式拒绝, got %v", err)
	} else if !strings.Contains(err.Error(), "douyin/kuaishou/xiaohongshu/tiktok/xianyu") {
		t.Errorf("错误应列出可发的渠道，方便改用, got %v", err)
	}
	if _, err := a.SendCard(ctx, "sms", "acc-1", "conv-1", "11"); !errors.Is(err, ErrChannelNotImplemented) {
		t.Errorf("短信同样没有卡片这一层, got %v", err)
	}
	if _, err := a.SendCard(ctx, "douyin", "acc-1", "conv-1", "abc"); !errors.Is(err, ErrInvalidInt64) {
		t.Errorf("card_id 非数字应报 ErrInvalidInt64, got %v", err)
	}
	if _, err := a.SendCard(ctx, "douyin", "acc-1", "conv-1", "9999"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Errorf("卡片不存在应透传 not found（那是 id 写错，不是配置缺失）, got %v", err)
	}
	if _, err := a.SendCard(ctx, "douyin", "", "conv-1", "11"); err == nil {
		t.Error("缺 account_id 必须报错")
	}
	if _, err := a.SendCard(ctx, "douyin", "acc-1", "", "11"); err == nil {
		t.Error("缺 external_user_id 必须报错")
	}

	// 库里没有这张卡片时，出站一行都不该有——上面每一种失败都发生在投递之前。
	for _, ch := range reachCardChannels {
		pending, err := svc.ListPendingOutbound(ctx, ch, "acc-1")
		if err != nil {
			t.Fatalf("读 %s 出站队列失败: %v", ch, err)
		}
		if len(pending) != 0 {
			t.Errorf("%s 在参数错误路径上写出 %d 条出站, want 0", ch, len(pending))
		}
	}
}

// PUBLIC_BASE_URL 是部署期声明的对外域名；没配就不能发卡片。
// 后台分享时前端补的是 window.location.origin，服务端没有这个概念，
// 把相对路径 /s/xxxx 发出去等于给客户一条点不开的死链。
func TestSendCard_WithoutPublicBaseURLRefuses(t *testing.T) {
	t.Setenv("PUBLIC_BASE_URL", "")
	db := newReachCardDB(t)
	svc := isolateBridgeOutbox(t, db)
	createReachShortLink(t, db, 51, "c-nobase", 1, nil)
	createReachCard(t, db, "douyin", 61, "没配域名", 51, true)

	a := NewIntegrationReachAdapterFromDB(db)
	_, err := a.SendCard(context.Background(), "douyin", "acc-nobase", "conv-1", "61")
	if !errors.Is(err, ErrCardNotDeliverable) {
		t.Fatalf("未配置公网域名时必须拒绝, got %v", err)
	}
	if !strings.Contains(err.Error(), "PUBLIC_BASE_URL") {
		t.Errorf("错误要指到那个配置项, got %v", err)
	}
	pending, err := svc.ListPendingOutbound(context.Background(), "douyin", "acc-nobase")
	if err != nil {
		t.Fatalf("读出站队列失败: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("没配域名仍写出 %d 条相对链接出站, want 0", len(pending))
	}
}

// 库句柄缺失（未装配）与"渠道不支持"是两类：前者是部署问题。
func TestSendCard_NilDBFailsLoud(t *testing.T) {
	a := NewIntegrationReachAdapterFromDB(nil)
	if _, err := a.SendCard(context.Background(), "douyin", "acc-1", "conv-1", "11"); !errors.Is(err, ErrIntegrationServiceNotConfigured) {
		t.Fatalf("无库时必须报未装配, got %v", err)
	}
}
