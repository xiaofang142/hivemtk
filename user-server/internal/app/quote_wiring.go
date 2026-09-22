// quote_wiring.go 报价竖两条腿的装配层（新规划任务清单 T-P6-02 生成 + T-P6-03 发送）。
//
// 一句话职责：把"有没有 DB 句柄"翻译成两个可注入的服务并登记到全局，让路由（现取全局，
// 决定回 200/202 还是 503）在同一时刻看到同一份。形状与 InitOpportunityRuntime 逐条相同，
// 只有两处是本竖特有的，都写在下面对应的注释里。
//
// 为什么必须在路由注册之前：路由挂的是 `controller.NewQuoteController(全局, 全局)`，
// 取到 nil 就是"端点在、底座不在"的那一档（全部回 503）。装配排在流量之后不会 panic，
// 只会留一个"看起来配好了而每一条都回 503"的窗口 —— 那是最难发现的一种失效。
//
// 为什么本竖不加旗子（与 T-P3-02 的审批运行时不同）：旗子守的是"改变既有时序或换存储介质"
// 的失败面；本卡写的是两张新表的新行，不改动任何既有读写路径。真正的关闸更彻底且已经在结构里：
// 不装配 ⇒ 两个全局为 nil ⇒ /api/quote/* 全部 503。业务侧那道总闸是 ltc.config 的 quote 阶段，
// 它关着时发送腿连一条审批都不会入队（顺序判据见 quote_send.go 文件头）。
//
// 与审批运行时的耦合（本文件最需要出声的一处）：裁决入口 `/api/approvals/:id/decide` 读的是
// **全局**审批服务，而那个服务只在 `FF_LTC_APPROVAL_RESUME` 不为 off 时才登记（T-P3-02）。
// 发送腿自己也要一个 Submit/Get 的门面。这里优先复用全局那一份；全局没有（旗子 off）时
// 就地构造一份只给发送腿用的 —— 构造而不复用，是因为"销售点了发送之后无处落 pending"比
// "批不了"更糟：前者是功能没起来，后者是功能起来了但流程缺一环。少的那一环必须在日志里点名，
// 否则运维会把它读成"审批没人批"，而事实是"没人能批"。
package app

