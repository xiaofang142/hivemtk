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
	// ErrChannelIdentityInvalid 已命中的主数据不完整或不一致，不能按未迁移回退。
	ErrChannelIdentityInvalid = errors.New("渠道身份主数据不一致")
	// ErrChannelIdentityLegacyUnmatched 表示仓储可用且查询成功，但该引用未命中
	// 新渠道账号主数据；只有这一种“明确未迁移”结果允许模块闸门兼容放行。
	ErrChannelIdentityLegacyUnmatched = errors.New("渠道账号未命中新主数据")
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
	out, _, err := r.resolveByIdentifyCode(ctx, code)
	return out, err
}

func (r *ChannelResolver) resolveByIdentifyCode(ctx context.Context, code string) (*ResolvedChannelAccount, *model.Channel, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, nil, ErrChannelIdentityUnresolved
	}
	if r == nil || r.accountRepo == nil || !r.accountRepo.Available() {
		return nil, nil, repository.ErrChannelStoreUnavailable
	}
	acc, err := r.accountRepo.GetByIdentifyCode(ctx, code)
	if err != nil {
		if errors.Is(err, repository.ErrAmbiguousIdentifyCode) {
			return nil, nil, ErrChannelIdentityAmbiguous
		}
		return nil, nil, err
	}
	if acc == nil {
		return nil, nil, ErrChannelIdentityUnresolved
	}
	return r.resolveAccountChannel(ctx, acc)
}

