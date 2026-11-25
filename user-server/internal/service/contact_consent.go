// contact_consent.go 可信同意判定（D04/F03）。
//
// 判定只认 contact_consent_events 里的真实事实（grant/revoke 追加账）与
// contact_preferences 偏好快照；请求 metadata 里的 consent/opt_in=true 是
// 调用方自述，只能标 legacy_assertion，永不构成可信授权——撤回事实永远压过它。
package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/repository"
)

// 同意判定状态与原因码（对外契约，进 step log 与审计）
const (
	ConsentStatusGranted     = "granted"
	ConsentStatusRevoked     = "revoked"
	ConsentStatusMissing     = "missing"
	ConsentStatusPaused      = "paused"
	ConsentStatusUnavailable = "unavailable"

	ConsentReasonRevoked               = "consent_revoked"
	ConsentReasonMissing               = "consent_missing"
	ConsentReasonPaused                = "consent_paused"
	ConsentReasonLegacyAssertion       = "consent_legacy_assertion"
	ConsentReasonUnavailable           = "consent_unavailable"
	ConsentReasonEventUnavailable      = "consent_event_unavailable"
	ConsentReasonPreferenceUnavailable = "consent_preference_unavailable"
	ConsentReasonIdentityUnavailable   = "consent_identity_unavailable"
)