import (
	"context"

	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// InitQuoteRuntime 装配报价的两条腿并登记为全局实例，可重复调用（后写覆盖）。
//
// 返回 true 的含义是"九个句柄都接上了、两条腿各自 Available()"，不是"能发出去东西"：
// 后者还取决于 ltc.config 的档位与运营配的模板/话术，那是业务闸门不是装配。
//
// db == nil 时必须把两个全局**一起清空**：Init 可被重复调用（测试与灰度重启都是真实路径），
// 只清一半会让另一条腿继续对着上一份实例回 200，而日志同时写着"未装配"。
func InitQuoteRuntime(db *gorm.DB) bool {
	if db == nil {
		service.SetGlobalQuoteService(nil)
		service.SetGlobalQuoteSendService(nil)
		logger.Warnf("[quote] ⚠️ 无 DB 句柄 ⇒ 报价两条腿都不装配：/api/quote/* 的生成、读回与发送全部回 503")
		return false
	}

	quoteRepo := repository.NewQuoteRepositoryWithDB(db)
	oppRepo := repository.NewOpportunityRepositoryWithDB(db)
	// 闸门取全局 LTC 配置服务（唯一一份缓存句柄）。它内部读的是 system_config_kv：
	// 那一份在**它自己**被装配的那一刻捕获全局句柄，与本函数的 db 是不是同一把，
	// 由装配顺序保证（router.Setup 里 gormDB 就是全局那句）。缓存与降级语义都在 T-P3-06。
	gate := service.GlobalLTCConfig()
	// 模板与话术指针走显式句柄：这两格读的是 system_config_kv，而版本行读的是 quotes 那张表，
	// 两句柄不同库的后果不是报错，是"用 A 库的价目表给 B 库的那一版报价出价"。
	kv := repository.NewSystemConfigKVRepositoryWithDB(db)
	scripts := quoteScriptPort(db, kv)

	gen := service.NewQuoteService(quoteRepo, oppRepo, gate)
	gen.SetConfigStore(kv)
	gen.SetScriptSource(scripts)
	service.SetGlobalQuoteService(gen)

	approvals, approvalsFromGlobal := quoteApprovalReader(db)
	send := service.NewQuoteSendService(
		quoteRepo, oppRepo, approvals,
		repository.NewApprovalRequestRepositoryWithDB(db),
		quoteReachSender(db),
		repository.NewSalesEventRepositoryWithDB(db),
		kv, scripts, gate,
	)
	service.SetGlobalQuoteSendService(send)

	// 启动日志刻意不写那张遗留 KV 表的表名：D12 守卫（config_param_guard_test.go）扫的是
	// 剥掉注释后的源码文本，字符串字面量也算 —— 写了不是"直查"，但会被判成直查。
	logger.Infof("[quote] ✅ 生成腿已装配：模板与话术指针走显式 KV 句柄，话术正文只取 script_versions 快照")
	if !approvalsFromGlobal {
		logger.Warnf("[quote] ⚠️ 审批运行时未装配（%s=off）⇒ 发送腿自带一个只用于入队/读结论的审批服务："+
			"pending 会正常落库、待办会正常投递，但 /api/approvals/* 回 503，**没人能在 HTTP 上裁决这一条**。"+
			"要出域须把该旗子开到 on|shadow 并重启", ApprovalResumeFlagEnv)
	}
	ok := gen.Available() && send.Available()
	if !ok {
		// 半装配比不装配更坏：不装配是清一色 503，一眼看得出来；半装配是"路由挂了、
		// 服务在、每条业务请求都失败"，而失败原因散在九个句柄里。所以这一条必须是 Warn 且点名。
		logger.Warnf("[quote] ❌ 两条腿已登记但 Available() 为假：生成=%t 发送=%t ⇒ 端点会回 503，检查上面哪一句装配没出声",
			gen.Available(), send.Available())
	}
	return ok
}

// quoteScriptPort 构造报价话术的生效版本读口（AC① 的那一侧）。
//
// AB 服务与话术源都就地构造：前者只为拿分桶（缺它不影响正文，见 NewQuoteScriptSource），
// 后者是**唯一**允许读 script_versions 快照给报价用的路径 —— 全局那一份（若存在）是给
// 话术工作台用的，语义是"编辑区"，与这里"只取已发布版本"不是一件事。
func quoteScriptPort(db *gorm.DB, kv repository.SystemConfigKVRepository) *service.QuoteScriptSource {
	repo := repository.NewScriptLibraryRepository(db)
	ab := service.NewScriptABService(repo)
	ab.SetKVStore(
		func(ctx context.Context, key string) (string, bool) {
			v, err := kv.Get(ctx, key)
			if err != nil || v == "" {
				return "", false
			}
			return v, true
		},
		func(ctx context.Context, key, val string) error {
			_, err := kv.Upsert(ctx, key, val)
			return err
		},
	)
	return service.NewQuoteScriptSource(repo, ab, nil)
}

// quoteApprovalReader 发送腿要的 Submit/Get 门面：优先复用审批运行时那一份，
// 没有就构造一份不带 auto-approve 策略的（policy=nil 的含义是**全部走人工**，
// 那是最保守的一档，不是"没策略所以没人被自动放行"的疏漏）。
//
// 第二个返回值说"用的是不是全局那份"：调用方要用它决定喊不喊那句"没人能在 HTTP 上裁决"。
func quoteApprovalReader(db *gorm.DB) (*service.ApprovalRequestService, bool) {
	if svc := service.GlobalApprovalRequestService(); svc != nil {
		return svc, true
	}
	svc := service.NewApprovalRequestService(repository.NewApprovalRequestRepositoryWithDB(db), nil)
	// 待办出口取全局：与 InitApprovalRuntime 同一句法，且这里同样是"每次调用现取全局"的适配器，
	// 所以 InitHumanTaskRuntime 有没有跑过都不影响构造（跑过就在，没跑过就是 nil ⇒ 只落审批行）。
	svc.SetTaskSink(service.GlobalHumanTaskApprovalSink())
	return svc, false
}

// quoteReachSender 发送腿的外发出口。与挽回 worker 那份同一口径：各自构造一份实例，
// 因为它们都无状态（只有 sender 注册表 + repo），共享反而会把两个装配点的闸门状态搅在一起。
//
// 只交身份、不填 Phone/Email 那条判据在调用方（quote_send.go 文件头），本层不改载荷。
func quoteReachSender(db *gorm.DB) *service.ProactiveReachService {
	reach := service.NewProactiveReachService(db, nil)
	service.BindProactiveReachSenders(reach, db)
	if !AttachReachGate(reach) {
		LogReachGateSkippedAssemblyPoint("app.InitQuoteRuntime")
	}
	return reach
}
