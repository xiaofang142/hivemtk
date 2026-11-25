package service

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var ErrConversationContextForbidden = errors.New("无权读取该账号的共享上下文")

// SnapshotForUser 是员工只读入口；内部双模块继续使用 Snapshot，不借用员工身份。
// 每次读取重新解析当前授权，不缓存角色或账号集合。
func (s *ConversationContextService) SnapshotForUser(ctx context.Context, userID, accountID uint, convID string, scopes ...ConversationContextScope) (*ConversationContextSnapshot, error) {
	if err := authorizeConversationContext(ctx, userID, accountID); err != nil {
		return nil, err
	}
	return s.Snapshot(ctx, accountID, convID, scopes...)
}

func authorizeConversationContext(ctx context.Context, userID, accountID uint) error {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || userID == 0 || accountID == 0 {
		return ErrConversationContextForbidden
	}
	user, err := repository.NewSystemUserRepository().GetByID(ctx, userID)
	if err != nil || user == nil || user.TenantID != tid || !user.Enabled {
		return errors.Join(ErrConversationContextForbidden, err)
	}
	resolver := NewChannelResolver()
	account, err := resolver.accountRepo.GetByID(ctx, tid, accountID)
	if err != nil || account == nil || account.TenantID != tid {
		return errors.Join(ErrConversationContextForbidden, err)
	}
	channel, err := resolver.channelRepo.GetByID(ctx, tid, account.ChannelID)
	if err != nil || channel == nil || channel.TenantID != tid {
		return errors.Join(ErrConversationContextForbidden, err)
	}
	business, err := NewBusinessScopeService().Resolve(ctx, userID)
	if err != nil {
		return errors.Join(ErrConversationContextForbidden, err)
	}
	if business.AllAccounts {
		return nil
	}
	grants, err := repository.NewChannelAccountAuthorizationRepository().ListByUser(ctx, userID)
	if err != nil {
		return errors.Join(ErrConversationContextForbidden, err)
	}
	for _, grant := range grants {
		if grant.TenantID == tid && grant.UserID == userID && grant.ChannelAccountID == accountID && (grant.Role == "viewer" || grant.Role == "owner") {
			return nil
		}
	}
	// 保留旧新授权并集；旧授权必须匹配真实渠道和账号，识别码不能充当 ACL。
	for _, grant := range business.Authorizations {
		if grant.TenantID != tid || grant.UserID != userID || grant.DeletedAt != nil ||
			(grant.Scope != "read" && grant.Scope != "write" && grant.Scope != "manage") {
			continue
		}
		if grant.Channel != "" && !strings.EqualFold(strings.TrimSpace(grant.Channel), channel.Code) && !strings.EqualFold(strings.TrimSpace(grant.Channel), channel.Type) {
			continue
		}
		if conversationGrantMatchesAccount(ctx, resolver, account, grant.Channel, grant.AccountID) {
			return nil
		}
	}
	return ErrConversationContextForbidden
}

func conversationGrantMatchesAccount(ctx context.Context, resolver *ChannelResolver, account *model.ChannelAccount, grantChannel, ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == strconv.FormatUint(uint64(account.ID), 10) {
		return true
	}
	// 只兼容能唯一自证的旧账号名，不将旧授权中的识别码或任意引用映射成授权。
	if ref == "" || ref == account.IdentifyCode || !strings.EqualFold(ref, strings.TrimSpace(account.Name)) {
		return false
	}
	scope := &leadHistoryScope{tenantID: account.TenantID}
	scope.Platform = strings.TrimSpace(grantChannel)
	if scope.Platform == "" {
		// 未指定渠道的旧名字授权只能在整个租户内唯一，不能按目标平台各自放行。
		rows, err := listChannelAccounts(ctx, resolver.accountRepo, repository.ChannelAccountListOptions{TenantID: account.TenantID, Keyword: ref})
		if err != nil {
			return false
		}
		for _, candidate := range rows {
			if candidate.ID != account.ID && (strings.EqualFold(strings.TrimSpace(candidate.Name), ref) || strings.EqualFold(strings.TrimSpace(candidate.IdentifyCode), ref)) {
				return false
			}
		}
		if id, err := strconv.ParseUint(ref, 10, 64); err == nil && id > 0 && uint(id) != account.ID {
			other, err := resolver.accountRepo.GetByID(ctx, account.TenantID, uint(id))
			if err != nil || other != nil {
				return false
			}
		}
		return true
	}
	return leadHistoryRefUnique(ctx, resolver, scope, account.ID, ref) == nil
}
