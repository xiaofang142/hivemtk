package service

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"

	"gorm.io/gorm"
)

// seedCanonicalBridgeOutboundAccount 为桥接出站用例播种权威渠道/账号主数据。
// 显式账号校验只认数值 ChannelAccount.ID，不再接受历史测试里的外部字符串账号。
func seedCanonicalBridgeOutboundAccount(t *testing.T, db *gorm.DB, tenantID uint, channelCode, name string) string {
	t.Helper()
	channel := &model.Channel{TenantID: tenantID, Name: channelCode, Code: channelCode, Type: "dm", Enabled: true}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(channel).Error; err != nil {
		t.Fatalf("写入 canonical 渠道夹具失败 channel=%s: %v", channelCode, err)
	}
	account := &model.ChannelAccount{
		TenantID: tenantID, ChannelID: channel.ID, Name: name,
		IdentifyCode: fmt.Sprintf("bridge-fixture-%s-%d-%s", channelCode, tenantID, name), Status: 1,
		DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(account).Error; err != nil {
		t.Fatalf("写入 canonical 账号夹具失败 channel=%s: %v", channelCode, err)
	}
	return strconv.FormatUint(uint64(account.ID), 10)
}

// localBridgeSubmitContextCustomEdition 在既有精确能力夹具上显式开放定制版渠道，
// 让账号校验走真实 ChannelRepository edition 门，而不是绕过版本控制。
func localBridgeSubmitContextCustomEdition(t *testing.T, ctx context.Context, product, account, msgType string) context.Context {
	t.Helper()
	return localBridgeSubmitContext(t, edition.WithContext(ctx, edition.TypeCustom), product, account, msgType)
}

func customEditionContext(ctx context.Context) context.Context {
	return edition.WithContext(ctx, edition.TypeCustom)
}
