package service

import (
	"context"
	"strings"
)

// ChannelIdentityResolver CH-08 自他识别的统一入口（平台稳定用户身份）。
//
// 与 ChannelResolver.Identify 的关系：Identify 保留基于成员角色的兼容判定；
// 本解析器额外接收平台稳定 self ID 与群成员角色映射，供真实消息入口消费。
// 识别码只用于认证归属，绝不作为聊天中真人身份证据。
type ChannelIdentityResolver struct {
	resolver *ChannelResolver
}

// NewChannelIdentityResolver 创建统一自他识别解析器。
func NewChannelIdentityResolver(r *ChannelResolver) *ChannelIdentityResolver {
	return &ChannelIdentityResolver{resolver: r}
}

// GroupMemberRole 群成员角色证据（平台稳定 userID + 显式角色）。
type GroupMemberRole struct {
	PlatformUserID string
	Role           string
}

// IdentifySpeaker 返回规范身份：self 只由稳定 userID 或显式 self 角色判定；
// 同名/缺身份/未知角色一律 unknown（调用方据此禁止猜测自动回复）。
func (i *ChannelIdentityResolver) IdentifySpeaker(
	ctx context.Context, tenantID, accountID uint, conversationType, speakerPlatformID, selfPlatformID string, members []GroupMemberRole,
) (*ChannelIdentity, error) {
	if i == nil || i.resolver == nil {
		return nil, ErrChannelIdentityUnresolved
	}
	typ := strings.ToLower(strings.TrimSpace(conversationType))
	if typ == "" {
		typ = "dm"
	}
	if typ != "dm" && typ != "single" && typ != "direct" && typ != "private" &&
		typ != "group" && typ != "group_chat" && typ != "chatroom" {
		return nil, ErrChannelIdentityUnresolved
	}
	role := ""
	if speakerPlatformID != "" && speakerPlatformID == strings.TrimSpace(selfPlatformID) {
		role = "self"
	} else if len(members) > 0 {
		// 群成员映射：同一稳定 ID 命中多条且角色冲突时拒绝（不猜）。
		var found []string
		for _, m := range members {
			if m.PlatformUserID != "" && m.PlatformUserID == speakerPlatformID {
				found = append(found, strings.ToLower(strings.TrimSpace(m.Role)))
			}
		}
		if len(found) == 1 {
			role = found[0]
		} else if len(found) > 1 {
			unique := map[string]bool{}
			for _, r := range found {
				unique[r] = true
			}
			if len(unique) > 1 {
				return nil, ErrChannelIdentityAmbiguous
			}
			role = found[0]
		}
	}
	out, err := i.resolver.Identify(ctx, tenantID, accountID, typ, speakerPlatformID, role)
	if err != nil {
		return nil, err
	}
	out.SpeakerID = strings.TrimSpace(speakerPlatformID)
	return out, nil
}

// IsUnknownIdentity 供业务闸使用：unknown 不触发猜测性自动回复。
func IsUnknownIdentity(identity *ChannelIdentity) bool {
	return identity == nil || strings.EqualFold(identity.Role, "unknown") || identity.SpeakerID == ""
}
