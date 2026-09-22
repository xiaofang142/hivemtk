package repository

import (
	"context"
	"fmt"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

// seedHub 铺一行入站消息；extra 原样带进 Extra，用于验证回填写入时不丢既有键。
func seedHubInboundMedia(t *testing.T, database *gorm.DB, platform, accountID, msgID string, extra model.JSONMap,
) *model.MessageHub {
	t.Helper()
	return seedHubInboundMediaIn(t, database, platform, accountID,
		"conv_"+accountID+"_"+msgID, msgID, extra)
}

// 会话维度要能单独指定：message_hub 的唯一键是 (platform, msg_id, conversation_id)，
// "同一个官方 msg_id 出现在两个会话里"是 schema 允许的真实形状，补腿必须铺得出这种双行。
func seedHubInboundMediaIn(t *testing.T, database *gorm.DB, platform, accountID, conversationID, msgID string,
	extra model.JSONMap,
) *model.MessageHub {
	t.Helper()
	hub := &model.MessageHub{
		Platform: platform, AccountID: accountID, ConversationID: conversationID,
		MsgID: msgID, MsgType: "text", Content: "占位", Direction: "inbound",
		Status: "delivered", Extra: extra,
	}
	if err := database.Create(hub).Error; err != nil {
		t.Fatalf("seed hub(%s/%s) 失败: %v", accountID, msgID, err)
	}
	return hub
}

// convOf 复刻 seed 帮助函数拼会话键的口径，让每条腿的调用参数与铺进去的行对得上。
func convOf(accountID, msgID string) string {
	return "conv_" + accountID + "_" + msgID

}

func TestSetInboundMediaURLsWritesFirstAndAll(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}
	ctx := context.Background()

	const platform, acc, msgID = "dingtalk", "1001", "dt_media_multi"
	seedHubInboundMedia(t, database, platform, acc, msgID, model.JSONMap{"sender_nick": "张三"})

	urls := []string{"/files/dingtalk/a.png", "/files/dingtalk/b.png"}
	found, err := repo.SetInboundMediaURLs(ctx, platform, acc, convOf(acc, msgID), msgID, urls)
	if err != nil {
		t.Fatalf("回填写库失败: %v", err)
	}
	if !found {
		t.Fatal("前置不成立：刚铺的 hub 行没被找到，后面的断言都无意义")
	}

	var row model.MessageHub
	if err := database.Where("platform = ? AND account_id = ? AND msg_id = ?", platform, acc, msgID).
		First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.MediaURL != urls[0] {
		t.Errorf("media_url = %q, want 首条 %q", row.MediaURL, urls[0])
	}
	if got := fmt.Sprint(row.Extra["media_urls"]); got != fmt.Sprint(urls) {
		t.Errorf("Extra.media_urls = %s, want 两张都在且按序（%s）", got, urls)
	}
	// 读-改-写必须保住既有键：只覆盖 media_urls，别把入站时落的 sender_nick 抹掉。
	if got, ok := row.Extra["sender_nick"]; !ok || got != "张三" {
		t.Errorf("Extra.sender_nick = %v (ok=%v)，want 保留「张三」——回填把既有 extra 整列替换掉了", got, ok)
	}
}

