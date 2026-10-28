// reach_quota.go 主动触达硬配额的持久预占账（D05/F03）。
//
// reach_quota_counters：每 (租户, scope, 自然日) 一行已用额度，行锁保证并发预占
// 不超上限（两进程并发也成立——真源在 PG，不在进程内存；Redis 只管 QPS 层）。
// reach_quota_reservations：一次动作意图的预占记录，幂等键 intent_id。
//
// 状态机（关键边界，全在仓储层用 CAS 落地）：
//
//	reserved ──commit──▶ committed      （成功回执；重复 commit 幂等）
//	reserved ──release─▶ released       （确定未发送才可释放并返还额度）
//	reserved ──unknown─▶ unknown        （外部结果未知：额度保留待核对，不释放）
//	unknown ──commit──▶ committed      （事后核对确认已发送）
//	committed/unknown ──release──▶ 拒绝 （取消已提交动作不得错误返额；unknown 不释放）
//
// 同一 intent_id + 相同 payload 哈希重入 = 复用同一预占（失败重试不重复占额）；
// 同一 intent_id + 不同 payload = 业务冲突，拒绝。
package model

import (
	"time"
)

// ReachQuotaCounter 每 scope 每自然日的已用额度（行锁真源）
type ReachQuotaCounter struct {
	// 唯一键 (tenant_id, scope, day)。
	TenantID uint `gorm:"not null;default:1;uniqueIndex:uniq_reach_quota_counter,priority:1" json:"tenant_id"`

	ID    uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Scope string `gorm:"type:varchar(128);not null;uniqueIndex:uniq_reach_quota_counter,priority:2" json:"scope"`
	Day   string `gorm:"type:varchar(10);not null;uniqueIndex:uniq_reach_quota_counter,priority:3" json:"day"`
	Used  int    `gorm:"not null;default:0" json:"used"`
}

// TableName 指定表名
func (ReachQuotaCounter) TableName() string { return "reach_quota_counters" }

// 预占状态
const (
	QuotaStateReserved  = "reserved"
	QuotaStateCommitted = "committed"
	QuotaStateReleased  = "released"
	QuotaStateUnknown   = "unknown"
)

// ReachQuotaReservation 动作意图的预占记录
type ReachQuotaReservation struct {
	TenantID uint `gorm:"not null;default:1;uniqueIndex:uniq_reach_quota_intent,priority:1" json:"tenant_id"`

	ID uint `gorm:"primaryKey;autoIncrement" json:"id"`
	// IntentID 动作意图幂等键（调用方生成；重试必须带同一个）
	IntentID    string `gorm:"type:varchar(64);not null;uniqueIndex:uniq_reach_quota_intent,priority:2" json:"intent_id"`
	PayloadHash string `gorm:"type:varchar(64)" json:"payload_hash"`
	Scope       string `gorm:"type:varchar(128);not null;index" json:"scope"`
	Day         string `gorm:"type:varchar(10);not null" json:"day"`
	State       string `gorm:"type:varchar(12);not null;default:'reserved';index" json:"state"`
	// Token 预占令牌：commit/release/unknown 回写必须持有（防串账）
	Token     string    `gorm:"type:varchar(64)" json:"token"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// TableName 指定表名
func (ReachQuotaReservation) TableName() string { return "reach_quota_reservations" }
