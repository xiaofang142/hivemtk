// order_draft_wiring.go 订单草稿竖的装配层（新规划任务清单 T-P2-06）。
//
// 一句话职责：T-P2-01 交付的三副底座（memory/db/shadow）+ T-P2-06 交付的定时清扫与
// 生产入口，在这里按一把旗子挂上运行时；在此之前整条竖全仓非测试构造点为 0
// （NewOrderDraftService* 无人 new、SalesWorkbenchService 零引用、TriggerAfterSales 无
// 生产调用方、ExpireOverdue/PurgeTerminal 零调用方），所以 order_drafts 这张表今天
// 一行都不会被生产路径写。判定这些事实的正是项8 那行 unwired 登记。
//
// 三态语义（顺序 = 风险递增，默认停在第一档）：
//
//	off（默认）  不装配任何东西：没有草稿服务、没有清扫协程、编排器不挂生产者。
//	             与 T-P2-01 交付态逐字节一致（AC③ 的断言对象）。
//	shadow     新建走影子底座：读一律走内存（对调用方零行为变化），写同时镜像进库，
//	             于是 order_drafts 里有真实行可与内存并排对照，而 Durable() 仍是 false。
//	on         新建走 DB 底座：权威副本进库，重启不丢、多副本共享同一份。
//
// 为什么"on 只影响新建"：底座选择发生在构造期，存量进程里已有的内存草稿不会凭空搬进
// 库。所以从 shadow 切到 on 时，库里只有切档之后被写过的那些行，观察端点上两个计数
// 必然对不上一次 —— 这是切档的已知代价，不是丢数据（本卡不做事后搬运，见卡面执行结果）。
//
// 与 FF_LTC_KB_CANARY / FF_TOOL_CIRCUIT_BREAKER 同一取舍：布尔式真值（true/1/yes/on）
// 只到 shadow。一把能让业务数据换存储介质的旗子，不该因为有人按习惯写了 `=true`
// 就直接拿到"权威副本进库"的能力；要进库必须显式写 on。认不出的值判 off 并告警。
//
// 五层归属：旗子读取 + 依赖拼装在本文件；底座选择、影子镜像、清扫节拍的业务判据
// 全在 service（order_draft_store.go / order_draft_sweep.go）。
package app

import (
	"context"
	"os"
	"strconv"
	"strings"
	"sync"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// OrderDraftFlagEnv 草稿持久化总开关。端点回显与运维口径都用这个常量，
// 免得文档写一个名字、代码读另一个。
const OrderDraftFlagEnv = "FF_LTC_ORDER_DRAFT_DB"

type orderDraftMode string

const (
	orderDraftOff    orderDraftMode = "off"
	orderDraftShadow orderDraftMode = "shadow"
	orderDraftOn     orderDraftMode = "on"
)

// parseOrderDraftMode 解析开关值（三态 + 真值降档，见文件头）。
func parseOrderDraftMode(raw string) orderDraftMode {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "":
		return orderDraftOff
	case "off", "false", "0", "no", "n", "none", "disabled":
		return orderDraftOff
	case "shadow", "observe", "watch", "mirror", "log":
		return orderDraftShadow
	case "on", "enforce", "active", "durable":
		return orderDraftOn
	}
	if b, err := strconv.ParseBool(v); err == nil {
		if b {
			return orderDraftShadow
		}
		return orderDraftOff
	}
	switch v {
	case "yes", "y":
		return orderDraftShadow
	}
	logger.Warnf("[order-draft] %s=%q 无法识别 ⇒ 按 off 处理（草稿不装配，仍是内存态且无人清扫）；可用值：off|shadow|on",
		OrderDraftFlagEnv, raw)
	return orderDraftOff
}

// OrderDraftStoreNone 未装配时的 store 回显值。
//
// 与三副底座的名字并列而不留空字符串：空串会被端点读成"字段没填"，
// 而"这个进程没有底座"是一个需要被明确说出来的状态。
const OrderDraftStoreNone = "none"

// OrderDraftRuntime 本进程的草稿运行时（服务 + 提取器 + 清扫 worker）。
type OrderDraftRuntime struct {
	svc       *service.OrderDraftService
	extractor *service.OrderIntentExtractor
	sweeper   *service.OrderDraftSweepWorker
	mode      orderDraftMode

	// producerAttached 编排器上到底挂没挂生产者。与 Assembled 分开记，是因为它们是
	// 两个独立的失败面：运行时装配在 Init 里，挂生产者在后一步。只回"运行时在"的话，
	// "装配了但没人调"这个本卡开工前的原病灶就会被端点说成已修好。
	producerAttached bool
}

// Mode 生效档位（off 时运行时根本不存在，故本方法只在已装配实例上调用）。
func (r *OrderDraftRuntime) Mode() string { return string(r.mode) }

