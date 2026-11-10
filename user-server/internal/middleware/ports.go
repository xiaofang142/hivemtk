package middleware

import (
	"context"
	"hivemtk-user/internal/pkg/utils/logger"
	"sync"
)

// PermChecker 权限检查窄接口（service.PermissionService 天然满足）
type PermChecker interface {
	CheckPermission(ctx context.Context, role, permission string) bool
}

var (
	permCheckerMu sync.RWMutex
	permChecker   PermChecker
)

// SetPermChecker 装配期注入权限检查实现
func SetPermChecker(p PermChecker) {
	permCheckerMu.Lock()
	defer permCheckerMu.Unlock()
	permChecker = p
}

func getPermChecker() PermChecker {
	permCheckerMu.RLock()
	defer permCheckerMu.RUnlock()
	return permChecker
}

// AuditEntry middleware 自有的审计日志条目（镜像 model.OperationLog 所需字段）
type AuditEntry struct {
	TenantID   uint
	UserID     uint
	Username   string
	Action     string
	Module     string
	Resource   string
	ResourceID string
	Detail     string
	IP         string
	UserAgent  string
}

// AuditSink 审计日志落库窄接口（由装配层适配到 repository）
type AuditSink interface {
	Save(ctx context.Context, entry *AuditEntry) error
}

var (
	auditSinkMu sync.RWMutex
	auditSink   AuditSink
)

// SetAuditSink 装配期注入审计落库实现
func SetAuditSink(s AuditSink) {
	auditSinkMu.Lock()
	defer auditSinkMu.Unlock()
	auditSink = s
}

func getAuditSink() AuditSink {
	auditSinkMu.RLock()
	defer auditSinkMu.RUnlock()
	return auditSink
}

// ChatChannelView middleware 自有的渠道视图（装配层从 model.ChatChannel 转换而来）
type ChatChannelView struct {
	ChannelID      string
	ChannelName    string
	Status         string
	Active         bool
	AllowedOrigins []string
	// TenantID 渠道归属租户（访客链租户解析源）
	TenantID uint
}

// ChatChannelResolver 渠道解析窄接口（装配层由 service.ChatChannelService 适配）
type ChatChannelResolver interface {
	ResolveByAppKey(ctx context.Context, appKey string) (*ChatChannelView, error)
	ResolveByChannelID(ctx context.Context, channelID string) (*ChatChannelView, error)
}

// TenantCodeResolver 租户短码解析窄接口（装配层由 service.TenantService 适配）。
//
// 为什么做成注入的窄接口而不是直接调用 service：service 包已经 import middleware，
// 反向依赖会成环；而"公开入口按短码定租户"这件事只有装配层知道该用哪个来源。
type TenantCodeResolver func(ctx context.Context, code string) (uint, error)

func warnPortMissing(port string) {
	logger.Warnf("[middleware] %s 未注入（装配遗漏），已降级处理", port)
}
