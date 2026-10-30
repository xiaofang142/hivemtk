// ltc_gate.go —— LTC 阶段闸门（新规划任务清单 T-P3-06）。
//
// 这张卡要修的是一句一直没落点的话："运营点一下就能整条 LTC 开/关"。
// 落点定在遗留 KV 配置表里的 `ltc.config` 一行（读写与校验在 internal/service/ltc_config.go，
// 走 repository.SystemConfigKVRepository，本处不写裸 SQL），
// 本文件只负责它面向 HTTP 的那一半：一条没被运营打开的 LTC 路由，请求必须**在进业务
// handler 之前**被拦下 —— 拦在 handler 里等于外发/写库已经发生过了，
// 而"总开关关着时不产生外发副作用"正是这张卡的 AC①。
//
// 状态码选 409 而不是 403/503，两个都不是顺手：
//   - 403 会把"运营没开这个功能"混进"你没权限"，坐席据此去申诉权限，
//     而真正该动的人拿着 ltc.config 在前台点两下就行；
//   - 503 带着"过会儿再来"的暗示，可在开关被打开之前重试一万次也是同一个结果。
//     409 = 请求与服务器当前状态冲突，且 reason 字段把冲突说清楚。
//
// 但响应体里的 code 不能由 409 折算（那是 DUPLICATE_ENTRY_3003「重复的记录」），
// 见 ltcRefusalCode：闸门拦下时一个键都没写，码却在说键冲突。
//
// fail-closed：配置读不动（degraded）时同样拦。"存储坏了 ⇒ 当作没开"与
// "存储坏了 ⇒ 当作全开"是两种事故，前者顶多被投诉功能不见了，后者是外发事故。
package middleware

import (
	"context"
	"fmt"
	"sync"

	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

// LTCConfigReader 是闸门需要的全部读能力（*service.LTCConfigService 结构上即满足）。
// 收成接口的原因是可测性：不注入的话，本包只能测到"默认全关"那一支，
// 而"打开后放行"与"关掉后不产生副作用"这两条恰恰是要害。
type LTCConfigReader interface {
	Config(ctx context.Context) *service.LTCConfig
}

var (
	ltcGuardMu   sync.Mutex
	ltcGuardedBy = map[service.LTCStage]int{}
)

// LTCStageGate 生成一道阶段闸门，挂在该阶段的任何业务路由之前。
//
// 登记"这个路由组被哪道闸分管"是函数构造时发生的：它记的是"装配过"，
// 不是"运行期真被调用过"。这一点必须与 LTCGuardedRoutes 的读数一起读，
// 单看计数会把"代码里挂上了但旗子全关"说成"现网已生效"。
func LTCStageGate(stage service.LTCStage) gin.HandlerFunc {
	ltcGuardMu.Lock()
	ltcGuardedBy[stage]++
	ltcGuardMu.Unlock()

	return ltcStageGate(stage, service.GlobalLTCConfig())
}

// LTCGuardedRoutes 返回各阶段已挂载的闸门数（副本，调用方改不动内部登记）。
// 未登记过的阶段不出现在 map 里 —— 与"登记了 0 条"是两回事，读侧按缺键渲染成 0。
func LTCGuardedRoutes() map[service.LTCStage]int {
	ltcGuardMu.Lock()
	defer ltcGuardMu.Unlock()
	out := make(map[service.LTCStage]int, len(ltcGuardedBy))
	for k, v := range ltcGuardedBy {
		out[k] = v
	}
	return out
}

// ltCShadowMu 保护 shadow 期统计（进程内，重启清零——shadow 期读数看日志聚合即可）。
var (
	ltcShadowMu          sync.Mutex
	ltcShadowWouldRefuse = map[service.LTCStage]int{}
)

// LTCShadowWouldRefuse 返回 shadow 期"本应拒绝"的累计请求数（副本）。
func LTCShadowWouldRefuse() map[service.LTCStage]int {
	ltcShadowMu.Lock()
	defer ltcShadowMu.Unlock()
	out := make(map[service.LTCStage]int, len(ltcShadowWouldRefuse))
	for k, v := range ltcShadowWouldRefuse {
		out[k] = v
	}
	return out
}

// LTCStageGateShadow 生成一道 **shadow 态**的阶段闸门（E-7 推荐第 1 步，2026-10-23）。
// 与 LTCStageGate 的差别只有一个：阶段未启用时**不拦截、不 abort**，请求照常进
// handler，只做两件事——
//  1. 打一条 warn 结构化日志（event=ltc_gate_shadow，含 stage/reason），shadow 期
//     据此统计"本应拒绝"的比例与原因（规划 §4.7 验收第 1 条）；
//  2. 累计进 LTCShadowWouldRefuse()，调试端点可读。
//
// 转强制的路径：观察一周后把挂载点从 LTCStageGateShadow 换成 LTCStageGate 即可，
// 判定逻辑（StageActive + reason）两态完全共用，切换只改一行装配。
func LTCStageGateShadow(stage service.LTCStage) gin.HandlerFunc {
	ltcGuardMu.Lock()
	ltcGuardedBy[stage]++
	ltcGuardMu.Unlock()

	return ltcStageGateShadow(stage, service.GlobalLTCConfig())
}

func ltcStageGateShadow(stage service.LTCStage, reader LTCConfigReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := reader.Config(c.Request.Context())
		active, reason := cfg.StageActive(stage)
		if active {
			c.Next()
			return
		}
		ltcShadowMu.Lock()
		ltcShadowWouldRefuse[stage]++
		ltcShadowMu.Unlock()
		logger.Ctx(c.Request.Context()).Warn().
			Str("event", "ltc_gate_shadow").
			Str("stage", string(stage)).
			Str("reason", reason).
			Bool("master_enabled", cfg.Enabled).
			Bool("degraded", cfg.Degraded).
			Str("path", c.Request.URL.Path).
			Msg("LTC shadow：本应拒绝的请求（只记录不阻断，转强制见 ltc_gate.go 头注）")
		c.Next()
	}
}

