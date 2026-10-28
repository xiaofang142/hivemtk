// contact_consent.go 客户同意与偏好（D04/F03）。
//
// contact_consent_events 是**追加式**授权/撤回事实账：grant/revoke 一律新行，
// 永不 UPDATE 历史事实；最新一条（occurred_at, id 最大）即当前有效状态。
// contact_preferences 是当前偏好快照（渠道/目的维度暂停等），可改写。
//
// 为什么不用请求 metadata 当同意凭据：metadata 是调用方可任意构造的布尔位，
// 只能记 legacy_assertion（历史断言），不能作为可信授权；撤回事实永远压过它。
package model

import (
	"time"
)

// 同意目的与决定（对外契约：进事件行 reason/审计，改字等于改契约）
const (
	ConsentPurposeMarketing     = "marketing"     // 主动营销（proactive reach）
	ConsentPurposeSupport       = "support"       // 被动客服
	ConsentPurposeTransactional = "transactional" // 事务通知（订单/验证码等）

	ConsentDecisionGrant  = "grant"
	ConsentDecisionRevoke = "revoke"

	ConsentSourceAPI          = "api"
	ConsentSourceSelfService  = "self_service"
	ConsentSourceImport       = "import"
	ConsentSourceLegacyAssert = "legacy_assertion"
)

// ContactConsentEvent 客户同意/撤回事件（追加式）
type ContactConsentEvent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// OneID 客户统一身份（customer.unified_id）
	OneID string `gorm:"type:varchar(64);index;not null" json:"one_id"`
	// Purpose 同意目的（marketing/support/transactional）
	Purpose string `gorm:"type:varchar(20);index;not null" json:"purpose"`
	// ChannelScope 渠道范围：空串=全渠道，否则为具体渠道（小写）
	ChannelScope string `gorm:"type:varchar(30)" json:"channel_scope"`
	// Decision grant / revoke
	Decision string `gorm:"type:varchar(10);not null" json:"decision"`
	// StatementVersion 同意声明文本版本（撤回判定与版本化口径）
	StatementVersion string `gorm:"type:varchar(32)" json:"statement_version"`
	// Source 来源（api/self_service/import/legacy_assertion）
	Source string `gorm:"type:varchar(30)" json:"source"`
	// Actor 操作者（登录上下文的 user_id；自助为 customer self）
	Actor string `gorm:"type:varchar(64)" json:"actor"`
	// EvidenceRef 证据引用（工单号/自助 token 流水号等）
	EvidenceRef string `gorm:"type:varchar(128)" json:"evidence_ref"`
	// OccurredAt 事实发生时刻（可回填历史；查询按它定最新）
	OccurredAt time.Time `gorm:"index;not null" json:"occurred_at"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 指定表名
func (ContactConsentEvent) TableName() string { return "contact_consent_events" }

// ContactPreference 客户偏好快照（渠道/目的维度）
type ContactPreference struct {
	// 唯一键 (tenant_id, one_id, purpose, channel)：Upsert 的 ON CONFLICT 依赖
	// 这个精确列集（42P10 教训见 delayed_outbound D-2 注释）。
	TenantID uint `gorm:"not null;default:1;index;uniqueIndex:uniq_contact_pref,priority:1" json:"tenant_id"`

	ID      uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	OneID   string `gorm:"type:varchar(64);not null;uniqueIndex:uniq_contact_pref,priority:2" json:"one_id"`
	Purpose string `gorm:"type:varchar(20);not null;uniqueIndex:uniq_contact_pref,priority:3" json:"purpose"`
	// Channel 空串=全渠道默认偏好；具体渠道行优先于全渠道行
	Channel string `gorm:"type:varchar(30);not null;default:'';uniqueIndex:uniq_contact_pref,priority:4" json:"channel"`
	// Paused 暂停该目的/渠道的主动触达
	Paused bool `gorm:"not null;default:false" json:"paused"`
	// PausedUntil 暂停截止时刻（nil= indefinite 直到手动恢复）
	PausedUntil *time.Time `json:"paused_until"`
	// Timezone 客户时区（IANA；静默时段/日历按它算，空=租户默认）
	Timezone  string    `gorm:"type:varchar(64)" json:"timezone"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ContactPreference) TableName() string { return "contact_preferences" }
