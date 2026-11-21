package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

type persistentQueueProof struct{}

func (persistentQueueProof) VerifyCapabilityProof(_ context.Context, q capregistry.CapabilityProofQuery) capregistry.CapabilityProof {
	return capregistry.CapabilityProof{RuntimeVerified: q.Evidence.RuntimeProofRef == "local-queue-proof",
		Identity: capregistry.CapabilityProofIdentity{Key: q.Key, Source: q.Source, Provenance: "local-queue-verifier", ProofRef: "local-queue-proof",
			ConnectionID: q.Evidence.ConnectionID, ConnectionVersion: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "local-contract"}}
}

// 能力表不可用时，生产轮询和推送认领应返回明确拒绝并保留历史队列状态。
func TestPersistentOutboundDelivery_UnavailableStoragePreservesQueue(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.CapabilitySourceBinding{})
	// 测试库复用 schema。将这次能力查询定向到不存在的表，稳定触发 PG 事务中断。
	if err := db.Callback().Query().Before("gorm:query").Register("test:missing_capability_source_table", func(tx *gorm.DB) {
		if tx.Statement.Table == (model.CapabilitySourceBinding{}).TableName() {
			tx.Statement.Table = "ec01_missing_capability_source_bindings"
		}
	}); err != nil {
		t.Fatal(err)
	}
	ctx := tenant.WithTenantID(context.Background(), 501)
	row := seedDeliveryPage(t, db, ctx, "local-account", 1, "text")[0]
	delivery := NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db))

	rows, err := delivery.ClaimPendingOutbound(ctx, row.Platform, row.AccountID, time.Minute, 1)
	if len(rows) != 0 {
		t.Fatalf("能力存储不可用时不得认领，实际 %v", rows)
	}
	requireSubmitDenial(t, err, "capability_storage_unavailable")
	assertDeliveryRowUnchanged(t, db, ctx, row)

	claimed, err := delivery.ClaimOutboundForPush(ctx, uint64(row.ID), row.Platform, row.AccountID, row.MsgType, time.Minute)
	if claimed {
		t.Fatal("能力存储不可用时不得认领推送")
	}
	requireSubmitDenial(t, err, "capability_storage_unavailable")
	assertDeliveryRowUnchanged(t, db, ctx, row)
}

// 在单连接池真实认领，验证同源不同动作独立读取和撤权后零认领。
func TestPersistentOutboundDelivery_SingleConnectionAndRevocation(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{}, &model.MerchantConnection{}, &model.CapabilitySourceBinding{}, &model.CapabilityEvidence{})
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	ctx, cancel := context.WithTimeout(tenant.WithTenantID(context.Background(), 501), 5*time.Second)
	defer cancel()
	expires := time.Now().Add(time.Hour)
	conn := model.MerchantConnection{TenantID: 501, Provider: "douyin_shop", ShopID: "local-shop", Version: 1, CredentialVersion: 1, ConsentVersion: 1, CredentialContract: "local-contract", Scopes: []string{"message.text.send"}, ExpiresAt: &expires}
	if err = db.WithContext(ctx).Create(&conn).Error; err != nil {
		t.Fatal(err)
	}
	binding := model.CapabilitySourceBinding{TenantID: 501, Source: capregistry.SubmissionSourceBridge, Product: conn.Provider, ResourceID: "local-account", ShopID: conn.ShopID, Region: "CN", ConnectionID: conn.ID, ConsentVersion: 1}
	evidence := model.CapabilityEvidence{TenantID: 501, ShopID: conn.ShopID, Product: conn.Provider, Region: "CN", Action: "message.text.send", Carrier: capregistry.ModeBridge, ConnectionID: conn.ID, ConnectionVersion: 1, CredentialVersion: 1, ConsentVersion: 1, ContractVersion: "bridge-v1", DocumentationVersion: "design-cell-v1", Scopes: conn.Scopes, ExpiresAt: expires, Documentation: true, Implementation: true, Provenance: "local-queue-verifier", RuntimeProofRef: "local-queue-proof"}
	if err = db.WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.WithContext(ctx).Select("*").Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if err = db.WithContext(ctx).Model(&evidence).Update("simulated", false).Error; err != nil {
		t.Fatal(err)
	}
	image := seedDeliveryPage(t, db, ctx, binding.ResourceID, 1, "image")[0]
	text := seedDeliveryPage(t, db, ctx, binding.ResourceID, 2, "text")
	repo := repository.NewMessageHubRepositoryWithDB(db)
	defaultDelivery := NewOutboundDeliveryService(repo)
	bound, cell := localSubmitFixture(t, capregistry.SubmissionSourceBridge, conn.Provider, binding.ResourceID, evidence.Action, evidence.Carrier)
	legacy := capregistry.WithCellEvidence(capregistry.WithSubmissionBinding(ctx, bound), cell)
	rows, err := defaultDelivery.ClaimPendingOutbound(legacy, conn.Provider, binding.ResourceID, time.Minute, 2)
	if err == nil || len(rows) != 0 {
		t.Fatalf("默认生产构造不可相信旧上下文或存储引用，rows=%v err=%v", rows, err)
	}
	service := &OutboundDeliveryService{repo: repo, verifier: persistentQueueProof{}}
	rows, err = service.ClaimPendingOutbound(ctx, conn.Provider, binding.ResourceID, time.Minute, 1)
	if err != nil || len(rows) != 1 || rows[0].ID != text[0].ID || rows[0].PushAttempts != 1 {
		t.Fatalf("只应认领一条文本，rows=%v err=%v", rows, err)
	}
	if err = db.WithContext(ctx).Model(&conn).Update("revoked", true).Error; err != nil {
		t.Fatal(err)
	}
	claimed, err := service.ClaimOutboundForPush(ctx, uint64(text[1].ID), conn.Provider, binding.ResourceID, "text", time.Minute)
	if claimed {
		t.Fatal("撤权提交后不得认领")
	}
	requireSubmitDenial(t, err, "permission_revoked")
	assertDeliveryRowUnchanged(t, db, ctx, text[1])
	assertDeliveryRowUnchanged(t, db, ctx, image)
}
