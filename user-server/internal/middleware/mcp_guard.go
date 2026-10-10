package middleware

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
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
				mcpRecordAuthAudit(ctx, clientID, 0, mcpReasonMissingKey, c.ClientIP())
				response.Error(c, 401, "缺少 X-API-Key")
				c.Abort()
				return
			}
			scope, ok, expired, reason := verifyMCPCredentialPair(ctx, clientID, apiKey)
			if !ok {
				// 鉴权失败也落审计：出事要能回答"谁在什么时候拿错钥匙撞门"。
				mcpRecordAuthAudit(ctx, clientID, scope.KeyID, reason, c.ClientIP())
				response.Error(c, 401, mcpAuthFailMessage(expired, reason))
				c.Abort()
				return
			}
			// 把"这把凭证是谁、能用哪些工具"落到上下文：MCP 入口按 tools 过滤与越权拒绝。
			c.Set(MCPCtxClientID, clientID)
			c.Set(MCPCtxCredentialID, scope.ID)
			c.Set(MCPCtxKeyID, scope.KeyID)
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
	mcpCredLastTouch sync.Map // "k:"+keyHash / "c:"+clientID -> time.Time（last_used 回写节流，1 次/分钟）
	// mcpCredNegPair client_id 负缓存（key 表未命中且 legacy 也查无此凭证时写入，
	// 60s 内同 client_id 的重复非法尝试不再打库）。与正向缓存分开：withCredSeam
	// 换假行时只清正向缓存也不会把"查无此凭证"误传染给新用例。
	mcpCredNegPair sync.Map
	// mcpCredLookup legacy 凭证行查询（seam，测试注入假实现；生产走 db.GetDB()）。
	// 只服务②legacy 兜底链：按 client_id 查行（enabled=true），再比 legacy 列 hash。
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
	// mcpKeyLookup 钥匙行查询（seam）：按 sha256 hex 查 mcp_credential_keys。
	mcpKeyLookup = func(ctx context.Context, keyHash string) (*model.MCPCredentialKey, error) {
		g := db.GetDB()
		if g == nil {
			return nil, gorm.ErrInvalidDB
		}
		var key model.MCPCredentialKey
		if err := g.WithContext(ctx).
			Where("key_hash = ?", keyHash).First(&key).Error; err != nil {
			return nil, err
		}
		return &key, nil
	}
	// mcpCredLookupByID 按 id 查凭证（key 命中后取归属凭证；不筛 enabled——
	// "凭证已停用"要作为一个可区分的失败原因回到调用方，而不是混进"无效"）。
	mcpCredLookupByID = func(ctx context.Context, id uint) (*model.MCPCredential, error) {
		g := db.GetDB()
		if g == nil {
			return nil, gorm.ErrInvalidDB
		}
		var cred model.MCPCredential
		if err := g.WithContext(ctx).First(&cred, id).Error; err != nil {
			return nil, err
		}
		return &cred, nil
	}
)

// mcpAuthAuditSink 鉴权失败审计落库（seam，测试注入假实现；生产走 repository，
// best-effort——审计写失败不改变 401 的判定与响应）。
var mcpAuthAuditSink = func(ctx context.Context, row *model.MCPCallAudit) {
	r := repository.NewMCPCallAuditRepo()
	if !r.Available() {
		return
	}
	_ = r.Create(ctx, row)
}

// mcpRecordAuthAudit 落一条鉴权失败审计（event=auth）。
// 只覆盖凭证对链路（client_id 非空）：单值链路的调用方是内部扩展/桥接，
// 没有可对账的凭证行，记了也无法归属（与 mcp_call_audits 的口径一致）。
func mcpRecordAuthAudit(ctx context.Context, clientID string, keyID uint, reason, ip string) {
	row := &model.MCPCallAudit{
		ClientID: clientID,
		Method:   "auth",
		Allowed:  false,
		OK:       false,
		ErrorMsg: mcpAuthFailMessage(false, reason),
		IP:       ip,
		Event:    "auth",
	}
	if keyID != 0 {
		row.KeyID = &keyID
	}
	mcpAuthAuditSink(ctx, row)
}

