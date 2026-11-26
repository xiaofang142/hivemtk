package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/edition"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

// 桥接扩展只带一枚实例级凭证（X-Bridge-Token，判据见 middleware.BridgeIngressGuard）：
// 它证明"这台机器允许上报"，不证明"这一条会话属于哪个租户"。on 模式下归属只能由
// 上报的 (channel, account_id) 自证（specs/001-saas-multi-tenant T023）：
//
//	① bridge_accounts 里该账号行只属于一个租户 ⇒ 绑它（日常流量的唯一形态）；
//	② 账号还没注册过（首次上报）或同一 (channel, account_id) 在两个租户各有一行
//	   ⇒ 只认显式 tenant_code(query) / X-Tenant-Code，由短码解析；
//	③ 两条线索都没有 ⇒ 拒绝。
//
// 判不清就拒绝，而不是"不绑定继续跑"：bridge_accounts、message_hub、conversations
// 全是租户表，无绑定时写入被隔离 callback 判错，而账号注册那条是异步的、错误只记
// 一行 "non-fatal" 日志，ingest 照样回 200 —— 扩展看见成功，客户消息一条都没落库。
// 在入口一次判清，换来一条能照着改的 4xx。
//
// off 模式整段跳过并保持"不绑定"：隔离 callback 在 off 下直通，既有行为逐字节等价。

// 桥接入口的租户判定失败形态，各自对应一条可扩展的处置动作。
var (
	// ErrBridgeTenantUnresolved 账号还没注册过且没带 tenant_code ⇒ 首次注册缺归属
	ErrBridgeTenantUnresolved = errors.New("该渠道账号尚未归属任何租户，首次上报请携带 tenant_code")
	// ErrBridgeTenantAmbiguous 同一 (channel, account_id) 在多个租户各有一行
	ErrBridgeTenantAmbiguous = errors.New("该渠道账号同时存在于多个租户，请携带 tenant_code 指明归属")
	// ErrBridgeTenantCodeInvalid 带了 tenant_code 但解析不到有效租户
	ErrBridgeTenantCodeInvalid = errors.New("tenant_code 无效")
)

// BridgeResolvedIdentityKey gin context key：identify_code 解析出的统一身份。
const BridgeResolvedIdentityKey = "bridge_resolved_identity"

// BridgeResolvedIdentity 统一 identify_code 身份（路由 1 只保留 identify_code）。
type BridgeResolvedIdentity struct {
	TenantID     uint
	Channel      string
	AccountID    string
	IdentifyCode string
}

// bridgeResolvedTenantCtx 在已由识别码证明租户后派生内部读取 ctx。OnlyTenant 会剥离
// 其它 value，因此显式保留 edition override，避免国内渠道在 repo gate 中被误判不存在。
func bridgeResolvedTenantCtx(rctx context.Context, tenantID uint) context.Context {
	ctx := tenant.OnlyTenant(rctx, tenantID)
	if version, ok := edition.FromContext(rctx); ok {
		ctx = edition.WithContext(ctx, version)
	}
	return ctx
}

func bridgeIdentifyHintFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	code := strings.TrimSpace(r.Header.Get("X-Channel-Identify-Code"))
	if code == "" {
		code = strings.TrimSpace(r.URL.Query().Get("identify_code"))
	}
	return code
}

func setBridgeIdentity(c *gin.Context, id *BridgeResolvedIdentity) {
	if c != nil && id != nil {
		c.Set(BridgeResolvedIdentityKey, id)
	}
}

// BridgeIdentityFromContext 读取 identify_code 解析出的身份。
func BridgeIdentityFromContext(c *gin.Context) (*BridgeResolvedIdentity, bool) {
	if c == nil {
		return nil, false
	}
	v, ok := c.Get(BridgeResolvedIdentityKey)
	if !ok || v == nil {
		return nil, false
	}
	id, ok := v.(*BridgeResolvedIdentity)
	return id, ok && id != nil
}

