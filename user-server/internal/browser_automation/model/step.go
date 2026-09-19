package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BrowserStep 任务内的执行步骤（每次执行产生一条 step 记录，用于回放 + 调试）
type BrowserStep struct {
	ID         uint           `gorm:"primaryKey" json:"id"`
	SessionID  uint           `gorm:"column:session_id;index;not null" json:"session_id"`
	TaskID     uint           `gorm:"column:task_id;index;not null" json:"task_id"`
	StepIndex  int            `gorm:"column:step_index;not null" json:"step_index"`
	Action     string         `gorm:"column:action;size:32;not null;index" json:"action"`                 // open_tab / click / type / snapshot / markdown / screenshot / wait / wait_for_selector / scroll / extract / close_tab
	Target     string         `gorm:"column:target;size:1024" json:"target"`                              // selector 或 @e3 refs
	Value      string         `gorm:"column:value;type:text" json:"value"`                                // type 动作的输入值
	Params     datatypes.JSON `gorm:"column:params;type:jsonb" json:"params"`                             // wait/scroll/extract/screenshot 等扩展参数
	Status     string         `gorm:"column:status;size:32;not null;default:pending;index" json:"status"` // pending / running / success / failed / skipped
	Result     datatypes.JSON `gorm:"column:result;type:jsonb" json:"result,omitempty"`                   // action 返回值（snapshot/extract 结果）
	DurationMs int64          `gorm:"column:duration_ms" json:"duration_ms"`
	ErrorMsg   string         `gorm:"column:error_msg;type:text" json:"error_msg,omitempty"`
	// SubmitState/TextHash 批6（F11b）不可逆写台账：status 记「这一步跑成什么样」，
	// submit_state 记「这条内容的提交是否可能发生」——两者在「send 到达但 verify 未见」时必然分叉
	// （步判 failed，提交却可能已生效），混成一列就会把结果未知态误当可重发。
	// 跨 session 自然键 = task_id + text_hash（批7 F-N4：原先还带 step_index，
	// Brain 模式的 stepIdx 每轮递增，同一条评论换轮重放会落在不同下标上而绕过闸门）。
	// 可空列由 gorm AutoMigrate 直加，零迁移文件。
	SubmitState string `gorm:"column:submit_state;size:16;index" json:"submit_state,omitempty"` // prepared / sent / verified / unattributed
	TextHash    string `gorm:"column:text_hash;size:16;index" json:"text_hash,omitempty"`       // fnv32a(去空白正文) hex

	// IsWrite 批7：服务端判定的「本步是不可逆写」落库留痕（步骤声明 ∪ 原语推导）。
	// 必须落列而不是只在内存里判：重试豁免、审计面板、以及「为什么这一步没重试」的归因都要读它。
	IsWrite bool `gorm:"column:is_write;not null;default:false" json:"is_write,omitempty"`

	CreatedAt time.Time      `gorm:"autoCreateTime" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (BrowserStep) TableName() string { return "browser_steps" }

// 写台账状态（列注释见 SubmitState 字段）。定义放 model：service 写、repository 查，
// 两处各写一遍字面量迟早漂移成「查询漏一个态」级别的漏双发。
const (
	StepSubmitPrepared     = "prepared"     // 文本进输入框，点击从未发生 → 可安全重下发
	StepSubmitSent         = "sent"         // 不可逆提交点已跨越，结局未知
	StepSubmitVerified     = "verified"     // 收紧回查确认命中（唯一可宣称「已发布」的态）
	StepSubmitUnattributed = "unattributed" // 尝试过但归因不到 → 仍拦重发
)

// StepSubmitAttemptedStates 「提交可能发生」的态集合（prepared 不在内）。
// 函数而非包级 var：可被调用方原地改写的共享切片是纯风险无收益。
func StepSubmitAttemptedStates() []string {
	return []string{StepSubmitSent, StepSubmitVerified, StepSubmitUnattributed}
}
