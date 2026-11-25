package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// leadHistoryScope 是本次分析冻结的来源边界，数值账号用于共享上下文，引用集合仅用于兼容历史行。
type leadHistoryScope struct {
	repository.MessageHubHistoryScope
	tenantID   uint
	accountID  uint
	channelID  uint
	accountKey string
	sourceRef  string
}

type leadHistoryScopeKey struct{}

func (s *Service) historyScope(ctx context.Context, hub *model.MessageHub) (*leadHistoryScope, error) {
	tid, ok := tenant.FromContext(ctx)
	if hub == nil || !ok || tid == 0 || tenant.IsSystemScope(ctx) || (hub.TenantID != 0 && hub.TenantID != tid) || strings.TrimSpace(hub.AccountID) == "" || strings.TrimSpace(hub.Platform) == "" {
		return nil, repository.ErrMessageHubHistoryScope
	}
	if scope, ok := ctx.Value(leadHistoryScopeKey{}).(*leadHistoryScope); ok && scope.tenantID == tid && scope.sourceRef == hub.AccountID && scope.Platform == hub.Platform && scope.IsGroup == hub.IsGroup {
		return scope, nil
	}
	scope := &leadHistoryScope{MessageHubHistoryScope: repository.MessageHubHistoryScope{
		Platform: hub.Platform, AccountIDs: []string{hub.AccountID}, IsGroup: hub.IsGroup,
	}, tenantID: tid, sourceRef: hub.AccountID, accountKey: "raw:" + hub.AccountID}
	r := s.channelResolver
	if r == nil {
		r = NewChannelResolver()
	}
	// 识别码只是寻址输入；歧义/跨租户/错平台必须拒绝，不能作为 ACL。
	byCode, codeErr := r.ResolveByIdentifyCode(ctx, hub.AccountID)
	if errors.Is(codeErr, ErrChannelIdentityAmbiguous) {
		return nil, codeErr
	}
	resolved, err := r.ResolveByAccountRef(ctx, hub.Platform, hub.AccountID)
	if byCode != nil {
		resolved, err = byCode, nil
	}
	if err != nil || resolved == nil {
		if errors.Is(err, ErrChannelIdentityAmbiguous) {
			return nil, err
		}
		logger.Ctx(ctx).Warn().Err(err).Str("account", hub.AccountID).Msg("[lead-mining] 账号解析降级：仅精确来源账号历史，禁用共享摘要")
		return scope, nil
	}
	if resolved.TenantID != tid || resolved.AccountID == 0 || r.accountRepo == nil || r.channelRepo == nil {
		return nil, ErrChannelTenantUnresolved
	}
	acc, err := r.accountRepo.GetByID(ctx, tid, resolved.AccountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.TenantID != tid {
		return nil, ErrChannelTenantUnresolved
	}
	ch, err := r.channelRepo.GetByID(ctx, tid, acc.ChannelID)
	if err != nil {
		return nil, err
	}
	if ch == nil || (!strings.EqualFold(ch.Code, hub.Platform) && !strings.EqualFold(ch.Type, hub.Platform)) {
		return nil, ErrChannelIdentityUnresolved
	}
	if err := leadHistoryRefUnique(ctx, r, scope, acc.ID, hub.AccountID); err != nil {
		return nil, err
	}
	scope.accountID, scope.channelID = acc.ID, acc.ChannelID
	id := strconv.FormatUint(uint64(acc.ID), 10)
	scope.accountKey = "id:" + id
	// 逐个引用重新自证；共享识别码/同名账号不进入查询集合。
	for _, ref := range []string{id, acc.Name, acc.IdentifyCode} {
		if strings.TrimSpace(ref) == "" || ref == hub.AccountID {
			continue
		}
		if _, err := r.ResolveByIdentifyCode(ctx, ref); errors.Is(err, ErrChannelIdentityAmbiguous) {
			continue
		}
		alias, err := r.ResolveByAccountRef(ctx, hub.Platform, ref)
		if err == nil && alias != nil && alias.TenantID == tid && alias.AccountID == acc.ID && alias.ChannelID == acc.ChannelID && leadHistoryRefUnique(ctx, r, scope, acc.ID, ref) == nil {
			scope.AccountIDs = append(scope.AccountIDs, ref)
		}
	}
	return scope, nil
}

// 解析器的识别码/数值ID优先级用于寻址，历史原始引用还必须排除同平台其他账号的同名歧义。
func leadHistoryRefUnique(ctx context.Context, resolver *ChannelResolver, scope *leadHistoryScope, accountID uint, ref string) error {
	rows, err := listChannelAccounts(ctx, resolver.accountRepo, repository.ChannelAccountListOptions{
		TenantID: scope.tenantID, Keyword: strings.TrimSpace(ref),
	})
	if err != nil {
		return err
	}
	if id, err := strconv.ParseUint(strings.TrimSpace(ref), 10, 64); err == nil && id > 0 && uint(id) != accountID {
		acc, err := resolver.accountRepo.GetByID(ctx, scope.tenantID, uint(id))
		if err != nil {
			return err
		}
		if acc != nil {
			rows = append(rows, *acc)
		}
	}
	for _, acc := range rows {
		if acc.ID == accountID || (strconv.FormatUint(uint64(acc.ID), 10) != strings.TrimSpace(ref) && !strings.EqualFold(strings.TrimSpace(acc.Name), strings.TrimSpace(ref)) && !strings.EqualFold(strings.TrimSpace(acc.IdentifyCode), strings.TrimSpace(ref))) {
			continue
		}
		ch, err := resolver.channelRepo.GetByID(ctx, scope.tenantID, acc.ChannelID)
		if err != nil {
			return err
		}
		if ch != nil && (strings.EqualFold(ch.Code, scope.Platform) || strings.EqualFold(ch.Type, scope.Platform)) {
			return ErrChannelIdentityAmbiguous
		}
	}
	return nil
}

func leadMiningDebounceKey(tenantID uint, platform, accountID, conversationID, groupID, senderID string, isGroup bool) string {
	kind, conversation := "dm", conversationID
	if isGroup {
		kind = "group"
		if groupID != "" {
			conversation = groupID
		}
	} else if conversation == "" {
		kind, conversation = "peer", senderID
	}
	if tenantID == 0 || platform == "" || accountID == "" || conversation == "" {
		return ""
	}
	return fmt.Sprintf("%d:%q:%q:%s:%q", tenantID, platform, accountID, kind, conversation)
}

func leadScopedConversation(ctx context.Context, repo repository.ConversationContextRepository, accountID uint, conversationID string) (*model.ConversationContext, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || repo == nil || accountID == 0 || conversationID == "" {
		return nil, repository.ErrMessageHubHistoryScope
	}
	return repo.Get(ctx, accountID, conversationID)
}

// 共享上下文包含平台和单/群聊类型，避免同账号下相同外部ID相撞。
// 不回读无作用域的旧摘要；后续判定从已隔离的消息历史重新生成。
func leadHistoryConversationID(hub *model.MessageHub) string {
	if hub == nil {
		return ""
	}
	return conversationContextID(hub.ConversationID, ConversationContextScope{Platform: hub.Platform, IsGroup: hub.IsGroup, GroupID: hub.GroupID})
}
