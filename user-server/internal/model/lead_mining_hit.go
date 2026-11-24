package model

import (
	"database/sql/driver"
	"encoding/json"
	"time"
)

// JSONPayload 通用 jsonb 对象载荷（证据 payload、通知送达记录等）。
// 与 JSONStrings 同族，放在 Model 层只做序列化，不含业务规则。
type JSONPayload map[string]any

func (j JSONPayload) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

func (j *JSONPayload) Scan(v any) error {
	if v == nil {
		*j = JSONPayload{}
		return nil
	}
	var data []byte
	switch t := v.(type) {
	case string:
		data = []byte(t)
	case []byte:
		data = t
	default:
		*j = JSONPayload{}
		return nil
	}
	if len(data) == 0 {
		*j = JSONPayload{}
		return nil
	}
	return json.Unmarshal(data, j)
}

var _ driver.Valuer = (JSONPayload)(nil)

// LeadMiningHit 三路并行检测的独立证据（keyword / semantic / llm）。
//
// 每一路检测结果单独落一行，不合并覆盖：二次 LLM 汇总判断（T039）与判定详情
// 回放都依赖这些原始证据。Complete=false + Degraded=true 表示 LLM 超时/未初始化
// 的降级命中，证据不完整，调用方必须 fail-visible，不得静默当作正常结论。
type LeadMiningHit struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID                 int64       `gorm:"primaryKey;autoIncrement" json:"id"`
	JudgementID        string      `gorm:"column:judgement_id;type:varchar(32);not null;index" json:"judgement_id"`
	Type               string      `gorm:"column:type;type:varchar(16);not null;index" json:"type"`
	Payload            JSONPayload `gorm:"column:payload;type:jsonb;default:'{}'" json:"payload"`
	EvidenceMessageIDs JSONStrings `gorm:"column:evidence_message_ids;type:jsonb;default:'[]'" json:"evidence_message_ids"`
	Complete           bool        `gorm:"column:complete;not null;default:true" json:"complete"`
	Degraded           bool        `gorm:"column:degraded;not null;default:false" json:"degraded"`
	Reason             string      `gorm:"column:reason;type:text;default:''" json:"reason"`
	CreatedAt          time.Time   `gorm:"autoCreateTime" json:"created_at"`
}

// TableName 三路检测证据表。
func (LeadMiningHit) TableName() string { return "lead_mining_hits" }