// bridgeTenantHintFromRequest 读显式租户短码（query 优先，其次请求头）。
//
// 只从 *http.Request 读：gin 与 channelgw 的 WS 握手都拿得到它，
// 短码的两个存放位置因此只有一处定义。
func bridgeTenantHintFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	code := strings.TrimSpace(r.URL.Query().Get("tenant_code"))
	if code == "" {
		code = strings.TrimSpace(r.Header.Get("X-Tenant-Code"))
	}
	return code
}

// resolveBridgeTenant 纯解析：只吃 ctx，不写 ResponseWriter、不改 Request，
// HTTP 入口与 WS 传输共用同一份判定。
//
// changed=false 表示"没有新绑定"（off 模式直通、或上游中间件已经绑好），
// 此时调用方保留原 ctx 即可，不该去改写 Request。
// resolveBridgeIdentity 仅接受 identify_code（决策1：只保留 identify_code）。
func resolveBridgeIdentity(rctx context.Context, channel, accountID, _codeHint, identifyCode string) (context.Context, bool, *BridgeResolvedIdentity, error) {
	identifyCode = strings.TrimSpace(identifyCode)
	if identifyCode == "" {
		// 上游（bridge 入站中间件 / WS 握手）已显式钉死租户时直通：
		// 这是与识别码、租户短码并列的合法归属来源（旧 resolveBridgeTenant 即如此）。
		// 仅认非零的真实租户；system-scope / 未绑定仍 fail-closed。
		if tid, bound := tenant.FromContext(rctx); bound && tid != 0 && !tenant.IsSystemScope(rctx) {
			return rctx, false, nil, nil
		}
		return rctx, false, nil, ErrBridgeTenantUnresolved
	}

	resolved, err := service.NewChannelResolver().ResolveByIdentifyCode(rctx, identifyCode)
	if err != nil && !errors.Is(err, service.ErrChannelIdentityUnresolved) {
		if errors.Is(err, service.ErrChannelIdentityAmbiguous) {
			return rctx, false, nil, ErrBridgeTenantAmbiguous
		}
		if errors.Is(err, service.ErrChannelIdentityInvalid) {
			return rctx, false, nil, ErrBridgeTenantUnresolved
		}
		return rctx, false, nil, err
	}
	if err != nil || resolved == nil || resolved.TenantID == 0 {
		// Fallback: channel-level identify code (channel_identify_codes table).
		resolved, err = resolveBridgeIdentityByChannelCode(rctx, identifyCode, accountID)
		if err != nil {
			return rctx, false, nil, err
		}
		if resolved == nil || resolved.TenantID == 0 {
			return rctx, false, nil, ErrBridgeTenantUnresolved
		}
	}
	// ChannelResolver 的 channel-level fallback 可能在未绑定 rctx 下读不到 ChannelCode。
	// 身份记录已证明 tenant；这里只用内部 child ctx 补读，不改写原 rctx/request。
	if resolved.ChannelCode == "" && resolved.ChannelID > 0 {
		ch, chErr := repository.NewChannelRepository().GetByID(
			bridgeResolvedTenantCtx(rctx, resolved.TenantID), resolved.TenantID, resolved.ChannelID,
		)
		if chErr != nil {
			return rctx, false, nil, chErr
		}
		if ch != nil {
			resolved.ChannelCode = ch.Code
		}
	}

	resolvedChannel := NormalizeBridgeChannel(resolved.ChannelCode)
	resolvedAccountID := ""
	if resolved.AccountID > 0 {
		resolvedAccountID = strconv.FormatUint(uint64(resolved.AccountID), 10)
	}
	if resolvedChannel == "" && resolved.ChannelID > 0 {
		resolvedChannel = NormalizeBridgeChannel(fmt.Sprintf("channel-%d", resolved.ChannelID))
	}
	if resolvedChannel == "sagex" || resolvedChannel == fmt.Sprintf("sagex-t%d", resolved.TenantID) {
		// 历史 SageX 绑定只证明租户和账号；同一服务会上报 QQ、个人微信。
		// 必须使用每条消息的真实平台，不能把服务编码写成渠道。
		switch NormalizeBridgeChannel(channel) {
		case model.ChannelQQ:
			resolvedChannel = model.ChannelQQ
		case model.ChannelPersonalWX:
			resolvedChannel = model.ChannelPersonalWX
		default:
			return rctx, false, nil, ErrBridgeTenantUnresolved
		}
	} else if channel != "" {
		normReq := NormalizeBridgeChannel(channel)
		if normReq != resolvedChannel && channel != strconv.FormatUint(uint64(resolved.ChannelID), 10) {
			// identify_code is authoritative; request channel may be a client hint (e.g. wechat/qq).
			// Keep resolved channel to avoid misrouting, but don't reject a valid identify_code.
			if resolvedChannel == "" {
				resolvedChannel = normReq
			}
		}
	}
	if accountID != "" {
		// 桥接扩展上报的 account_id 是第三方平台账号/会话标识（如小红书用户 ID），
		// 不是本系统 channel_accounts 行 ID。identify_code 已解析出内部账号时，
		// 外部标识只作为客户端提示，不能覆盖内部账号，也不能因格式不同判歧义。
		// 仅当调用方显式传了本系统行 ID（纯数字）且与识别码结果冲突时才拒绝。
		if _, numeric := strconv.ParseUint(accountID, 10, 64); numeric == nil && resolvedAccountID != "" && accountID != resolvedAccountID {
			return rctx, false, nil, ErrBridgeTenantAmbiguous
		}
		if resolvedAccountID == "" {
			resolvedAccountID = accountID
		}
	}
	id := &BridgeResolvedIdentity{TenantID: resolved.TenantID, Channel: resolvedChannel, AccountID: resolvedAccountID, IdentifyCode: resolved.IdentifyCode}
	if !tenant.IsMultiTenant() {
		return rctx, false, id, nil
	}
	if tid, bound := tenant.FromContext(rctx); bound && tid != 0 {
		if tid != resolved.TenantID {
			return rctx, false, nil, ErrBridgeTenantAmbiguous
		}
		return rctx, false, id, nil
	}
	return bridgeResolvedTenantCtx(rctx, resolved.TenantID), true, id, nil
}

