package service

import (
	"context"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// 只在独立测试连接注入查询超时，验证真实仓储到发送闸的故障与恢复。
func TestPipeline_ConsentQueryFailureAndRecovery(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ContactConsentEvent{}, &model.ContactPreference{}, &model.ContactConsentIdentityLink{})
	svc := NewContactConsentService(repository.NewContactConsentRepository(database))
	ctx := context.Background()
	for _, oneID := range []string{"consent-fault-grant", "consent-fault-paused", "consent-fault-revoke"} {
		decision := model.ConsentDecisionGrant
		if oneID == "consent-fault-revoke" {
			decision = model.ConsentDecisionRevoke
		}
		if err := svc.Record(ctx, ConsentRecordInput{OneID: oneID, Purpose: model.ConsentPurposeMarketing, Decision: decision}); err != nil {
			t.Fatalf("写入测试同意事实失败: %v", err)
		}
	}
	if err := svc.SetPreference(ctx, "consent-fault-paused", model.ConsentPurposeMarketing, "", true, nil, ""); err != nil {
		t.Fatalf("写入测试暂停偏好失败: %v", err)
	}
	faultTable := ""
	if err := database.Callback().Query().Before("gorm:query").Register("test:consent-query-fault", func(tx *gorm.DB) {
		if tx.Statement.Table == faultTable {
			tx.AddError(context.DeadlineExceeded)
		}
	}); err != nil {
		t.Fatalf("注册查询故障失败: %v", err)
	}
	t.Cleanup(func() { database.Callback().Query().Remove("test:consent-query-fault") })

	channels := []string{"web", "card", "qq", "unknown-channel"}
	for channel := range defaultThirdPartyRateLimitByChannel {
		channels = append(channels, channel)
	}
	for _, fault := range []struct{ table, reason string }{
		{"contact_consent_identity_links", "consent_identity_unavailable"},
		{"contact_consent_events", "consent_event_unavailable"},
		{"contact_preferences", "consent_preference_unavailable"},
	} {
		for _, mode := range []string{ConsentModeRevocationOnly, ConsentModeEnforce} {
			for _, channel := range channels {
				t.Run(fault.table+"/"+mode+"/"+channel, func(t *testing.T) {
					faultTable = fault.table
					defer func() { faultTable = "" }()
					dec := svc.CheckSendConsent(ctx, "consent-fault-grant", model.ConsentPurposeMarketing, channel)
					if dec.Status != "unavailable" || dec.Trusted || dec.Reason != fault.reason {
						t.Errorf("查询失败应独立标记 unavailable/不可信/来源原因，实际: %+v", dec)
					}
					primary, backup := newSuccessAdapter("primary"), newSuccessAdapter("backup")
					audit := NewMemorySendAuditLogger(10)
					cfg := DefaultSendPipelineConfig(primary)
					cfg.ConsentChecker, cfg.ConsentMode, cfg.AuditLogger = svc, mode, audit
					cfg.FallbackAdapters["backup"] = backup
					pipeline := NewSendPipeline(cfg)
					req := &ReachSendRequest{
						Channel: channel, CustomerID: "consent-fault-grant", RecipientID: "simulated-recipient", Content: "模拟触达",
						Metadata: map[string]string{"consent": "true", "opt_in": "true"},
						Fallback: &FallbackConfig{Enabled: true, BackupChannel: "backup", MaxAttempts: 1},
					}
					resp := pipeline.Send(ctx, req)
					if resp.Success || resp.Deferred || resp.MessageID != "" || primary.Count(ctx) != 0 || backup.Count(ctx) != 0 {
						t.Fatalf("查询故障必须零外发且不得成功/入队，主=%d 备=%d 回执=%+v", primary.Count(ctx), backup.Count(ctx), resp)
					}
					step := findStepResult(resp, SendStepPermission)
					if step == nil || step.Success || step.Skipped || len(step.Output) != 1 {
						t.Fatalf("必须在 permission 步骤拒发并留痕: %+v", step)
					}
					note, ok := step.Output[0].(map[string]any)
					if !ok || note["reason"] != fault.reason || note["retryable"] != true || note["status"] != "unavailable" {
						t.Fatalf("故障来源与可重试语义应保留: %+v", step.Output)
					}
					entries := audit.Entries(ctx)
					if len(entries) != 1 || entries[0].Success || !strings.Contains(entries[0].Error, fault.reason) {
						t.Fatalf("审计必须记录拒发及来源原因: %+v", entries)
					}
					faultTable = ""
					resp = pipeline.Send(ctx, req)
					if !resp.Success || primary.Count(ctx) != 1 || backup.Count(ctx) != 0 {
						t.Fatalf("故障恢复后真实 grant 应发送且不走回退: %+v", resp)
					}
				})
			}
		}
	}

	// 恢复不改写客户事实；撤回、暂停与无历史策略仍按真实状态判定。
	for _, tc := range []struct{ oneID, status string }{
		{"consent-fault-revoke", ConsentStatusRevoked},
		{"consent-fault-paused", ConsentStatusPaused},
		{"consent-fault-missing", ConsentStatusMissing},
	} {
		dec := svc.CheckSendConsent(ctx, tc.oneID, model.ConsentPurposeMarketing, "wecom")
		if dec.Status != tc.status {
			t.Fatalf("恢复后 %s 应为 %s，实际 %+v", tc.oneID, tc.status, dec)
		}
		for _, mode := range []string{ConsentModeRevocationOnly, ConsentModeEnforce} {
			adapter := newSuccessAdapter("recovered")
			cfg := DefaultSendPipelineConfig(adapter)
			cfg.ConsentChecker, cfg.ConsentMode = svc, mode
			resp := NewSendPipeline(cfg).Send(ctx, &ReachSendRequest{Channel: "wecom", CustomerID: tc.oneID})
			wantSend := tc.status == ConsentStatusMissing && mode == ConsentModeRevocationOnly
			if resp.Success != wantSend || (adapter.Count(ctx) > 0) != wantSend {
				t.Fatalf("恢复后 %s/%s 应发送=%v，实际 %+v", tc.oneID, mode, wantSend, resp)
			}
		}
	}
}

