package bridge

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/dbstate"
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

func bridgeIdentifyHintFromRequest(r *http.Request) string {
	if r == nil { return "" }
	code := strings.TrimSpace(r.Header.Get("X-Channel-Identify-Code"))
	if code == "" { code = strings.TrimSpace(r.URL.Query().Get("identify_code")) }
	return code
}

func setBridgeIdentity(c *gin.Context, id *BridgeResolvedIdentity) {
	if c != nil && id != nil { c.Set(BridgeResolvedIdentityKey, id) }
}

// BridgeIdentityFromContext 读取 identify_code 解析出的身份。
func BridgeIdentityFromContext(c *gin.Context) (*BridgeResolvedIdentity, bool) {
	if c == nil { return nil, false }
	v, ok := c.Get(BridgeResolvedIdentityKey)
	if !ok || v == nil { return nil, false }
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
		return rctx, false, nil, ErrBridgeTenantUnresolved
	}

	resolved, err := service.NewChannelResolver().ResolveByIdentifyCode(rctx, identifyCode)
	if err != nil || resolved == nil || resolved.TenantID == 0 {
		// Fallback: channel-level identify code (channel_identify_codes table).
		resolved, err = resolveBridgeIdentityByChannelCode(rctx, identifyCode)
		if err != nil || resolved == nil || resolved.TenantID == 0 {
			return rctx, false, nil, ErrBridgeTenantUnresolved
		}
	}

	resolvedChannel := NormalizeBridgeChannel(resolved.ChannelCode)
	resolvedAccountID := ""
	if resolved.AccountID > 0 {
		resolvedAccountID = strconv.FormatUint(uint64(resolved.AccountID), 10)
	}
	if channel != "" && NormalizeBridgeChannel(channel) != resolvedChannel {
		return rctx, false, nil, ErrBridgeTenantAmbiguous
	}
	if accountID != "" && resolvedAccountID != "" && accountID != resolvedAccountID {
		return rctx, false, nil, ErrBridgeTenantAmbiguous
	}
	if accountID != "" {
		resolvedAccountID = accountID
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
	return tenant.OnlyTenant(rctx, resolved.TenantID), true, id, nil
}

func resolveBridgeIdentityByChannelCode(rctx context.Context, identifyCode string) (*service.ResolvedChannelAccount, error) {
	codeRepo := repository.NewChannelIdentifyCodeRepository()
	rec, err := codeRepo.GetByCode(rctx, identifyCode)
	if err != nil || rec == nil || rec.TenantID == 0 || rec.ChannelID == 0 {
		return nil, ErrBridgeTenantUnresolved
	}
	out := &service.ResolvedChannelAccount{
		TenantID:     rec.TenantID,
		ChannelID:    rec.ChannelID,
		IdentifyCode: rec.Code,
	}
	if chRepo := repository.NewChannelRepository(); chRepo.Available() {
		if ch, chErr := chRepo.GetByID(rctx, rec.TenantID, rec.ChannelID); chErr == nil && ch != nil {
			out.ChannelCode = ch.Code
		}
	}
	// Pick an active account for this channel if present.
	accRepo := repository.NewChannelAccountRepository()
	if accRepo != nil {
		rows, listErr := accRepo.List(rctx, repository.ChannelAccountListOptions{
			TenantID: rec.TenantID, ChannelID: rec.ChannelID, Limit: 20,
		})
		if listErr == nil {
			for i := range rows {
				if rows[i].Status == 1 {
					out.AccountID = rows[i].ID
					break
				}
			}
			if out.AccountID == 0 && len(rows) > 0 {
				out.AccountID = rows[0].ID
			}
		}
	}
	return out, nil
}

func resolveBridgeTenant(rctx context.Context, channel, accountID, codeHint string) (context.Context, bool, error) {
	if !tenant.IsMultiTenant() {
		return rctx, false, nil
	}
	if id, bound := tenant.FromContext(rctx); bound && id != 0 {
		return rctx, false, nil
	}

	tid, lookupErr := bridgeAccountTenant(rctx, channel, accountID)
	if lookupErr == nil && tid != 0 {
		// OnlyTenant 而非 WithTenantID：归属由账号行/短码自证后必须钉死，
		// 上游若带 SystemScope，隔离 callback 会放行跨租户读并跳过写入填列。
		return tenant.OnlyTenant(rctx, tid), true, nil
	}
	if lookupErr != nil && !errors.Is(lookupErr, tenant.ErrAmbiguousTenantKey) {
		// 反查本身坏了：不能当成"没有归属"放行，也不能瞎猜一个租户。
		return rctx, false, lookupErr
	}
	if codeHint == "" {
		if errors.Is(lookupErr, tenant.ErrAmbiguousTenantKey) {
			return rctx, false, ErrBridgeTenantAmbiguous
		}
		return rctx, false, ErrBridgeTenantUnresolved
	}
	id, err := service.NewTenantService().ResolvePublicTenantID(rctx, codeHint)
	if err != nil {
		return rctx, false, fmt.Errorf("%w: %v", ErrBridgeTenantCodeInvalid, err)
	}
	return tenant.OnlyTenant(rctx, id), true, nil
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
// bridgeAccountTenant 在系统作用域下反查账号行的归属租户。
//
// 走 dbstate 全局句柄而不是 GlobalBridgeAccountRepo：那个接口是"账号状态读写"的
// 契约（测试里有多枚桩实现），往里塞一个租户反查方法会把桩一起改掉；而这条查询
// 本来就要求跨租户可见，与仓库的租户语义无关。
func bridgeAccountTenant(ctx context.Context, channel, accountID string) (uint, error) {
	gdb := dbstate.Get()
	if gdb == nil {
		return 0, errors.New("bridge 租户自证：db 未初始化")
	}
	if channel == "" || accountID == "" {
		return 0, nil
	}
	return tenant.SingleTenantByKey(ctx, gdb, "bridge_accounts",
		"channel = ? AND account_id = ?", channel, accountID)
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