// GatedGroup 返回挂了**强制**阶段门的子路由组（E-7 装配用）。
// 不用 auth.Use —— 那会波及之后注册在 auth 上的所有路由；子组的作用域才是闸门应有的。
//
// 2026-10-25 由 shadow 转强制（D-1，用户强制授权）：阶段关着 ⇒ 409 拒绝，
// 不再"只记录不阻断"。观察期语义（含 would-refuse 计数）保留在
// LTCStageGateShadow，历史对照读数不受影响。注意 A 型闸没有 whitelist
// 中间档（D-1 执行包 §2.1）：灰度手段是 ltc 配置逐阶段开关，不是档位。
func GatedGroup(auth *gin.RouterGroup, stage service.LTCStage) *gin.RouterGroup {
	return auth.Group("", LTCStageGate(stage))
}

func ltcStageGate(stage service.LTCStage, reader LTCConfigReader) gin.HandlerFunc {
	return func(c *gin.Context) {
		cfg := reader.Config(c.Request.Context())
		active, reason := cfg.StageActive(stage)
		if active {
			c.Next()
			return
		}

		data := gin.H{
			"stage":          string(stage),
			"reason":         reason,
			"master_enabled": cfg.Enabled,
			"degraded":       cfg.Degraded,
			"config_source":  cfg.Source,
		}
		if cfg.Degraded {
			data["degrade_reason"] = cfg.DegradeReason
		}
		// D-1 7×24h 观察窗（P-1）：强制门此前只回响应、不留日志，
		// 导致「按 stage+reason 计量」在生产上无从取数。这里补一条结构化 warn，
		// 与 shadow 的 event 区分开（shadow 是"本应拒绝"，这里是"已拒绝"）。
		logger.Ctx(c.Request.Context()).Warn().
			Str("event", "ltc_gate_refused").
			Str("stage", string(stage)).
			Str("reason", reason).
			Bool("master_enabled", cfg.Enabled).
			Bool("degraded", cfg.Degraded).
			Str("path", c.Request.URL.Path).
			Msg("LTC 阶段门拒绝请求（已阻断，无外发/写库副作用）")
		code := ltcRefusalCode(reason)
		response.Error(c, code, ltcRefusalMessage(stage, reason), data)
		c.Abort()
	}
}

// ltcRefusalMessage 把"拦在哪、为什么、有没有副作用"一次说完整。
// 文案必须随 reason 变：degraded 时说"未启用"，运维会去问运营为什么把功能关了，
// 而运营那边什么都没动 —— 这种对话在事故里是纯粹的损耗（与上面分码同一个理由）。
func ltcRefusalMessage(stage service.LTCStage, reason string) string {
	const suffix = "：请求没有进入业务处理，未产生外发或写库副作用"
	switch reason {
	case service.LTCReasonDegraded:
		return fmt.Sprintf("LTC 阶段 %s 的配置读不动，已按「关闭」拦下%s", stage, suffix)
	case service.LTCReasonUnknownStage:
		return fmt.Sprintf("LTC 阶段 %q 未注册（装配错误，改配置不会放行）%s", string(stage), suffix)
	default:
		return fmt.Sprintf("LTC 阶段 %s 未启用（%s）%s", stage, reason, suffix)
	}
}

// ltcRefusalCode 把拒绝原因映射为域内错误码。
//
// 不用 response.Error(c, 409, …)：HTTP 派生出来的码是 DUPLICATE_ENTRY_3003
// （「重复的记录」），而闸门拦下时既没写库也没撞唯一键——码会在讲一件根本没
// 发生的事故，前端若按码分流就会把"功能没开"引去查重复提交。
// master_off 与 stage_off 故意共用一个码：两件事的修法都是"去 LTC 配置页点开关"。
func ltcRefusalCode(reason string) utils.ErrorCode {
	switch reason {
	case service.LTCReasonDegraded:
		return utils.ErrorCodeLTCConfigDegraded
	case service.LTCReasonUnknownStage:
		return utils.ErrorCodeLTCStageUnknown
	default:
		return utils.ErrorCodeLTCStageDisabled
	}
}