type mcpCredCacheEntry struct {
	// hash 校验基准：钥匙路径=key.KeyHash；legacy 路径=credential.APIKeyHash
	hash string
	// keyID 命中的钥匙行 id（legacy 路径=0）
	keyID     uint
	keyStatus string // 钥匙状态（legacy 路径为空串）
	// credentialID / clientID / allowedTools / credEnabled 归属凭证的执行面
	credentialID uint
	clientID     string
	allowedTools string
	credEnabled  bool
	// keyExpiresAt 钥匙独立过期（legacy=与 credExpiresAt 同源，回填时已复制）
	keyExpiresAt  *time.Time
	credExpiresAt *time.Time // 凭证过期
	expires       time.Time  // 缓存自身 TTL
}

// =============================================================================
// 凭证作用域（2026-10-10 新增）：凭证级工具授权范围
// =============================================================================

// 单值链路（X-MCP-Token / 桥接凭证回落）没有"某一行凭证"，因此不带作用域，
// 语义保持与拆分前一致：可调用全部工具。
const (
	// MCPCtxClientID 凭证对的 client_id（单值链路为空）
	MCPCtxClientID = "mcp_client_id"
	// MCPCtxCredentialID mcp_credentials.id（单值链路为 0）
	MCPCtxCredentialID = "mcp_credential_id"
	// MCPCtxKeyID 命中的钥匙 mcp_credential_keys.id（1:N 之后"哪把钥匙干的"全靠它）
	MCPCtxKeyID = "mcp_key_id"
	// MCPCtxAllowedTools 逗号分隔的工具白名单；空串 = 不限制
	MCPCtxAllowedTools = "mcp_allowed_tools"
)

// 鉴权失败原因字面量（审计行 error_msg 与响应措辞共用一套口径）。
const (
	mcpReasonMissingKey  = "missing_api_key"     // 带了 X-Client-Id 但没带钥匙
	mcpReasonInvalid     = "invalid"             // 钥匙不存在 / 凭证不存在 / 比对失败
	mcpReasonExpired     = "expired"             // 密钥正确但已过期（钥匙级或凭证级）
	mcpReasonKeyDisabled = "key_disabled"        // 钥匙被禁用（可再启用）
	mcpReasonKeyRevoked  = "key_revoked"         // 钥匙被停用/吊销（终态）
	mcpReasonCredOff     = "credential_disabled" // 凭证级停用（伞开关）
)

// mcpCredScope 校验通过后要带回调用面的信息。
type mcpCredScope struct {
	ID           uint
	KeyID        uint
	AllowedTools string
}

// mcpAuthFailMessage 失败原因 → 对外措辞。过期 vs 无效 vs 状态类分开：
// 运维排障时"钥匙被禁用"和"钥匙错了"是两个完全不同的下一步。
func mcpAuthFailMessage(expired bool, reason string) string {
	switch {
	case expired || reason == mcpReasonExpired:
		return "凭证已过期，请在管理端续期或重新签发"
	case reason == mcpReasonMissingKey:
		return "缺少 X-API-Key"
	case reason == mcpReasonKeyDisabled:
		return "API Key 已禁用（可在管理端重新启用）"
	case reason == mcpReasonKeyRevoked:
		return "API Key 已停用（吊销不可恢复，请重新签发）"
	case reason == mcpReasonCredOff:
		return "凭证已停用"
	default:
		return "Client ID 或 API Key 无效（或已停用）"
	}
}

func mcpKeyHash(apiKey string) string {
	sum := sha256.Sum256([]byte(apiKey))
	return hex.EncodeToString(sum[:])
}

