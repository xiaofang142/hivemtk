package service

import (
	"fmt"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

func TestSendOutbound_CardlessPayloadPreservesCardFacts(t *testing.T) {
	for index, tc := range []struct {
		name    string
		channel WebhookChannel
		content string
	}{
		{name: "抖音完整文字替代", channel: ChannelDouyin, content: "这里是推荐方案。"},
		{name: "抖音纯卡片也有可见文字", channel: ChannelDouyin},
		{name: "抖音不承诺点击不可见卡片", channel: ChannelDouyin, content: "我已经为你展示了卡片，请点击卡片查看详情。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, baseCtx := newBridgeSentTestService(t)
			t.Cleanup(func() { svc.Stop(baseCtx) })
			ctx := tenant.WithTenantID(baseCtx, c01TenantA)
			accountID := seedCanonicalOutboundAccount(t, svc.eventRepo.GetDB(), c01TenantA, string(tc.channel), "card-fallback-account")
			hub := bridgeInboundHub(string(tc.channel), accountID, "card-fallback-conversation", "card-fallback-customer")
			hub.TenantID = c01TenantA
			useLegacyBridgeFixture(t)
			ctx = localBridgeSubmitContext(t, ctx, hub.Platform, hub.AccountID, "text")
			eventID := fmt.Sprintf("card-fallback-%d", index)
			releaseReplyClaimForTest(t, eventID)
			cards := []model.RichCard{{
				Title: "合成部署方案", Description: "本地部署，数据保留在企业内。",
				Fields: map[string]string{"内存": "32GB", "费用": "以实际报价为准"},
			}}
			sent, err := svc.sendOutbound(ctx, tc.channel, hub.AccountID, &ParsedPayload{
				EventID: eventID, Sender: hub.SenderID, ChatID: hub.ConversationID, Content: "合成方案咨询",
			}, tc.content, hub, cards)
			if !sent || err != nil {
				t.Fatalf("完整文字载荷应正常入队：sent=%v err=%v", sent, err)
			}
			var rows []model.MessageHub
			if err := svc.eventRepo.GetDB().Where("direction = ? AND status = ?", "outbound", "pending").Find(&rows).Error; err != nil {
				t.Fatalf("查询替代载荷失败：%v", err)
			}
			if len(rows) != 1 {
				t.Fatalf("不得拆多条刷屏：实际=%d", len(rows))
			}
			for _, fact := range []string{"合成部署方案", "数据保留在企业内", "32GB", "以实际报价为准"} {
				if !strings.Contains(rows[0].Content, fact) {
					t.Errorf("对客载荷缺必要事实 %q：%q", fact, rows[0].Content)
				}
			}
			if strings.Contains(rows[0].Content, "请点击卡片") || strings.Contains(rows[0].Content, "展示了卡片") {
				t.Errorf("无载体不应许诺可见卡片：%q", rows[0].Content)
			}
			if asCount(rows[0].Extra["cards_fallback_count"]) != 1 {
				t.Errorf("实际文字替代应可追溯：%+v", rows[0].Extra)
			}
		})
	}
}
