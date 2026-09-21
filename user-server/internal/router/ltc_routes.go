// ltc_routes.go —— LTC-25「运营一键开启」的管理端点（新规划任务清单 T-P3-06）。
//
// 与 /agent/tools/circuit、/agent/order-drafts/stats 同一口径：把"接线状态本身"做成可读的。
// 本卡交付的是开关的落点与判据，不是已经生效的开关面 —— 六阶段一条业务路由都还没建
// （P4~P7 才逐段接入），所以 GET 的响应里必须同时看得见"配成什么样"和"有几条路由归它管"。
// 只看前半句会把"我把开关全打开了"读成"现网行为变了"，而后半句才是那件事的证据。
//
// 为什么这两个端点挂在 AdminAuthMiddleware 之后：`/manage/config-params` 那族参数写入口
// 今天只要求"任意登录用户"（已登记为遗留项），本卡不重复那个口径 ——
// 能改"整条 LTC 是否对外发东西"的那个按钮，不该是所有登录态都点得动的。
package router

import (
	"errors"
	"io"
	"net/http"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func setupLTCRoutes(auth *gin.RouterGroup) {
	admin := auth.Group("/manage/ltc", middleware.AdminAuthMiddleware())
	admin.GET("/config", handleLTCConfigGet)
	admin.PUT("/config", handleLTCConfigPut)
}

// ltcConfigView 把"读到的那份配置"渲染成响应体。
//
// 抽成纯函数是因为端点侧的分支（degraded / 挂载数为 0）在真实装配里很难凑出来，
// 走 handler 只能测到最顺的那一支。
func ltcConfigView(cfg *service.LTCConfig, guarded map[service.LTCStage]int) gin.H {
	stages := make(map[string]bool, len(service.LTCKnownStages))
	stageStatus := make([]gin.H, 0, len(service.LTCKnownStages))
	guardedRoutes := make(map[string]int, len(service.LTCKnownStages))
	guardedTotal := 0
	for _, st := range service.LTCKnownStages {
		on, _ := cfg.StagesEnabled.Get(st)
		stages[string(st)] = on
		active, reason := cfg.StageActive(st)
		stageStatus = append(stageStatus, gin.H{"stage": string(st), "active": active, "reason": reason})
		n := guarded[st]
		guardedRoutes[string(st)] = n
		guardedTotal += n
	}

	view := gin.H{
		"enabled":        cfg.Enabled,
		"stages_enabled": stages,
		"stage_status":   stageStatus,
		"thresholds":     cfg.Thresholds,
		// T-P5-04：durable 放量档。整份配置是"读回来再整体写回"的形状，
		// 这一节不在 GET 里出现，改档位就会顺手把灰度名单清空。
		// 这里只回答"配成了什么"；"现在到底拦不拦"在 /agent/tools/reach-gate（它才知道旗子）。
		"reach_rollout": gin.H{
			"mode":              cfg.ReachRollout.Mode,
			"whitelist":         cfg.ReachRollout.Whitelist,
			"whitelist_entries": len(cfg.ReachRollout.Whitelist),
		},
		"source":            cfg.Source,
		"degraded":          cfg.Degraded,
		"guarded_routes":    guardedRoutes,
		"guarded_total":     guardedTotal,
		"reading_hints":     cfg.ReadingHints(guarded),
		"cache_ttl_seconds": int(service.LTCConfigCacheTTL / time.Second),
		"kv_key":            service.LTCConfigKVKey,
		"max_bytes":         service.LTCConfigMaxBytes,
		"threshold_ranges": gin.H{
			"lead_score":       "1~100（0 等于拆掉这道闸门，C5 要求它与 confidence 各自独立）",
			"confidence":       "0.001~1",
			"discount_percent": "0~100（0 是合法的，含义是「任何折扣都要审批」）",
			"win_probability":  "0.001~1",
		},
		// 同一批数字的机器可读那份：管理端输入框照它渲染，不再自己写一份范围。
		// 两份数字并存迟早会各说各话，而且漂移的那一侧永远是"表单允许、后端拒收"。
		"threshold_bounds": service.LTCKnownThresholdBounds(),
	}
	if cfg.Degraded {
		view["degrade_reason"] = cfg.DegradeReason
	}
	return view
}

func handleLTCConfigGet(c *gin.Context) {
	svc := service.GlobalLTCConfig()
	response.Success(c, ltcConfigView(svc.Config(c.Request.Context()), middleware.LTCGuardedRoutes()), "ok")
}

func handleLTCConfigPut(c *gin.Context) {
	// LimitReader 多留 1 字节：正好到上限的请求要被判成"超了"，
	// 而读满就截断会让一份被切掉尾巴的 JSON 以"格式错误"的面目回来。
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, service.LTCConfigMaxBytes+1))
	if err != nil {
		response.Error(c, http.StatusBadRequest, "读取请求体失败: "+err.Error())
		return
	}
	if len(body) > service.LTCConfigMaxBytes {
		response.Error(c, http.StatusRequestEntityTooLarge,
			"ltc.config 超过 8KB 上限：整份策略是一个原子文档，不该被拆着写")
		return
	}

	cfg, perr := service.ParseLTCConfig(body)
	if perr != nil {
		// 拒收的措辞直接给到底层原因：这里拼的每一条都指向"哪一行写错了"，
		// 换成"参数错误"就等于让运营在六个阶段名里猜哪个拼错了。
		response.Error(c, http.StatusBadRequest, "ltc.config 未保存："+perr.Error())
		return
	}

	svc := service.GlobalLTCConfig()
	res, serr := svc.Save(c.Request.Context(), cfg, c.GetUint("user_id"))
	if serr != nil {
		if errors.Is(serr, service.ErrLTCStoreUnavailable) {
			response.Error(c, http.StatusServiceUnavailable,
				"ltc.config 未写入（存储不可用）："+serr.Error()+
					"；当前生效的仍是改动前那一份，闸门不会因此放行任何路由")
			return
		}
		response.Error(c, http.StatusBadRequest, "ltc.config 未保存："+serr.Error())
		return
	}

	out := gin.H{
		"persisted":     res.Persisted,
		"audit_written": res.AuditWritten,
		"stored_bytes":  res.StoredBytes,
		"stages_on":     res.StagesOn,
		"effective":     ltcConfigView(svc.Config(c.Request.Context()), middleware.LTCGuardedRoutes()),
	}
	if res.AuditError != "" {
		out["audit_error"] = res.AuditError
	}
	response.Success(c, out, "ltc.config 已更新")
}