// verifyMCPCredentialPair 校验凭证对，两条链按序尝试：
//
//	① 钥匙表（mcp_credential_keys，1:N 拆表后的真值源）：sha256 查行 → 状态/过期 → 归属凭证；
//	② legacy 兜底：按 client_id 查凭证行，比对遗留 api_key_hash 列
//	  （回填前的存量窗口、旧二进制回滚窗口、测试 seam 都靠它）。
//
// 校验通过节流回写 last_used_at（钥匙行+凭证行，每把至多 1 次/分钟）。
// 返回值：
//   - expired=true 表示**密钥正确但已过期**（上层应回"已过期"而非"无效"）；
//   - reason 给出更细的失败归因（禁用/吊销/凭证停用/无效），审计与措辞共用；
//   - scope 在钥匙可识别时即使失败也带回归属（审计行能对回凭证/钥匙）。
func verifyMCPCredentialPair(ctx context.Context, clientID, apiKey string) (scope mcpCredScope, ok, expired bool, reason string) {
	now := time.Now()

	// ① 钥匙表路径（缓存按 keyHash 键；负缓存在②里按 clientID 键，两不干扰）。
	if s, ok2, exp2, r2, decided := verifyMCPKeyEntry(ctx, mcpKeyHash(apiKey), apiKey, now); decided {
		if ok2 {
			touchMCPKeyLastUsed(ctx, s, now)
		}
		return s, ok2, exp2, r2
	}

	// ② legacy 兜底。先看负缓存：同一 client_id 60s 内已经"查无此凭证"过，
	// 就不再打库（防拿不存在的 client_id 撞门）。
	if until, neg := mcpCredNegPair.Load(clientID); neg && now.Before(until.(time.Time)) {
		return mcpCredScope{}, false, false, mcpReasonInvalid
	}

	cacheKey := "c:" + clientID
	mcpCredMu.RLock()
	entry, cached := mcpCredCache[cacheKey]
	mcpCredMu.RUnlock()
	if cached && now.Before(entry.expires) {
		s, ok2, exp2, r2 := judgeMCPCredEntry(entry, apiKey, now)
		if ok2 {
			touchMCPCredLastUsed(ctx, cacheKey, entry.credentialID, now)
		}
		return s, ok2, exp2, r2
	}
	cred, err := mcpCredLookup(ctx, clientID)
	if err != nil {
		// 查无此凭证：负缓存（与"有凭证但钥匙错"分开——后者不写负缓存，
		// 该 client_id 下的合法新钥匙随时可以进来）。
		mcpCredNegPair.Store(clientID, now.Add(mcpCredCacheTTL))
		return mcpCredScope{}, false, false, mcpReasonInvalid
	}
	entry = mcpCredCacheEntry{
		hash:          cred.APIKeyHash,
		credentialID:  cred.ID,
		clientID:      cred.ClientID,
		allowedTools:  cred.AllowedTools,
		credEnabled:   cred.Enabled,
		credExpiresAt: cred.ExpiresAt,
		expires:       now.Add(mcpCredCacheTTL),
	}
	mcpCredMu.Lock()
	if mcpCredCache == nil {
		mcpCredCache = map[string]mcpCredCacheEntry{}
	}
	mcpCredCache[cacheKey] = entry
	mcpCredMu.Unlock()
	s, ok2, exp2, r2 := judgeMCPCredEntry(entry, apiKey, now)
	if ok2 {
		touchMCPCredLastUsed(ctx, cacheKey, entry.credentialID, now)
	}
	return s, ok2, exp2, r2
}

// verifyMCPKeyEntry 钥匙表路径。decided=false 表示钥匙表未命中（查无此钥匙或库不可达），
// 调用方落 legacy 兜底——**不区分**这两种情况：库不可达时 legacy 同样不可达，结果一致。
func verifyMCPKeyEntry(ctx context.Context, keyHash, apiKey string, now time.Time) (mcpCredScope, bool, bool, string, bool) {
	mcpCredMu.RLock()
	entry, cached := mcpCredCache[keyHash]
	mcpCredMu.RUnlock()
	if !cached || now.After(entry.expires) {
		key, err := mcpKeyLookup(ctx, keyHash)
		if err != nil {
			return mcpCredScope{}, false, false, mcpReasonInvalid, false
		}
		cred, err := mcpCredLookupByID(ctx, key.CredentialID)
		if err != nil {
			// 钥匙在、凭证不在（被并发删除的竞态窗口）：当无效处理，不落缓存。
			return mcpCredScope{}, false, false, mcpReasonInvalid, true
		}
		entry = mcpCredCacheEntry{
			hash:          key.KeyHash,
			keyID:         key.ID,
			keyStatus:     key.Status,
			credentialID:  cred.ID,
			clientID:      cred.ClientID,
			allowedTools:  cred.AllowedTools,
			credEnabled:   cred.Enabled,
			keyExpiresAt:  key.ExpiresAt,
			credExpiresAt: cred.ExpiresAt,
			expires:       now.Add(mcpCredCacheTTL),
		}
		mcpCredMu.Lock()
		if mcpCredCache == nil {
			mcpCredCache = map[string]mcpCredCacheEntry{}
		}
		mcpCredCache[keyHash] = entry
		mcpCredMu.Unlock()
	}
	s, ok, exp, r := judgeMCPCredEntry(entry, apiKey, now)
	return s, ok, exp, r, true
}

