// order_draft_routes.go 订单草稿竖的观察端点（新规划任务清单 T-P2-06 ④）。
//
// 为什么要有这条路由：T-P2-01 到 T-P2-06 之间，这条竖的全部对外可观察面就是
// "函数存在、单测通过"。灰度期最容易被读反的一句话是"草稿已经落库了" ——
// 而 shadow 档库里确实有行，权威那份却仍是内存，两句话在响应里必须同时看得见。
// 与 /agent/tools/circuit、/agent/tools/approval 同一口径：把接线状态本身做成可读的。
package router

import (
	"context"

	"hivemtk-user/internal/app"
	"hivemtk-user/internal/pkg/utils/response"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func setupOrderDraftRoutes(auth *gin.RouterGroup) {
	auth.GET("/agent/order-drafts/stats", handleOrderDraftStats)
}

// orderDraftStatsPayload 把快照渲染成响应体。
//
// 抽成纯函数的原因与同包 approvalStatePayload 一样：旗子档位是装配期读一次的，测试进程
// 里没有第二次装配也没有 HTTP，走 handle 只能测到 off 那一支。
//
// 计数与"读没读到"必须分开表达：counts 为 null = 本进程压根没读（旗子关着），
// 为 {} = 读了且真的一条都没有。两者都渲染成 {} 等于拿一次装配缺失去支撑
// "现在没有待处理草稿"这句业务结论。
func orderDraftStatsPayload(snap app.OrderDraftSnapshot) gin.H {
	out := gin.H{
		"mode":              snap.Mode,
		"assembled":         snap.Assembled,
		"store":             snap.Store,
		"durable":           snap.Durable,
		"producer_attached": snap.ProducerAttached,
		"counts":            nil,
		"env_hint": app.OrderDraftFlagEnv + "=off|shadow|on（off=不装配（默认）；shadow=读走内存、写镜像进库，" +
			"durable 仍为 false；on=权威副本进库。档位只在装配期读一次，改完须重启）",
	}
	// 多条告警拼在一个 key 上而不是互相覆盖：store=memory 且生产者没挂是同时成立的
	// 两种故障（没拿到句柄 + 装配顺序不对），后写的把先写的顶掉就等于少报一条。
	warn := func(msg string) {
		if prev, ok := out["warning"].(string); ok && prev != "" {
			out["warning"] = prev + "；" + msg
			return
		}
		out["warning"] = msg
	}
	if !snap.Assembled {
		// sweep 整个字段留 null 而不是给一份空节拍：interval="" / rounds=0 读起来像
		// "有清扫器但还没跑过"，而事实是本进程压根没装。
		out["note"] = "本进程未装配草稿运行时（旗子关着或值无法识别）⇒ 没有发生任何读取，counts 为 null 而不是 0；" +
			"此时 order_drafts 没有写入方，到期草稿也无人清扫"
		return out
	}
	out["counts"] = snap.Counts
	out["sweep"] = gin.H{
		"interval": snap.SweepInterval,
		"running":  snap.SweepRunning,
		"rounds":   snap.SweepRounds,
		"last":     snap.SweepLast,
	}
	if !snap.ProducerAttached {
		warn("运行时已装配但编排器没挂生产者 ⇒ 不会有新草稿产生（AI 回复不建草稿），查装配顺序")
	}

	switch snap.Store {
	case service.DraftStoreKindShadow:
		if snap.Mirror != nil {
			out["mirror"] = gin.H{
				"available":  snap.Mirror.Available,
				"row_counts": snap.Mirror.RowCounts,
				"read_error": snap.Mirror.ReadError,
				"failures":   snap.Mirror.Failures,
				"last_error": snap.Mirror.LastError,
			}
			switch {
			case !snap.Mirror.Available:
				out["mirror_warning"] = "shadow 档却没拿到 DB 句柄 ⇒ 已退回纯内存，没有对照数据可看"
			case snap.Mirror.Failures > 0:
				out["mirror_warning"] = "镜像写有失败计数 ⇒ 库侧行数可能少于内存侧，两个计数对不上是这个原因，不是丢单"
			case snap.Mirror.ReadError != "":
				out["mirror_warning"] = "库侧计数读不动 ⇒ 本次只回内存侧计数，mirror.row_counts 为 null"
			}
			if snap.Durable {
				// 走到这里说明底座与 durable 自相矛盾（影子的 durable 恒为 false）。
				// 不静默：本卡的核心断言就是"影子期不许声称重启不丢"。
				out["invariant_violation"] = "store=shadow 但 Durable()=true ⇒ 与影子底座的定义冲突，端点读数不可信"
			}
		}
	case service.DraftStoreKindDB:
		if !snap.Durable {
			out["warning_db_unavailable"] = "store=db 但 Durable()=false ⇒ 仓库句柄不可用，读到的是空库而非「没有草稿」"
		}
	case service.DraftStoreKindMemory:
		// mode=on 却拿到内存底座：InitOrderDraftRuntime 已在启动日志里喊过，这里让它在
		// 响应里同样看得见 —— 运维不会翻启动日志，但一定会打这个端点。
		warn("mode=" + snap.Mode + " 而底座是 memory ⇒ 没拿到 DB 句柄，草稿仍会重启即丢")
	}
	return out
}

func handleOrderDraftStats(c *gin.Context) {
	snap := app.GetOrderDraftSnapshot(context.Background())

	if snap.Assembled && snap.CountsError != "" {
		// 装配在位却读不到计数 ⇒ 503，不回空计数（AC⑤）。口径与 T-P1-08 的审计读侧一致：
		// "0 张草稿"是一句业务结论，拿一次查询故障去支撑它，等于让运维以为一切正常。
		response.Error(c, 503, "草稿计数读取失败（运行时在位但底座读不动）: "+snap.CountsError)
		return
	}
	response.Success(c, orderDraftStatsPayload(snap), "ok")
}