func resolveBridgeIdentityByChannelCode(rctx context.Context, identifyCode, accountID string) (*service.ResolvedChannelAccount, error) {
	codeRepo := repository.NewChannelIdentifyCodeRepository()
	rec, err := codeRepo.GetByCode(rctx, identifyCode)
	if err != nil {
		return nil, err
	}
	if rec == nil || rec.TenantID == 0 || rec.ChannelID == 0 {
		return nil, ErrBridgeTenantUnresolved
	}
	out := &service.ResolvedChannelAccount{
		TenantID:     rec.TenantID,
		ChannelID:    rec.ChannelID,
		IdentifyCode: rec.Code,
	}
	// 识别码记录已经证明租户归属；subordinate reads 必须在这个已解析租户的
	// 内部 child ctx 上执行，但不能改写调用方原 rctx/request。off 模式仍只解析身份。
	tenantCtx := bridgeResolvedTenantCtx(rctx, rec.TenantID)
	if chRepo := repository.NewChannelRepository(); chRepo.Available() {
		ch, chErr := chRepo.GetByID(tenantCtx, rec.TenantID, rec.ChannelID)
		if chErr != nil {
			return nil, chErr
		}
		if ch != nil {
			out.ChannelCode = ch.Code
		}
	}
	// CH-07：渠道码只证明 tenant+channel，账号必须显式消歧；多账号时禁止取首条 fallback。
	rows, listErr := service.NewChannelAccountService().ListAccounts(tenantCtx, repository.ChannelAccountListOptions{
		TenantID: rec.TenantID, ChannelID: rec.ChannelID, Limit: 20,
	})
	if listErr != nil {
		return nil, listErr
	}
	if len(rows) == 0 {
		return nil, ErrBridgeTenantUnresolved
	}
	// 显式账号：必须命中该渠道下的账号。
	if strings.TrimSpace(accountID) != "" {
		for i := range rows {
			if strconv.FormatUint(uint64(rows[i].ID), 10) == strings.TrimSpace(accountID) {
				out.AccountID = rows[i].ID
				break
			}
		}
		if out.AccountID == 0 {
			return nil, ErrBridgeTenantUnresolved
		}
	} else if len(rows) == 1 {
		out.AccountID = rows[0].ID
	} else {
		// 多账号且未提供消歧账号：拒绝，避免取首条。
		return nil, ErrBridgeTenantUnresolved
	}
	return out, nil
}

