package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/repository"
)

// 凭证取值做成包级变量而不是直接调用真实来源：本入口的判据完全取决于配了哪一种凭证，
// 而真实来源要查 system_config_kv（需要数据库句柄）。有了这两个 seam，所有分支
// （专用凭证 / 回落桥接凭证 / 两者都没有）都能在无库环境里跑完。
// 测试替换后必须成对还原（同包用例共享进程）。
var (
	mcpTokenLookup = func(ctx context.Context, key string) (string, error) {
		return repository.NewPlatformConfigKVRepository().Get(ctx, key)
	}
	// bridgeCredentialCandidates（回落候选源）定义在 bridge_ingress_guard.go，
	// 切换后真实来源是渠道识别码表，不再读旧 Bridge token KV。
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
	// 新增了一个运营看不见、硬闸也数不清的环境变量。
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
//
// 租户归属（on 模式必需，两条链各自自证，互不覆写）：
//   - 凭证对：mcp_credentials 是租户表，签发时落在签发者所在租户 ⇒ 由 client_id
//     反查归属；命中行分属多个租户（T008 之后 client_id 只在租户内唯一）⇒ 拒。
//   - 单值凭证：KV/env 里那枚是**实例级**密钥，本身不含租户语义 ⇒ 调用方必须显式
//     带 tenant_code（查询参数或 X-Tenant-Code 头）。缺码或解析不出就 403：
//     工具调用会写客户数据、发消息，"整台实例共用一把钥匙"推不出"落在哪个租户"。
//
// resolveTenant 由装配层注入（本包不能 import service：service 已 import middleware，
// 反向依赖会成环）。注入为 nil 时 on 模式下的单值链路一律 403——缺的是判据，
// 不是"跳过这道检查"。
func MCPGuard(resolveTenant TenantCodeResolver) gin.HandlerFunc {
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
			tenantID, ok := verifyMCPCredentialPair(ctx, clientID, apiKey)
			if !ok {
				response.Error(c, 401, "Client ID 或 API Key 无效（或已停用）")
				c.Abort()
				return
			}
			// OnlyTenant：凭证行自带的租户是权威归属，不接受请求头/查询参数覆写。
			c.Request = c.Request.WithContext(tenant.OnlyTenant(c.Request.Context(), tenantID))
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
				response.Error(c, 401, "缺少 X-MCP-Token（本入口未配置专用凭证时也可用 "+identifyCodeHeader+"）")
			}
			c.Abort()
			return
		}
		for _, want := range candidates {
			if subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1 {
				tid, err := sharedTokenTenantID(c, resolveTenant)
				if err != nil {
					response.Error(c, 403, "MCP 单值凭证需要明确的租户线索: "+err.Error())
					c.Abort()
					return
				}
				if tid != 0 {
					c.Request = c.Request.WithContext(tenant.OnlyTenant(c.Request.Context(), tid))
				}
				c.Next()
				return
			}
		}
		response.Error(c, 401, "MCP token 无效")
		c.Abort()
	}
}

// extractMCPToken 只负责**取**凭证，不负责判它算不算数：能不能过由上面的候选集合决定。
// 三种携带形状都接受（X-MCP-Token / Authorization Bearer / X-Channel-Identify-Code），
// 因为隔离靠的是「配了专用凭证后，候选集合里就只有专用凭证」——回落的那枚识别码在专用分支里
// 根本不在集合内，于是它从哪个头进来都换不到工具调用权限。
func extractMCPToken(c *gin.Context) string {
	if v := strings.TrimSpace(c.GetHeader("X-MCP-Token")); v != "" {
		return v
	}
	if v := c.GetHeader("Authorization"); len(v) > 7 && strings.EqualFold(v[:7], "bearer ") {
		return strings.TrimSpace(v[7:])
	}
	return strings.TrimSpace(c.GetHeader(identifyCodeHeader))
}

// =============================================================================
// ClientID + APIKey 凭证对校验（mcp_credentials 表）
// =============================================================================

var (
	mcpCredMu        sync.RWMutex
	mcpCredCache     map[string]mcpCredCacheEntry
	mcpCredCacheTTL  = 60 * time.Second
	mcpCredLastTouch sync.Map // clientID -> time.Time（last_used 回写节流，1 次/分钟）
)

// mcpCredCacheEntry 一条 client_id 的校验结论，**含归属租户**。
//
// 缓存键只有 client_id（不带租户）：T008 之后 mcp_credentials 的唯一索引是
// (tenant_id, client_id)，同一枚 client_id 可能在两个租户各有一行。判据取自
// SingleTenantByKey——歧义即拒（entry 落成 enabled=false），所以缓存里出现的
// tenantID 一定是"当时全库唯一归属"。
//
// 残留的只是新鲜度：租户 1 先建行 ⇒ 缓存记下 (1, H1)；租户 2 随后建同名
// client_id 时管理端会 InvalidateMCPCredentialCache 清表；即便没清，拿 H1 比
// 租户 2 的密钥只会**拒**（不会把租户 1 的身份发给了租户 2），最坏是 TTL 内的
// 一次误拒。宁可误拒，不可误绑。
type mcpCredCacheEntry struct {
	tenantID uint
	hash     string
	enabled  bool
	expires  time.Time
}

