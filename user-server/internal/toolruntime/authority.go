// Package toolruntime 定义工具执行准入端口，运行层不依赖持久化实现。
package toolruntime

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
)

var (
	ErrUnavailable = errors.New("tool admission: authoritative state unavailable")
	ErrDisabled    = errors.New("tool admission: tool disabled")
	ErrRevoked     = errors.New("tool admission: source revoked")
	ErrChanged     = errors.New("tool admission: source configuration changed; rediscovery required")
)

// Authority 每次实际尝试前检查权威状态，不缓存许可；已准入的在途调用可结束。
type Authority interface {
	CheckToolAdmission(context.Context, string) error
	CheckCustomTool(context.Context, model.CustomTool, string) error
	CheckMCPServer(context.Context, model.MCPServer, string) error
}