// account_id 这一维挡的**不是**跨账号串写：message_hub 的唯一键是
// (platform, msg_id, conversation_id)（见 model.MessageHub 上的
// uni_message_hub_platform_msg_conv），不含 account_id ⇒ "同一 msg_id 同一会话挂在两个账号下"
// 这种双行根本铺不出来。它挡的是账号传错时的越权回填：四键里任一键不匹配就判「没有这一行」，
// 宁可漏一次回填（有 warn 日志）也不去写别人名下的会话行（fail-closed）。
// 必须带后半段"账号对上时必须命中"的对照：只断言 found=false 的话，键拼错也能绿，
// 那条断言就成了空转（同一形状的前置在别处一律钉成 Fatal）。
func TestSetInboundMediaURLsScopesByAccount(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}
	ctx := context.Background()

	const platform, msgID = "dingtalk", "dt_shared_msgid"
	// 会话键故意写成与账号无关的字面量：证明"账号错、会话对"是真能撞上一行的形状。
	row := seedHubInboundMediaIn(t, database, platform, "2001", "conv_dt_shared_msgid", msgID, nil)

	found, err := repo.SetInboundMediaURLs(ctx, platform, "2002", "conv_dt_shared_msgid", msgID,
		[]string{"/files/dingtalk/not-mine.png"})
	if err != nil {
		t.Fatalf("账号不匹配不该是错误，却拿到 err=%v", err)
	}
	if found {
		t.Error("found=true, want false：回填绕过了 account_id 判据，写到另一账号名下的会话行")
	}
	var afterWrong model.MessageHub
	if err := database.Where("id = ?", row.ID).First(&afterWrong).Error; err != nil {
		t.Fatalf("读回 seed 行失败: %v", err)
	}
	if afterWrong.MediaURL != "" {
		t.Errorf("账号不匹配却把 media_url 写成 %q, want 空", afterWrong.MediaURL)
	}
	if _, ok := afterWrong.Extra["media_urls"]; ok {
		t.Errorf("账号不匹配却写了 Extra.media_urls = %v, want 不存在", afterWrong.Extra["media_urls"])
	}

	// 对照组：只把账号换成铺进去的那个，其余三键一字不动 ⇒ 必须命中并写成功。
	found, err = repo.SetInboundMediaURLs(ctx, platform, "2001", "conv_dt_shared_msgid", msgID,
		[]string{"/files/dingtalk/mine.png"})
	if err != nil || !found {
		t.Fatalf("前置不成立：账号对上时没命中（found=%v err=%v）——上面的「不写」就成了空转", found, err)
	}
	var afterRight model.MessageHub
	if err := database.Where("id = ?", row.ID).First(&afterRight).Error; err != nil {
		t.Fatalf("复读 seed 行失败: %v", err)
	}
	if afterRight.MediaURL != "/files/dingtalk/mine.png" {
		t.Errorf("对照组没写进去：media_url = %q, want %q",
			afterRight.MediaURL, "/files/dingtalk/mine.png")
	}
}

func TestSetInboundMediaURLsMissingRowIsNotFoundNotError(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}

	found, err := repo.SetInboundMediaURLs(context.Background(), "dingtalk", "3001",
		convOf("3001", "dt_row_never_written"), "dt_row_never_written", []string{"/files/dingtalk/x.png"})
	if err != nil {
		t.Fatalf("行不存在不该是错误，却拿到 err=%v", err)
	}
	if found {
		t.Error("found=true，want  false（没有这一行却说写到了）")
	}
}

// 读库真失败（上下文已取消）与"没有这一行"必须是两种读数：把前者说成后者，
// 运维就会在"其实库连不上/查询被杀"的现场去找一条不存在的消息。
func TestSetInboundMediaURLsReadFailureIsNotReportedAsNotFound(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}
	const platform, acc, msgID = "dingtalk", "4001", "dt_ctx_canceled"
	seedHubInboundMedia(t, database, platform, acc, msgID, nil)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	found, err := repo.SetInboundMediaURLs(canceled, platform, acc, convOf(acc, msgID), msgID,
		[]string{"/files/dingtalk/y.png"})
	if err == nil {
		t.Fatal("上下文已取消却拿到 err=nil：读失败被吞掉了")
	}
	if found {
		t.Error("found=true，want false（读都没读成功却说写成了）")
	}

	var row model.MessageHub
	if err := database.Where("platform = ? AND account_id = ? AND msg_id = ?", platform, acc, msgID).
		First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.MediaURL != "" {
		t.Errorf("读失败后 media_url 被写成 %q, want 空", row.MediaURL)
	}
}

