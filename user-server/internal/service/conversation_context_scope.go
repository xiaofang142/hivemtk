package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

// ConversationContextScope 是双模块和管理端共用的会话引用，不是新的权限系统。
// 账号权限仍由租户上下文及渠道账号归属确定；群聊优先使用 GroupID。
type ConversationContextScope struct {
	Platform string
	IsGroup  bool
	GroupID  string
}

var ErrConversationContextScope = errors.New("共享上下文会话归属不明确")

func conversationContextID(conversationID string, scope ConversationContextScope) string {
	kind := "dm"
	if scope.IsGroup {
		kind = "group"
		if scope.GroupID != "" {
			conversationID = scope.GroupID
		}
	}
	if strings.TrimSpace(scope.Platform) == "" || strings.TrimSpace(conversationID) == "" {
		return ""
	}
	key := sha256.Sum256([]byte(fmt.Sprintf("%q:%s:%q", scope.Platform, kind, conversationID)))
	return fmt.Sprintf("lead-history:v1:%x", key)
}

// resolveConversationContextID 只接受本租户真实账号；旧无平台 API 仅在消息证据能
// 唯一定位平台/单群时兼容。绝不读取或迁移可能已混合的无 scope 旧摘要。
func resolveConversationContextID(ctx context.Context, accountID uint, convID string, scopes []ConversationContextScope) (string, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) || accountID == 0 || len(scopes) > 1 {
		return "", ErrConversationContextScope
	}
	r := NewChannelResolver()
	acc, err := r.accountRepo.GetByID(ctx, tid, accountID)
	if err != nil {
		return "", err
	}
	if acc == nil || acc.TenantID != tid {
		return "", ErrConversationContextScope
	}
	ch, err := r.channelRepo.GetByID(ctx, tid, acc.ChannelID)
	if err != nil {
		return "", err
	}
	if ch == nil || ch.TenantID != tid {
		return "", ErrConversationContextScope
	}
	if len(scopes) == 1 {
		scope := scopes[0]
		if !strings.EqualFold(scope.Platform, ch.Code) && !strings.EqualFold(scope.Platform, ch.Type) {
			return "", ErrConversationContextScope
		}
		key := conversationContextID(convID, scope)
		if key == "" || (!scope.IsGroup && scope.GroupID != "") {
			return "", ErrConversationContextScope
		}
		return key, nil
	}
	if strings.TrimSpace(convID) == "" {
		return "", ErrConversationContextScope
	}
	key := ""
	seenPlatforms := make(map[string]bool)
	for _, platform := range []string{ch.Code, ch.Type} {
		if platform == "" || seenPlatforms[platform] {
			continue
		}
		seenPlatforms[platform] = true
		scope := &leadHistoryScope{MessageHubHistoryScope: repository.MessageHubHistoryScope{Platform: platform}, tenantID: tid}
		refs := []string{}
		for _, ref := range []string{strconv.FormatUint(uint64(accountID), 10), acc.Name, acc.IdentifyCode} {
			if ref != "" && leadHistoryRefUnique(ctx, r, scope, accountID, ref) == nil {
				resolved, err := r.ResolveByAccountRef(ctx, platform, ref)
				if err == nil && resolved != nil && resolved.TenantID == tid && resolved.AccountID == accountID {
					refs = append(refs, ref)
				}
			}
		}
		if len(refs) == 0 {
			continue
		}
		rows, err := repository.NewMessageHubRepository().ListHistoryConversationRefs(ctx, repository.MessageHubHistoryScope{Platform: platform, AccountIDs: refs}, convID)
		if err != nil {
			return "", err
		}
		for _, row := range rows {
			candidate := conversationContextID(row.ConversationID, ConversationContextScope{Platform: row.Platform, IsGroup: row.IsGroup, GroupID: row.GroupID})
			if candidate == "" || (key != "" && key != candidate) {
				return "", ErrConversationContextScope
			}
			key = candidate
		}
	}
	if key == "" {
		return "", ErrConversationContextScope
	}
	return key, nil
}