// ConsentDecision 一次同意判定的结果。
type ConsentDecision struct {
	// Status granted/revoked/missing/paused/unavailable
	Status string
	// Trusted 是否有真实事件凭据支撑（missing/unavailable 时 false）
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
//   - ConsentModeRevocationOnly：撤回、暂停与查询故障拒发，正常无历史凭据仍放行
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
//
// EC-23-A 约束（统一发送闸门，不赋予营销权）：本函数只做目的归一，**不**把
// 未声明目的的请求自动降级为 support/transactional 以绕过营销同意；客服/事务发送
// 必须由调用方显式写入 metadata.purpose=support|transactional。G1 阶段 marketing
// 仍保持关闭，B 启用前任何营销外发都必须经 EC-22 许可与 EC-23-B 出口治理。
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
//
// EC-25：表单/Widget/活动线索接入时，服务咨询与可选营销同意必须分开；
// 未明确勾选营销时不得调用本函数写入 marketing grant，公开采集线索默认无营销许可。
//
// EC-28 交叉引用（M28 旅程/实验编排）：实时撤回事实必须覆盖冻结快照——营销实验/旅程
//
//	分支执行前须重查本同意事实；控制组/holdout 不得因 consent 缺失被静默当作已授权；
//	撤回后未派发任务取消，已派发任务按 EC-31 回执/退订链路处理。
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
	// 写入事实账不接受静默回落：purpose/source 拼错必须当场拒绝，
	// 否则一条「bogus」会被记成 marketing，事后无法分辨真实授权目的。
	purpose, err := ParseConsentPurpose(in.Purpose)
	if err != nil {
		return err
	}
	source, err := ParseConsentSource(in.Source)
	if err != nil {
		return err
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
		Source:           source,
		Actor:            in.Actor,
		EvidenceRef:      in.EvidenceRef,
		Brand:            strings.TrimSpace(in.Brand), LegalBasis: strings.TrimSpace(in.LegalBasis), SourceRef: strings.TrimSpace(in.SourceRef), ExpiresAt: in.ExpiresAt,
		OccurredAt: occurredAt,
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
	Brand            string
	LegalBasis       string
	SourceRef        string
	ExpiresAt        *time.Time
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
	purpose, err := ParseConsentPurpose(purpose)
	if err != nil {
		return err
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
// 查询故障返回独立 unavailable 状态，所有模式拒发，恢复后重新读取事实。
//
// EC-22：发送前实时裁决。通过 LinkedOneIDs 遍历合并身份连通组，任一原身份的
// revoke/pause 都会压过另一身份的 grant；合并只追加身份关联，从不复制/升级同意，
// 因此合并后的身份集合按"最严格"判定，不会扩大许可。purpose/channel 仍按调用方
// 指定的合法必要服务或营销目的独立评估，营销撤回不会删除事务通知事实。
func (s *ContactConsentService) CheckSendConsent(ctx context.Context, oneID, purpose, channel string) ConsentDecision {
	if s == nil || s.repo == nil {
		return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonEventUnavailable}
	}
	if strings.TrimSpace(oneID) == "" {
		return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
	}
	purpose = normalizeConsentPurpose(purpose)
	channel = strings.ToLower(strings.TrimSpace(channel))

	ids, err := s.repo.LinkedOneIDs(ctx, strings.TrimSpace(oneID))
	if err != nil {
		return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonIdentityUnavailable}
	}
	granted, paused := false, false
	var pausedUntil *time.Time
	now := time.Now()
	for _, id := range ids {
		ev, err := s.repo.LatestEvent(ctx, id, purpose, channel)
		if err != nil {
			return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonEventUnavailable}
		}
		if ev != nil {
			if ev.Decision == model.ConsentDecisionRevoke {
				return ConsentDecision{Status: ConsentStatusRevoked, Reason: ConsentReasonRevoked}
			}
			granted = granted || ev.Decision == model.ConsentDecisionGrant
		}
		// 每个原身份内部保持精确渠道优先；跨身份的恢复不覆盖另一身份的暂停。
		pref, err := s.repo.GetPreference(ctx, id, purpose, channel)
		if err != nil {
			return ConsentDecision{Status: ConsentStatusUnavailable, Reason: ConsentReasonPreferenceUnavailable}
		}
		if pref != nil && pref.Paused && (pref.PausedUntil == nil || pref.PausedUntil.After(now)) {
			if !paused || (pausedUntil != nil && (pref.PausedUntil == nil || pref.PausedUntil.After(*pausedUntil))) {
				pausedUntil = pref.PausedUntil
			}
			paused = true
		}
	}
	if paused {
		return ConsentDecision{Status: ConsentStatusPaused, Trusted: granted, PausedUntil: pausedUntil, Reason: ConsentReasonPaused}
	}
	if granted {
		return ConsentDecision{Status: ConsentStatusGranted, Trusted: true}
	}
	return ConsentDecision{Status: ConsentStatusMissing, Reason: ConsentReasonMissing}
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

// ParseConsentPurpose 严格解析写入用的同意目的：空值/未知值一律报错。
// （管线侧的 CheckSendConsent 仍用容错的 normalizeConsentPurpose，
// 因为渠道传来的 purpose 不可控，容错默认 marketing 是既定清洗口径。）
func ParseConsentPurpose(p string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(p))
	switch v {
	case model.ConsentPurposeMarketing, model.ConsentPurposeSupport, model.ConsentPurposeTransactional:
		return v, nil
	default:
		return "", fmt.Errorf("purpose 必须是 marketing / support / transactional 之一（当前: %q）", strings.TrimSpace(p))
	}
}

// ParseConsentSource 严格解析事实来源。空值默认 api；
// legacy_assertion（调用方自述的 metadata 布尔位）明确禁止写入事实账——
// 它能被任意构造，写进来就等于凭空造出一条「可信授权」。
func ParseConsentSource(src string) (string, error) {
	v := strings.ToLower(strings.TrimSpace(src))
	switch v {
	case "":
		return model.ConsentSourceAPI, nil
	case model.ConsentSourceAPI, model.ConsentSourceSelfService, model.ConsentSourceImport:
		return v, nil
	case model.ConsentSourceLegacyAssert:
		return "", fmt.Errorf("source 不接受 %s：历史断言不是可信同意凭据，请改用 api / self_service / import", model.ConsentSourceLegacyAssert)
	default:
		return "", fmt.Errorf("source 必须是 api / self_service / import 之一（当前: %q）", strings.TrimSpace(src))
	}
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
	_db.RegisterExtraModels(&model.ContactConsentEvent{}, &model.ContactPreference{}, &model.ContactConsentIdentityLink{})
}
