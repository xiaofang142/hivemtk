// route_policy.go 集中式"这条口只有超管能用"的判定表。
//
// 背景：/api 认证组下大量路由只要求"登录"，其中一批是**全局能力**
// —— 备份/恢复、重启进程、LLM 与 MCP 凭据 CRUD、系统配置写、给他人重置口令、
// 功能开关放量。它们和"这个坐席名下的会话"无关，任何登录态账号打上就能改写整台实例
// 的行为。逐条在注册处补 admin 守卫不现实（同一 handler 常在两处注册，
// 容易出现"权威口挂 admin、兼容别名裸登录"的降级对），所以把判定收在一处：
//
//   - 守卫挂在 auth.Use(JWTAuthMiddleware()) 之后，覆盖整组；
//   - 表里每一行都必须在常驻门里被 staff 令牌实测成 403、admin 令牌实测成"非 403"，
//     所以往表里加一行 = 必须同时给出它的行为证据，写错路径会当场红（模式匹配不到
//     任何 live 路由也算红）；
//   - 表只做"升到 admin"，不做降级。默认档仍是登录即可，避免一次改动把大量
//     路由推进一个没人核对过的口径。
//
// 第二档（能力面表，见 route_capability.go）：那批"页面看着像管理页、接口却只对
// 登录态开放"的口，判据由菜单授权自己说——侧栏不给看的，接口也不给调；
// 管理员勾了菜单，接口同时开。那张表是生成件（scripts/menu-route-role-audit.py --emit-go），
// 口径与消费证据同源。
//
// 匹配对象是 c.Request.URL.Path（真实请求路径），不是 c.FullPath()：FullPath 带的是
// 注册时的参数名，表里写 `/api/users/*/password` 时不需要知道代码里那段叫 :id 还是
// :user_id，表的读者也不必去对照两种命名。
package middleware

import (
	"net/http"
	"strings"

	"hivemtk-user/internal/pkg/utils/logger"

	"github.com/gin-gonic/gin"
)

// adminRouteRule 一条"仅超管"规则。
//
// Methods 为空表示任意方法；Pattern 按段匹配，`*` 吃掉恰好一段，`**` 只允许出现在
// 末尾、吃掉零或多段（所以 `/api/kb-releases/**` 也含 `/api/kb-releases` 本身）。
type adminRouteRule struct {
	Methods []string
	Pattern string
}

// matchSegments 段级匹配。pattern 段数不足或超出都算不匹配，`**` 之外每段一一对应。
func matchSegments(pattern, path string) bool {
	ps := splitRouteSegments(pattern)
	rs := splitRouteSegments(path)
	for i, seg := range ps {
		if seg == "**" {
			return true
		}
		if i >= len(rs) {
			return false
		}
		if seg != "*" && seg != rs[i] {
			return false
		}
	}
	return len(ps) == len(rs)
}

func splitRouteSegments(p string) []string {
	return strings.Split(strings.Trim(p, "/"), "/")
}

// adminOnlyRoutes 只有超管可用的能力面。
//
// 入选判据（每条都要满足，缺一就留到登录档）：
//  1. 改的是**全局/共享**状态或凭据，不是调用者自己的数据；
//  2. 前端零消费点（在 user-web 全量调用清单里搜不到该路径前缀），或它的页面只在
//     admin 菜单节点下出现。
//
// 判据 2 是"不弄坏界面"保险：升到 admin 前不需要先做菜单核对，因此这一批
// 可以安全落地；其余"有消费点但页面看着像管理页"的（system/users、system/roles、
// mcp/credentials、llm/models 等）留在菜单核对之后再进表。
var adminOnlyRoutes = []adminRouteRule{
	// —— 实例级运维：备份落盘、恢复、重启进程 ——
	{Methods: []string{"POST"}, Pattern: "/api/backup/*/restore"},
	{Methods: []string{"POST"}, Pattern: "/api/system/backup"},
	{Methods: []string{"POST"}, Pattern: "/api/system/restore"},
	{Methods: []string{"POST"}, Pattern: "/api/system/restart"},

	// —— 全局配置的写侧（读侧留给登录档：注册页要显示口令规则）——
	{Methods: []string{"PUT"}, Pattern: "/api/auth/password-policy"},
	{Pattern: "/api/system/configs/**"},
	{Pattern: "/api/sms/configs/**"},
	{Pattern: "/api/manage/co-pilot/config"},
	{Pattern: "/api/agent/co-pilot/config"},
	{Methods: []string{"PUT"}, Pattern: "/api/agent/tool-integrations"},

	// —— 对象存储配置：回的是桶地址与凭据材料 ——
	{Pattern: "/api/obs-configs/**"},

	// —— LLM / MCP / RAG 侧的凭据与路由策略 ——
	{Pattern: "/api/llm/mcp/credentials/**"},
	{Pattern: "/api/llm/kb-connectors/**"},
	{Pattern: "/api/llm/providers/**"},
	{Pattern: "/api/llm/strategies"},
	{Pattern: "/api/llm/scenario-stats"},
	{Pattern: "/api/llm/embedding-config"},
	{Pattern: "/api/llm/llm-config"},
	{Pattern: "/api/llm/rerank-config"},
	{Pattern: "/api/llm-routings/policy"},

	// —— 知识库发布治理：发布/回滚/恢复会改动线上产物 ——
	{Pattern: "/api/kb-releases/**"},

	// —— 功能开关的放量与诊断（列表读侧仍开放）——
	{Pattern: "/api/feature-flags/*/audit"},
	{Pattern: "/api/feature-flags/*/eval-log"},
	{Pattern: "/api/feature-flags/*/enable"},
	{Pattern: "/api/feature-flags/*/disable"},
	{Pattern: "/api/feature-flags/*/rollout"},

	// —— 运维/评测读面与批量导出 ——
	{Pattern: "/api/manage/rag-eval/**"},
	{Pattern: "/api/manage/data-export/*"},
	{Pattern: "/api/manage/rules"},
	{Pattern: "/api/manage/slo/**"},
	{Pattern: "/api/manage/session-chain/sla-config"},
	{Pattern: "/api/manage/typing-predict"},

	// —— 智能体工具白名单：决定 AI 能调用哪些工具 ——
	{Pattern: "/api/tools/permission/**"},

	// —— 给他人重置口令（本人改密走 /api/auth/change-password）——
	{Methods: []string{"PUT"}, Pattern: "/api/users/*/password"},

	// —— 渠道账号清单的兼容别名（权威口在 admin 组，别名曾落在登录档）——
	{Pattern: "/api/feishu/accounts/list"},
	{Pattern: "/api/telegram/accounts/list"},

	// —— 统一看板：租户内 LLM 成本/送达/渠道健康聚合 ——
	// 判据 1（全局共享态）：看板回的是整个租户的经营/成本口径，不是调用者自己的数据；
	// 判据 2（前端零消费点或仅 admin 菜单）：页面只在 admin 菜单节点
	// observabilityDashboard 下出现，无其它角色消费点。
	{Pattern: "/api/observability/dashboard"},
}

