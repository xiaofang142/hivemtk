package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// 品牌许可不跨品牌扩权，历史自述和到期证据不能营销。
func TestScopedConsent_BrandEvidenceExpiryAndWithdrawal(t *testing.T) {
	svc, repo := setupConsentService(t)
	ctx := tenant.WithTenantID(t.Context(), 881)
	in := ConsentRecordInput{OneID: "buyer", Purpose: model.ConsentPurposeMarketing, ChannelScope: "email", Decision: model.ConsentDecisionGrant, Source: model.ConsentSourceImport, Brand: "brand-a", LegalBasis: "consent", StatementVersion: "v1", EvidenceRef: "signed-form", SourceRef: "official-export"}
	require.NoError(t, svc.Record(ctx, in))
	require.Equal(t, ConsentStatusGranted, svc.CheckScopedSendConsent(ctx, "buyer", "marketing", "email", "brand-a").Status)
	require.Equal(t, ConsentStatusMissing, svc.CheckScopedSendConsent(ctx, "buyer", "marketing", "email", "brand-b").Status)
	require.Equal(t, ConsentStatusMissing, svc.CheckScopedSendConsent(ctx, "buyer", "marketing", "sms", "brand-a").Status)
	in.Source = model.ConsentSourceLegacyAssert
	require.Error(t, svc.Record(ctx, in))
	require.NoError(t, repo.AppendEvent(ctx, &model.ContactConsentEvent{OneID: in.OneID, Purpose: in.Purpose, ChannelScope: in.ChannelScope, Decision: in.Decision, Source: in.Source, Brand: in.Brand, StatementVersion: in.StatementVersion, EvidenceRef: in.EvidenceRef, SourceRef: in.SourceRef, LegalBasis: in.LegalBasis, OccurredAt: time.Now()}))
	in.Source = model.ConsentSourceSelfService
	past := time.Now().Add(-time.Minute)
	in.ExpiresAt = &past
	require.NoError(t, svc.Record(ctx, in))
	require.Equal(t, ConsentStatusMissing, svc.CheckScopedSendConsent(ctx, "buyer", "marketing", "email", "brand-a").Status)
	in.ExpiresAt = nil
	in.Decision = model.ConsentDecisionRevoke
	in.Brand = ""
	require.NoError(t, svc.Record(ctx, in))
	require.Equal(t, ConsentStatusRevoked, svc.CheckScopedSendConsent(ctx, "buyer", "marketing", "email", "brand-a").Status)
}

// 导入没有许可证据可以保留事实，但不得进入营销发送。
func TestScopedConsent_UnprovenImportNeverSends(t *testing.T) {
	svc, _ := setupConsentService(t)
	ctx := tenant.WithTenantID(t.Context(), 882)
	require.NoError(t, svc.Record(ctx, ConsentRecordInput{OneID: "lead", Purpose: "marketing", Decision: "grant", Source: "import", Brand: "brand-a"}))
	adapter := newSuccessAdapter("should-not-send")
	cfg := DefaultSendPipelineConfig(adapter)
	cfg.ConsentChecker = svc
	cfg.RequireScopedConsent = true
	resp := NewSendPipeline(cfg).Send(ctx, &ReachSendRequest{CustomerID: "lead", Channel: "email", RecipientID: "fixture", Metadata: map[string]string{"brand": "brand-a"}})
	require.False(t, resp.Success)
	require.Zero(t, adapter.Count(ctx))
}
