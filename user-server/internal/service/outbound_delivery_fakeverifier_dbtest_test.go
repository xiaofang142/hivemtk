//go:build dbtest

package service

import (
	"context"
	"os"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// TestOutboundDelivery_FakeVerifier_DB_OptIn is an opt-in DB-backed integration test.
// It exercises the simulated path: with a DB row having Provenance="fake-verifier" and RuntimeProofRef set,
// and CAPABILITY_PROOF_FAKE=1 (+ CAPABILITY_PROOF_FAKE_PROVENANCE=fake-verifier), authorization behaves
// via FakeCapabilityProofVerifier through PersistentCapabilityResolver / OutboundDeliveryService.
//
// Run (from hivemtk/user-server):
//
//	HIVEMTK_DBTEST=1 CAPABILITY_PROOF_FAKE=1 CAPABILITY_PROOF_FAKE_PROVENANCE=fake-verifier \
//	  go test -tags dbtest ./internal/service -run TestOutboundDelivery_FakeVerifier_DB_OptIn -count=1 -v
//
// If DB is not configured, testutil.NewTestDB will fail per project rules; this test does not skip.
func TestOutboundDelivery_FakeVerifier_DB_OptIn(t *testing.T) {
	os.Setenv("CAPABILITY_PROOF_FAKE", "1")
	os.Setenv("CAPABILITY_PROOF_FAKE_PROVENANCE", "fake-verifier")
	defer os.Unsetenv("CAPABILITY_PROOF_FAKE")
	defer os.Unsetenv("CAPABILITY_PROOF_FAKE_PROVENANCE")

	db := testutil.NewTestDB(t,
		&model.MessageHub{},
		&model.MerchantConnection{},
		&model.CapabilitySourceBinding{},
		&model.CapabilityEvidence{},
	)
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)

	ctx := tenant.WithTenantID(context.Background(), 601)
	expires := time.Now().Add(1 * time.Hour)

	conn := model.MerchantConnection{
		TenantID:           601,
		Provider:           "douyin_shop",
		ShopID:             "fake-shop",
		Version:            1,
		CredentialVersion:  1,
		ConsentVersion:     1,
		CredentialContract: "fake-contract",
		Scopes:             []string{"message.text.send"},
		ExpiresAt:          &expires,
	}
	if err := db.WithContext(ctx).Create(&conn).Error; err != nil {
		t.Fatal(err)
	}

	binding := model.CapabilitySourceBinding{
		TenantID:       601,
		Source:         capregistry.SubmissionSourceBridge,
		Product:        conn.Provider,
		ResourceID:     "fake-account",
		ShopID:         conn.ShopID,
		Region:         "CN",
		ConnectionID:   conn.ID,
		ConsentVersion: 1,
	}
	if err := db.WithContext(ctx).Create(&binding).Error; err != nil {
		t.Fatal(err)
	}

	evidence := model.CapabilityEvidence{
		TenantID:           601,
		ShopID:             conn.ShopID,
		Product:            conn.Provider,
		Region:             "CN",
		Action:             "message.text.send",
		Carrier:            capregistry.ModeBridge,
		ConnectionID:       conn.ID,
		ConnectionVersion:  1,
		CredentialVersion:  1,
		ConsentVersion:     1,
		ContractVersion:    "bridge-v1",
		DocumentationVersion: "design-cell-v1",
		Scopes:             conn.Scopes,
		ExpiresAt:          expires,
		Documentation:      true,
		Implementation:     true,
		Simulated:          false,
		Provenance:         "fake-verifier",
		RuntimeProofRef:    "fake-proof-ref-1",
	}
	if err := db.WithContext(ctx).Select("*").Create(&evidence).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.WithContext(ctx).Model(&evidence).Update("simulated", false).Error; err != nil {
		t.Fatal(err)
	}

	// Seed an outbound pending row.
	row := model.MessageHub{
		TenantID:       601,
		Platform:       conn.Provider,
		AccountID:      binding.ResourceID,
		Direction:      "outbound",
		Status:         "pending",
		MsgType:        "text",
		MsgID:          "fakeverifier-msg-1",
		Content:        "hello",
		ConversationID: "conv",
	}
	if err := db.WithContext(ctx).Create(&row).Error; err != nil {
		t.Fatal(err)
	}

	repo := repository.NewMessageHubRepositoryWithDB(db)
	// OutboundDeliveryService will pick up CAPABILITY_PROOF_FAKE from env via NewOutboundDeliveryService.
	svc := NewOutboundDeliveryService(repo)
	rows, err := svc.ClaimPendingOutbound(ctx, conn.Provider, binding.ResourceID, time.Minute, 10)
	if err != nil {
		t.Fatalf("expected no error with fake verifier + valid DB evidence, got: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("expected to claim seeded row, got rows=%v err=%v", rows, err)
	}
	t.Logf("claimed row id=%d status=%s push_attempts=%d", rows[0].ID, rows[0].Status, rows[0].PushAttempts)
}
