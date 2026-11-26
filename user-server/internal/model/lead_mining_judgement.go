package model

import "time"

// LeadMiningJudgement 线索发掘判定记录（LM-PR1）。每次判定可审计、可回放。
type LeadMiningJudgement struct {
	TenantID       uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID             int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	JudgementID    string     `gorm:"type:varchar(32);not null;index" json:"judgement_id"`
	MessageID      string     `gorm:"type:varchar(128);not null;default:'';index" json:"message_id"`
	ConversationID string     `gorm:"type:varchar(128);not null;default:'';index" json:"conversation_id"`
	Platform       string     `gorm:"type:varchar(32);not null;default:''" json:"platform"`
	AccountID      string     `gorm:"type:varchar(128);not null;default:''" json:"account_id"`
	IsGroup        bool       `gorm:"not null;default:false" json:"is_group"`
	GroupID        string     `gorm:"type:varchar(128);not null;default:''" json:"group_id"`
	SenderID       string     `gorm:"type:varchar(128);not null;default:''" json:"sender_id"`
	ContentHash    string     `gorm:"type:varchar(64);not null;default:''" json:"content_hash"`
	ContextJSON    string     `gorm:"type:jsonb;not null;default:'{}'" json:"context_json"`
	ResultJSON     string     `gorm:"type:jsonb;not null;default:'{}'" json:"result_json"`
	Matched        bool       `gorm:"not null;default:false" json:"matched"`
	LeadRole       string     `gorm:"type:varchar(16);not null;default:'unknown'" json:"lead_role"`
	IntentScore    int        `gorm:"not null;default:0" json:"intent_score"`
	Reason         string     `gorm:"type:text;not null;default:''" json:"reason"`
	FeedbackStatus string     `gorm:"type:varchar(16);not null;default:'none';index" json:"feedback_status"` // none/false_positive/valid
	FeedbackReason string     `gorm:"type:text;not null;default:''" json:"feedback_reason"`
	FeedbackBy     string     `gorm:"type:varchar(64);not null;default:''" json:"feedback_by"`
	FeedbackAt     *time.Time `json:"feedback_at,omitempty"`
	CreatedAt      time.Time  `gorm:"autoCreateTime" json:"created_at"`

	// US2 T039 二次 LLM 汇总判断扩展。Matched 保留既有语义（是否命中线索），
	// IsClue 是同一结论的显式别名，供归档/通知链路判读；两者由 Service 同步写入。
	IsClue            bool        `gorm:"column:is_clue;not null;default:false" json:"is_clue"`
	ClueType          string      `gorm:"column:clue_type;type:varchar(32);not null;default:''" json:"clue_type"`
	Summary           string      `gorm:"column:summary;type:text;not null;default:''" json:"summary"`
	ContextMessageIDs JSONStrings `gorm:"column:context_message_ids;type:jsonb;default:'[]'" json:"context_message_ids"`
	SourceHitIDs      JSONStrings `gorm:"column:source_hit_ids;type:jsonb;default:'[]'" json:"source_hit_ids"`
	EvidenceComplete  bool        `gorm:"column:evidence_complete;not null;default:true" json:"evidence_complete"`
	Degraded          bool        `gorm:"column:degraded;not null;default:false" json:"degraded"`
	ChannelAccountID  uint        `gorm:"column:channel_account_id;type:bigint;not null;default:0;index" json:"channel_account_id"`
}

func (LeadMiningJudgement) TableName() string { return "lead_mining_judgements" }

// LeadMiningNotifyRoute 线索发掘通知路由（LM-PR4）。
type LeadMiningNotifyRoute struct {
	TenantID      uint       `gorm:"not null;default:1;index" json:"tenant_id"`
	ID            int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	RouteID       string     `gorm:"type:varchar(32);not null;index" json:"route_id"`
	Channel       string     `gorm:"type:varchar(32);not null;default:'';index" json:"channel"`
	AccountID     string     `gorm:"type:varchar(128);not null;default:'';index" json:"account_id"`
	NotifyChannel string     `gorm:"type:varchar(16);not null;default:'in_app'" json:"notify_channel"` // in_app/telegram/feishu
	Target        string     `gorm:"type:varchar(255);not null;default:''" json:"target"`
	Enabled       bool       `gorm:"not null;default:true" json:"enabled"`
	CreatedBy     *uint      `json:"created_by,omitempty"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeletedAt     *time.Time `gorm:"index" json:"deleted_at,omitempty"`

	// T042 通知路由扩展：账号维护人 → 渠道维护人 → 租户默认的三级回退。
	// Scope: account/channel/tenant；ChannelAccountID/Channel 只是路由作用域键
	// （渠道账号来自 channel_accounts 下拉源），不是智能体绑定。
	// T05 语义契约：NotifyUserIDs 是本系统员工（system_users.id 的字符串形式），
	// 是唯一收件人来源；NotifyTypes 为站内/TG/飞书投递通道；Target 是 TG/飞书
	// 机器人或群聊投递目标（accountID:chatID），也不是智能体。
	// 本表与客服智能体（ChannelAccount.AutoReplyAgentID）/线索智能体
	// （ChannelAccount.LeadMiningAgentID、lead_mining_agents 注册表）完全无关，
	// 禁止把线索通知配置改成智能体绑定或混用两类语义。
	// DeliveryRecords 是每次送达的业务码结果数组（宪法 IV：判业务码不判 HTTP 码）。
	ChannelAccountID uint        `gorm:"column:channel_account_id;type:bigint;not null;default:0;index" json:"channel_account_id"`
	Scope            string      `gorm:"column:scope;type:varchar(16);not null;default:'tenant'" json:"scope"`
	Priority         int         `gorm:"column:priority;not null;default:0" json:"priority"`
	NotifyUserIDs    JSONStrings `gorm:"column:notify_user_ids;type:jsonb;default:'[]'" json:"notify_user_ids"`
	NotifyTypes      JSONStrings `gorm:"column:notify_types;type:jsonb;default:'[]'" json:"notify_types"`
	DeliveryRecords  string      `gorm:"column:delivery_records;type:jsonb;default:'[]'" json:"delivery_records"`
}

func (LeadMiningNotifyRoute) TableName() string { return "lead_mining_notify_routes" }
