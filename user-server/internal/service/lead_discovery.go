package service

import (
	"context"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// LeadDiscoveryService 线索发现智能体模块的独立启停闸门（FR-017/019，T049）。
//
// 启停键：channel_accounts.lead_mining_agent_id（非空且账号启用=开启）。
// 边界：本模块只做商机识别 / 归档，整条链路**不持有 AITrigger、不回调 auto_reply**
// （R8.3-B）。启用后仅委托既有线索发掘 Service.Enqueue（异步 worker）。
type LeadDiscoveryService struct {
	mining *Service
	ctxSvc *ConversationContextService
}

func NewLeadDiscoveryService(mining *Service) *LeadDiscoveryService {
	return &LeadDiscoveryService{mining: mining, ctxSvc: NewConversationContextService()}
}

func NewLeadDiscoveryServiceWithCtx(mining *Service, ctxSvc *ConversationContextService) *LeadDiscoveryService {
	return &LeadDiscoveryService{mining: mining, ctxSvc: ctxSvc}
}

// Enabled 判断该渠道账号是否启用线索发现模块。
func (s *LeadDiscoveryService) Enabled(ctx context.Context, platform, accountID string) bool {
	decision := resolveModuleGateDecision(ctx, accountID, func(acc *model.ChannelAccount) uint {
		if acc.LeadMiningAgentID == nil {
			return 0
		}
		return *acc.LeadMiningAgentID
	})
	return decision.Enabled()
}

// Process ingest 后的线索发现入口：仅在模块启用时委托异步线索发掘。
// 这里刻意不引用任何 AITrigger / 自动回复接口——关闭自动回复不影响本腿，
// 本腿也永不产生自动回复下发（FR-019）。
func (s *LeadDiscoveryService) Process(ctx context.Context, hub *model.MessageHub) {
	if s == nil || hub == nil {
		return
	}
	if !s.Enabled(ctx, hub.Platform, hub.AccountID) {
		return
	}

	// 共享上下文：登记群成员角色（T052）并读取自动回复侧备注（T051 交叉引用）。
	// 这些写读都是 best-effort，失败不阻断线索发现主链，但记 warn（fail-visible）。
	mining := s.mining
	if mining == nil {
		mining = &Service{}
	}
	account, scopeErr := mining.historyScope(ctx, hub)
	if scopeErr == nil && account.accountID > 0 && s.ctxSvc != nil && leadHistoryConversationID(hub) != "" {
		accountID := account.accountID
		scope := ConversationContextScope{Platform: hub.Platform, IsGroup: hub.IsGroup, GroupID: hub.GroupID}
		if hub.IsGroup && hub.SenderID != "" {
			// LM-11：普通发言只登记群成员身份；有证据的业务角色由判定链写入，
			// 这里不把已证实的 demand/supply/broker 覆盖回 member。
			role := resolveGroupMemberRole(hub.SenderID, "member", "")
			if err := s.ctxSvc.SetMemberRole(ctx, accountID, hub.ConversationID, hub.SenderID, role, scope); err != nil {
				logger.Warnf("[lead-discovery] 登记群成员角色失败 account=%d conv=%s: %v", accountID, hub.ConversationID, err)
			}
			if _, _, err := s.ctxSvc.AggregateGroupContext(ctx, accountID, hub.ConversationID, scope); err != nil {
				logger.Warnf("[lead-discovery] 群聊上下文聚合失败 account=%d conv=%s: %v", accountID, hub.ConversationID, err)
			}
		}
		if notes := s.ReferAutoReplyNotes(ctx, accountID, hub.ConversationID, scope); len(notes) > 0 {
			logger.Infof("[lead-discovery] 交叉引用自动回复备注 account=%d conv=%s notes=%v", accountID, hub.ConversationID, notes)
		}
		if err := s.RecordNote(ctx, accountID, hub.ConversationID, "线索发现链路已介入", scope); err != nil {
			logger.Warnf("[lead-discovery] 写交叉引用备注失败 account=%d conv=%s: %v", accountID, hub.ConversationID, err)
		}
	} else {
		logger.Warnf("[lead-discovery] 跳过未自证账号的共享上下文 account=%s: %v", hub.AccountID, scopeErr)
	}

	if s.mining == nil {
		return
	}
	s.mining.Enqueue(hub)
}

// RecordNote 写一条线索发现侧备注到共享上下文（T051）。
func (s *LeadDiscoveryService) RecordNote(ctx context.Context, accountID uint, convID, note string, scopes ...ConversationContextScope) error {
	if s == nil || s.ctxSvc == nil {
		return errConversationContextUnavailable
	}
	return s.ctxSvc.AddNote(ctx, accountID, convID, ContextNoteNamespaceLeadMining, note, scopes...)
}

// ReferAutoReplyNotes 读取自动回复智能体写入的备注（T051：被对方交叉引用）。
func (s *LeadDiscoveryService) ReferAutoReplyNotes(ctx context.Context, accountID uint, convID string, scopes ...ConversationContextScope) []string {
	if s == nil || s.ctxSvc == nil {
		return nil
	}
	notes, err := s.ctxSvc.CrossReferenceNotes(ctx, accountID, convID, ContextNoteNamespaceLeadMining, scopes...)
	if err != nil {
		logger.Warnf("[lead-discovery] 读取自动回复侧交叉引用失败 account=%d conv=%s: %v", accountID, convID, err)
		return nil
	}
	return notes
}
