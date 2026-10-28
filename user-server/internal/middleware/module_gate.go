package middleware

import (
	"context"
	"strings"
	"sync"

	"hivemtk-user/internal/domain/errors"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"

	"github.com/gin-gonic/gin"
)

// 模块闸（specs .specify/002-modular-subscription · FR-5.3/5.4）
//
// ===== 为什么用「路径模式」而不是逐条路由标注 =====
// 仓内已有 2000+ 条路由、37 个路由文件，且跨模块交叉引用严重（AI 销冠的接口
// 要用客户中心的 service）。逐条挂 RequireModule(k) 会漏挂、也无法用测试钉死。
// 因此形态完全复刻既有的 model/menu_capability_routes.go（251 条能力面）：
// 一张「路径模式 → 模块 key」对照表，按段匹配。
//
// ===== 执行位置 =====
// 必须挂在**鉴权之后、角色权限之前**：超管是「本系统内权限最高」，
// 不是「买了所有模块」。闸放在权限之后，超管就成了绕过付费的后门。
//
// ===== 未覆盖路由的处置====
// 未命中任何模式 → 放行 + 记 WARN + 进 uncovered-routes 清单。
// 判据：上线一个新接口忘了标注就全站 403 是灾难；漏拦最多让某个接口多暴露。
// 真正的兜底是 TestModuleRoutePatternsCoverShippedRoutes。

// moduleGateChecker 判定函数（装配期由 service.ModuleGateService 注入）
type moduleGateChecker interface {
	// IsEnabled ctx 必须是**请求上下文**：闸从 ctx 解析租户（Service 内的
	// gateTenantOf），按**该租户**的镜像状态判定。中间件这里若改用
	// context.Background()，on 模式下解析不出调用方 → 回落到默认租户视图 ——
	// 租户 2+ 的请求会被拿租户 1 的开通集判定（该放的被拦、该拦的被放），
	// 隔离与付费闸同时失真。
	IsEnabled(ctx context.Context, moduleKey string) bool
}

var (
	moduleGateMu            sync.RWMutex
	moduleGateCheckerV      moduleGateChecker
	moduleUncoveredMu       sync.Mutex
	moduleUncovered         = map[string]bool{} // path → true
	moduleUncoveredWarnOnce sync.Once
)

// SetModuleGateChecker 注入模块闸判定（cmd/api/main.go 装配期调用）
func SetModuleGateChecker(c moduleGateChecker) {
	moduleGateMu.Lock()
	moduleGateCheckerV = c
	moduleGateMu.Unlock()
}

func currentModuleGate() moduleGateChecker {
	moduleGateMu.RLock()
	defer moduleGateMu.RUnlock()
	return moduleGateCheckerV
}

// ModuleOwnerOf 返回该请求路径所属模块；未覆盖返回空串
func ModuleOwnerOf(path string) string {
	return matchModulePattern(path)
}

// UncoveredModuleRoutes 返回「已见到但未挂模块」的真实路径
func UncoveredModuleRoutes() []string {
	moduleUncoveredMu.Lock()
	defer moduleUncoveredMu.Unlock()
	out := make([]string, 0, len(moduleUncovered))
	for p := range moduleUncovered {
		out = append(out, p)
	}
	sortStrings(out)
	return out
}

// ResetUncoveredModuleRoutes 清空清单（测试用）
func ResetUncoveredModuleRoutes() {
	moduleUncoveredMu.Lock()
	moduleUncovered = map[string]bool{}
	moduleUncoveredMu.Unlock()
}

func noteUncovered(path string) {
	moduleUncoveredMu.Lock()
	moduleUncovered[path] = true
	moduleUncoveredMu.Unlock()
	moduleUncoveredWarnOnce.Do(func() {
		logger.Warn("[module-gate] 存在未标注模块的路由，已放行并计入巡检清单。" +
			"GET /api/modules/uncovered-routes 可查看；新增收费能力时务必补 module_route_patterns.go")
	})
}

