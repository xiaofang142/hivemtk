package app

import (
	"testing"
	"time"

	knowledgesvc "hivemtk-user/internal/aiagent/knowledge/service"
	"hivemtk-user/internal/model"
	appsvc "hivemtk-user/internal/service"

	"github.com/stretchr/testify/require"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/testutil"
)

var wantSalesMiddlewareKnowledgeWired = []string{
	"sales.identity_max_attempts",
	"middleware.audit_flush_interval",
	"middleware.mfa_recent_verify_ttl",
	"knowledge.max_upload_file_size",
	"knowledge.merchant_knowledge_max_len",
}

func resetSalesMiddlewareKnowledgeProviders(t *testing.T) {
	t.Helper()
	// 生产 setter 传 nil 是「不注入」不是「复位」，复位只能同包直接写 provider 变量。
	t.Cleanup(func() {
		appsvc.SetIdentityMaxAttemptsProvider(nil)
		middleware.SetAuditFlushIntervalProvider(nil)
		middleware.SetMfaRecentVerifyTTLProvider(nil)
		knowledgesvc.SetMaxUploadFileSizeProvider(nil)
		knowledgesvc.SetMerchantKnowledgeMaxLenProvider(nil)
	})
}

func TestWireSalesMiddlewareKnowledgeWecomChannelGWConfigParamsKeys(t *testing.T) {
	resetSalesMiddlewareKnowledgeProviders(t)
	got := WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams()
	require.Equal(t, wantSalesMiddlewareKnowledgeWired, got, "接线键名必须逐条相等且顺序稳定")

	seen := map[string]bool{}
	for _, k := range got {
		require.False(t, seen[k], "接线键名重复：%s", k)
		seen[k] = true
	}
}

func TestWiredSalesMiddlewareKnowledgeKeysExistInSeed(t *testing.T) {
	// 刻意写死期望值而不是从 DefaultParamDefs 里过滤：拼错 group 也算 misc 组，
	// 那样漏接一条根本没人会发现。
	inSeed := map[string]bool{}
	for _, d := range appsvc.DefaultParamDefs() {
		inSeed[d.Group+"."+d.Key] = true
	}
	for _, k := range wantSalesMiddlewareKnowledgeWired {
		require.True(t, inSeed[k], "%s 不在 DefaultParamDefs 里，接线指向了不存在的参数", k)
	}
}

func newSalesMiddlewareKnowledgeParamFixture(t *testing.T) {
	t.Helper()
	resetSalesMiddlewareKnowledgeProviders(t)
	db := testutil.NewTestDBOrSkip(t, &model.ConfigParam{}, &model.ConfigParamAuditLog{})
	require.NoError(t, appsvc.SeedConfigParams(t.Context(), db))
	appsvc.SetGlobalForTest(appsvc.NewConfigParamService(db))
	t.Cleanup(func() { appsvc.SetGlobalForTest(nil) })
}

func TestWireSalesMiddlewareKnowledgeWecomChannelGWConfigParamsReachesReaders(t *testing.T) {
	newSalesMiddlewareKnowledgeParamFixture(t)
	cp := appsvc.GlobalConfigParam()
	bg := t.Context()

	require.NoError(t, cp.UpdateValue(bg, "sales", "identity_max_attempts", "11", 1))
	require.NoError(t, cp.UpdateValue(bg, "middleware", "audit_flush_interval", "17", 1))
	require.NoError(t, cp.UpdateValue(bg, "middleware", "mfa_recent_verify_ttl", "1900", 1))
	require.NoError(t, cp.UpdateValue(bg, "knowledge", "max_upload_file_size", "20971520", 1))
	require.NoError(t, cp.UpdateValue(bg, "knowledge", "merchant_knowledge_max_len", "1234", 1))

	WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams()

	require.Equal(t, 11, appsvc.ProbeIdentityMaxAttempts())
	require.Equal(t, 17*time.Second, middleware.AuditFlushInterval())
	require.Equal(t, 1900*time.Second, middleware.MFARecentVerifyTTL())
	require.Equal(t, int64(20971520), knowledgesvc.MaxUploadFileSize())
	require.Equal(t, 1234, knowledgesvc.MerchantKnowledgeMaxLen())
}

func TestWireSalesMiddlewareKnowledgeWecomChannelGWConfigParamsWithoutDB(t *testing.T) {
	resetSalesMiddlewareKnowledgeProviders(t)
	appsvc.SetGlobalForTest(nil)
	t.Cleanup(func() { appsvc.SetGlobalForTest(nil) })

	got := WireSalesMiddlewareKnowledgeWecomChannelGWConfigParams()
	require.Len(t, got, 5, "无库时仍应报告接了 5 条，只是取值全部回落兜底")

	require.Equal(t, appsvc.DefaultIdentityMaxAttempts, appsvc.ProbeIdentityMaxAttempts())
	require.Equal(t, middleware.DefaultAuditFlushInterval, middleware.AuditFlushInterval())
	require.Equal(t, middleware.DefaultMfaRecentVerifyTTL, middleware.MFARecentVerifyTTL())
	require.Equal(t, knowledgesvc.DefaultMaxUploadFileSize, knowledgesvc.MaxUploadFileSize())
	require.Equal(t, knowledgesvc.DefaultMerchantKnowledgeMaxLen, knowledgesvc.MerchantKnowledgeMaxLen())
}
