package service

import (
	"context"
	"fmt"
	"os"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/capregistry"
	bizerr "hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// ValidateBridgeOutboundCapabilityPreflight 在「渠道-智能体绑定」配置期就校验该渠道是否登记了
// 桥接出站能力格（message.text.send/bridge）。否则线上 ai_result 才拿到
// capability_submit_denied: capability_mapping_missing，且这是 non-retryable 失败，消息会永久
// 卡在 pending（生产 16 条抖音滞留 1099 分钟即此形态）。配置期 fail fast 把失败前移到可修复点。
func ValidateBridgeOutboundCapabilityPreflight(channel, accountID string) error {
	normalized := NormalizeChannelType(channel)
	if normalized == "" {
		return bizerr.New(bizerr.CodeParamInvalid, "渠道不能为空，无法校验桥接出站能力")
	}
	for _, cell := range capregistry.CellDefinitions() {
		if cell.Product == normalized && cell.Action == "message.text.send" && cell.Carrier == capregistry.ModeBridge {
			return nil
		}
	}
	return bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf(
		"渠道 %s（归一为 %s，账号 %s）未登记桥接出站能力 message.text.send/bridge，不能绑定智能体自动回复；请先在能力矩阵登记该渠道的发送能力格",
		channel, normalized, accountID))
}

// BridgeCapabilityAuthorizer is an explicitly composed, local authorization strategy.
// When omitted, production uses the persistent evidence reader.
type BridgeCapabilityAuthorizer func(context.Context, string, string, string, time.Time) error

func (s *InboxIngressService) bridgeCapabilityAuthorizer() BridgeCapabilityAuthorizer {
	if s != nil {
		return s.outboundAuthorizer
	}
	return nil
}

// defaultCapabilityResolver builds the production-ish persistent resolver.
// Env/switch behavior (local/simulated only, never enable in production):
//   - CAPABILITY_PROOF_FAKE=1: attach capregistry.NewFakeCapabilityProofVerifier (dev/test). Also enabled when GIN_MODE=="debug".
//   - CAPABILITY_PROOF_FAKE_PROVENANCE: allowed provenance string for the fake verifier (defaults to "fake-verifier" if empty).
//   - When unset/false in non-debug mode, Verifier is nil => no runtime proof verification => cells typically remain pending_live_validation per capregistry rules.
func defaultCapabilityResolver() capregistry.PersistentCapabilityResolver {
	resolver := capregistry.PersistentCapabilityResolver{Reader: repository.DefaultCapabilityEvidenceRepository()}
	// Opt-in fake verifier for dev/test only.
	if os.Getenv("CAPABILITY_PROOF_FAKE") == "1" || os.Getenv("GIN_MODE") == "debug" {
		prov := os.Getenv("CAPABILITY_PROOF_FAKE_PROVENANCE")
		resolver.Verifier = capregistry.NewFakeCapabilityProofVerifier(prov)
	}
	return resolver
}

func authorizePersistentBridge(ctx context.Context, resolver capregistry.PersistentCapabilityResolver, channel, accountID, msgType string) error {
	source, action, ok := capregistry.CapabilityBridgeTarget(channel, accountID, msgType)
	if !ok {
		return &capregistry.SubmissionDeniedError{ReasonCode: "unknown_capability"}
	}
	return resolver.Authorize(ctx, source, action, capregistry.ModeBridge, "", "", time.Now().UTC())
}

// authorizeBridgeOutboundSubmit is shared by the public outbox producer and the AI
// webhook sender boundary. Both paths use the same exact-cell policy and diagnostics.
func authorizeBridgeOutboundSubmit(ctx context.Context, channel, accountID, msgType string) error {
	return authorizeBridgeForIngress(ctx, GlobalInboxIngressService(), channel, accountID, msgType)
}

