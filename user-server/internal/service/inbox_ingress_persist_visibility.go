package service

import (
	"context"
	"strconv"
	"strings"

	agentcontext "hivemtk-user/internal/aiagent/context"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
)

// hubIngressVisibility 是入站消息落 hub 时的权威可见性证明。
// 群聊只使用 group_id 作为 audience 根；私聊只使用稳定 ID，绝不使用昵称。
type hubIngressVisibility struct {
	Visibility string
	Audience   []string
	Evidence   model.JSONMap
}

func hubIngressVisibilityFromEvent(event *model.MessageEvent) hubIngressVisibility {
	result := hubIngressVisibility{
		Visibility: model.VisibilityUnknown,
		Audience:   []string{},
		Evidence:   model.JSONMap{},
	}
	if event == nil {
		return result
	}

	if event.IsGroup {
		groupID := strings.TrimSpace(event.GroupID)
		if groupID == "" {
			return result
		}
		result.Visibility = model.VisibilityMember
		result.Audience = []string{groupID}
		result.Evidence = model.JSONMap{
			"source":   "channel_ingress",
			"is_group": true,
			"group_id": groupID,
		}
		return result
	}

	peer := strings.TrimSpace(event.SenderID)
	if peer == "" {
		peer = strings.TrimSpace(event.ConversationID)
	}
	if peer == "" {
		return result
	}
	result.Visibility = model.VisibilityPublic
	result.Audience = []string{peer}
	result.Evidence = model.JSONMap{"source": "channel_ingress"}
	return result
}

// hubIngressBusLLMSessionID 优先使用消息总线提供的 canonical key；总线缺失时
// 由服务端按权威 scope 确定性派生一次，保证同群跨会话/跨成员仍复用同一条链。
func hubIngressBusLLMSessionID(ctx context.Context, accountID string, event *model.MessageEvent) string {
	if event != nil {
		if v := strings.TrimSpace(event.BusLLMSessionID); v != "" {
			return v
		}
	}
	if event == nil {
		return ""
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 || tenant.IsSystemScope(ctx) {
		return ""
	}
	account, err := strconv.ParseUint(strings.TrimSpace(accountID), 10, 64)
	if err != nil || account == 0 {
		return ""
	}
	identity, err := agentcontext.DeriveScopeIdentity(agentcontext.ConversationScope{
		TenantID:         tid,
		Platform:         event.Channel,
		ChannelAccountID: uint(account),
		ConversationID:   event.ConversationID,
		IsGroup:          event.IsGroup,
		GroupID:          event.GroupID,
		StablePeerID:     event.SenderID,
	})
	if err != nil {
		return ""
	}
	return identity.BusLLMSessionID
}

// hubIngressAuthorityExtraFromEvent 返回只能由服务端写入 hub.Extra 的权威键。
// 客户端自称值已在 channelgw 层拒绝，这里再次覆盖，避免旧调用方带入不可信值。
func hubIngressAuthorityExtraFromEvent(ctx context.Context, accountID string, event *model.MessageEvent) model.JSONMap {
	result := model.JSONMap{}
	if event == nil {
		return result
	}
	if v := hubIngressBusLLMSessionID(ctx, accountID, event); v != "" {
		result["llm_session_id"] = v
	}
	if v := strings.TrimSpace(event.EventID); v != "" {
		result["correlation_id"] = v
		result["llm_correlation_id"] = v
	}
	return result
}