// bindBridgeTenant 解析 (channel, accountID) 的归属租户并绑定进请求上下文。
//
// 返回的 ctx 与就地改写后的 c.Request 携带同一个租户：handler 里既有"用返回值"的
// 链路，也有直接取 c.Request.Context() 的链路（异步账号注册、SSE 心跳刷新），
// 两处必须看到同一个租户，否则会出现"消息落进 A、在线位刷到 B"。
//
// channel 必须是 NormalizeBridgeChannel 之后的规范名：bridge_accounts 只存规范渠道，
// 别名（douyin_web）会让自证查不到行，把已注册的账号误判成首次注册。
func bindBridgeTenant(c *gin.Context, channel, accountID string) (context.Context, error) {
	rctx := c.Request.Context()
	identifyCode := bridgeIdentifyHintFromRequest(c.Request)
	bound, changed, identity, err := resolveBridgeIdentity(rctx, channel, accountID, bridgeTenantHintFromRequest(c.Request), identifyCode)
	if err != nil {
		return rctx, err
	}
	if changed {
		c.Request = c.Request.WithContext(bound)
	}
	if identity != nil {
		setBridgeIdentity(c, identity)
	}
	return bound, nil
}

// BridgeTenantForRequest 是渠道网关 WS 传输的租户绑定入口（由 router 注入，
// 判据见 channelgw.WSTransport.TenantBinder）。
//
// WS 的 channel/account_id 来自 register 帧而不是 URL 路径，所以自证只能在读到
// register 帧之后做；首次注册（库里还没有账号行）的租户短码走握手 URL：
// `?tenant_code=xxx` 或握手请求头 `X-Tenant-Code`。
func BridgeTenantForRequest(r *http.Request, channel, accountID string) (context.Context, error) {
	rctx := r.Context()
	identifyCode := bridgeIdentifyHintFromRequest(r)
	ctx2, _, identity, err := resolveBridgeIdentity(rctx, channel, accountID, bridgeTenantHintFromRequest(r), identifyCode)
	if err != nil {
		return rctx, err
	}
	if identity != nil && identity.TenantID > 0 {
		return ctx2, nil
	}
	return ctx2, nil
}

// bridgeTenantHTTPStatus 租户判定失败 → 状态码：404 是"照着提示补 tenant_code 就能重试"，
// 500 是反查自身故障（重试帮不上忙，需要人看日志）。
func bridgeTenantHTTPStatus(err error) int {
	switch {
	case errors.Is(err, ErrBridgeTenantUnresolved),
		errors.Is(err, ErrBridgeTenantAmbiguous),
		errors.Is(err, ErrBridgeTenantCodeInvalid):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

// writeBridgeTenantError 统一出口（outbox / ack / sse 三个入口共用同一份错误体形状）。
func writeBridgeTenantError(c *gin.Context, err error) {
	status := bridgeTenantHTTPStatus(err)
	reason := err.Error()
	if status != http.StatusNotFound {
		reason = "tenant lookup failed: " + err.Error()
	}
	c.JSON(status, gin.H{"status": "error", "code": "tenant_unresolved", "message": reason})
}
