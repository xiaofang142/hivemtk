package service

import (
	"context"
	"strings"

	"hivemtk-user/internal/repository"
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

// IdentifySpeakerWithSenderName 在统一自他识别上叠加「每个账号的标记」弱提示。
//
// 账号标记（channel_accounts.self_identify_name，取自上报消息里的发送者名称）只是
// 弱证据：①强证据（平台稳定 userID / 显式成员角色）已定论时不覆盖；②仅能把 unknown
// 提升为 self，绝不据名称推断 customer；③标记为空、名称不匹配、同租户多账号共用同一
// 标记（歧义）时一律保持 unknown（fail-closed），由业务闸拒绝猜测性自动回复。
func (i *ChannelIdentityResolver) IdentifySpeakerWithSenderName(
	ctx context.Context, tenantID, accountID uint, conversationType, speakerPlatformID, senderName, selfPlatformID string, members []GroupMemberRole,
) (*ChannelIdentity, error) {
	out, err := i.IdentifySpeaker(ctx, tenantID, accountID, conversationType, speakerPlatformID, selfPlatformID, members)
	if err != nil || out == nil {
		return out, err
	}
	if out.Evidence != "unknown" || strings.TrimSpace(senderName) == "" {
		return out, nil
	}
	if i.resolver == nil || i.resolver.accountRepo == nil {
		return out, nil
	}
	acc, err := i.resolver.accountRepo.GetByID(ctx, tenantID, accountID)
	if err != nil || acc == nil {
		return out, err
	}
	mark := strings.TrimSpace(acc.SelfIdentifyName)
	if mark == "" {
		return out, nil // 存量账号未设标记：不猜，保持 unknown
	}
	name := strings.TrimSpace(senderName)
	if !strings.EqualFold(mark, name) {
		return out, nil
	}
	// 歧义守卫：同租户另一个账号也用同名标记时，名称不足以归属，保持 unknown。
	others, err := i.resolver.accountRepo.List(ctx, repository.ChannelAccountListOptions{TenantID: tenantID})
	if err != nil {
		return out, err
	}
	matches := 0
	for _, other := range others {
		if strings.EqualFold(strings.TrimSpace(other.SelfIdentifyName), mark) {
			matches++
		}
	}
	if matches != 1 {
		return out, nil
	}
	out.Self = true
	out.Role = "self"
	out.Evidence = "sender_name"
	return out, nil
}

// IsUnknownIdentity 供业务闸使用：unknown 不触发猜测性自动回复。
func IsUnknownIdentity(identity *ChannelIdentity) bool {
	return identity == nil || strings.EqualFold(identity.Role, "unknown") || identity.SpeakerID == ""
}
