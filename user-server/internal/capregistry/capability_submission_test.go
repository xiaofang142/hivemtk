package capregistry

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/tenant"
)

// 精确裁决必须拒绝无租户、系统范围及空账号/订单，不以合成证据替代请求身份。
func TestAuthorizeSubmission_RequiresExactRequestIdentity(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	key := CellKey{TenantID: 501, ShopID: "local-shop", Product: "douyin_shop", Region: "CN", Action: "message.text.send", Carrier: ModeBridge}
	ctx := tenant.WithTenantID(context.Background(), key.TenantID)
	binding := SubmissionBinding{Source: SubmissionSourceBridge, Platform: key.Product, ResourceID: "local-account", Key: key}
	evidence := readyCellEvidence(key, now)
	cases := []struct {
		name, resource, reason string
		ctx                    context.Context
	}{
		{"nil上下文", "local-account", "tenant_context_missing", nil},
		{"无租户", "local-account", "tenant_context_missing", context.Background()},
		{"系统范围", "local-account", "tenant_scope_invalid", tenant.WithSystemScope(ctx)},
		{"空请求账号", "", "capability_mapping_incomplete", ctx},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			requestBinding := binding
			requestBinding.ResourceID = tc.resource
			requestContext := tc.ctx
			if requestContext != nil {
				requestContext = WithCellEvidence(WithSubmissionBinding(requestContext, requestBinding), evidence)
			}
			err := AuthorizeBridgeSubmission(requestContext, key.Product, tc.resource, "text", now)
			var denied *SubmissionDeniedError
			if !errors.As(err, &denied) || denied.CapabilityReasonCode() != tc.reason {
				t.Fatalf("无精确身份必须拒绝，期望 %s，实际 %v", tc.reason, err)
			}
		})
	}
}

// 每次调用重新按当前时间和证据裁决；文本/图片、退款/售后均使用各自精确 action。
func TestAuthorizeSubmission_ExactCellsAndNoCachedPermission(t *testing.T) {
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	for _, definition := range CellDefinitions() {
		if definition.ExecutionMode != ModeBridge && definition.ExecutionMode != ModeAPIAdapter {
			continue // 工作台与平台托管的格没有自动提交路径。
		}
		if definition.Action == "order.address.change" {
			continue // 该动作由既有 connector gate 覆盖，非本轮新增边界。
		}
		t.Run(definition.Product+"/"+definition.Action, func(t *testing.T) {
			key := CellKey{TenantID: 501, ShopID: "local-shop", Product: definition.Product, Region: definition.Region, Action: definition.Action, Carrier: definition.Carrier}
			evidence := readyCellEvidence(key, now)
			evidence.Scopes = definition.RequiredScopes
			source := SubmissionSourceBridge
			if key.Carrier == ModeAPIAdapter {
				source = SubmissionSourceAfterSale
			}
			binding := SubmissionBinding{Source: source, Platform: key.Product, ResourceID: "local-resource", Key: key}
			ctx := WithSubmissionBinding(tenant.WithTenantID(context.Background(), key.TenantID), binding)
			authorize := func(e CellEvidence, at time.Time) error {
				ctx := WithCellEvidence(ctx, e)
				if source == SubmissionSourceAfterSale {
					return AuthorizeAfterSaleSubmission(ctx, key.Product, binding.ResourceID, key.Action, at)
				}
				msgType := "text"
				if key.Action == "message.image.send" {
					msgType = "image"
				}
				return AuthorizeBridgeSubmission(ctx, key.Product, binding.ResourceID, msgType, at)
			}
			if err := authorize(evidence, now); err != nil {
				t.Fatalf("精确本地证据应通过纯裁决，实际 %v", err)
			}
			if err := authorize(evidence, *evidence.ExpiresAt); !IsSubmissionDenied(err) {
				t.Fatalf("到期不能复用旧结果，实际 %v", err)
			}
			evidence.Revoked = true
			if err := authorize(evidence, now); !IsSubmissionDenied(err) {
				t.Fatalf("撤权不能复用旧结果，实际 %v", err)
			}
			evidence.Revoked = false
			evidence.Version = "old-version"
			if err := authorize(evidence, now); !IsSubmissionDenied(err) {
				t.Fatalf("版本变化不能复用旧结果，实际 %v", err)
			}
		})
	}
}
