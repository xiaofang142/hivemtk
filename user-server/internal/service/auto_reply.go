package service

import (
	"context"
	"errors"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

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
	decision := resolveModuleGateDecision(ctx, accountID, func(acc *model.ChannelAccount) uint {
		if acc.AutoReplyAgentID == nil {
			return 0
		}
		return *acc.AutoReplyAgentID
	})
	return decision.Enabled()
}

// EnabledForAccount 按数字账号 ID 判断（管理端/装配点用）；无租户上下文时 fail-closed 返回 false。
func (s *AutoReplyService) EnabledForAccount(ctx context.Context, accountID uint) bool {
	return enabledForAccount(ctx, accountID, repository.NewChannelAccountRepository())
}

func enabledForAccount(ctx context.Context, accountID uint, repo repository.ChannelAccountRepository) bool {
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
	if repo == nil || !repo.Available() {
		return false
	}
	acc, err := repo.GetByID(ctx, tid, accountID)
	if err != nil || acc == nil || acc.ID == 0 || acc.ID != accountID || acc.TenantID != tid {
		return false
	}
	return acc.Status == 1 && acc.AutoReplyAgentID != nil && *acc.AutoReplyAgentID > 0
}

// RecordNote 写一条自动回复侧备注到共享上下文（T051：转人工/情感等事件）。
func (s *AutoReplyService) RecordNote(ctx context.Context, accountID uint, convID, note string, scopes ...ConversationContextScope) error {
	if s == nil || s.ctxSvc == nil {
		return errConversationContextUnavailable
	}
	return s.ctxSvc.AddNote(ctx, accountID, convID, ContextNoteNamespaceAutoReply, note, scopes...)
}

// ReferLeadNotes 读取线索发现智能体写入的备注（T051：被对方交叉引用）。
func (s *AutoReplyService) ReferLeadNotes(ctx context.Context, accountID uint, convID string, scopes ...ConversationContextScope) []string {
	if s == nil || s.ctxSvc == nil {
		return nil
	}
	notes, err := s.ctxSvc.CrossReferenceNotes(ctx, accountID, convID, ContextNoteNamespaceAutoReply, scopes...)
	if err != nil {
		logger.Warnf("[auto-reply] 读取线索侧交叉引用失败 account=%d conv=%s: %v", accountID, convID, err)
		return nil
	}
	return notes
}

// ModuleGateDecision 双模块执行闸门的显式判定。
// 未迁移与故障必须分开：故障不能因为「未绑定」而放行开启模块。
type ModuleGateDecision int

const (
	ModuleGateUnmigrated ModuleGateDecision = iota // 仓储可用且查询确认未命中，维持 legacy 行为
	ModuleGateEnabled                              // 明确启用
	ModuleGateDisabled                             // 明确关闭（含账号停用/未绑定 Agent）
	ModuleGateError                                // 解析或仓储故障，fail-closed
)

// ModuleGateInput 闸门判定的可注入输入，便于纯函数反例测试。
type ModuleGateInput struct {
	Bound     bool  // 是否解析到真实渠道账号主数据
	LookupErr error // 解析/仓储查询错误
	Status    int   // 账号状态，1 启用
	AgentID   uint  // 对应模块的 Agent ID
}

// Enabled 返回该判定是否应触发模块；仅明确启用或未迁移放行，故障/关闭一律不触发。
func (d ModuleGateDecision) Enabled() bool {
	return d == ModuleGateEnabled || d == ModuleGateUnmigrated
}

// decideModuleGate 把解析结果归一为显式决策。
func decideModuleGate(in ModuleGateInput) ModuleGateDecision {
	if in.LookupErr != nil {
		return ModuleGateError
	}
	if !in.Bound {
		return ModuleGateUnmigrated
	}
	if in.Status != 1 || in.AgentID == 0 {
		return ModuleGateDisabled
	}
	return ModuleGateEnabled
}

// resolveModuleGateDecision 统一解析账号并给出模块闸门决策。
// 只有查询成功确认未命中的账号保留 legacy 兼容放行；解析/仓储故障一律 fail-closed。
func resolveModuleGateDecision(ctx context.Context, accountID string, agentOf func(*model.ChannelAccount) uint) ModuleGateDecision {
	repo := repository.NewChannelAccountRepository()
	resolver := NewChannelResolver()
	return resolveModuleGateDecisionWith(ctx, accountID, agentOf, repo, func(ctx context.Context, ref string) (*ResolvedChannelAccount, error) {
		return resolver.ResolveByAccountRef(ctx, "", ref)
	})
}

func resolveModuleGateDecisionWith(
	ctx context.Context,
	accountID string,
	agentOf func(*model.ChannelAccount) uint,
	repo repository.ChannelAccountRepository,
	resolve func(context.Context, string) (*ResolvedChannelAccount, error),
) ModuleGateDecision {
	code := strings.TrimSpace(accountID)
	if code == "" || repo == nil || !repo.Available() || resolve == nil {
		return ModuleGateError
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return ModuleGateError
		}
		tid = tenant.DefaultTenantID
	}
	resolved, err := resolve(ctx, code)
	if err != nil {
		if errors.Is(err, ErrChannelIdentityLegacyUnmatched) {
			// 只有仓储可用且解析成功确认无匹配时，才保留 R8.4 的 legacy 放行。
			return ModuleGateUnmigrated
		}
		return ModuleGateError
	}
	if resolved == nil || resolved.AccountID == 0 {
		return ModuleGateError
	}
	if resolved.TenantID == 0 || resolved.TenantID != tid {
		return ModuleGateError
	}
	acc, err := repo.GetByID(ctx, resolved.TenantID, resolved.AccountID)
	if err != nil || acc == nil || acc.ID == 0 || acc.ID != resolved.AccountID || acc.TenantID != resolved.TenantID {
		return ModuleGateError
	}
	return decideModuleGate(ModuleGateInput{
		Bound: true, Status: acc.Status, AgentID: agentOf(acc),
	})
}
