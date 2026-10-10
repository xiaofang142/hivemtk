package utils

import (
	"context"
	"errors"
	"strconv"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

// 令牌按用户维度即时吊销（第十八轮，2026-09-19）
//
// 背景：JWT 是无状态的，BlacklistJWT 只能按「具体某一条 token 的 SHA256」作废，
// 因此**禁用账号 / 删除账号 / 改角色 / 改密**这类"要踢掉该用户所有在线会话"的事件
// 在原实现里没有任何收口手段——被禁用/删除的用户手持旧令牌仍可访问到过期（24h），
// 且旧实现 RefreshToken 只信令牌里的旧 claims，等于把 24h 无限续期。
//
// 做法：为每个用户存一个"作废水位线"（revoked_before，秒级 unix），
// 校验时比较令牌的 iat：iat < 水位线 ⇒ 该令牌签发于吊销之前 ⇒ 拒绝。
// 缓存键 TTL = 25h > 令牌最长寿命 24h，保证水位线存活期覆盖所有可能被它作废的令牌，
// 不会出现"键先过期 → 旧令牌复活"的窗口。
const (
	jwtRevokeKeyPrefix = "jwt:revoked_before:"
	// jwtRevokeTTL 必须 **>** DefaultJWTConfig.ExpiresHours（24h），
	// 与 jwtBlacklistTTL 同口径；否则水位线会先于待作废令牌消失。
	jwtRevokeTTL = 25 * time.Hour
	// revocationCacheBudget 是水位线读写的**独立**时间预算（见 detachedRevocationCtx）。
	// 取值只需覆盖"缓存正常但抖动"的量级（本地 Redis 读亚毫秒级）；
	// 真正的缓存不可用仍会在预算到期后回到 fail-closed 口径。
	revocationCacheBudget = 2 * time.Second
)

// detachedRevocationCtx 把水位线的读写与**调用方（HTTP 请求）的取消链**解绑。
//
// 为什么必须解绑：客户端在 SPA 路由切换/重复请求时随时会 abort 掉受保护请求，
// Gin 的 request ctx 随即 context.Canceled。而吊销判定是"这个令牌还能不能用"的**权威读**，
// 它失败与"该用户已被吊销"是两件事——原实现把前者落进 default 分支 fail-closed 判成后者，
// 于是"浏览器取消了请求"表现为"合法令牌被 401 拒绝"，前端收到 401 清 token 打回登录页，
// 用户被无端登出（实测 user_id=21 连续 5 次 context canceled → 整批页面踢回 #/login）。
// 同一件事发生在写侧更糟：禁用/改密请求被客户端取消 ⇒ 水位线没写进去 ⇒ 该用户旧令牌
// 在最长 24h 内仍然可用，正是这套机制要堵的洞。
//
// 因此读写都带自己的预算跑完（口径同 IsJWTBlacklisted 用 context.Background）；
// 用 WithoutCancel 而非 Background 是为了保住 ctx 上的 trace 值，审计链不断。
func detachedRevocationCtx(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), revocationCacheBudget)
}

func revokeKey(userID uint) string {
	return jwtRevokeKeyPrefix + strconv.FormatUint(uint64(userID), 10)
}

// RevokeUserTokens 作废 userID 在**本时刻之前**签发的全部 JWT。
//
// 调用即生效于下一个受保护的请求（无需等待令牌自然过期）。
// 边界：与 IsTokenRevoked 的严格 `<` 配对 ⇒ 与本调用处于同一秒内签发的令牌不判失效
// （避免"改密后立刻重新登录"被自己刚拿到的令牌误伤）；代价是吊销前 ≤1s 的窗口，
// 该窗口已由 RefreshToken 改为回源校验账号状态而不再能被续期放大。
//
// 写失败只告警不阻断业务流程：调用点的业务变更（禁用/改密）已经落库，
// 回滚业务反而更危险；未落水位线时最坏退化为"等令牌自然过期（≤24h）"，
// 且此时 RefreshToken 已不会为失效账号续期。
func RevokeUserTokens(ctx context.Context, userID uint) {
	if userID == 0 {
		return
	}
	key := revokeKey(userID)
	cutoff := time.Now().Unix()
	wctx, cancel := detachedRevocationCtx(ctx)
	defer cancel()
	if err := cache.GetGlobalCache().Set(wctx, key, strconv.FormatInt(cutoff, 10), jwtRevokeTTL); err != nil {
		logger.Errorf("RevokeUserTokens 写缓存失败（该用户旧令牌将退化为最长 24h 自然过期）user_id=%d: %v", userID, err)
	}
}

// IsTokenRevoked 判定「以 issuedAt 签发、属于 userID」的令牌是否已被吊销。
//
// 三态口径与 IsSessionLockedForHuman 一致：
//   - 读成功：按水位线比较；
//   - key 不存在（redis.Nil / cache.ErrCacheMiss）：该用户从未触发吊销 ⇒ 未作废；
//   - 其它错误（缓存故障／自己的读预算到期）：**fail-closed 判为已作废**，与
//     IsJWTBlacklisted 同口径（缓存故障时受保护请求本就已全部拒绝，不引入新的可用性风险）。
//     注意调用方 ctx 被取消（客户端 abort）**不在**这一类：见 detachedRevocationCtx。
//
// issuedAt 为 nil（令牌没有 iat）时，只要存在水位线即判失效——无法证明它签发于吊销之后。
func IsTokenRevoked(ctx context.Context, userID uint, issuedAt *jwt.NumericDate) bool {
	if userID == 0 {
		return false
	}
	key := revokeKey(userID)
	rctx, cancel := detachedRevocationCtx(ctx)
	defer cancel()
	val, err := cache.GetGlobalCache().Get(rctx, key)
	switch {
	case err == nil:
		cutoff, convErr := strconv.ParseInt(val, 10, 64)
		if convErr != nil {
			logger.Errorf("IsTokenRevoked 水位线值非法，fail-closed 拒绝 user_id=%d val=%q: %v", userID, val, convErr)
			return true
		}
		if issuedAt == nil {
			return true
		}
		return issuedAt.Unix() < cutoff
	case errors.Is(err, redis.Nil), errors.Is(err, cache.ErrCacheMiss):
		return false
	default:
		logger.Errorf("IsTokenRevoked 缓存读取失败（fail-closed，拒绝令牌）user_id=%d: %v", userID, err)
		return true
	}
}
