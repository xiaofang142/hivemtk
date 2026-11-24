package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// resolveModuleChannelAccount 由渠道账号标识（识别码 / 平台账号 ID）反查渠道账号主数据。
//
// 返回 bound=false 表示未纳入新配置（legacy 账号 / 仓储不可用 / 测试无 DB）。
// 按 R8.4 兼容口径：未绑定 → 调用方维持既有行为；已绑定 → 严格按 agent_id 字段启停。
func resolveModuleChannelAccount(ctx context.Context, accountID string) (*model.ChannelAccount, bool) {
	code := strings.TrimSpace(accountID)
	if code == "" {
		return nil, false
	}
	repo := repository.NewChannelAccountRepository()
	if repo == nil || !repo.Available() {
		return nil, false
	}
	acc, err := repo.GetByIdentifyCode(ctx, code)
	if err != nil {
		logger.Warnf("[module-gate] 渠道账号解析失败 account=%s: %v", code, err)
		return nil, false
	}
	if acc == nil || acc.ID == 0 {
		return nil, false
	}
	return acc, true
}

// AutoReplyService 自动回复智能体模块的独立启停闸门（FR-017/018，T048）。
//
// 启停键：channel_accounts.auto_reply_agent_id（非空且账号启用=开启）。
// 本服务只管「该不该触发自动回复」与「往共享上下文写/读交叉引用备注」，
// 不直接调线索发现模块；实际回复仍由既有 AITrigger/SmartCSOrchestrator 链路完成。
type AutoReplyService struct {
	ctxSvc *ConversationContextService
}

func NewAutoReplyService() *AutoReplyService {
	return &AutoReplyService{ctxSvc: NewConversationContextService()}
}

func NewAutoReplyServiceWithCtx(ctxSvc *ConversationContextService) *AutoReplyService {
	return &AutoReplyService{ctxSvc: ctxSvc}
}

// Enabled 判断该渠道账号是否启用自动回复模块。
func (s *AutoReplyService) Enabled(ctx context.Context, platform, accountID string) bool {
	acc, bound := resolveModuleChannelAccount(ctx, accountID)
	if !bound {
		return true // 未绑定渠道账号主数据：维持既有行为（R8.4）
	}
	if acc.Status != 1 {
		return false
	}
	return acc.AutoReplyAgentID != nil && *acc.AutoReplyAgentID > 0
}

// EnabledForAccount 按数字账号 ID 判断（管理端/装配点用）；无租户上下文时 fail-closed 返回 false。
func (s *AutoReplyService) EnabledForAccount(ctx context.Context, accountID uint) bool {
	if accountID == 0 {
		return false
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return false
		}
		tid = tenant.DefaultTenantID
	}
	repo := repository.NewChannelAccountRepository()
	if repo == nil || !repo.Available() {
		return true
	}
	acc, err := repo.GetByID(ctx, tid, accountID)
	if err != nil || acc == nil {
		return true
	}
	return acc.Status == 1 && acc.AutoReplyAgentID != nil && *acc.AutoReplyAgentID > 0
}

// RecordNote 写一条自动回复侧备注到共享上下文（T051：转人工/情感等事件）。
func (s *AutoReplyService) RecordNote(ctx context.Context, accountID uint, convID, note string) error {
	if s == nil || s.ctxSvc == nil {
		return errConversationContextUnavailable
	}
	return s.ctxSvc.AddNote(ctx, accountID, convID, ContextNoteNamespaceAutoReply, note)
}

// ReferLeadNotes 读取线索发现智能体写入的备注（T051：被对方交叉引用）。
func (s *AutoReplyService) ReferLeadNotes(ctx context.Context, accountID uint, convID string) []string {
	if s == nil || s.ctxSvc == nil {
		return nil
	}
	notes, err := s.ctxSvc.CrossReferenceNotes(ctx, accountID, convID, ContextNoteNamespaceAutoReply)
	if err != nil {
		logger.Warnf("[auto-reply] 读取线索侧交叉引用失败 account=%d conv=%s: %v", accountID, convID, err)
		return nil
	}
	return notes
}