// resolveAccountChannel 对所有账号入口复用同一份渠道、租户及主键校验。
func (r *ChannelResolver) resolveAccountChannel(ctx context.Context, acc *model.ChannelAccount) (*ResolvedChannelAccount, *model.Channel, error) {
	if acc == nil || acc.ID == 0 || acc.TenantID == 0 || acc.ChannelID == 0 {
		return nil, nil, ErrChannelIdentityInvalid
	}
	if tid, bound := tenant.FromContext(ctx); bound && tid != 0 && tid != acc.TenantID {
		return nil, nil, ErrChannelIdentityAmbiguous
	}
	if r.channelRepo == nil || !r.channelRepo.Available() {
		return nil, nil, repository.ErrChannelStoreUnavailable
	}
	ch, err := r.channelRepo.GetByID(ctx, acc.TenantID, acc.ChannelID)
	if err != nil {
		return nil, nil, err
	}
	if ch == nil || ch.ID != acc.ChannelID || ch.TenantID != acc.TenantID || strings.TrimSpace(ch.Code) == "" {
		return nil, nil, ErrChannelIdentityInvalid
	}
	out := &ResolvedChannelAccount{
		TenantID:     acc.TenantID,
		AccountID:    acc.ID,
		ChannelID:    acc.ChannelID,
		ChannelCode:  ch.Code,
		IdentifyCode: acc.IdentifyCode,
	}
	return out, ch, nil
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
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			return nil, ErrChannelTenantUnresolved
		}
		tid = tenant.DefaultTenantID
	}
	ctx = tenant.WithTenantID(ctx, tid)
	platform = strings.ToLower(strings.TrimSpace(platform))
	channelMatches := func(ch *model.Channel) bool {
		if platform == "" {
			return true
		}
		return strings.EqualFold(strings.TrimSpace(ch.Code), platform) ||
			strings.EqualFold(strings.TrimSpace(ch.Type), platform)
	}
	resolved, ch, identifyErr := r.resolveByIdentifyCode(ctx, ref)
	if identifyErr == nil {
		if !channelMatches(ch) {
			return nil, ErrChannelIdentityUnresolved
		}
		return resolved, nil
	}
	if !errors.Is(identifyErr, ErrChannelIdentityUnresolved) {
		// 只有健康未命中才允许 ID/名称回退；已命中但不完整的身份与故障均拒绝。
		return nil, identifyErr
	}

	if numericID, parseErr := strconv.ParseUint(ref, 10, 64); parseErr == nil && numericID > 0 {
		acc, getErr := r.accountRepo.GetByID(ctx, tid, uint(numericID))
		if getErr != nil {
			return nil, getErr
		}
		if acc == nil {
			return nil, ErrChannelIdentityLegacyUnmatched
		}
		if acc.ID != uint(numericID) {
			return nil, ErrChannelIdentityInvalid
		}
		out, ch, matchErr := r.resolveAccountChannel(ctx, acc)
		if matchErr != nil {
			return nil, matchErr
		}
		if !channelMatches(ch) {
			return nil, ErrChannelIdentityUnresolved
		}
		return out, nil
	}

	rows, err := listChannelAccounts(ctx, r.accountRepo, repository.ChannelAccountListOptions{
		TenantID: tid, Keyword: ref, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	// 固定上限的结果不能证明没有匹配，也不能证明当前匹配唯一。
	if len(rows) >= 50 {
		return nil, ErrChannelIdentityUnresolved
	}
	var identityMatches []*model.ChannelAccount
	for i := range rows {
		acc := &rows[i]
		if !strings.EqualFold(strings.TrimSpace(acc.Name), ref) &&
			!strings.EqualFold(strings.TrimSpace(acc.IdentifyCode), ref) {
			continue
		}
		identityMatches = append(identityMatches, acc)
	}
	if len(identityMatches) == 0 {
		return nil, ErrChannelIdentityLegacyUnmatched
	}
	var matches []*ResolvedChannelAccount
	for _, acc := range identityMatches {
		out, ch, matchErr := r.resolveAccountChannel(ctx, acc)
		if matchErr != nil {
			return nil, matchErr
		}
		if channelMatches(ch) {
			matches = append(matches, out)
		}
	}
	if len(matches) == 0 {
		return nil, ErrChannelIdentityUnresolved
	}
	if len(matches) > 1 {
		return nil, ErrChannelIdentityAmbiguous
	}
	return matches[0], nil
}

// ResolveTenant 渠道回调租户解析入口。webhook.ResolveTenantByChannelAccount 统一委托本方法。
func (r *ChannelResolver) ResolveTenant(ctx context.Context, channel WebhookChannel, accountID string) (uint, error) {
	if r == nil {
		return 0, ErrChannelTenantUnresolved
	}
	// ① 识别码优先：命中后必须复核渠道码匹配，不能只换 tenant 后继续用路径账号落库。
	if accountID != "" {
		resolved, err := r.ResolveByIdentifyCode(ctx, accountID)
		if err == nil && resolved != nil {
			if ctxTid, bound := tenant.FromContext(ctx); bound && ctxTid != 0 && ctxTid != resolved.TenantID {
				return 0, ErrChannelIdentityAmbiguous // 可信 ctx 与识别码冲突，拒绝
			}
			if resolved.ChannelCode != "" && !channelMatchesWebhookCode(channel, resolved.ChannelCode) {
				return 0, ErrChannelIdentityAmbiguous // 识别码渠道与请求渠道冲突
			}
			return resolved.TenantID, nil
		}
		if err != nil && !errors.Is(err, ErrChannelIdentityUnresolved) {
			return 0, err
		}
	}
	// ② 已绑定租户仅在识别码健康未命中时作为 legacy 自证输入；path/query 冲突由
	//    ResolveTrusted 统一复核。这里只返回租户，供旧 webhook 链继续工作。
	if tid, bound := tenant.FromContext(ctx); bound && tid != 0 {
		return tid, nil
	}
	return r.resolveTenantLegacy(ctx, channel, accountID)
}

// channelMatchesWebhookCode 比对 Webhook 渠道枚举与规范渠道码。
// 旧平台别名（如 tg→telegram、wx→wechat）显式映射，未知组合一律拒绝。
func channelMatchesWebhookCode(c WebhookChannel, code string) bool {
	got := strings.ToLower(strings.TrimSpace(string(c)))
	want := strings.ToLower(strings.TrimSpace(code))
	if got == want {
		return true
	}
	aliases := map[string]string{
		"telegram": "telegram", "tg": "telegram",
		"qq": "qq", "feishu": "feishu", "wecom": "wecom", "whatsapp": "whatsapp",
	}
	a, okA := aliases[got]
	b, okB := aliases[want]
	return okA && okB && a == b
}

// ResolveTrusted 统一可信账号身份（CH-07）。
//
// 识别码 + 平台账号映射 + 可信 ctx 联合校验后生成唯一身份：
//   - 识别码必须能反查到单一账号，且渠道码与请求 platform 一致；
//   - ctx 已绑定租户时，必须与识别码归属相同（冲突拒绝，不取首条/不换号）；
//   - accountRef 若显式提供（路径/query/header），必须指向同一账号 ID；
//   - 账号停用/渠道关闭一律拒绝（消费 CH-05 状态契约）。
func (r *ChannelResolver) ResolveTrusted(ctx context.Context, identifyCode, platform, accountRef string) (*ResolvedChannelAccount, error) {
	if r == nil || r.accountRepo == nil || strings.TrimSpace(identifyCode) == "" {
		return nil, ErrChannelIdentityUnresolved
	}
	resolved, ch, err := r.resolveByIdentifyCode(ctx, identifyCode)
	if err != nil {
		if errors.Is(err, repository.ErrAmbiguousIdentifyCode) {
			return nil, ErrChannelIdentityAmbiguous
		}
		return nil, err
	}
	if resolved == nil || resolved.TenantID == 0 || resolved.AccountID == 0 {
		return nil, ErrChannelIdentityUnresolved
	}
	if ctxTid, bound := tenant.FromContext(ctx); bound && ctxTid != 0 && ctxTid != resolved.TenantID {
		return nil, ErrChannelIdentityAmbiguous
	}
	if strings.TrimSpace(platform) != "" && resolved.ChannelCode != "" && !channelMatchesWebhookCode(WebhookChannel(platform), resolved.ChannelCode) {
		return nil, ErrChannelIdentityAmbiguous
	}
	// 显式账号引用必须指向同一规范账号（数值 ID 或外部别名解析后复核）。
	if ref := strings.TrimSpace(accountRef); ref != "" {
		if id, parseErr := strconv.ParseUint(ref, 10, 64); parseErr == nil {
			if uint(id) != resolved.AccountID {
				return nil, ErrChannelIdentityAmbiguous
			}
		} else if ref != resolved.IdentifyCode {
			other, oerr := r.ResolveByAccountRef(tenant.WithTenantID(ctx, resolved.TenantID), platform, ref)
			if oerr != nil || other == nil || other.AccountID != resolved.AccountID {
				return nil, ErrChannelIdentityAmbiguous
			}
		}
	}
	// 渠道/账号状态：fail-closed（不重建 CH-05 服务，直接查仓储行状态）。
	if !ch.Enabled {
		return nil, ErrChannelDisabled
	}
	acc, accErr := r.accountRepo.GetByID(ctx, resolved.TenantID, resolved.AccountID)
	if accErr != nil {
		return nil, accErr
	}
	if acc == nil || acc.Status != 1 {
		return nil, ErrChannelAccountDisabled
	}
	if acc.ID != resolved.AccountID || acc.TenantID != resolved.TenantID || acc.ChannelID != resolved.ChannelID {
		return nil, ErrChannelIdentityInvalid
	}
	return resolved, nil
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
		// 单聊 self 只认平台稳定用户 ID（selfPlatformID）与明确成员角色；
		// 识别码是认证归属凭证、显示名可变、数据库账号 ID 是内部键，都不能当真人身份。
		role := strings.ToLower(strings.TrimSpace(memberRole))
		if isSelfRole(role) {
			out.Self = true
			out.Role = role
		} else if role == "customer" || role == "member" || role == "guest" {
			out.Role = role
		} else {
			out.Role = "unknown" // 缺稳定身份/角色：unknown，不猜 self
		}
	case "group", "group_chat", "chatroom":
		role := strings.ToLower(strings.TrimSpace(memberRole))
		switch {
		case isSelfRole(role):
			out.Self = true
			out.Role = role
		case role == "member" || role == "customer" || role == "guest":
			out.Role = role
		default:
			out.Role = "unknown" // 同名/未知成员：不猜，不触发猜测自动回复
		}
	default:
		return nil, ErrChannelIdentityUnresolved
	}
	return out, nil
}

// isSelfRole 明确表示"自己/员工/机器人自己"的成员角色集合。
func isSelfRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "owner", "self", "agent", "operator", "staff", "human":
		return true
	default:
		return false
	}
}
