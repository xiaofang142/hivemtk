package model

import (
	"time"

	"gorm.io/datatypes"
)

// ego-lite 腿四张表（设计文档 §一 表模型定案：物理新表 + 列形状对齐 + 语义复用）。
// 禁止跨域外键直连 browser_automation 表；seq 独立生成；审计纪律与现链同源。

// EgoliteExecution ego-lite 执行（对标 browser_sessions 的状态机骨架，列形状独立）
// 状态机：created → dispatching → running ⇄ waiting_confirm → completed/failed/stopped/timeout
type EgoliteExecution struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID         uint   `gorm:"primaryKey" json:"id"`
	UserID     uint   `gorm:"index;not null" json:"user_id"`
	FlowID     uint   `gorm:"index" json:"flow_id"`               // 用户流程（私享草稿执行时指向 egolite_flows）
	AssetID    string `gorm:"size:128;index" json:"asset_id"`     // 市场资产（正式执行时）
	AssetVer   string `gorm:"size:32" json:"asset_version"`
	Origin     string `gorm:"size:16;not null;default:draft" json:"origin"` // draft / market
	Entry      string `gorm:"size:256" json:"entry"`
	ParamsJSON datatypes.JSON `gorm:"column:params;type:jsonb" json:"params"`

	SpaceName     string `gorm:"size:128" json:"space_name"`
	SpaceID       string `gorm:"size:128" json:"space_id"` // resume 用（DF-5：同 space 续跑）
	Round         int    `gorm:"not null;default:1" json:"round"`
	ConfirmPolicy string `gorm:"size:16;not null;default:none" json:"confirm_policy"` // none / require
	ConfirmTTLSec int    `gorm:"not null;default:300" json:"confirm_ttl_sec"`
	TimeoutSec    int    `gorm:"not null;default:1800" json:"timeout_sec"`

	State        string     `gorm:"size:16;not null;default:created;index" json:"state"`
	DeviceID     string     `gorm:"size:64" json:"device_id"` // dispatch 时 runner 指纹
	DispatchedAt *time.Time `json:"dispatched_at"`
	StartedAt    *time.Time `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	FailReason   string     `gorm:"size:512" json:"fail_reason,omitempty"`
	ArtifactsJSON datatypes.JSON `gorm:"column:artifacts;type:jsonb" json:"artifacts"` // 产物账（截图URL/extract 概要）

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdatedAt" json:"updated_at"`
}

// 执行状态常量（与调研 DF-4/DF-5 对齐）
const (
	EgoExecCreated        = "created"
	EgoExecDispatching    = "dispatching"
	EgoExecRunning        = "running"
	EgoExecWaitingConfirm = "waiting_confirm"
	EgoExecCompleted      = "completed"
	EgoExecFailed         = "failed"
	EgoExecStopped        = "stopped"
	EgoExecTimeout        = "timeout"
)

// EgoExecTerminalStates 终态集合（看门狗/状态机合法性判定用）
var EgoExecTerminalStates = map[string]bool{
	EgoExecCompleted: true, EgoExecFailed: true, EgoExecStopped: true, EgoExecTimeout: true,
}

func (EgoliteExecution) TableName() string { return "egolite_executions" }

// EgoEventLog 事件日志（列形状对标 browser_command_log；append-only 无 Update/Delete）
// Ok 三态 *bool 无默认值——"不把试过写成成了"（v3.44 裁定）指针纪律原样继承。
type EgoEventLog struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID          uint           `gorm:"primaryKey" json:"id"`
	ExecutionID uint           `gorm:"index;not null" json:"execution_id"`
	Seq         int            `gorm:"not null" json:"seq"` // execution 内单调（runner 透传）
	Type        string         `gorm:"size:24;not null" json:"type"` // action/snapshot/artifact/handoff/extract/confirm/done/error/runner
	Action      string         `gorm:"size:64" json:"action"`
	Ok          *bool          `gorm:"column:ok" json:"ok"` // 三态：nil=事件行未携带结论
	Payload     datatypes.JSON `gorm:"column:payload;type:jsonb" json:"payload"`
	DurationMs  int64          `gorm:"column:duration_ms" json:"duration_ms"`
	CreatedAt   time.Time      `gorm:"autoCreateTime;index" json:"created_at"`
}