// 全局运行时的锁口径：写入发生在 router.Setup()（HTTP 尚未开始收流量），读发生在收流量
// 之后与测试里。用锁而不是"约定先写后读"，是因为 Init 可被重复调用（见其文档），
// 而重复调用时端点的读侧协程已经在跑了。
var (
	orderDraftMu      sync.RWMutex
	orderDraftRuntime *OrderDraftRuntime
)

// currentOrderDraftRuntime 读全局运行时；未装配（旗子 off / 值无法识别）返回 nil。
func currentOrderDraftRuntime() *OrderDraftRuntime {
	orderDraftMu.RLock()
	defer orderDraftMu.RUnlock()
	return orderDraftRuntime
}

// InitOrderDraftRuntime 按旗子装配草稿运行时；off 档返回 nil（并出声）。
//
// 先停旧的再装新的，可重复调用。不是防御性洁癖：
//   - 生产里 router.Setup 只跑一次，但测试进程里多个用例各自 Setup/Setup 两遍的话，
//     只装不停会攒出一堆并发清扫器，彼此抢同一批到期草稿（SKIP LOCKED 让它们各拿一份，
//     于是没有任何一轮看到全量，而每轮日志都自称"扫完了"）；
//   - off 分支也必须走到这一步：测试用 t.Setenv 把旗子切回 off 时，上一份的协程
//     不能留在进程里继续扫。
func InitOrderDraftRuntime(db *gorm.DB) *OrderDraftRuntime {
	mode := parseOrderDraftMode(os.Getenv(OrderDraftFlagEnv))

	orderDraftMu.Lock()
	prev := orderDraftRuntime
	orderDraftRuntime = nil
	orderDraftMu.Unlock()
	if prev != nil && prev.sweeper != nil {
		prev.sweeper.Stop(context.Background())
	}

	if mode == orderDraftOff {
		logger.Infof("[order-draft] %s=off ⇒ 不装配草稿运行时：order_drafts 无写入方、到期草稿无人清扫"+
			"（与 T-P2-01 交付态一致）", OrderDraftFlagEnv)
		return nil
	}

	cfg := &service.OrderDraftConfig{}
	var svc *service.OrderDraftService
	switch mode {
	case orderDraftOn:
		svc = service.NewOrderDraftServiceWithDB(cfg, db)
	case orderDraftShadow:
		svc = service.NewOrderDraftServiceShadow(cfg, db)
	}

	rt := &OrderDraftRuntime{
		svc:       svc,
		extractor: service.NewOrderIntentExtractor(),
		mode:      mode,
	}
	// 清扫 worker 两档都装：ExpireOverdue 在内存底座上同样要跑 ——
	// "7 天未确认自动过期"这条口径此前只存在于注释里，从来没有调用方。
	rt.sweeper = service.NewOrderDraftSweepWorker(svc, service.DefaultOrderDraftSweepInterval, 0)
	rt.sweeper.Start(context.Background())

	orderDraftMu.Lock()
	orderDraftRuntime = rt
	orderDraftMu.Unlock()

	logger.Infof("[order-draft] ✅ 草稿运行时已装配：mode=%s store=%s durable=%t（开关 %s；"+
		"shadow 读走内存并镜像进库、Durable 仍为 false，on 才把权威副本交给库）",
		mode, svc.StoreKind(), svc.Durable(), OrderDraftFlagEnv)
	if mode == orderDraftShadow {
		logger.Infof("[order-draft] ⚠️ shadow 态：库里行数只用于对照，**重启仍会丢**；" +
			"转 on 前先核对 /api/agent/order-drafts/stats 的 counts 与 mirror.row_counts")
	}
	if mode == orderDraftOn && !svc.Durable() {
		// 这一条必须单独喊：mode=on 却拿到内存底座 = 运维以为已经重启不丢了。
		logger.Warnf("[order-draft] ❌ mode=on 但底座退回内存（未拿到 DB 句柄）⇒ 草稿仍会重启即丢，查启动日志里的装配告警")
	}
	return rt
}

// StopOrderDraftRuntime 停掉清扫协程并清空全局引用（幂等）。
//
// 为什么现在没有调用方而不是"留个 TODO"：优雅停机的正解位置是 cmd/api/main.go 的
// 退出序列（与 InitRecoveryWorker 返回的 worker 同一个坑位），而本卡的改动范围不含
// main.go（原因写在 attachOrderDraftProducer 的注释里，不是偏好）。入口先备好，
// 那一行接得上；没有它，进程退出时清扫协程就是无人回收的泄漏。
func StopOrderDraftRuntime() {
	orderDraftMu.Lock()
	rt := orderDraftRuntime
	orderDraftRuntime = nil
	orderDraftMu.Unlock()
	if rt != nil && rt.sweeper != nil {
		rt.sweeper.Stop(context.Background())
		logger.Infof("[order-draft] 清扫 worker 已停止（轮次=%d）", rt.sweeper.Rounds())
	}
}

