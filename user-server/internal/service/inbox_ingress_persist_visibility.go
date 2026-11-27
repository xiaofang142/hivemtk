package service

import (
	"strings"

	"hivemtk-user/internal/model"
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

// hubIngressAuthorityExtraFromEvent 返回只能由服务端写入 hub.Extra 的权威键。
// 客户端自称值已在 channelgw 层拒绝，这里再次覆盖，避免旧调用方带入不可信值。
func hubIngressAuthorityExtraFromEvent(event *model.MessageEvent) model.JSONMap {
	result := model.JSONMap{}
	if event == nil {
		return result
	}
	if v := strings.TrimSpace(event.BusLLMSessionID); v != "" {
		result["llm_session_id"] = v
	}
	if v := strings.TrimSpace(event.EventID); v != "" {
		result["correlation_id"] = v
		result["llm_correlation_id"] = v
	}
	return result
}
