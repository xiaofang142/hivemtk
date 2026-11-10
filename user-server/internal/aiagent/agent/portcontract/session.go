package portcontract

import (
	"context"

	"hivemtk-user/internal/model"
)

// CreateSessionInput 开启会话请求投影。
//
// 与 service.CreateSessionRequest 字段对齐，工具层无需 import service 包。
type CreateSessionInput struct {
	Platform  string
	AccountID string
	UserID    string
	UserName  string
	UserPhone string
	UserEmail string
}

// SendMessageInput 发送会话消息请求投影。
type SendMessageInput struct {
	SessionID   string
	SenderType  string
	SenderID    string
	Content     string
	ContentType string
	// MediaURL 媒体附件地址（ContentType 非 text 时有效）。002-multi-type-messages：
	// 工具层此前把 media_url 参数丢弃（`_ = mediaURL`），智能体永远发不出媒体——
	// 投影补上该字段，由 SessionPortAdapter 映射进 SendMessageRequest.MediaURL。
	MediaURL string
	// SenderName 发送者昵称（可选）。此前同样被工具层丢弃；会话 UI 归因需要它。
	SenderName string
}

// SessionPort 私信 / 会话域端口
// （实现方 service.CustomerSessionService，消费方 tooluse/private_message_tools.go 等会话相关工具）。
// 注意：所有"发送者身份"必须由 service 层在 controller 鉴权上下文派生，
// 工具层不得伪造 sender_type 字段。
type SessionPort interface {
	CreateSession(ctx context.Context, in *CreateSessionInput) (*model.CustomerSession, error)
	GetMessages(sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error)
	SendMessage(ctx context.Context, in *SendMessageInput) (*model.SessionMessage, error)
}
