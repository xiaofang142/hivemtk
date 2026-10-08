package middleware

import (
	"context"
	"crypto/subtle"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

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
//   - 配了专用凭证（KV mcp_token / env MCP_TOKEN / MCP_TOKEN_PREV）就只认它，
//     请求头接受 X-MCP-Token 或 Authorization: Bearer；此时桥接那枚值不在候选集合里，
//     从哪个头进来都换不到工具调用权限；
//   - 没配专用凭证时**回落到桥接凭证**（等价于拆分前的行为，存量部署不会当场 401）；
//   - 两者都没有：fail-closed 503，且绝不读 BRIDGE_INGEST_AUTH=off。
func MCPGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()
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
