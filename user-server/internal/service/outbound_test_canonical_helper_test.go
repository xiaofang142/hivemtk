package service

import (
	"context"
	"fmt"
	"testing"

	"gorm.io/gorm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// seedCanonicalOutboundAccount 在测试库播种 canonical Channel/ChannelAccount，
// 返回可直接传给 resolveOutboundAccount 的数值账号 ID 字符串。
// E3 显式账号契约要求 bridge 出站账号必须是可解析的数值 ID，旧字符串账号夹具须迁移。
func seedCanonicalOutboundAccount(t *testing.T, db *gorm.DB, tenantID uint, channelCode, name string) string {
	t.Helper()
	sys := tenant.WithSystemScope(context.Background())
	var ch model.Channel
	if err := db.WithContext(sys).Where("tenant_id = ? AND code = ?", tenantID, channelCode).First(&ch).Error; err != nil {
		ch = model.Channel{TenantID: tenantID, Name: channelCode, Code: channelCode, Type: "dm", Enabled: true}
		if err := db.WithContext(sys).Create(&ch).Error; err != nil {
			t.Fatalf("播种 canonical 渠道失败: %v", err)
		}
	}
	acc := &model.ChannelAccount{
		TenantID: tenantID, ChannelID: ch.ID, Name: name,
		IdentifyCode: fmt.Sprintf("canonical-%s-%d-%s", channelCode, tenantID, name),
		Status:       1, DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(sys).Create(acc).Error; err != nil {
		t.Fatalf("播种 canonical 账号失败: %v", err)
	}
	return fmt.Sprintf("%d", acc.ID)
}
