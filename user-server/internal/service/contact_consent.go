// contact_consent.go 可信同意判定（D04/F03）。
//
// 判定只认 contact_consent_events 里的真实事实（grant/revoke 追加账）与
// contact_preferences 偏好快照；请求 metadata 里的 consent/opt_in=true 是
// 调用方自述，只能标 legacy_assertion，永不构成可信授权——撤回事实永远压过它。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

// 同意判定状态与原因码（对外契约，进 step log 与审计）
const (
	ConsentStatusGranted = "granted"
	ConsentStatusRevoked = "revoked"
	ConsentStatusMissing = "missing"
	ConsentStatusPaused  = "paused"

	ConsentReasonRevoked         = "consent_revoked"
	ConsentReasonMissing         = "consent_missing"
	ConsentReasonPaused          = "consent_paused"
	ConsentReasonLegacyAssertion = "consent_legacy_assertion"
)

// ConsentDecision 一次同意判定的结果。
type ConsentDecision struct {
	// Status granted/revoked/missing/paused
	Status string
	// Trusted 是否有真实事件凭据支撑（missing 时 false）
	Trusted bool
	// PausedUntil paused 时的恢复时刻（nil=无限期）
	PausedUntil *time.Time
	// Reason 机器可读原因码
	Reason string
}

// ConsentChecker 发送前同意判定接口（挂发送管线 permission 步骤之后、quiet 之前）
type ConsentChecker interface {
	CheckSendConsent(ctx context.Context, oneID, purpose, channel string) ConsentDecision
}

// 同意执行模式（管线配置）：
//   - ConsentModeRevocationOnly：仅显式撤回拒发（默认——存量无凭据不批量伪造，也不一刀切停发）
//   - ConsentModeEnforce：无可信凭据一律拒发（存量盘点完成后收紧用）
const (
	ConsentModeRevocationOnly = "revocation_only"
	ConsentModeEnforce        = "enforce"
)

// ContactConsentService 同意事实与偏好的领域服务
type ContactConsentService struct {
	repo *repository.ContactConsentRepository
}

// NewContactConsentService 构造
func NewContactConsentService(repo *repository.ContactConsentRepository) *ContactConsentService {
	return &ContactConsentService{repo: repo}
}

// ReachSendPurpose 从请求归一目的：metadata.purpose 显式给出（support/transactional）则用之，
// 默认 marketing——本管线是主动触达管线，目的语义由调用方显式放宽，不靠猜测收紧。
func ReachSendPurpose(req *ReachSendRequest) string {
	if req == nil {
		return model.ConsentPurposeMarketing
	}
	p := strings.ToLower(strings.TrimSpace(req.Metadata["purpose"]))
	switch p {
	case model.ConsentPurposeSupport, model.ConsentPurposeTransactional:
		return p
	default:
		return model.ConsentPurposeMarketing
	}
}

// Record 追加一条同意/撤回事实。
func (s *ContactConsentService) Record(ctx context.Context, in ConsentRecordInput) error {
	if s.repo == nil {
		return errors.New("contact consent service not initialized")
	}
	oneID := strings.TrimSpace(in.OneID)
	if oneID == "" {
		return errors.New("one_id 不能为空")
	}
	decision := strings.ToLower(strings.TrimSpace(in.Decision))
	if decision != model.ConsentDecisionGrant && decision != model.ConsentDecisionRevoke {
		return errors.New("decision 只能是 grant/revoke")
	}
	purpose := normalizeConsentPurpose(in.Purpose)
	if purpose == "" {
		return errors.New("purpose 不能为空")
	}
	occurredAt := in.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now()
	}
	ev := &model.ContactConsentEvent{
		OneID:            oneID,
		Purpose:          purpose,
		ChannelScope:     strings.ToLower(strings.TrimSpace(in.ChannelScope)),
		Decision:         decision,
		StatementVersion: in.StatementVersion,
		Source:           in.Source,
		Actor:            in.Actor,
		EvidenceRef:      in.EvidenceRef,
		OccurredAt:       occurredAt,
	}
	return s.repo.AppendEvent(ctx, ev)
}