// IsAdminOnlyRoute 判定 (method, path) 是否落在"仅超管"表内。
// 导出给常驻门与文档门复用，避免"门的判据"和"守卫的实现"各写一份匹配规则。
func IsAdminOnlyRoute(method, path string) bool {
	for _, r := range adminOnlyRoutes {
		if !matchSegments(r.Pattern, path) {
			continue
		}
		if len(r.Methods) == 0 {
			return true
		}
		for _, m := range r.Methods {
			if strings.EqualFold(m, method) {
				return true
			}
		}
	}
	return false
}

// MatchAdminOnlyPattern 判定单个 Pattern 是否匹配某条真实路径（不含方法维）。
// 导出给常驻门做"每条规则至少命中一条 live 路由"的逐规则归属判定：
// IsAdminOnlyRoute 只回布尔，无法区分是哪一行命中的，写错路径的死行在它的读数里
// 和命中无数条的活行长得一模一样。
func MatchAdminOnlyPattern(pattern, path string) bool {
	return matchSegments(pattern, path)
}

// AdminOnlyRoutePatterns 返回规则表的只读副本（方法列表为空表示任意方法），
// 供测试逐条核对"这条规则至少命中一条 live 路由"。
func AdminOnlyRoutePatterns() [][2][]string {
	out := make([][2][]string, 0, len(adminOnlyRoutes))
	for _, r := range adminOnlyRoutes {
		out = append(out, [2][]string{r.Methods, {r.Pattern}})
	}
	return out
}

// RoutePolicyGuard 按 adminOnlyRoutes 与能力面表拦截非超管／未授权角色。
//
// 两档判据，先后有序：
//  1. 仅超管表（adminOnlyRoutes）：无条件要超管，菜单授权开不了它；
//  2. 能力面表（model.MenuCapabilities，生成件）：要的是**该功能的菜单授权**，
//     超管恒放行，其余角色按 role_menu_permissions 的授予判。
//
// 与 AdminAuthMiddleware / RequireAdminMiddleware 的差别只在"要不要走表"：
// 401/403 的文案与形状沿用 RequireAdminMiddleware，让客户端看到同一个口径。
// 这里刻意**不**加测试二进制豁免：这条守卫的价值恰恰是"路由表里每一行都真的拦得住"，
// 豁免等于把门自己关掉。
func RoutePolicyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !IsAdminOnlyRoute(c.Request.Method, c.Request.URL.Path) {
			roleStr, _ := c.Get("role")
			roleName, _ := roleStr.(string)
			inTable, allowed := CapabilityAllows(c.Request.Context(), roleName, c.Request.URL.Path)
			if inTable && !allowed {
				logger.Infof("RoutePolicyGuard: 角色未被授予该能力菜单，已拒绝 method=%s path=%s role=%s",
					c.Request.Method, c.Request.URL.Path, roleName)
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"code":    403,
					"message": capabilityDeniedMsg,
				})
				return
			}
			c.Next()
			return
		}
		role, exists := c.Get("role")
		if !exists {
			// 表命中却没有身份：说明这行被挂到了没跑 JWT 的组上，属于装配错误。
			// 向拒的方向收（401），不给"漏挂守卫"留一条静默通路。
			logger.Warnf("RoutePolicyGuard: 命中仅超管表但上下文无身份 role 缺失 method=%s path=%s",
				c.Request.Method, c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    401,
				"message": "未授权，请先登录",
			})
			return
		}
		roleStr, ok := role.(string)
		if !ok || roleStr != "admin" {
			logger.Info("RoutePolicyGuard: 非超管访问仅超管能力面，已拒绝 method=" +
				c.Request.Method + " path=" + c.Request.URL.Path + " role=" + roleStr)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    403,
				"message": "权限不足，需要超管权限",
			})
			return
		}
		c.Next()
	}
}
