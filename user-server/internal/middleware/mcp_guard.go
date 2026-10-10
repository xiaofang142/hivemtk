package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
)

// 凭证取值做成包级变量而不是直接调用真实来源：本入口的判据完全取决于配了哪一种凭证，
// 而真实来源要查 system_config_kv（需要数据库句柄）。有了这两个 seam，所有分支
// （专用凭证 / 回落桥接凭证 / 两者都没有）都能在无库环境里跑完。
// 测试替换后必须成对还原（同包用例共享进程）。
var (
	mcpTokenLookup = func(ctx context.Context, key string) (string, error) {
		return repository.NewSystemConfigKVRepository().Get(ctx, key)
	}
	bridgeCredentialCandidates = bridgeTokenCandidates
)

// mcpDedicatedCandidates 读 MCP 入口的**专用**凭证：KV 优先，其次环境变量。
// KV 与 env 都取（不是「有 KV 就只看 KV」）：轮换期让运维在 KV 里写新值时，
// env 里的旧值仍然可用，避免一次改配置就把全部 MCP 调用方踢下线。
func mcpDedicatedCandidates(ctx context.Context) []string {
	out := []string{}
	if v, err := mcpTokenLookup(ctx, "mcp_token"); err == nil && strings.TrimSpace(v) != "" {
		out = append(out, strings.TrimSpace(v))
	}
	// 两个键各自一行字面量（不是 range 一个名单）：配置面可发现性门
	// （scripts/check-env-coverage.py）只认 os.Getenv 里的字面键名，绕开它等于
	// 新增了一个运营看不见、门禁也数不清的环境变量。
	if v := strings.TrimSpace(os.Getenv("MCP_TOKEN")); v != "" {
		out = append(out, v)
	}
	if v := strings.TrimSpace(os.Getenv("MCP_TOKEN_PREV")); v != "" {
		out = append(out, v)
	}
	return out
}

// MCPGuard 是 POST /api/mcp 的专属凭证门。
//
// 为什么不能继续挂在桥接组里：桥接那条链路上「调用方」是浏览器扩展，能力只有上报私信与
// 领取出站消息；MCP 这条链路上的调用方能直接触发服务端工具（发消息、写客户数据、跑触达管线）。
// 两者共用一组中间件，最坏的那一条就同时生效：
//   - BRIDGE_INGEST_AUTH=off 这条历史逃生阀会把 MCP 入口一起放开，
//     而「内网里扩展可以不带凭证」根本推不出「内网里工具调用可以不带凭证」；
//   - 拿到扩展凭证（存在于每台运营机器上）就等于拿到工具调用凭证。
//
// 语义：
//   - 带 X-Client-Id + X-API-Key 头的请求走**凭证对**校验（mcp_credentials 表，
//     sha256 恒时比较 + enabled 检查）——管理端签发，供外部 AI Skill/Agent 接入；
//     校验失败直接 401，不再落到单值链路（防两把锁互相兜底）；
//   - 不带凭证对头的老调用方继续走单值链路：
//     配了专用凭证（KV mcp_token / env MCP_TOKEN / MCP_TOKEN_PREV）就只认它，
//     请求头接受 X-MCP-Token 或 Authorization: Bearer；此时桥接那枚值不在候选集合里，
//     从哪个头进来都换不到工具调用权限；
//     没配专用凭证时**回落到桥接凭证**（等价于拆分前的行为，存量部署不会当场 401）；
//   - 两者都没有：fail-closed 503，且绝不读 BRIDGE_INGEST_AUTH=off。
func MCPGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 凭证对分支（ClientID + APIKey）：只要带了 X-Client-Id 就只认这条链。
		if clientID := strings.TrimSpace(c.GetHeader("X-Client-Id")); clientID != "" {
			apiKey := strings.TrimSpace(c.GetHeader("X-API-Key"))
			if apiKey == "" {
				response.Error(c, 401, "缺少 X-API-Key")
				c.Abort()
				return
			}
			scope, ok, expired := verifyMCPCredentialPair(ctx, clientID, apiKey)
			if !ok {
				if expired {
					response.Error(c, 401, "凭证已过期，请在管理端续期或重新签发")
				} else {
					response.Error(c, 401, "Client ID 或 API Key 无效（或已停用）")
				}
				c.Abort()
				return
			}
			// 把"这把凭证是谁、能用哪些工具"落到上下文：MCP 入口按 tools 过滤与越权拒绝。
			c.Set(MCPCtxClientID, clientID)
			c.Set(MCPCtxCredentialID, scope.ID)
			c.Set(MCPCtxAllowedTools, scope.AllowedTools)
			c.Next()
			return
		}

		candidates := mcpDedicatedCandidates(ctx)
		dedicated := len(candidates) > 0
		if !dedicated {
			candidates = bridgeCredentialCandidates()
		}
		if len(candidates) == 0 {
			response.Error(c, 503, "MCP 入口需要配置 MCP_TOKEN 或桥接凭证（本入口不跟随 BRIDGE_INGEST_AUTH=off）")
			c.Abort()
			return
		}

		got := extractMCPToken(c)
		if got == "" {
			if dedicated {
				response.Error(c, 401, "缺少 X-MCP-Token")
			} else {
				response.Error(c, 401, "缺少 X-MCP-Token（本入口未配置专用凭证时也可用 X-Bridge-Token）")
			}
			c.Abort()
			return
		}
		for _, want := range candidates {
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
				c.Next()
				return
			}
		}
		response.Error(c, 401, "MCP token 无效")
		c.Abort()
	}
}