// judgeMCPCredEntry 按缓存行裁决：过期判据永远以"当前时刻"为准
// （TTL 60s 内被续期最多晚 60s 生效，可接受；先过期后续期的窗口绝不能放行）。
func judgeMCPCredEntry(entry mcpCredCacheEntry, apiKey string, now time.Time) (mcpCredScope, bool, bool, string) {
	scope := mcpCredScope{ID: entry.credentialID, KeyID: entry.keyID, AllowedTools: entry.allowedTools}
	if entry.keyExpiresAt != nil && now.After(*entry.keyExpiresAt) {
		return scope, false, true, mcpReasonExpired
	}
	if entry.credExpiresAt != nil && now.After(*entry.credExpiresAt) {
		return scope, false, true, mcpReasonExpired
	}
	switch entry.keyStatus {
	case model.MCPKeyStatusDisabled:
		return scope, false, false, mcpReasonKeyDisabled
	case model.MCPKeyStatusRevoked:
		return scope, false, false, mcpReasonKeyRevoked
	}
	if !entry.credEnabled {
		return scope, false, false, mcpReasonCredOff
	}
	if subtle.ConstantTimeCompare([]byte(mcpKeyHash(apiKey)), []byte(entry.hash)) != 1 {
		return scope, false, false, mcpReasonInvalid
	}
	return scope, true, false, ""
}

// touchMCPKeyLastUsed 钥匙命中后的回写：钥匙行 + 凭证行，节流按钥匙（1 次/分钟）。
func touchMCPKeyLastUsed(ctx context.Context, scope mcpCredScope, now time.Time) {
	touchKey := fmt.Sprintf("k:%d:%d", scope.KeyID, scope.ID)
	if last, seen := mcpCredLastTouch.Load(touchKey); seen && now.Sub(last.(time.Time)) <= time.Minute {
		return
	}
	mcpCredLastTouch.Store(touchKey, now)
	if g := db.GetDB(); g != nil {
		if scope.KeyID != 0 {
			_ = g.WithContext(ctx).Model(&model.MCPCredentialKey{}).
				Where("id = ?", scope.KeyID).Update("last_used_at", now).Error
		}
		if scope.ID != 0 {
			_ = g.WithContext(ctx).Model(&model.MCPCredential{}).
				Where("id = ?", scope.ID).Update("last_used_at", now).Error
		}
	}
}

// touchMCPCredLastUsed legacy 命中后的回写（沿用旧口径：按 client_id 更新凭证行）。
func touchMCPCredLastUsed(ctx context.Context, cacheKey string, credentialID uint, now time.Time) {
	if last, seen := mcpCredLastTouch.Load(cacheKey); seen && now.Sub(last.(time.Time)) <= time.Minute {
		return
	}
	mcpCredLastTouch.Store(cacheKey, now)
	if g := db.GetDB(); g != nil && credentialID != 0 {
		_ = g.WithContext(ctx).Model(&model.MCPCredential{}).
			Where("id = ?", credentialID).Update("last_used_at", now).Error
	}
}

// InvalidateMCPCredentialCache 管理端增删改凭证/钥匙后调用，立刻生效。
func InvalidateMCPCredentialCache() {
	mcpCredMu.Lock()
	mcpCredCache = nil
	mcpCredMu.Unlock()
}