func authorizeBridgeForIngress(ctx context.Context, ingress *InboxIngressService, channel, accountID, msgType string) error {
	check := ingress.bridgeCapabilityAuthorizer()
	var err error
	if check != nil {
		err = check(ctx, channel, accountID, msgType, time.Now().UTC())
	} else {
		err = authorizePersistentBridge(ctx, defaultCapabilityResolver(), channel, accountID, msgType)
	}
	logCapabilityDenial(ctx, "bridge", channel, err)
	return err
}

// 回复持久化继承入站行租户，必须与本次精确授权使用同一租户。
func authorizeBridgeReplySubmit(ctx context.Context, channel WebhookChannel, accountID string, hub *model.MessageHub) error {
	if hub != nil && hub.TenantID != 0 {
		if tenantID, ok := tenant.FromContext(ctx); ok && tenantID != hub.TenantID {
			err := &capregistry.SubmissionDeniedError{ReasonCode: "tenant_binding_mismatch"}
			logCapabilityDenial(ctx, "bridge", string(channel), err)
			return bridgeCapabilityFailure(channel, err)
		}
	}
	if err := authorizeBridgeOutboundSubmit(ctx, string(channel), accountID, "text"); err != nil {
		return bridgeCapabilityFailure(channel, err)
	}
	return nil
}

func authorizeAfterSaleSubmit(ctx context.Context, req *portcontract.AfterSaleRequest) error {
	if req == nil || req.Platform == "" {
		err := &capregistry.SubmissionDeniedError{ReasonCode: "capability_mapping_missing"}
		logCapabilityDenial(ctx, "aftersale", "", err)
		return err
	}
	var action string
	switch req.Type {
	case portcontract.AfterSaleRefund:
		action = "refund.write"
	case "return", "exchange":
		action = "aftersale.write"
	default:
		err := &capregistry.SubmissionDeniedError{ReasonCode: "unknown_capability"}
		logCapabilityDenial(ctx, "aftersale", req.Platform, err)
		return err
	}
	source := capregistry.CapabilitySource{Kind: capregistry.SubmissionSourceAfterSale, Product: req.Platform, ResourceID: req.OrderID}
	err := defaultCapabilityResolver().Authorize(ctx, source, action, capregistry.ModeAPIAdapter, "", "", time.Now().UTC())
	if err == nil {
		// The legacy HTTP client uses a global URL and key, with no connection revision binding.
		err = &capregistry.SubmissionDeniedError{ReasonCode: "execution_target_unbound"}
	}
	logCapabilityDenial(ctx, "aftersale", req.Platform, err)
	return err
}

func logCapabilityDenial(ctx context.Context, source, platform string, err error) {
	if err == nil {
		return
	}
	reason := "capability_not_production_ready"
	if denied, ok := err.(interface{ CapabilityReasonCode() string }); ok && denied.CapabilityReasonCode() != "" {
		reason = denied.CapabilityReasonCode()
	}
	logger.Ctx(ctx).Warn().
		Str("module", "capability_submit").
		Str("source", source).
		Str("platform", platform).
		Str("reason_code", reason).
		Msg("external submit denied before side effect")
}

// Preserve both the machine-readable decision and the channel's non-retryable contract.
type capabilityChannelError struct {
	channelError *ChannelError
}

func (e *capabilityChannelError) Error() string                { return e.channelError.Error() }
func (e *capabilityChannelError) Unwrap() error                { return e.channelError }
func (e *capabilityChannelError) CapabilityReasonCode() string { return e.channelError.Code }

func bridgeCapabilityFailure(channel WebhookChannel, err error) error {
	reason := "capability_not_production_ready"
	if denied, ok := err.(interface{ CapabilityReasonCode() string }); ok && denied.CapabilityReasonCode() != "" {
		reason = denied.CapabilityReasonCode()
	}
	return &capabilityChannelError{channelError: &ChannelError{
		Channel: string(channel), Category: CategoryBadRequest, Retryable: false,
		Code: reason, Raw: "capability_submit_denied: " + reason,
	}}
}