// extractMCPToken 只负责**取**凭证，不负责判它算不算数：能不能过由上面的候选集合决定。
// 三种携带形状都接受（X-MCP-Token / Authorization Bearer / X-Bridge-Token），因为隔离靠的是
// 「配了专用凭证后，候选集合里就只有专用凭证」——桥接那枚值在专用分支里根本不在集合内，
// 于是它从哪个头进来都换不到工具调用权限。反过来说：如果哪天想按头名再判一道，那才是多余的旋钮。
func extractMCPToken(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-MCP-Token")); v != "" {
		return v
	}
	if v := c.GetHeader("Authorization"); len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return strings.TrimSpace(c.GetHeader("X-Bridge-Token"))
}

// =============================================================================
// ClientID + APIKey 凭证对校验（mcp_credentials 表）
// =============================================================================

var (
	mcpCredMu        sync.RWMutex
	mcpCredCache     map[string]mcpCredCacheEntry
	mcpCredCacheTTL  = 60 * time.Second
	mcpCredLastTouch sync.Map // clientID -> time.Time（last_used 回写节流，1 次/分钟）
	// mcpCredLookup 凭证行查询（seam，测试注入假实现；生产走 db.GetDB()）。
	// 与 mcpTokenLookup 同型同理由：MCPGuard 的用例不连库，靠 seam 覆盖
	// "库里有/没有这行、行过期/停用"各自的分支。
	mcpCredLookup = func(ctx context.Context, clientID string) (*model.MCPCredential, error) {
		g := db.GetDB()
		if g == nil {
			return nil, gorm.ErrInvalidDB
		}
		var cred model.MCPCredential
		if err := g.WithContext(ctx).
			Where("client_id = ? AND enabled = ?", clientID, true).
			First(&cred).Error; err != nil {
			return nil, err
		}
		return &cred, nil
	}
)

type mcpCredCacheEntry struct {
	hash         string
	enabled      bool
	id           uint
	allowedTools string
	expires      time.Time  // 缓存自身 TTL（min(凭证过期, 60s) 的取小项见下）
	expiresAt    *time.Time // 凭证自身过期时间（nil=永不）；缓存窗口内到点也按过期判
}

// =============================================================================
// 凭证作用域（2026-10-10 新增）：凭证级工具授权范围
// =============================================================================
//
// 为什么放在中间件：MCPGuard 是唯一"能拿到这把凭证是哪一行"的地方（它查 mcp_credentials
// 做 sha256 校验）。作用域必须在这里落到请求上下文，后面的 MCP 入口才能按它过滤
// tools/list 并拒绝越权 tools/call —— 否则就要在 controller 里再查一次库，等于两处事实源。
//
// 单值链路（X-MCP-Token / 桥接凭证回落）没有"某一行凭证"，因此不带作用域，
// 语义保持与拆分前一致：可调用全部工具。
const (
	// MCPCtxClientID 凭证对的 client_id（单值链路为空）
	MCPCtxClientID = "mcp_client_id"
	// MCPCtxCredentialID mcp_credentials.id（单值链路为 0）
	MCPCtxCredentialID = "mcp_credential_id"
	// MCPCtxAllowedTools 逗号分隔的工具白名单；空串 = 不限制
	MCPCtxAllowedTools = "mcp_allowed_tools"
)

