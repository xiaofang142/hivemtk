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

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func setupLTCRoutes(auth *gin.RouterGroup) {
	admin := auth.Group("/manage/ltc", middleware.AdminAuthMiddleware())
	admin.GET("/config", handleLTCConfigGet)
	admin.PUT("/config", handleLTCConfigPut)
	// T-P7-03：催收腿的运行时读数。与上一条同一个组 ⇒ 同一把管理员闸门。
	admin.GET("/collection", handleCollectionStatusGet)
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

// ---- 催收腿的运行时读数（T-P7-03）-------------------------------------------
//
// 为什么这条腿要单开一个读数面，而挽回 worker 没有：催收是回款域里第一条**主动发消息给
// 客户**的腿，它的六种"没动"在响应里全都长成 reminded_total 停在 0，而排查方向两两相反。
// /manage/ltc/config 回答的是"配成了什么"（运营写入的那份文档），这里回答的是
// "这一进程里那条腿现在到底在不在跑、为什么一条都不催" —— 后者只有装配后的运行时知道。

// blocker 键值。字符串是 API 契约的一部分（前端与运维手册都按它检索），改名等于改接口。
const (
	CollectionBlockerNotAssembled      = "not_assembled"
	CollectionBlockerDependencyMissing = "dependency_missing"
	CollectionBlockerModeOff           = "mode_off"
	CollectionBlockerNotRunning        = "not_running"
	CollectionBlockerStageOff          = "stage_off"
	CollectionBlockerShadowOnly        = "shadow_only"
)

// collectionStatusView 把快照渲染成响应体，并在渲染的这一刻判出"第一处断掉的地方"。
//
// 判据抽成纯函数（与 ltcConfigView 同一理由）：这些分支在真实装配里几乎凑不出来 ——
// 要凑一个"旗子 enforce、阶段开着、协程却没起"的进程，得让 Start 半途失败。
// 走 handler 只能测到最顺的那一支，而那一支恰恰是最不需要排障的一支。
//
// mode 那两格比的是 `string(service.RecoveryWorkerMode…)` 而不是字面量："这一档会不会真发"
// 这件事在 service 那一格里只有一个答案，视图里再抄一遍字符串就是第二个答案 ——
// 与旗子复用 parseRecoveryWorkerMode 同一条理由（改一边漏一边是必然）。
func collectionStatusView(snap app.CollectionSnapshot) gin.H {
	blocker, note := "", ""
	switch {
	case !snap.Assembled:
		blocker, note = CollectionBlockerNotAssembled,
			"本进程没装催收腿 ⇒ 逾期应收既不会被提醒也不会升级人工。先看启动日志里 [collection] 那一句，"+
				"再确认 DB 句柄与装配调用（它排在 router.Setup 之后）"
	case !snap.Available:
		blocker, note = CollectionBlockerDependencyMissing,
			"装配了但五条依赖不齐（账单读口/商机读口/触达出口/待办投递/阶段开关）⇒ 每轮在第一格退出。"+
				"这是装配问题，不是配置问题：开旗子不会有任何变化"
	case snap.Mode == string(service.RecoveryWorkerModeOff):
		blocker, note = CollectionBlockerModeOff,
			"旗子是 off ⇒ 协程没起，逾期单既不会被提醒也不会升级人工。先把 "+snap.FlagEnv+
				"=shadow 跑一轮观察（shadow 只 DryRun、不外发），看清 would_remind/would_escalate 之后再谈 enforce"
	case !snap.Running:
		blocker, note = CollectionBlockerNotRunning,
			"旗子是 "+snap.Mode+" 而协程没在跑 ⇒ 这一档本该有动静却没动。看启动日志里 [CollectionJob] 那几行，"+
				"并确认没人调过 Stop（优雅关停会调）"
	case !snap.StageOn:
		blocker, note = CollectionBlockerStageOff,
			"旗子开着但 ltc.config 的 collection 阶段没放行（reason="+snap.StageReason+"）⇒ 连扫描都不发。"+
				"这是第二把锁，改它要在 /manage/ltc/config 整体写回"
	case snap.Mode == string(service.RecoveryWorkerModeShadow):
		blocker, note = CollectionBlockerShadowOnly,
			"shadow 档：每轮都跑、每单都问出口，但触达服务收到的是 DryRun ⇒ 客户一条都收不到，"+
				"待办也不会投。看 last.would_remind / last.would_escalate 判断要不要放量"
	}

	v := gin.H{
		"assembled":     snap.Assembled,
		"mode":          snap.Mode,
		"running":       snap.Running,
		"available":     snap.Available,
		"blocker":       blocker,
		"will_send_now": blocker == "",
		"flag_env":      snap.FlagEnv,
		"batch_env":     snap.BatchEnv,
		"interval_env":  snap.IntervalEnv,
		"batch":         snap.Batch,
		"interval":      snap.Interval,
		// 口径四格：不走 env、不可运行时改，所以这个端点是它们唯一的对外读数。
		"grace_days":          snap.GraceDays,
		"escalate_after_days": snap.EscalateAfterDays,
		"remind_window":       snap.RemindWindow,
		"escalate_window":     snap.EscalateWindow,
		"stage_on":            snap.StageOn,
		"reminded_total":      snap.RemindedTotal,
		"escalated_total":     snap.EscalatedTotal,
		"reach_gate_checked":  snap.ReachGateChecked,
		"reach_gated":         snap.ReachGated,
	}
	if note != "" {
		v["blocker_note"] = note
	}
	if snap.StageReason != "" {
		v["stage_reason"] = snap.StageReason
	}
	if snap.Last != nil {
		v["last"] = snap.Last
	}
	if snap.ReachGateNote != "" {
		v["reach_gate_note"] = snap.ReachGateNote
	}
	if snap.UnassembledHint != "" {
		v["unassembled_hint"] = snap.UnassembledHint
	}
	return v
}

// handleCollectionStatusGet 催收腿读数。未装配也回 200 —— "不在"就是这条读数的答案。
//
// @Summary  催收腿运行时状态
// @Description 逾期扫描任务当前的档位、节奏、口径（宽限期/升级线/两把频控窗）、跨轮累计与最近一轮读数。
// @Description 账单金额与账期都不在这里改写：这里是纯读，写只有 POST /api/bill 与订单 webhook。
// @Tags     LTC
// @Produce  json
// @Success  200 {object} response.Response
// @Router   /api/manage/ltc/collection [get]
// @x-Permissions [] "admin"
func handleCollectionStatusGet(c *gin.Context) {
	response.Success(c, collectionStatusView(app.GetCollectionSnapshot(c.Request.Context())), "ok")
}
