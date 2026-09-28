package model

import "time"

// BrowserWriteClaim 不可逆写的**存储层独占声明**（/ §8.3-20 A12）。
//
// 一行 = 「(task_id, text_hash) 这把坑被 step_row_id 这一步占着，它正要去跨提交点」。
// 它的存在只为一件事：把「这次提交是否已被记过」的裁决权从应用层的
// 「先 SELECT 再判断再 INSERT」（两条并发腿会读到同一条「没做过」）下推到
// 数据库的唯一约束——占坑即 INSERT，撞约束即拒发。
//
// 与 browser_steps.submit_state 的分工（不是替代）：
//   - 台账三态（sent/unattributed/verified）回答「历史上是否真的发生过一次尝试」，
//     是跨会话、跨重启的事实，也是双发闸的读侧；
//   - 本表回答「此刻谁有权去跨那个提交点」，生命周期只有几步之间：
//     过闸前占坑 → 跨越则一直保留（与台账同生死）→ 证明「从未发生」则释放（腾出键位，
//     否则一次浮层遮挡就把唯一正确的处置——等页面停下再跑一次——永久拦死）。
//
// 刻意**不带** gorm.DeletedAt：软删等于把键位腾出来，而这张表存在的全部理由就是键位不腾。
// 同理它不参与任何裁剪（一行约 40 字节，与 command_log 不是一个量级，留多久都不心疼）。
type BrowserWriteClaim struct {
	ID        uint   `gorm:"primaryKey" json:"id"`
	TaskID    uint   `gorm:"column:task_id;not null;uniqueIndex:uk_browser_write_claims_task_text,priority:1" json:"task_id"`
	TextHash  string `gorm:"column:text_hash;size:16;not null;uniqueIndex:uk_browser_write_claims_task_text,priority:2" json:"text_hash"`
	StepRowID uint   `gorm:"column:step_row_id;not null;index" json:"step_row_id"`
	SessionID uint   `gorm:"column:session_id;not null;index" json:"session_id"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (BrowserWriteClaim) TableName() string { return "browser_write_claims" }
