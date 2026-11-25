package service

import (
	"context"
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
	ModuleGateUnmigrated ModuleGateDecision = iota // 未纳入新配置，维持既有行为
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
// 账号可信解析后，读取仓储失败归为 ModuleGateError（fail-closed）；
// 未迁移或无法解析的账号仍保持既有兼容放行。
func resolveModuleGateDecision(ctx context.Context, accountID string, agentOf func(*model.ChannelAccount) uint) ModuleGateDecision {
	code := strings.TrimSpace(accountID)
	if code == "" {
		return ModuleGateUnmigrated
	}
	repo := repository.NewChannelAccountRepository()
	if repo == nil || !repo.Available() {
		// 仓储不可用等价于「尚未纳入新配置」：沿用旧行为放行，不能把单机/测试部署误判为模块故障关闭。
		return ModuleGateUnmigrated
	}
	resolved, err := NewChannelResolver().ResolveByAccountRef(ctx, "", code)
	if err != nil {
		// 解析阶段任何错误都等价于「无法纳入新配置」：沿用旧行为放行。
		// 只有账号已被可信解析后、读取模块配置失败才 fail-closed，
		// 否则单机/未建表部署会把所有模块误关（与 R8.4 未迁移兼容口径冲突）。
		return ModuleGateUnmigrated
	}
	if resolved == nil || resolved.AccountID == 0 {
		return ModuleGateUnmigrated
	}
	acc, err := repo.GetByID(ctx, resolved.TenantID, resolved.AccountID)
	if err != nil {
		return ModuleGateError
	}
	if acc == nil || acc.ID == 0 {
		return ModuleGateUnmigrated
	}
	return decideModuleGate(ModuleGateInput{
		Bound: true, Status: acc.Status, AgentID: agentOf(acc),
	})
}
