package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

// TestNewWebhookService_DrainsDueDelayedOnBootWithoutShopAuthorization 锁住 T-P0-07 的接线：免打扰到期回复的
// 消费循环必须由**服务装配**起来，而不是等"下一次又命中免打扰入队"时才顺带启动 ——
// 那种惰性启动在进程重启后等于没有消费者，已到期的 AI 回复会永久搁置。
//
// 被测路径刻意只调用 NewWebhookService（生产装配入口），不直接调 dispatchDueDelayedOutbound，
// 否则测试就退化成对派发函数本身的覆盖，接线的洞依然无人看守。
// 后台没有可信店铺映射加载源，消费到期行后须明确拒绝且零出站。
// 反向测试：删掉 webhook.go 中的 s.startDelayedOutboundDispatch() ⇒ 行保持 pending，测试失败。
func TestNewWebhookService_DrainsDueDelayedOnBootWithoutShopAuthorization(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{}, &DelayedOutboundReply{}, &model.InboxConversation{}, &model.CustomerSession{}, &model.Channel{}, &model.ChannelAccount{}, &model.BridgeAccount{}, &model.CapabilitySourceBinding{})
	priorEdition := edition.Current()
	edition.SetCurrent(edition.TypeCustom)
	t.Cleanup(func() { edition.SetCurrent(priorEdition) })
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	account := seedCanonicalOutboundAccount(t, db, tenant.DefaultTenantID, "douyin", "acct-boot-"+suffix)
	conv := "conv-boot-" + suffix

	rec := &DelayedOutboundReply{
		TenantID:       tenant.DefaultTenantID,
		Platform:       string(ChannelDouyin),
		AccountID:      account,
		ConversationID: conv,
		SenderID:       "sender-boot-" + suffix,
		Content:        "重启后应被投递的到期回复 " + suffix,
		SendAt:         time.Now().Add(-time.Minute),
		Status:         "pending",
	}
	if err := db.WithContext(ctx).Create(rec).Error; err != nil {
		t.Fatalf("预置到期记录失败: %v", err)
	}
	seedReplayAISession(t, db, ctx, rec)

	svc := NewWebhookService()
	defer svc.Stop(ctx)

	deadline := time.Now().Add(10 * time.Second)
	var after DelayedOutboundReply
	for {
		if err := db.WithContext(ctx).First(&after, rec.ID).Error; err != nil {
			t.Fatalf("回读延迟记录失败: %v", err)
		}
		if after.Status == model.DelayedStatusFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("服务装配后10s内到期回复未消费并明确拒绝，status=%q reason=%q", after.Status, after.LastError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !strings.Contains(after.LastError, "capability_mapping_missing") || after.Attempts != 0 || after.SentAt != nil {
		t.Fatalf("缺店铺授权应留下不可重试拒绝且无成功时间，reason=%q attempts=%d sent_at=%v", after.LastError, after.Attempts, after.SentAt)
	}
	var count int64
	if err := db.WithContext(ctx).Model(&model.MessageHub{}).Where("direction = ? AND conversation_id = ?", "outbound", conv).Count(&count).Error; err != nil {
		t.Fatalf("查询出站队列失败: %v", err)
	}
	if count != 0 {
		t.Fatalf("后台不能靠账号租户归属代替店铺授权，实际出站 %d 条", count)
	}
}
