package model

import (
	"time"

	"gorm.io/datatypes"
)

// BrowserOutreachReceipt 触达回执（Chunk5：验收交付物 = 截图 + 帖子链接 + 文案快照）。
//
// 与 browser_outreach_dedupe 分表而不是合表，是三处形状差异逼出来的：
//  1. 去重表的唯一键是四元组（platform/target/action/copy_hash），RecordOutreachSend 用
//     OnConflict{DoNothing} 幂等吞重复插入——回执表用它会让「内容已发出但列表里没有回执」
//     这两件事无法区分，而回执的用途正是回答「到底发出去了没有」。
//  2. 去重行是闸门输入（能不能发），回执行是闸门输出（发了什么）。一行一旦被当闸门读，
//     「没有行」就不能等同于「没发过」。
//  3. 回执要按帖子链接/文案被人检索与统计，extracted_data 的 JSONB 追加数组做不到。
//
// copy_hash 与去重行同键（writeStepKey），保证回执↔去重行可 join：
// 「有回执无去重行」= 落库失败，「有去重行无回执」= 收口截图或回执写入失败，两者都可见。
type BrowserOutreachReceipt struct {
	ID           uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID       uint   `gorm:"column:task_id;not null;index" json:"task_id"`
	SessionID    uint   `gorm:"column:session_id;not null;index" json:"session_id"`
	Platform     string `gorm:"column:platform;type:varchar(64);not null" json:"platform"`
	Action       string `gorm:"column:action;type:varchar(64);not null" json:"action"`
	TargetURL    string `gorm:"column:target_url;type:text;not null" json:"target_url"`
	CopyHash     string `gorm:"column:copy_hash;type:varchar(64);not null;index" json:"copy_hash"`
	CopySnapshot string `gorm:"column:copy_snapshot;type:text" json:"copy_snapshot"`
	// ScreenshotURL 存 LocalDriver 的公开 URL，不落 base64（与 browser_sessions.final_screenshot_url 同纪律）
	ScreenshotURL string         `gorm:"column:screenshot_url;type:varchar(1024)" json:"screenshot_url"`
	Evidence      datatypes.JSON `gorm:"column:evidence;type:jsonb" json:"evidence"`
	// Verified 回查是否见到自己的文字。false 不等于没发出去，只等于「平台侧归因不到」——
	// 这条区分正是双发闸把人叫来裁决的依据，所以回执行也要原样带出去。
	Verified  bool      `gorm:"column:verified;default:false" json:"verified"`
	CreatedAt time.Time `gorm:"column:created_at;autoCreateTime" json:"created_at"`
}

// TableName 表名。
func (BrowserOutreachReceipt) TableName() string { return "browser_outreach_receipts" }