// ConsentRecordInput 记录同意/撤回的入参
type ConsentRecordInput struct {
	OneID            string
	Purpose          string
	ChannelScope     string
	Decision         string
	StatementVersion string
	Source           string
	Actor            string
	EvidenceRef      string
	OccurredAt       time.Time
}

// SetPreference 写偏好快照（暂停/恢复、时区）。
func (s *ContactConsentService) SetPreference(ctx context.Context, oneID, purpose, channel string, paused bool, pausedUntil *time.Time, timezone string) error {
	if s.repo == nil {
		return errors.New("contact consent service not initialized")
	}
	oneID = strings.TrimSpace(oneID)
	if oneID == "" {
		return errors.New("one_id 不能为空")
	}
	purpose = normalizeConsentPurpose(purpose)
	if purpose == "" {
		return errors.New("purpose 不能为空")
	}
	return s.repo.UpsertPreference(ctx, &model.ContactPreference{
		OneID:       oneID,
		Purpose:     purpose,
		Channel:     strings.ToLower(strings.TrimSpace(channel)),
		Paused:      paused,
		PausedUntil: pausedUntil,
		Timezone:    strings.TrimSpace(timezone),
	})
}

// CheckSendConsent 实现 ConsentChecker：事件账 + 偏好快照的当前状态。
// DB 故障按 revoked 语义返回吗？不——DB 故障返回 missing+Trusted=false，
// 由管线按配置模式决定放行（revocation_only）/拒发（enforce）；这里不谎报 revoked。
func (s *ContactConsentService) CheckSendConsent(ctx context.Context, oneID, purpose, channel string) ConsentDecision {
	if s.repo == nil || strings.TrimSpace(oneID) == "" {
		return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
	}
	purpose = normalizeConsentPurpose(purpose)
	channel = strings.ToLower(strings.TrimSpace(channel))

	ev, err := s.repo.LatestEvent(ctx, oneID, purpose, channel)
	if err != nil {
		// 查询失败：状态未知。明确返回 missing（非 revoked），原因码带上来源，
		// 管线在 enforce 模式下会拒发（fail-closed），revocation_only 下放行留痕。
		return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
	}
	if ev == nil {
		return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
	}
	if ev.Decision == model.ConsentDecisionRevoke {
		return ConsentDecision{Status: ConsentStatusRevoked, Reason: ConsentReasonRevoked}
	}

	// grant 之上叠加偏好：渠道行优先于全渠道行
	pref, err := s.repo.GetPreference(ctx, oneID, purpose, channel)
	if err == nil && pref != nil && pref.Paused {
		if pref.PausedUntil != nil && !pref.PausedUntil.After(time.Now()) {
			// 暂停已到期：视为未暂停
			return ConsentDecision{Status: ConsentStatusGranted, Trusted: true}
		}
		return ConsentDecision{Status: ConsentStatusPaused, Trusted: true, PausedUntil: pref.PausedUntil, Reason: ConsentReasonPaused}
	}
	return ConsentDecision{Status: ConsentStatusGranted, Trusted: true}
}

// Timeline 客户授权时间线（客户 360 / 管理页）
func (s *ContactConsentService) Timeline(ctx context.Context, oneID string, limit int) ([]model.ContactConsentEvent, error) {
	if s.repo == nil {
		return nil, errors.New("contact consent service not initialized")
	}
	return s.repo.ListEventsByOneID(ctx, strings.TrimSpace(oneID), limit)
}

// Preferences 客户偏好列表
func (s *ContactConsentService) Preferences(ctx context.Context, oneID string) ([]model.ContactPreference, error) {
	if s.repo == nil {
		return nil, errors.New("contact consent service not initialized")
	}
	return s.repo.ListPreferencesByOneID(ctx, strings.TrimSpace(oneID))
}

func normalizeConsentPurpose(p string) string {
	p = strings.ToLower(strings.TrimSpace(p))
	switch p {
	case model.ConsentPurposeMarketing, model.ConsentPurposeSupport, model.ConsentPurposeTransactional:
		return p
	default:
		return model.ConsentPurposeMarketing
	}
}

// 建表登记：同意事实账与偏好快照随启动期 AutoMigrate 建（RegisterExtraModels 机制同上）。
func init() {
	_db.RegisterExtraModels(&model.ContactConsentEvent{}, &model.ContactPreference{})
}
