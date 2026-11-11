// bridge_follow_event.go —— 新用户（非关注好友）事件与自动关注动作审计（M-6，2026-11-11）。
//
// 一张表两类事件（EventType 区分）：
//   - unfollowed_first_touch：扩展端命中「关注关系」系统文案/未关注 tab 归属的消息
//     首次到达（每会话一条，dedup_key 幂等）→ 运营观测「非互关客户首触」量；
//   - follow_action：服务端策略触发自动回关动作行的留痕（动作行本身落 message_hub，
//     hub 行终态 delivered/failed 即投递结果；本表补触发来源与上下文）。
package model

import "time"

// 事件类型常量（EventType 列取值）
const (
	// BridgeFollowEventFirstTouch 首触未关注用户（识别信号到达）
	BridgeFollowEventFirstTouch = "unfollowed_first_touch"
	// BridgeFollowEventFollowAction 自动关注动作（策略触发动作行）
	BridgeFollowEventFollowAction = "follow_action"
)

// 关注信号来源词表（与扩展端 core/follow-signal.js FOLLOW_SIGNAL_SOURCES 同口径，改动两端同步）
const (
	FollowSignalMutual        = "mutual_follow"     // 互相关注已建立
	FollowSignalPeerFollow    = "peer_followed_you" // 对方关注了我们
	FollowSignalNotMutual     = "not_mutual"        // 明确提示非好友/非互关
	FollowSignalUnfollowedTab = "unfollowed_tab"    // bilibili 未关注人消息 tab 归属
	FollowSignalFriendRequest = "friend_request"    // 对方好友申请类
)

// BridgeFollowEvent 新用户（非关注好友）事件/自动关注动作审计行（表 bridge_follow_events）。
type BridgeFollowEvent struct {
	TenantID uint `gorm:"not null;default:1;index" json:"tenant_id"`

	ID        uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel   string `gorm:"type:varchar(32);not null;index" json:"channel"`
	AccountID string `gorm:"type:varchar(128);index" json:"account_id"`
	// ConversationID 会话定位键（bridge 帧原值）
	ConversationID string `gorm:"type:varchar(191);not null;index" json:"conversation_id"`
	// SenderName 首触消息发送者昵称（可空）
	SenderName string `gorm:"type:varchar(128)" json:"sender_name"`
	// EventType：unfollowed_first_touch / follow_action（常量见上）
	EventType string `gorm:"type:varchar(32);not null;index" json:"event_type"`
	// Signal 关注信号来源（FollowSignal* 词表；动作行留触发时的信号来源）
	Signal string `gorm:"type:varchar(32)" json:"signal"`
	// Result 动作结果：first_touch 恒 'registered'；follow_action 为
	// 'enqueued'（动作行已入队，投递终态看 message_hub 同 msg_id 行 status）。
	Result string `gorm:"type:varchar(32);not null" json:"result"`
	// Detail 结构化上下文 JSON（触发来源/账号/消息 event_id 等）
	Detail string `gorm:"type:text" json:"detail"`
	// MsgID 关联 message_hub 的行键：first_touch=首触消息 msg_id；follow_action=动作行 msg_id
	MsgID string `gorm:"type:varchar(128);index" json:"msg_id"`
	// DedupKey 幂等键（唯一索引）：
	//   first_touch = "uft:<channel>:<conversation_id>"（每会话一条首触）；
	//   follow_action = "fa:<channel>:<conversation_id>:<unix_ms>"（每次尝试独立可追溯）。
	DedupKey string `gorm:"type:varchar(255);not null;uniqueIndex" json:"dedup_key"`

	CreatedAt time.Time `gorm:"autoCreateTime;index" json:"created_at"`
}

// TableName GORM 表名
func (BridgeFollowEvent) TableName() string { return "bridge_follow_events" }
