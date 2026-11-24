package bridge

import (
	"context"
	"fmt"
	"strconv"
	"testing"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type bridgeIdentifyFixture struct {
	Code        string
	AccountCode string
	AccountID   string
	TenantID    uint
	ChannelID   uint
}

// seedCanonicalBridgeFixture 播种权威 Channel/ChannelAccount/ChannelIdentifyCode 三件套。
// 返回数值 ChannelAccount.ID，显式账号校验与 identify_code 解析使用同一份主数据。
func seedCanonicalBridgeFixture(t *testing.T, db *gorm.DB, tenantID uint, channel, accountName string) bridgeIdentifyFixture {
	t.Helper()
	return seedCanonicalBridgeFixtureWithID(t, db, tenantID, channel, accountName, 0)
}

func seedCanonicalBridgeFixtureWithID(t *testing.T, db *gorm.DB, tenantID uint, channel, accountName string, accountID uint) bridgeIdentifyFixture {
	t.Helper()
	previousEdition := edition.Current()
	edition.ResetForTest(edition.TypeCustom)
	t.Cleanup(func() { edition.ResetForTest(previousEdition) })
	channel = NormalizeBridgeChannel(channel)
	var ch model.Channel
	err := db.WithContext(tenant.WithSystemScope(context.Background())).
		Where("tenant_id = ? AND code = ?", tenantID, channel).First(&ch).Error
	if err != nil {
		if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(&model.Channel{
			TenantID: tenantID, Name: channel, Code: channel, Type: "dm", Enabled: true,
		}).Error; err != nil {
			t.Fatalf("写入 canonical 渠道夹具失败 channel=%s: %v", channel, err)
		}
		if err := db.WithContext(tenant.WithSystemScope(context.Background())).
			Where("tenant_id = ? AND code = ?", tenantID, channel).First(&ch).Error; err != nil {
			t.Fatalf("回读 canonical 渠道夹具失败 channel=%s: %v", channel, err)
		}
	}
	account := &model.ChannelAccount{
		ID: accountID, TenantID: tenantID, ChannelID: ch.ID, Name: accountName,
		IdentifyCode: fmt.Sprintf("account-fixture-%s-%d-%s", channel, tenantID, accountName), Status: 1,
		DataScope: "{}", OwnerUserIDs: model.JSONStrings{},
	}
	if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(account).Error; err != nil {
		t.Fatalf("写入 canonical 账号夹具失败 channel=%s: %v", channel, err)
	}
	code := fmt.Sprintf("fixture-identify-%s-%d", channel, tenantID)
	var existing model.ChannelIdentifyCode
	err = db.WithContext(tenant.WithSystemScope(context.Background())).Where("channel_id = ?", ch.ID).First(&existing).Error
	if err == nil {
		code = existing.Code
	} else if err := db.WithContext(tenant.WithSystemScope(context.Background())).Create(&model.ChannelIdentifyCode{
		TenantID: tenantID, ChannelID: ch.ID, Code: code, Version: 1,
	}).Error; err != nil {
		t.Fatalf("写入 canonical 识别码夹具失败 channel=%s: %v", channel, err)
	}
	return bridgeIdentifyFixture{
		Code: code, AccountCode: account.IdentifyCode, AccountID: strconv.FormatUint(uint64(account.ID), 10),
		TenantID: tenantID, ChannelID: ch.ID,
	}
}

func bindingCtxWithIdentifyCode(t *testing.T, rawQuery, identifyCode string) *gin.Context {
	t.Helper()
	c := bindingCtx(t, rawQuery, nil)
	c.Request = c.Request.WithContext(edition.WithContext(c.Request.Context(), edition.TypeCustom))
	if identifyCode != "" {
		c.Request.Header.Set("X-Channel-Identify-Code", identifyCode)
	}
	return c
}

func newCanonicalAckHandler(t *testing.T, channel, accountName string) (*gorm.DB, *BridgeIngestHandler, bridgeIdentifyFixture) {
	t.Helper()
	database := setupBridgeTestDB(t, &model.Channel{}, &model.ChannelAccount{}, &model.ChannelIdentifyCode{}, &model.MessageHub{})
	fixture := seedCanonicalBridgeFixture(t, database, 1, channel, accountName)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = service.NewInboxIngressService(capregistry.AuthorizeBridgeSubmission)
	return database, h, fixture
}