func TestSetInboundMediaURLsEmptyURLsWriteNothing(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}
	const platform, acc, msgID = "dingtalk", "5001", "dt_no_urls"
	seedHubInboundMedia(t, database, platform, acc, msgID, nil)

	// 守卫被拿掉时真实的失效形状是 urls[0] 越界 panic，而 panic 会带走整个测试二进制
	// （其余腿一条都不报，看起来倒像"干净的一杀"）⇒ 把崩溃关在本腿里，判成红而不是带走别人。
	found, err := func() (bool, error) {
		defer func() {
			if p := recover(); p != nil {
				t.Errorf("空 urls 本应被守卫挡在写库之前，却走到了崩溃：%v", p)
			}
		}()
		return repo.SetInboundMediaURLs(context.Background(), platform, acc, convOf(acc, msgID), msgID, nil)
	}()
	if err != nil {
		t.Fatalf("空 urls 不该报错: %v", err)
	}
	if found {
		t.Error("found=true，want false（一条链接都没有却说写了）")
	}
	var row model.MessageHub
	if err := database.Where("platform = ? AND account_id = ? AND msg_id = ?", platform, acc, msgID).
		First(&row).Error; err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if row.MediaURL != "" {
		t.Errorf("media_url = %q, want 空", row.MediaURL)
	}
}

// 装配漏了 DB 句柄（router 传下 nil gormDB）时，回填必须报"我做不了"，不能报"没这一行"：
// 后者会把运维派去找一条根本不存在的消息，而真因是服务压根没连库。
func TestSetInboundMediaURLsNilHandleIsErrorNotNotFound(t *testing.T) {
	repo := &MessageHubRepository{}
	found, err := repo.SetInboundMediaURLs(context.Background(), "dingtalk", "6001",
		"conv_6001_dt_no_db_handle", "dt_no_db_handle", []string{"/files/dingtalk/z.png"})
	if err == nil {
		t.Fatal("没有 DB 句柄却拿到 err=nil——回填把手工接线失败说成\"消息不存在\"")
	}
	if found {
		t.Error("found=true，want false（没句柄却说写成了）")
	}
}

// 唯一键是 (platform, msg_id, conversation_id)，而 (platform, account_id, msg_id) 不是：
// 同一条官方消息被两个会话各上报一次就是两行。只按后者 + First 会把媒体写进 id 最小的那条，
// 真正在等的工作台上那条永远留着 [图片]。
func TestSetInboundMediaURLsScopesByConversation(t *testing.T) {
	database := testutil.NewTestDB(t, &model.MessageHub{})
	repo := &MessageHubRepository{db: database}
	const platform, acc, msgID = "dingtalk", "8001", "dt_two_convs"

	// "不该被碰"的那一行先铺 ⇒ id 更小 ⇒ 任何丢掉会话维度的查询都会先命中它。
	other := seedHubInboundMediaIn(t, database, platform, acc, "conv_other", msgID, nil)
	target := seedHubInboundMediaIn(t, database, platform, acc, "conv_target", msgID, nil)

	found, err := repo.SetInboundMediaURLs(context.Background(), platform, acc, "conv_target", msgID,
		[]string{"/files/dingtalk/right.png"})
	if err != nil {
		t.Fatalf("回填失败: %v", err)
	}
	if !found {
		t.Fatal("前置不成立：请求的 conv_target 那一行没找到，下面「没串写」的断言就只是空转")
	}

	var got model.MessageHub
	if err := database.Where("id = ?", target.ID).First(&got).Error; err != nil {
		t.Fatalf("读回目标行失败: %v", err)
	}
	if got.MediaURL != "/files/dingtalk/right.png" {
		t.Errorf("目标行 media_url = %q, want %q", got.MediaURL, "/files/dingtalk/right.png")
	}
	var touched model.MessageHub
	if err := database.Where("id = ?", other.ID).First(&touched).Error; err != nil {
		t.Fatalf("读回另一会话行失败: %v", err)
	}
	if touched.MediaURL != "" {
		t.Errorf("另一会话的 media_url 被写成 %q, want 空（回填按 msg_id 命中了 id 更小的那行）", touched.MediaURL)
	}
	if _, ok := touched.Extra["media_urls"]; ok {
		t.Errorf("另一会话的 Extra.media_urls 被写成 %v, want 不存在", touched.Extra["media_urls"])
	}
}