// mcpCredScope 校验通过后要带回调用面的信息。
type mcpCredScope struct {
	ID           uint
	AllowedTools string
}

// verifyMCPCredentialPair 校验凭证对：client_id 查缓存/库，sha256 恒时比较。
// 校验通过节流回写 last_used_at（每凭证至多 1 次/分钟，避免写风暴）。
// 返回值：
//   - expired=true 表示**密钥正确但凭证已过期**（上层应回"已过期"而非"无效"）；
//   - expired 与 ok 同时为 true 不可能（过期即拒绝）。
//
// 返回该凭证的作用域（id + 工具白名单），供上层做授权范围控制。
func verifyMCPCredentialPair(ctx context.Context, clientID, apiKey string) (scope mcpCredScope, ok, expired bool) {
	now := time.Now()
	mcpCredMu.RLock()
	entry, cached := mcpCredCache[clientID]
	if cached {
		// 缓存窗口内到点也算过期：TTL 60s 内被续期的凭证最多晚 60s 生效（可接受）；
		// 但**先过期后续期**的窗口里不能放行——过期判据永远以"当前时刻"为准。
		if entry.expiresAt != nil && now.After(*entry.expiresAt) {
			mcpCredMu.RUnlock()
			return mcpCredScope{}, false, true
		}
	}
	mcpCredMu.RUnlock()
	if !cached || now.After(entry.expires) {
		cred, err := mcpCredLookup(ctx, clientID)
		if err != nil {
			mcpCredMu.Lock()
			if mcpCredCache == nil {
				mcpCredCache = map[string]mcpCredCacheEntry{}
			}
			mcpCredCache[clientID] = mcpCredCacheEntry{expires: now.Add(mcpCredCacheTTL), enabled: false}
			mcpCredMu.Unlock()
			return mcpCredScope{}, false, false
		}
		entry = mcpCredCacheEntry{
			hash:         cred.APIKeyHash,
			enabled:      cred.Enabled,
			id:           cred.ID,
			allowedTools: cred.AllowedTools,
			expiresAt:    cred.ExpiresAt,
			expires:      now.Add(mcpCredCacheTTL),
		}
		mcpCredMu.Lock()
		if mcpCredCache == nil {
			mcpCredCache = map[string]mcpCredCacheEntry{}
		}
		mcpCredCache[clientID] = entry
		mcpCredMu.Unlock()
		// 刚下探出来的行直接判过期（不等到下一次请求）。
		if entry.expiresAt != nil && now.After(*entry.expiresAt) {
			return mcpCredScope{}, false, true
		}
	}
	if !entry.enabled {
		return mcpCredScope{}, false, false
	}
	sum := sha256.Sum256([]byte(apiKey))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(entry.hash)) != 1 {
		return mcpCredScope{}, false, false
	}
	// last_used_at 回写节流：命中缓存前提下每凭证 1 次/分钟
	if last, seen := mcpCredLastTouch.Load(clientID); !seen || now.Sub(last.(time.Time)) > time.Minute {
		mcpCredLastTouch.Store(clientID, now)
		if g := db.GetDB(); g != nil {
			if err := g.WithContext(ctx).Model(&model.MCPCredential{}).
				Where("client_id = ?", clientID).
				Update("last_used_at", now).Error; err != nil {
				mcpCredLastTouch.Delete(clientID)
			}
		}
	}
	return mcpCredScope{ID: entry.id, AllowedTools: entry.allowedTools}, true, false
}

// InvalidateMCPCredentialCache 管理端增删改凭证后调用，立刻生效。
func InvalidateMCPCredentialCache() {
	mcpCredMu.Lock()
	mcpCredCache = nil
	mcpCredMu.Unlock()
}
