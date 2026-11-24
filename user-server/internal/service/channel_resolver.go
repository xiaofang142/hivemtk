package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/dbstate"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var (
	// ErrChannelTenantUnresolved 渠道账号无法归属任何租户（含歧义）：控制器按 404 拒绝。
	ErrChannelTenantUnresolved = errors.New("渠道账号未归属任何租户")
	// ErrChannelIdentityAmbiguous 识别码/账号归属多条，fail-closed 拒绝。
	ErrChannelIdentityAmbiguous = errors.New("渠道身份歧义，拒绝解析")
	// ErrChannelIdentityUnresolved 身份信息不足，无法自证。
	ErrChannelIdentityUnresolved = errors.New("渠道身份无法自证")
)

// ResolvedChannelAccount 识别码解析结果（租户 + 账号 + 渠道）。
type ResolvedChannelAccount struct {
	TenantID     uint
	AccountID    uint
	ChannelID    uint
	ChannelCode  string
	IdentifyCode string
}

// ChannelIdentity 统一自他识别结果。
// 单聊按双方账号自证；群聊按成员角色判定，未知角色一律 unknown（不猜 self）。
type ChannelIdentity struct {
	TenantID         uint
	ChannelAccountID uint
	ConversationType string
	SpeakerID        string
	Self             bool
	Role             string
}

// ChannelResolver 渠道统一解析器（FR-028/030）。
//
// 解析优先级：① 识别码（全局唯一）→ ② 渠道账号表按 id 自证 →
// ③ integration_accounts 按平台名自证 → ④ 无 account_id 时全表单租户。
// 任何一步歧义/失败都返回错误，**绝不回落默认租户**。
type ChannelResolver struct {
	accountRepo repository.ChannelAccountRepository
	channelRepo repository.ChannelRepository
}

// NewChannelResolver 创建解析器（全局 DB 装配）。
func NewChannelResolver() *ChannelResolver {
	return &ChannelResolver{
		accountRepo: repository.NewChannelAccountRepository(),
		channelRepo: repository.NewChannelRepository(),
	}
}

// NewChannelResolverWithRepos 用显式仓储构造（测试用）。
func NewChannelResolverWithRepos(acc repository.ChannelAccountRepository, ch repository.ChannelRepository) *ChannelResolver {
	return &ChannelResolver{accountRepo: acc, channelRepo: ch}
}

// ResolveByIdentifyCode 按识别码解析租户/账号。
func (r *ChannelResolver) ResolveByIdentifyCode(ctx context.Context, code string) (*ResolvedChannelAccount, error) {
	if r == nil || r.accountRepo == nil {
		return nil, ErrChannelIdentityUnresolved
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, ErrChannelIdentityUnresolved
	}
	acc, err := r.accountRepo.GetByIdentifyCode(ctx, code)
	if err != nil {
		if errors.Is(err, repository.ErrAmbiguousIdentifyCode) {
			return nil, ErrChannelIdentityAmbiguous
		}
		return nil, err
	}
	if acc == nil || acc.TenantID == 0 || acc.ID == 0 {
		return nil, ErrChannelIdentityUnresolved
	}
	out := &ResolvedChannelAccount{
		TenantID:     acc.TenantID,
		AccountID:    acc.ID,
		ChannelID:    acc.ChannelID,
		IdentifyCode: acc.IdentifyCode,
	}
	if r.channelRepo != nil && r.channelRepo.Available() {
		if ch, err := r.channelRepo.GetByID(ctx, acc.TenantID, acc.ChannelID); err == nil && ch != nil {
			out.ChannelCode = ch.Code
		}
	}
	return out, nil
}

