package model

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GeoAutopilotRun GEO 一键自动驾驶运行记录（单行 = 一次单关键词全链路运行）。
//
// steps 为 JSON 文本列（workflow.go 同款取舍：量级小、结构随版本演进，
// JSONB 化属 doc.go 声明的后续演进项）。status ∈ running|success|partial|failed。
type GeoAutopilotRun struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID           string     `gorm:"type:varchar(36);primaryKey" json:"id"`
	Keyword      string     `gorm:"type:varchar(255);not null" json:"keyword"`
	SiteURL      string     `gorm:"type:varchar(500)" json:"site_url"`
	ArticleCount int        `gorm:"default:3" json:"article_count"`
	Status       string     `gorm:"type:varchar(20);default:'running';index" json:"status"`
	Progress     int        `gorm:"default:0" json:"progress"`
	CurrentStep  string     `gorm:"type:varchar(64)" json:"current_step"`
	Steps        string     `gorm:"type:text" json:"steps"`
	Summary      string     `gorm:"type:text" json:"summary"`
	Error        string     `gorm:"type:text" json:"error"`
	StartedAt    time.Time  `gorm:"autoCreateTime" json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (GeoAutopilotRun) TableName() string { return "geo_autopilot_runs" }

func (m *GeoAutopilotRun) BeforeCreate(_ *gorm.DB) error {
	if m.ID == "" {
		m.ID = uuid.New().String()
	}
	return nil
}

// AutopilotStep 单步执行记录（steps JSON 数组元素）
type AutopilotStep struct {
	Name       string         `json:"name"`
	Label      string         `json:"label"`
	Status     string         `json:"status"` // running|success|skipped|failed
	Detail     string         `json:"detail,omitempty"`
	DurationMs int64          `json:"duration_ms"`
	Result     map[string]any `json:"result,omitempty"`
}

// GetSteps 反序列化 steps JSON（坏数据按空处理，不 panic）
func (m *GeoAutopilotRun) GetSteps() []AutopilotStep {
	out := []AutopilotStep{}
	if strings.TrimSpace(m.Steps) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(m.Steps), &out)
	return out
}

// SetSteps 序列化 steps；失败时存空串（宁可少存不可脏读）
func (m *GeoAutopilotRun) SetSteps(steps []AutopilotStep) {
	b, err := json.Marshal(steps)
	if err != nil {
		m.Steps = ""
		return
	}
	m.Steps = string(b)
}