func (EgoEventLog) TableName() string { return "ego_event_log" }

// EgoliteFlow 用户流程（需求②：用户自建/agent 沉淀的草稿→发布→版本）
type EgoliteFlow struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID         uint   `gorm:"primaryKey" json:"id"`
	UserID     uint   `gorm:"index;not null" json:"user_id"`
	Name       string `gorm:"size:128;not null" json:"name"`
	DomainsJSON datatypes.JSON `gorm:"column:domains;type:jsonb" json:"domains"`
	WritesJSON  datatypes.JSON `gorm:"column:writes;type:jsonb" json:"writes"`
	ParamsSchema datatypes.JSON `gorm:"column:params_schema;type:jsonb" json:"params_schema"`
	Script     string `gorm:"type:text" json:"script"` // flow JS 全文（draft 唯一真相源）
	Status     string `gorm:"size:16;not null;default:draft;index" json:"status"` // draft / published / submitted
	Version    int    `gorm:"not null;default:1" json:"version"`
	ScanPassed bool   `gorm:"not null;default:false" json:"scan_passed"` // 最近一次扫描门结论
	ScanReport datatypes.JSON `gorm:"column:scan_report;type:jsonb" json:"scan_report"`
	CreatedAt  time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt  time.Time `gorm:"autoUpdatedAt" json:"updated_at"`
}

// 流程状态常量
const (
	EgoFlowDraft     = "draft"
	EgoFlowPublished = "published"
	EgoFlowSubmitted = "submitted" // 已提交平台审核（SubmitToPlatform 先例）
)

func (EgoliteFlow) TableName() string { return "egolite_flows" }

// EgoliteWriteClaim 写台账（语义照搬 browser_steps.submit_state / write_claim 分工，物理新表）
// 三态回答「是否真的发生过一次尝试」；ego 腿的 confirm 挂起是四态外的执行级状态（在 execution 上）。
// 坑键 uniqueIndex(tenant_id, execution_id, action)：tenancyidx.Normalize 会把标签里的
// 单列形状复合化；ON CONFLICT 的三列形状必须与它一致（repository.Claim 同句）。
type EgoliteWriteClaim struct {
	TenantID uint `gorm:"not null;default:1;uniqueIndex:uq_ego_wclaim,priority:1" json:"tenant_id"`

	ID          uint      `gorm:"primaryKey" json:"id"`
	ExecutionID uint      `gorm:"not null;uniqueIndex:uq_ego_wclaim,priority:2" json:"execution_id"`
	Action      string    `gorm:"size:64;not null;uniqueIndex:uq_ego_wclaim,priority:3" json:"action"` // writes[] 声明的动作名
	UserID      uint      `gorm:"index;not null" json:"user_id"`
	AssetID     string    `gorm:"size:128" json:"asset_id"`
	ClaimState  string    `gorm:"size:16;not null" json:"claim_state"` //见下方常量
	ClaimedAt   time.Time `gorm:"autoCreateTime" json:"claimed_at"`
	VerifiedAt  *time.Time `json:"verified_at"`
}

// 台账状态（step.go:49-52 语义原样：防止双发的最小事实集）
const (
	EgoWriteClaimed      = "claimed"       // 已声明要写（占位，唯一索引粒度：execution+action 首次）
	EgoWriteSent         = "sent"          // 写已发出但未验证
	EgoWriteVerified     = "verified"      // 写后验证通过
	EgoWriteUnattributed = "unattributed"  // 尝试过但归因不到——拦重发
)

func (EgoliteWriteClaim) TableName() string { return "egolite_write_claims" }