// ResolveByAccountRef 把上报消息里的账号标识解析为渠道账号数值 ID。
//
// 识别码（cid_...）优先；其次数值账号 id；最后按渠道账号表的 name/identify_code
// 精确匹配外部账号名（bridge/webhook 真实上报多为外部名）。平台约束按渠道
// Code 或 Type，歧义一律 fail-closed，绝不挑第一条或回落默认账号。
func (r *ChannelResolver) ResolveByAccountRef(ctx context.Context, platform, ref string) (*ResolvedChannelAccount, error) {
	ref = strings.TrimSpace(ref)
	if r == nil || r.accountRepo == nil || ref == "" {
		return nil, ErrChannelIdentityUnresolved
	}
	if resolved, err := r.ResolveByIdentifyCode(ctx, ref); err == nil && resolved != nil {
		return resolved, nil
	}

	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, ErrChannelIdentityUnresolved
		}
		tid = tenant.DefaultTenantID
	}
	platform = strings.ToLower(strings.TrimSpace(platform))
	resolveOut := func(acc *model.ChannelAccount) *ResolvedChannelAccount {
		out := &ResolvedChannelAccount{
			TenantID: acc.TenantID, AccountID: acc.ID, ChannelID: acc.ChannelID,
			ChannelCode: platform, IdentifyCode: acc.IdentifyCode,
		}
		if r.channelRepo != nil && r.channelRepo.Available() {
			if ch, chErr := r.channelRepo.GetByID(ctx, acc.TenantID, acc.ChannelID); chErr == nil && ch != nil {
				out.ChannelCode = ch.Code
			}
		}
		return out
	}
	channelMatches := func(acc *model.ChannelAccount) bool {
		if platform == "" || r.channelRepo == nil || !r.channelRepo.Available() {
			return true
		}
		ch, chErr := r.channelRepo.GetByID(ctx, acc.TenantID, acc.ChannelID)
		return chErr == nil && ch != nil &&
			(strings.EqualFold(strings.TrimSpace(ch.Code), platform) ||
				strings.EqualFold(strings.TrimSpace(ch.Type), platform))
	}

	if numericID, parseErr := strconv.ParseUint(ref, 10, 64); parseErr == nil && numericID > 0 {
		acc, getErr := r.accountRepo.GetByID(ctx, tid, uint(numericID))
		if getErr != nil {
			return nil, getErr
		}
		if acc == nil || !channelMatches(acc) {
			return nil, ErrChannelIdentityUnresolved
		}
		return resolveOut(acc), nil
	}

	rows, err := r.accountRepo.List(ctx, repository.ChannelAccountListOptions{
		TenantID: tid, Keyword: ref, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	var matches []*model.ChannelAccount
	for i := range rows {
		acc := &rows[i]
		if (!strings.EqualFold(strings.TrimSpace(acc.Name), ref) &&
			!strings.EqualFold(strings.TrimSpace(acc.IdentifyCode), ref)) || !channelMatches(acc) {
			continue
		}
		matches = append(matches, acc)
	}
	if len(matches) == 0 {
		return nil, ErrChannelIdentityUnresolved
	}
	if len(matches) > 1 {
		return nil, ErrChannelIdentityAmbiguous
	}
	return resolveOut(matches[0]), nil
}

// ResolveTenant 渠道回调租户解析入口。webhook.ResolveTenantByChannelAccount 统一委托本方法。
func (r *ChannelResolver) ResolveTenant(ctx context.Context, channel WebhookChannel, accountID string) (uint, error) {
	if r == nil {
		return 0, ErrChannelTenantUnresolved
	}
	if tid, bound := tenant.FromContext(ctx); bound && tid != 0 {
		return tid, nil
	}

	// ① 识别码优先：命中歧义直接拒绝，不能退回旧逻辑"猜"一个租户。
	if accountID != "" {
		resolved, err := r.ResolveByIdentifyCode(ctx, accountID)
		if err == nil {
			return resolved.TenantID, nil
		}
		if errors.Is(err, ErrChannelIdentityAmbiguous) {
			return 0, err
		}
	}
	return r.resolveTenantLegacy(ctx, channel, accountID)
}

// resolveTenantLegacy 保留 webhook 历史自证链（表主键全库唯一 + 歧义拒绝），
// 使老账号 id / 无专用表的渠道在识别码生效前不丢消息。
func (r *ChannelResolver) resolveTenantLegacy(ctx context.Context, channel WebhookChannel, accountID string) (uint, error) {
	gdb := dbstate.Get()
	if gdb == nil {
		return 0, fmt.Errorf("db 未初始化")
	}
	table, hasTable := webhookAccountTableByChannel[channel]
	if hasTable && accountID != "" {
		if id, err := strconv.ParseUint(accountID, 10, 32); err == nil && id > 0 {
			tid, err := tenant.SingleTenantByKey(ctx, gdb, table, "id = ?", uint(id))
			if err != nil {
				return 0, err
			}
			if tid != 0 {
				return tid, nil
			}
		}
	}
	tid, err := tenant.SingleTenantByKey(ctx, gdb, "integration_accounts", "platform = ?", string(channel))
	if err != nil {
		return 0, err
	}
	if tid != 0 {
		return tid, nil
	}
	if accountID == "" && hasTable {
		tid, err = tenant.SingleTenantByKey(ctx, gdb, table, "id > ?", 0)
		if err != nil {
			return 0, err
		}
		if tid != 0 {
			return tid, nil
		}
	}
	return 0, fmt.Errorf("%w: channel=%s account=%s", ErrChannelTenantUnresolved, channel, accountID)
}

// Identify 统一自他识别。
// conversationType: dm/single/direct/private 走单聊双方账号；group/group_chat/chatroom
// 走成员角色；未知类型 fail-closed 返回 ErrChannelIdentityUnresolved。
func (r *ChannelResolver) Identify(ctx context.Context, tenantID, accountID uint, conversationType, speakerID, memberRole string) (*ChannelIdentity, error) {
	if r == nil || r.accountRepo == nil || tenantID == 0 || accountID == 0 {
		return nil, ErrChannelIdentityUnresolved
	}
	acc, err := r.accountRepo.GetByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil {
		return nil, ErrChannelIdentityUnresolved
	}
	typ := strings.ToLower(strings.TrimSpace(conversationType))
	if typ == "" {
		typ = strings.ToLower(strings.TrimSpace(acc.Type))
	}
	out := &ChannelIdentity{
		TenantID:         tenantID,
		ChannelAccountID: accountID,
		ConversationType: typ,
		SpeakerID:        strings.TrimSpace(speakerID),
		Role:             "unknown",
	}
	switch typ {
	case "dm", "single", "direct", "private":
		selfIDs := map[string]bool{
			acc.IdentifyCode:                       acc.IdentifyCode != "",
			acc.SelfIdentifyName:                   acc.SelfIdentifyName != "",
			strconv.FormatUint(uint64(acc.ID), 10): true,
		}
		if out.SpeakerID != "" && selfIDs[out.SpeakerID] {
			out.Self = true
			out.Role = "owner"
		} else {
			out.Role = "customer"
		}
	case "group", "group_chat", "chatroom":
		role := strings.ToLower(strings.TrimSpace(memberRole))
		switch role {
		case "owner", "self", "agent", "operator":
			out.Self = true
			out.Role = role
		case "member", "customer", "guest":
			out.Role = role
		default:
			out.Role = "unknown"
		}
	default:
		return nil, ErrChannelIdentityUnresolved
	}
	return out, nil
}