// lookupMCPCredentialPair 反查凭证行：先自证归属租户，再在**该租户**作用域里取哈希。
//
// 为什么不能一把 `First()` 取到就算：不带租户绑定的请求 ctx 在 on 模式下会被隔离
// callback 直接拒（凭证链全量 401），而用 SystemScope 取整行再把 ctx 绑过去，等于
// "先跨租户捞，再挑一行"——同键多租户时取到哪一行由数据库顺序决定。两步走把
// "谁的凭证"和"用哪行的哈希"分开，中间那道判据就是歧义必须拒。
func lookupMCPCredentialPair(ctx context.Context, clientID string) (mcpCredCacheEntry, error) {
	g := db.GetDB()
	if g == nil {
		return mcpCredCacheEntry{}, errors.New("db 未初始化")
	}
	tid, err := tenant.SingleTenantByKey(ctx, g, "mcp_credentials", "client_id = ? AND enabled = ?", clientID, true)
	if err != nil {
		return mcpCredCacheEntry{}, err
	}
	if tid == 0 {
		return mcpCredCacheEntry{}, fmt.Errorf("凭证不存在或已停用: %s", clientID)
	}
	var cred model.MCPCredential
	if err := g.WithContext(tenant.OnlyTenant(ctx, tid)).
		Where("client_id = ? AND enabled = ?", clientID, true).
		First(&cred).Error; err != nil {
		return mcpCredCacheEntry{}, err
	}
	return mcpCredCacheEntry{tenantID: cred.TenantID, hash: cred.APIKeyHash, enabled: cred.Enabled}, nil
}

// verifyMCPCredentialPair 校验凭证对，返回凭证所属租户（0=不通过）。
// 校验通过节流回写 last_used_at（每凭证至多 1 次/分钟，避免写风暴）。
func verifyMCPCredentialPair(ctx context.Context, clientID, apiKey string) (uint, bool) {
	now := time.Now()
	mcpCredMu.RLock()
	entry, ok := mcpCredCache[clientID]
	mcpCredMu.RUnlock()
	if !ok || now.After(entry.expires) {
		got, err := lookupMCPCredentialPair(ctx, clientID)
		if err != nil {
			logger.Warnf("[MCP] 凭证反查失败 client=%s: %v", clientID, err)
			got = mcpCredCacheEntry{enabled: false}
		}
		got.expires = now.Add(mcpCredCacheTTL)
		mcpCredMu.Lock()
		if mcpCredCache == nil {
			mcpCredCache = map[string]mcpCredCacheEntry{}
		}
		mcpCredCache[clientID] = got
		mcpCredMu.Unlock()
		entry = got
	}
	if !entry.enabled || entry.tenantID == 0 {
		return 0, false
	}
	sum := sha256.Sum256([]byte(apiKey))
	if subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(entry.hash)) != 1 {
		return 0, false
	}
	// last_used_at 回写节流：命中缓存前提下每凭证 1 次/分钟。
	// 写必须带凭证自己的租户：不带绑定在 on 模式被隔离 callback 拒（回写永远失败，
	// 而失败又被 Delete 吞掉，管理端看到的"最近使用"从此不动）；带 SystemScope 则会
	// 把别的租户同名 client_id 那行一起更新时间戳。
	if last, seen := mcpCredLastTouch.Load(clientID); !seen || now.Sub(last.(time.Time)) > time.Minute {
		mcpCredLastTouch.Store(clientID, now)
		if g := db.GetDB(); g != nil {
			err := g.WithContext(tenant.OnlyTenant(ctx, entry.tenantID)).Model(&model.MCPCredential{}).
				Where("client_id = ?", clientID).
				Update("last_used_at", now).Error
			if err != nil {
				mcpCredLastTouch.Delete(clientID)
			}
		}
	}
	return entry.tenantID, true
}

// =============================================================================
// 租户归属线索（单值凭证链路）
// =============================================================================

// sharedTokenTenantID 为单值凭证链路解析归属租户；返回 0 表示"无需绑定"（off 模式）。
//
// 实例级密钥（KV mcp_token / env MCP_TOKEN / 桥接回落）不含租户语义，所以 on 模式下
// 必须显式带 tenant_code 且能解析成启用中的租户。解析器缺失（装配层没注入）同样按
// "判据不在场"处理——不是"跳过这道检查"。
func sharedTokenTenantID(c *gin.Context, resolveTenant TenantCodeResolver) (uint, error) {
	if !tenant.IsMultiTenant() {
		return 0, nil
	}
	if id, bound := tenant.FromContext(c.Request.Context()); bound && id != 0 {
		return 0, nil // 上游已绑定（例如测试装配或别的中间件先绑），不重复解析
	}
	if resolveTenant == nil {
		return 0, errors.New("服务端未配置租户解析器")
	}
	code := tenantHintFromRequest(c)
	if code == "" {
		return 0, errors.New("缺少 tenant_code 查询参数或 X-Tenant-Code 请求头")
	}
	id, err := resolveTenant(c.Request.Context(), code)
	if err != nil {
		return 0, err
	}
	if id == 0 {
		return 0, errors.New("租户短码解析结果为空")
	}
	return id, nil
}

// InvalidateMCPCredentialCache 管理端增删改凭证后调用，立刻生效。
func InvalidateMCPCredentialCache() {
	mcpCredMu.Lock()
	mcpCredCache = nil
	mcpCredMu.Unlock()
}