// 未初始化依赖不能伪装无历史记录而绕过默认模式。
func TestContactConsent_UninitializedFailsClosed(t *testing.T) {
	for _, svc := range []*ContactConsentService{
		NewContactConsentService(nil),
		NewContactConsentService(repository.NewContactConsentRepository(nil)),
	} {
		dec := svc.CheckSendConsent(context.Background(), "one-fault", model.ConsentPurposeMarketing, "wecom")
		if dec.Status != "unavailable" || dec.Trusted {
			t.Errorf("未初始化仓储应 unavailable/不可信，实际 %+v", dec)
		}
	}
}

type fixedConsentDecision struct{ decision ConsentDecision }

func (f fixedConsentDecision) CheckSendConsent(context.Context, string, string, string) ConsentDecision {
	return f.decision
}

// 未知判定和空权限检查器不能使已装配同意闸失效。
func TestPipeline_ConsentUnknownDecisionFailsClosed(t *testing.T) {
	for _, status := range []string{"", "unexpected", "unavailable"} {
		for _, noPermissionChecker := range []bool{false, true} {
			adapter := newSuccessAdapter("must-not-send")
			cfg := DefaultSendPipelineConfig(adapter)
			cfg.ConsentChecker = fixedConsentDecision{ConsentDecision{Status: status}}
			if noPermissionChecker {
				cfg.PermissionChecker = nil
			}
			resp := NewSendPipeline(cfg).Send(context.Background(), &ReachSendRequest{Channel: "web", CustomerID: "one-fault"})
			if resp.Success || adapter.Count(context.Background()) != 0 {
				t.Errorf("状态 %q / 空权限检查器=%v 必须拒发，实际 %+v", status, noPermissionChecker, resp)
			}
		}
	}
}