// RequireModule 模块闸中间件。
//
// 直接挂在具体路由组上：auth.GET("/geo-tools/:x", RequireModule("geo"), ctrl.X)
func RequireModule(moduleKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		// 空 moduleKey = 本路由没归属任何可售模块（系统骨架页）。
		// 必须**在查判定之前**短路：判据是「调用方忘了填」，
		// 而 checker.IsEnabled(ctx, "") 返回 false（本地镜像里当然没有空 key 的模块），
		// 于是漏填一次就等于把这批路由全关掉 —— 而现象是"某几个页面突然 403"，
		// 排查时根本想不到是这里少传了一个参数。
		if moduleKey == "" {
			c.Next()
			return
		}
		checker := currentModuleGate()
		if checker == nil {
			// 未注入 = 装配缺失。放行而不是拦：拦住会让整个系统不可用，
			// 而"没装配闸"只意味着还没启用收费，不该让免费用户也进不来。
			logger.Warnf("[module-gate] 未装配模块闸判定，%s 放行", c.Request.URL.Path)
			c.Next()
			return
		}
		if checker.IsEnabled(c.Request.Context(), moduleKey) {
			c.Next()
			return
		}
		response.ErrorWithBusinessCode(c, errors.CodeModuleNotEnabled,
			"该功能模块未开通，请先在「功能模块市场」订阅开通", gin.H{"module_key": moduleKey})
		c.Abort()
	}
}

// GuardModulePath 请求级模块闸（供无法逐组挂载的路由文件统一调用）。
//
// 它读「路径模式表」，而不是要求调用方传 moduleKey —— 后者等于把
// "这条路由属于哪个模块"的答案交给每个调用点，208 个调用点就会开始漂移。
func GuardModulePath() gin.HandlerFunc {
	return func(c *gin.Context) {
		owner := ModuleOwnerOf(c.Request.URL.Path)
		if owner == "" {
			noteUncovered(c.Request.URL.Path)
			c.Next()
			return
		}
		// 注意：即便 off 模式要「全部放行」，也必须**先调用 checker**——
		// checker 的 ctx 透传是既有契约（TestGateThreadsRequestContextToChecker），
		// 直接 return 会把「模块开关读到的是哪个租户」这条证据链掐断。
		_ = tenant.IsMultiTenant()
		checker := currentModuleGate()
		if checker == nil {
			c.Next()
			return
		}
		enabled := checker.IsEnabled(c.Request.Context(), owner)
		// 私域独立部署（TENANT_MODE=off）没有「订阅」这层商业关系：整实例就是
		// 一个商户的自有系统，未订阅不等于不可用（A17）。
		if enabled || !tenant.IsMultiTenant() {
			c.Next()
			return
		}
		response.ErrorWithBusinessCode(c, errors.CodeModuleNotEnabled,
			"该功能模块未开通，请先在「功能模块市场」订阅开通", gin.H{"module_key": owner})
		c.Abort()
	}
}

// compiledModuleRoutes 预编译的规则表（避免每请求重建切片）
var compiledModuleRoutes = ModuleRouteAll()

// matchModulePattern 段级路径匹配（`*` 吃掉恰好一段）。
// 与 model/menu_capability_routes.go 的匹配语义同形，那边的等价性由测试钉住。
func matchModulePattern(path string) string {
	segs := splitModulePath(path)
	if len(segs) == 0 {
		return ""
	}
	for _, r := range compiledModuleRoutes {
		if matchModuleSegs(r.Pattern, segs) {
			return r.ModuleKey
		}
	}
	return ""
}

func splitModulePath(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// matchModuleSegs 模式段与真实段逐段比对。
// 语义：`*` 吃掉恰好一段（段数必须相等，与能力面守卫同口径，既有测试钉死）；
// 尾部 `**` 吃掉「剩余全部段」（≥0 段）——补充件的多层路由腿（如 /api/egolite/**）
// 靠它一次盖住任意深度，否则深层路径（/api/egolite/runner/tokens/:id/revoke）漏出
// 模块闸。生成件不使用 `**`，既有单 `*` 行为不受影响。
func matchModuleSegs(pattern string, segs []string) bool {
	ps := splitModulePath(pattern)
	if len(ps) == 0 {
		return false
	}
	if ps[len(ps)-1] == "**" {
		// 尾部 **：前缀逐段比对 + 路径段数 ≥ 前缀段数（** 自身可吃 0 段）
		prefix := ps[:len(ps)-1]
		if len(segs) < len(prefix) {
			return false
		}
		for i, p := range prefix {
			if p != segs[i] {
				return false
			}
		}
		return true
	}
	if len(ps) != len(segs) {
		return false
	}
	for i, p := range ps {
		if p == "*" {
			continue
		}
		if p != segs[i] {
			return false
		}
	}
	return true
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