// attachOrderDraftProducer 把"AI 响应 → 意向提取 → 建草稿"挂到编排器上。
//
// 调用方：BuildSmartOrchestrator（internal/app/sales_engine_factory.go）。
// 为什么挂在那儿而不是 cmd/api/main.go：
//   - 本卡的改动集刻意不含 main.go —— 它与 BuildSalesEngine 同属并行会话正在改的文件
//     （git status 里它是 unstaged），把它 staged 进本卡提交会连带把别人的未提交工作
//     一起提交进去，那是拿别人的 WIP 换我这卡的装配位置；
//   - BuildSmartOrchestrator 是这条竖**唯一**的编排器构造点（实测：router.go 一处调用，
//     gormDB 形参现成），拿 DB 句柄不需要新增参数，装配时机与旗子读取都正好在
//     HTTP 收流量之前。
//
// 运行时为 nil（旗子 off）时什么都不挂：与"挂了一个什么都不做的生产者"不同，
// 前者是可断言的零改动（AC③ 靠这个形状）。
func attachOrderDraftProducer(o *service.SmartCSOrchestrator, rt *OrderDraftRuntime) bool {
	if o == nil || rt == nil {
		return false
	}
	o.SetOrderDraftProducer(orderDraftProduceFunc(rt))
	orderDraftMu.Lock()
	rt.producerAttached = true
	orderDraftMu.Unlock()
	logger.Info("[order-draft] ✅ 编排器已挂订单草稿生产者（AI 回复后提取意向建草稿）")
	return true
}

// orderDraftProduceFunc 构造要挂到编排器上的那个闭包。
//
// 单列出来的唯一理由是"测的那份就是生产用的那份"：内联在 attach 里的话，集成测试要么
// 去调 rt.svc 的某个方法（那是另一条代码路径，测过了也不证明闭包正确），要么没法测。
// 现在闭包的构造与注入共用这一个口。
func orderDraftProduceFunc(rt *OrderDraftRuntime) func(context.Context, string, string, *service.SalesResponse) {
	return func(ctx context.Context, customerID, ownerID string, resp *service.SalesResponse) {
		rt.svc.CreateDraftsFromSalesResponse(ctx, rt.extractor, customerID, ownerID, resp)
	}
}

// OrderDraftSnapshot 观察端点的输入（app 层快照，router 只负责渲染）。
//
// 为什么一次读这么多件：端点要回答的问题不是"有几张草稿"，而是"这个进程的草稿竖
// 现在到底是什么状态"。少了 mode/store/durable，运维就只能从"有计数"反推"已落库"，
// 而 shadow 档恰恰是"有库里的行、但权威副本不在这儿"的那种状态。
type OrderDraftSnapshot struct {
	// Mode 生效档位。已装配时 = **装配期**读到的那一次（改旗须重启，与熔断/审批门同一
	// 口径）；未装配时只能读当前 env —— 那正是"为什么没装配"的答案。
	Mode string
	// Assembled false = 旗子关着（或值无法识别），本进程没有草稿运行时。
	Assembled bool

	Store   string
	Durable bool
	// Counts 当前底座的各状态计数；读失败时 nil，配合 CountsError 看。
	Counts      map[string]int64
	CountsError string

	// Mirror 影子档的对照读数（库侧行数 + 镜像失败计数）；非影子档为 nil。
	Mirror *service.OrderDraftMirrorStatus

	ProducerAttached bool

	SweepInterval string
	SweepRunning  bool
	SweepRounds   int64
	SweepLast     *service.OrderDraftSweepReport
}

// GetOrderDraftSnapshot 读当前草稿运行时状态。端点在 off 档也要能答上话，
// 所以这里永不返回 nil，只返回 Assembled=false 的快照。
func GetOrderDraftSnapshot(ctx context.Context) OrderDraftSnapshot {
	orderDraftMu.RLock()
	rt := orderDraftRuntime
	var attached bool
	if rt != nil {
		attached = rt.producerAttached
	}
	orderDraftMu.RUnlock()

	snap := OrderDraftSnapshot{Store: OrderDraftStoreNone, ProducerAttached: attached}
	if rt == nil {
		snap.Mode = string(parseOrderDraftMode(os.Getenv(OrderDraftFlagEnv)))
		return snap
	}
	snap.Mode = rt.Mode()
	snap.Assembled = true
	snap.Store = rt.svc.StoreKind()
	snap.Durable = rt.svc.Durable()

	counts, err := rt.svc.DraftStatusCounts(ctx)
	if err != nil {
		snap.CountsError = err.Error()
	} else {
		snap.Counts = counts
	}
	if m, ok := rt.svc.MirrorStatus(ctx); ok {
		snap.Mirror = m
	}
	if rt.sweeper != nil {
		snap.SweepInterval = rt.sweeper.Interval().String()
		snap.SweepRunning = rt.sweeper.Running()
		snap.SweepRounds = rt.sweeper.Rounds()
		snap.SweepLast = rt.sweeper.LastReport()
	}
	return snap
}
