#!/usr/bin/env bash
# =============================================================================
# check-unwired-assets.sh
# "已实现未接线"资产基线校验（新规划任务清单 T-P0-04）
#
# 背景：本项目的主缺口形态不是"没实现"，而是"实现了但没人调用"（第二步调研 判定 A，
# 共 7 组）。这类代码能编译、能过 lint、单测甚至很绿，却在生产路径上永不执行。
# 把 7 组固化成可重跑判定，用于防止：
#   ① 接线状态悄悄变化后无人回灌文档        → 退出码 1
#   ② 被登记的能力改名/删除致检查形同虚设  → 退出码 2（fail-closed）
#
# 判定口径：
#   定义存在 = defpat 在非测试 .go 文件命中 ≥1（命中不到即 exit 2）
#   已接线   = callpat 在 scope 内命中，且剔除注释行与 func 定义行后 ≥1
#   与登记一致 = 实际接线状态 == 该行 expect（空 = unwired）
#
# expect=wired 的行是**防回退**登记：曾经接线、后来又被拆掉（改了装配、删了调用点）
# 同样报 exit 1。T-P1 每接完一条就把该行改成 wired，基线随之从"待办清单"变成
# "契约清单"——既盯未接线，也盯已接线的别偷偷退化。
#
# 用法：
#   bash scripts/check-unwired-assets.sh            # 输出表格 + 退出码
#   bash scripts/check-unwired-assets.sh --verbose   # 附接线命中的 文件:行
#
# 新增条目：P1+ 每实现一个新能力若暂无生产调用点，就在这里加一行登记，
# 而不是留下一个"看起来能用、其实永不执行"的符号。
#
# 兼容 bash 3.2（macOS 自带），不使用关联数组。
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVER_DIR="$(cd "$SCRIPT_DIR/.." && pwd)/user-server"

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[1;33m'; NC='\033[0m'

VERBOSE=0
if [[ "${1:-}" == "--verbose" ]]; then VERBOSE=1; fi

if [[ ! -d "$SERVER_DIR/internal" ]]; then
  echo -e "${RED}❌ 找不到 user-server 源码目录：$SERVER_DIR（本脚本须在 hivemtk 仓内运行）${NC}"
  exit 2
fi

# -----------------------------------------------------------------------------
# 基线表：item|描述|defpat|callpat|scope|expect
#   - callpat 一律带调用括号或包限定，避免命中同名散文
#   - scope 为相对 SERVER_DIR 的目录（空格分隔）；留空 = 整个 user-server
#   - expect 留空 = 登记为"未接线"；wired = 登记为"已接线"（回退即漂移）
# -----------------------------------------------------------------------------
BASELINE=(
  "1|冷触达审批门（checker 为 nil 即直接放行）|func SetGlobalApprovalChecker|SetGlobalApprovalChecker\(|internal/app|wired"
  "1|冷触达审批门白名单构造|func NewWhiteList|NewWhiteList\(|internal/app|wired"
  "1|冷触达审批门装配入口（executor 配置真赋值）|func applyApprovalGate|applyApprovalGate\(|internal/app|wired"
  "2|Saga 补偿管理器注入|func \\(d \\*SOPExecutionDispatcher\\) SetCompensationManager|SetCompensationManager\(||wired"
  "2|Saga 补偿装配入口（生产装配点真调用）|func InitSOPCompensation|InitSOPCompensation\(|cmd/api|wired"
  "3|工具熔断器注册中心|func NewCircuitBreakerRegistry|NewCircuitBreakerRegistry\(|internal/app|wired"
  "3|工具熔断装配入口（executor 配置真赋值）|func applyToolCircuitBreaker|applyToolCircuitBreaker\(|internal/app|wired"
  "4|工具审计 DB 持久化|func NewDBAuditLogger|NewDBAuditLogger\(|internal/app|wired"
  "4|工具审计 内存+DB+告警 组合器|func NewCompositeAuditLogger|NewCompositeAuditLogger\(|internal/app|wired"
  "4|工具审计落库装配入口（executor 配置真赋值）|func applyToolAuditPersistence|applyToolAuditPersistence\(|internal/app|wired"
  # 项5 = T-P0-04 登记、T-P5-02 接线。登记时它是全仓最典型的"名义字段"：`Resolver` 有实现、
  # 有单测，却没有任何装配点调用它；更要命的是 `LoadContext` 压根不往上下文里写 agent_mode
  # （读出来恒为空串）⇒ 就算有人接了 Resolver 也只能拿到空模式。两处一起修才算接线：
  # service 侧带上模式（TestAIAgentService_LoadContextCarriesAgentMode）＋ app 侧用 Resolver
  # 分派（internal/app/agent_lifecycle_wiring.go 的 NewAgentLifecycleRuntime）。
  # scope 刻意收成 internal/app：这一行盯的是"装配点还在用 Resolver"，不是"全仓某处调过它" ——
  # 装配点被删、别处留下一个测试之外的调用，也算回退。
  "5|Agent 双模式分派（passive/active）|func Resolver|lifecycle\\.Resolver\\(|internal/app|wired"
  "6|挽回队列的定时消费者|type RecoveryQueue struct|RecoveryQueue|internal/cron internal/app|wired"
  "7|Agent 断点续跑 存点|func SaveCheckpoint|SaveCheckpoint\(||wired"
  "7|Agent 断点续跑 取点|func LoadLatestCheckpoint|LoadLatestCheckpoint\(||wired"
  "7|Agent 断点续跑 续跑阶段|func ResumeStage|ResumeStage\(||wired"
  # 项8 = T-P2-01 新增、T-P2-06 接线：整条销售草稿竖在接线前全仓非测试构造点为 0
  # （NewOrderDraftService* 无人 new、SalesWorkbenchService 零引用、TriggerAfterSales 无
  # 生产调用方、ExpireOverdue/PurgeTerminal 零调用方）⇒ order_drafts 一张表一行都不会被
  # 生产路径写。四行全部登记为 **wired**（防回退），不是待办：
  # 8a 是持久化底座的构造口，8b 是三态旗子的唯一装配入口，8c 是"没人调就等于没有保留期"
  #   那条定时路径的调用方，8d 是生产者注入点（它一旦被删，AI 谈单就不再产草稿，
  #   而编译、单测、真机对话全都不会红——只有这一行会红）。
  #   一符号一行是这张表的硬约束（IFS='|' 切列，正则里的或会被拦腰截断）。
  "8|订单草稿持久化底座的装配入口|func NewOrderDraftServiceWithDB|NewOrderDraftServiceWithDB\(|internal/app cmd/api|wired"
  "8|订单草稿运行时装配（FF_LTC_ORDER_DRAFT_DB 三态）|func InitOrderDraftRuntime|InitOrderDraftRuntime\(|internal/app internal/router|wired"
  "8|订单草稿到期/终态清扫的定时调用方|func NewOrderDraftSweepWorker|NewOrderDraftSweepWorker\(|internal/app|wired"
  "8|AI 响应→建草稿的生产者注入点|func \\(o \\*SmartCSOrchestrator\\) SetOrderDraftProducer|SetOrderDraftProducer\(|internal/app|wired"
  # 项9 = T-P2-04 新增：sales_events 这张表**当时**在生产路径上一行都不写。实测三项零命中：
  # NewSalesEventStatsService 无生产构造点、注入点 SetStats( 零命中、唯一被接线的
  # FollowUpService 走 `if s.stats != nil` 保护（stats 恒 nil）。本卡给这张表加了
  # opportunity_id / quote_id 两个 LTC 预留列，若不登记，"商机事件已入库"这种话在
  # 接线前可以悄悄讲出口——正是 R-4 那条僵尸表（conversion_funnels）的原样翻版。
  # 【T-P6-03 后更正】"这张表零写入"今天只剩一半：**写**侧有了真实生产写入方
  #   （internal/service/quote_send.go 的报价外发事件，句柄由 app/quote_wiring.go 递进去），
  #   而本项盯的**读/统计**侧仍然零接线（callpat 的 scope 里没有 internal/service，
  #   sales_event_stats.go 里那个 NewSalesEventRepository() 是统计服务自己的构造，不算证据）。
  #   所以这一格继续留白 = 待办，但口径要说准：现在缺的不是"没人写"，是"写了没人读"——
  #   而后者才是僵尸表的形状（漏斗读的是 opportunities 表，不是这条事件流）。
  "9|销售事件统计服务的装配入口（写侧 T-P6-03 起有真实生产写入方，读侧仍零接线）|func NewSalesEventStatsService|NewSalesEventStatsService\(|internal/app cmd/api internal/controller internal/router|"
  # 项10 = T-P2-05 新增：四行全部登记为 **wired**（防回退），不是待办。
  # 10a 是答案缓存版本路由的唯一决策口：它的调用点在 smart_cs_orchestrator.go 里，
  #   一旦被重构掉，编排器会静默退回挂载前的写死 "v1"，管理端配的版本/灰度当场变成
  #   没人读的摆设——编译、单测、真机对话全都不会红，只有这条判定会红。
  # 10b–10d 是运营侧写入口：三个 service 方法若失去 controller 调用点，"KB 能按客户分桶
  #   放量"这句话就没有入口支撑（旗子 FF_LTC_KB_CANARY 抬到 on 也没人配得动参数）。
  #   一符号一行：BASELINE 用 IFS='|' 切列，正则里的正则或（a|b）会被当场拦腰截断——
  #   这条不是风格偏好，是这张表的硬约束（写错时 defpat 未命中 ⇒ exit 2 自曝）。
  "10|KB 答案缓存版本路由（决策函数）|func KBAnswerVersionFor|KBAnswerVersionFor\\(|internal/service|wired"
  "10|KB 版本转正/回滚写入口|func \\(s \\*KnowledgeBaseService\\) PublishKBVersion|PublishKBVersion\\(|internal/controller|wired"
  "10|KB 灰度参数写入口|func \\(s \\*KnowledgeBaseService\\) SetKBCanary|SetKBCanary\\(|internal/controller|wired"
  "10|KB 版本现状读出口|func \\(s \\*KnowledgeBaseService\\) KBVersionInfo|KBVersionInfo\\(|internal/controller|wired"
  # 项11 = T-P2-06 新增：本卡把草稿竖的**生产者侧**接上了（AI 回复→建草稿、清扫节拍、
  # 观察端点），但装配过程中实测出**读侧与售后侧仍然没人注入**：
  #   11a `SalesWorkbenchService.SetDraft` 在非测试代码零调用 ⇒ 工作台聚合待办里永远没有
  #       草稿项，销售打开系统看不到"我有几条待确认草稿"（本卡只改了它的错误口径）；
  #   11b `SalesActionTrigger.SetDraftService` 同理零调用 ⇒ 售后触发器提取的意向不落草稿；
  #   11c `OrderDraftService.SetOrderService` 同理零调用 ⇒ `Confirm` 走到
  #       `createOrderFromDraft` 只能回 "orderService 未注入"（实测：它明确报错，不会静默
  #       造一条假订单，但"草稿可确认成单"这句话在注入补齐前说不出口）。
  # 三行按 UNWIRED 登记而不是留白：否则"AI 谈单会产草稿、销售在工作台确认草稿"这半句话
  # 会被读成整句都成立。兑现卡未在清单里指派（P4 是商机域、P8 是看板），开工前须先认领。
  "11|销售工作台草稿读侧的注入点|func \\(s \\*SalesWorkbenchService\\) SetDraft|SetDraft\(|internal/app cmd/api internal/controller internal/router|"
  "11|售后触发器草稿写侧的注入点|func \\(t \\*SalesActionTrigger\\) SetDraftService|SetDraftService\(|internal/app cmd/api internal/controller internal/router|"
  "11|草稿确认成单所需的订单服务注入点|func \\(s \\*OrderDraftService\\) SetOrderService|SetOrderService\(|internal/app cmd/api internal/controller internal/router|"
  # 项12 = T-P3-01 建表、T-P3-02 接线。T-P3-01 交付时 approval_requests 在生产路径上一行
  # 都不会写（装配入口零构造、恢复读入口零调用），两行按 UNWIRED 登记而不是留白：否则
  # "审批闸门已经建好"会被读成"P3 已经闭环"。
  # T-P3-02 把挂起端与恢复端都接上了，于是三行全部翻成 **wired**（防回退，不是待办）：
  #   12a 装配入口有构造（internal/app/approval_runtime_wiring.go）。注意这一格只证明
  #       "有人构造了服务"，不证明"运行中一定装配了"：总开关 FF_LTC_APPROVAL_RESUME
  #       默认 off，off 档直接 return nil。开关在装配层，基线看不见它（要靠装配测试守住，
  #       见 internal/app/approval_runtime_wiring_test.go）。
  #   12b 恢复读入口有调用（sop_approval_resume.go 点火时回读）。callpat 写成
  #       `\.ByResumeToken\(` 而不是裸名：仓储层自己的 GetByResumeToken 含同名子串，
  #       那是实现的内部调用、不是接线证据。
  #   12c 到期清扫有节拍器调用（T-P3-01 移交项：方法写得再完整，零调用方就等于
  #       pending 只增不减）。callpat 用"两个实参"这一形状与草稿侧的 ExpireOverdue(ctx)
  #       分开 —— 两条竖的 ExpireOverdue 同名不同签名，不区分的话删掉清扫器也不会红。
  # 仍未接线的端（各由其卡登记，不在本基线留白即视为已闭环）：T-P9-02 知识库变更 ——
  # 它的 subject_type 今天没有任何生产 Submit。
  # 【T-P6-03 后更正】这一行原先还列着"T-P6-03 报价发送"：本卡交付后 subject_type=quote
  #   有了唯一的生产 Submit（service/quote_send.go 的 submitGate：没带结论号时入队一条待办），
  #   所以它从这张"待接的 subject_type"清单上划掉，改由下面 21c/21d 两格守装配点。
  #   符号名当时写的是 verdict，T-P6-04 把它重构成了 submitGate —— 注释里的符号名也是证据，
  #   留着旧名等于让人照着它去 grep 一个已经不存在的东西。
  # 【T-P6-04 后补充】"唯一的生产 Submit"说的是**入口唯一**，不是"每个 subject 只有一条
  #   pending"：同一 subject 今天可以有两道门（policy_key = quote.send 与
  #   quote.discount_high），串行 —— 前一道批完、再点一次发送，才开出后一道。挡重复的是
  #   uq_approval_request_open 那条 (subject_type, subject_id, policy_key) 的 partial
  #   unique index，两档互不挡 —— 别把"入口唯一"读成"每 subject 至多一条待办"。
  # 【T-P5-03 后更正】这一行原先把"T-P5-03 外联闸门"也列在零 Submit 名单里，判据本身没错、
  #   名字点错了：外发节点走的是**已有 Submit 的 `sop_node` 这一 subject_type**（图上挂起
  #   复用 T-P3-02 那座桥），所以本卡交付后它不该再出现在这张"待接的 subject_type"清单上。
  #   本卡的闸门是**发送前那道 W-1 进程内门**（判定键=客户身份），它压根不落 approval_requests
  #   行 —— 那是设计而非漏接，两处判据的区别写在 sop_reach_send.go 文件头。
  "12|审批检查点服务的装配入口（开关 FF_LTC_APPROVAL_RESUME，默认 off）|func NewApprovalRequestService|NewApprovalRequestService\(|internal/app cmd/api internal/controller internal/router|wired"
  "12|审批挂起流程的恢复读入口（点火时回读结论）|func \\(s \\*ApprovalRequestService\\) ByResumeToken|\\.ByResumeToken\\(|internal/service internal/app cmd/api internal/controller internal/router|wired"
  "12|到期 pending 审批的清扫调用方|func \\(s \\*ApprovalRequestService\\) ExpireOverdue|\\.ExpireOverdue\\([^)]*,|internal/service internal/app cmd/api internal/controller internal/router|wired"
  # 项13 = T-P3-03 新增：统一人工待办（N-9）。四行全部 **wired**（防回退，不是待办）。
  # 本卡的失败面很特别：待办这张表写不进去时**会话侧一切正常**（转人工照样把 status 改成
  # waiting，只在日志里说一句"投递失败"），于是"有人在等"这件事从池子、从读数、从值班
  # 视野里同时消失，而没有任何一条既有测试会红。四行各守一个删除即失灵的点：
  #   13a 装配入口（internal/app/human_task_wiring.go）。它只证明"有人构造了服务"，
  #       不证明运行时一定装配了：db==nil 时它把全局清成 nil（本卡没有旗子，
  #       "没 DB 句柄"就是唯一的关闸，守住它的是装配测试而不是这一格）。
  #   13b 生产者注入点：删掉它 = 转人工不再投待办，而编排器一切照旧（与 8d 同一形状）。
  #   13c 会话结束时的撤销钩子：删掉它 = 池子里长期留着"会话早已结束、待办还挂着"的行，
  #       total_open 与逾期读数被这类死行灌水，而没人会注意到少了一行调用。
  #   13d 路由挂载：端点没挂上时坐席只能用 UI 猜，服务侧逻辑再对也无人可访问。
  # 仍未接线的端（登记在此而不是留白，否则"三类待办统一收口"会被读成整句成立）：
  # 只有 conversation_handoff 有生产投递方，collection_escalation 至今**没有任何生产
  # Submit**（催收竖 T-P7-03 才建），届时回来补一行。
  "13|人工待办服务的装配入口|func NewHumanTaskService|NewHumanTaskService\\(|internal/app cmd/api internal/controller internal/router|wired"
  "13|转人工→投递会话待办的生产者注入点|func \\(o \\*SmartCSOrchestrator\\) SetHumanTaskProducer|SetHumanTaskProducer\\(|internal/app|wired"
  "13|会话结束时撤销开放待办的钩子调用方|func cancelOpenHumanTaskForSession|cancelOpenHumanTaskForSession\\(|internal/service|wired"
  "13|统一待办 API 的挂载入口|func setupHumanTaskRoutes|setupHumanTaskRoutes\\(|internal/router|wired"
  # T-P3-04 把审批这一端接进池子（approval 类待办的生产投递方从此有了），追加三行 wired。
  # 上面预告的是两行，实到三行：出口装配与路由挂载之外，"全局登记"那一格删掉后也只有
  # HTTP 侧会坏，与另外两格是三种互不掩盖的失灵面。每行守的都是"删掉之后别处全绿"的接线：
  #   13e 出口装配（审批 → 待办）：删掉 SetTaskSink = pending 审批永远不进池子，而审批行、
  #       流程挂起、裁决唤醒四侧全部照旧 —— 唯一的变化是值班看不见。
  #   13f 审批服务的全局登记：删掉它 = /api/approvals/* 恒 503（路径在、底座不在），
  #       审批服务在流程内路径上依旧完好，只跑 service 层的测试全绿。
  #       callpat 刻意写 `(svc)` 而不是裸名：同名的"清成 nil"两处调用（Init 前置清、Stop）
  #       占了 3 个命中里的 2 个，裸名会让"登记那一行被删"仍显示 WIRED（变异实测过）。
  #       "清成 nil"那两半不在这里守，由装配测试守（Off 与 Stop 两条用例）。
  #   13g 裁决 API 的挂载入口：与 13d 同一形状，删掉 router.go 里那一行时
  #       setupApprovalRoutes 函数还在，vet 与编译都不会说一句话。
  "13|审批→统一待办的出口装配点|func \\(s \\*ApprovalRequestService\\) SetTaskSink|SetTaskSink\\(|internal/app|wired"
  "13|审批服务的全局登记调用方|func SetGlobalApprovalRequestService|SetGlobalApprovalRequestService\\(svc\\)|internal/app|wired"
  "13|审批裁决 API 的挂载入口|func setupApprovalRoutes|setupApprovalRoutes\\(|internal/router|wired"
  # 项14 = T-P3-06 新增：LTC-25「运营一键开启」的落点。
  # 14a 是**刻意留的绊线**：闸门中间件今日零业务挂载点（六阶段的业务路由要到 P4~P7 才逐段建）
  #     ⇒ 按 UNWIRED 登记。等第一条 LTC 路由挂上闸门时，这一格会自己变红（DRIFT_NEW），
  #     逼着那次接线回来把 expect 改成 wired 并回灌文档 —— 在此之前"运营一键开启 LTC"
  #     成立的只是"落点与判据已定"，不是"现网有开关面"。
  # 14b 管理端点已经挂进 router.go ⇒ wired（防回退）：删掉 router.go 里那一行时
  #     setupLTCRoutes 函数还在、编译与单测都不会红，只有这一行会红。
  # callpat 里的 `LTCStageGate\(` 之所以不会被本包测试污染：hits() 一律跳过 _test.go。
  "14|LTC 阶段闸门的业务路由挂载点（今日六阶段无一条业务路由）|func LTCStageGate|LTCStageGate\\(|internal/router cmd/api|"
  "14|LTC 运营开关管理 API 的挂载入口|func setupLTCRoutes|setupLTCRoutes\\(|internal/router|wired"
  # 项15 = T-P3-07 新增：非工具外发路径的发送前闸门（G13 补盲）。五行全 wired（防回退）。
  # 本卡的失败面与项1 是同一类，只是换了条路：闸门装在 ReachByCustomer 内部，
  # 一旦哪半被删，外发**照样成功**、日志照样漂亮，只有"客户收到了一条没被批准的消息"
  # 这个事实消失了 —— 编译与既有测试全绿。每行守一个"删掉之后别处不红"的点：
  #   15a 出口处的判据调用：删掉 enforcePreSendApproval 的调用 = 钩子还挂着但没人问，
  #       这是最纯粹的假闸门形态（service 侧的 g 侧用例守行为，这一格守"调用还在"）。
  #   15b/15c 两个装配点各一行。分开而不是合并成"AttachReachGate 有人调"：
  #       合并后删掉任意一边都仍显示 WIRED，而"只接了 HTTP、cron 那条照发"恰是
  #       本卡最容易发生又最难发现的漏法（快照 attached_services 只数当前进程装了几个，
  #       数不出生产里该接的几个）。callpat 用各自的实参名锁定那一条调用。
  #   15d 观察端点：删掉路由 = 转阻断前的 would_deny 报告无处可看，准入证据链断掉，
  #       而 shadow 态一切照跑、没人会察觉。
  #   15e 队列侧的"拒发不烧尝试次数"分支：删掉它 ⇒ 被拒的挽回项会被算成失败并耗尽
  #       重试后终止（与补授权可重发的语义相反），而 sent 计数照样是零、看不出差别。
  #       defpat 必须容忍空白：gofmt 会把结构体字段对齐成多个空格，写死单空格的式样
  #       会在一次纯格式化之后判成"定义缺失"（本轮就踩到了：gofmt -w 之后 15e 立刻红，
  #       而字段与分支都还在）。这里守的是资产存在，不是它对齐成什么样。
  # 已知的未接线端（留此登记，不留白）：reach 流水线 dispatchOutbound → sender.SendReach
  # 那条批量群发路**不经过** ReachByCustomer，今日仍不受闸门约束，由达阈值时的卡另登。
  "15|外发出口处的发送前判据调用|func \\(s \\*ProactiveReachService\\) enforcePreSendApproval|enforcePreSendApproval\\(|internal/service|wired"
  "15|挽回队列侧的外发闸门装配点|func AttachReachGate|AttachReachGate\\(reach\\)|internal/app|wired"
  "15|直接 API 侧的外发闸门装配点|func setupProactiveReachRoutes|AttachReachGate\\(proactiveSvc\\)|internal/router|wired"
  "15|外发闸门观察端点的挂载入口|func handleReachGateState|/agent/tools/reach-gate|internal/router|wired"
  "15|被拒挽回项不烧尝试次数的分支|BlockedByApproval[[:space:]]+int|errors\\.Is\\(sendErr, ErrReachApprovalDenied\\)|internal/service|wired"
  # 项16 = T-P4-01 新增：商机域的第一层（opportunities 表 + 两套值域）交付了，
  # 但**今日没有任何生产写入方** —— 构造与自动分配在 T-P4-05、跃迁与赢率在 T-P4-03。
  # 按 UNWIRED 登记而不是留白：这张表今天的真实形状就是"能建、没人往里写"，
  # 而它下一步要长出的读方（漏斗按 stage 计数、闭环率按 status 计数）会直接把这格
  # 读成"商机为 0"，与"取数失败"同形（G16 那一类）。
  # 16a/16b 两格在 T-P4-02 之后必须**分开**，起因是实测：仓储层一落地（internal/repository/
  # opportunity.go 里的 `Model(&model.Opportunity{})` 两处）就把原来那一格推成 WIRED、
  # 门 rc=1 —— 而今天依然没有任何人往这张表写行业务数据。一个文件提到自己的模型
  # 不等于接线，所以 16b 的 scope 里**没有** internal/repository。
  # 同理 16a 盯的是"谁构造了这个仓储"（装配入口在 internal/app 或 main），
  # 仓储自己的实现文件永远不含那个调用。两格今天都是 UNWIRED。
  # scope 也刻意排除 internal/pkg/db：那里的 `&model.Opportunity{}` 是**建表登记**
  # 而不是写入，把它算成接线会让这一格从第一天起就是假绿。
  # 16c 是 T-P4-03 新加的一格：服务层（跃迁表 + 赢率式）今天落地，但它与 16a 是两件事 ——
  # 16a 盯"有没有人构造仓储"，16c 盯"有没有人构造这个服务"。判据分开是因为接线有两个
  # 断点（装配仓储 / 挂路由），只盯一个会让另一个断了也没人知道。scope 沿用项12
  # （审批服务的同一格）的形状：只看装配面 internal/app + cmd/api + controller + router，
  # **不含 internal/service** —— 服务自己的构造函数定义不算接线，测试文件由 hits 剔除。
  #
  # T-P4-04 之后 16a 与 16c **同时**翻 wired，比原计划（16a 在 T-P4-05 翻）早一张卡。
  # 起因是这一卡交付的是 HTTP 出口，而出口必须自带底座：app.InitOpportunityRuntime 一行
  # 同时构造了仓储与服务，两个断点在同一次装配里接上了。这不是把两格并成一格 ——
  # 判据仍然分开跑，因为"摘掉仓储那一行"与"摘掉路由那一行"是两种不同的破坏，
  # 合并之后只剩一条能红。
  # 16a 的 callpat 一并放宽成 (WithDB)?：装配点用的是带句柄的那个构造函数（本竖刻意
  # 不走全局 DB），原写法只看得到 `NewOpportunityRepository()`，会漏掉真实的接线形状。
  #
  # 16d 是这一卡新加的第三格：**路由挂载点有没有人调**。它盯的是 16a/16c 都看不见的那把刀 ——
  # 装配函数与控制器都在、构造函数照跑、全套 Go 用例照绿，但 router.go 里没人调
  # setupOpportunityRoutes ⇒ 端点根本不在路由树上，而"挂错位置（挂到鉴权组之前）"同样是
  # 这一格红、上面两格绿。台账比 Go 用例更早发现这类破坏，因为用例自己挂自己测的那棵树。
  "16|商机仓储的装配入口（T-P4-04 由 app.InitOpportunityRuntime 接上）|type OpportunityRepository interface|NewOpportunityRepository(WithDB)?\\(|internal/app cmd/api internal/service internal/controller|wired"
  # 16b 在 T-P4-05 由 UNWIRED 翻成 wired：转换层第一次往这张表写行业务数据
  # （internal/service/opportunity_convert.go 的 newRow），原注释里"今日没有任何生产写入方"
  # 已经不成立。翻的不是一个符号，是一句口径：从这张卡起，"商机为 0"只能是真的没有商机，
  # 不能再解释成"写入方还没来"。
  "16|商机行的生产写入点（T-P4-05 由转换层接上）|type Opportunity struct|model\\.Opportunity\\{|internal/service internal/controller internal/app|wired"
  "16|商机服务的装配入口（T-P4-04 由 app.InitOpportunityRuntime 接上）|func NewOpportunityService|NewOpportunityService\\(|internal/app cmd/api internal/controller internal/router|wired"
  # 16e 盯的是这一卡的电池里唯一一把 16a/16c/16d 三格都看不见、全套 Go 用例（M33 第一版）也
  # 看不见的刀：router.go 里**没人调用 app.InitOpportunityRuntime**。摘掉之后仓储构造、服务构造、
  # 挂载函数三个字面量全都还在，端点也照样在路由树上，只是运行时全局句柄永远是 nil ⇒ 八个端点
  # 全部回 503。Go 侧现在由 TestOpportunityRoutes_LiveThroughRealSetup 守（带合法令牌读得到那一行）；
  # 这一格是同一件事的静态面：台账读起来是"启动路径上没有装配点"，不必依赖用例跑起来才知道。
  "16|商机底座的启动装配点（router.go 里必须有人调用）|func InitOpportunityRuntime|InitOpportunityRuntime\\(|internal/router|wired"
  "16|商机 API 的挂载入口（T-P4-04，摘掉即端点不在路由树上）|func setupOpportunityRoutes|setupOpportunityRoutes\\(|internal/router|wired"
  # ---- T-P4-05（线索一键转商机 + 自动分配）新增四格 --------------------------------
  # 这四格各守一把上面所有格子都看不见的刀。共同背景：转换层的用例全在 service 包里
  # 自己 new 自己测，"生产路径上有没有人把它接上"这件事它们一律看不见（M33 那一课的原形）。
  #   16f 装配点登记全局转换器：摘掉 SetGlobalOpportunityConverter(conv) 这一行，
  #       构造、Available()、全套转换用例照绿，而挖掘那条接缝永远读到 nil ⇒ 一条商机都不建。
  #       defpat 在 internal/service，callpat 只看 internal/app ⇒ 定义自身不会被算成调用。
  #   16g 挖掘侧那一跳：callpat 特意写全称 `conv := GlobalOpportunityConverter(`，
  #       因为 `GlobalOpportunityConverter\(` 会先命中它自己的函数定义行（同包，scope 分不开）。
  #       这一格红 = 线索照写、商机静默不建，是整张卡最坏 also 最安静的一种破坏。
  #   16h 在册名单接的是哪副底座：接错（或漏接）的症状不是报错而是每一单 owner 恒为空，
  #       看起来完全像"这个商户还没配销售"。scope 同样只放装配面。
  #   16i post-migrate 钩子：它是 clue_id 那条部分唯一索引的**唯一**建法（GORM 标签表达不了
  #       WHERE），摘掉之后幂等的库级防线消失，而 service 侧第一层防线照样绿。
  #       callpat 锁带全局句柄的那一次调用（`(DB)` 大写），定义行的 `(db *gorm.DB)` 不进账。
  "16|转换竖的启动登记点（app 装配后全局才有转换器）|func SetGlobalOpportunityConverter|SetGlobalOpportunityConverter\\(conv\\)|internal/app cmd/api|wired"
  "16|挖掘侧到转换层的接缝（lead_mining 里那一跳）|func GlobalOpportunityConverter|conv := GlobalOpportunityConverter\\(|internal/service|wired"
  "16|在册销售名单适配器的装配点|func NewSalesEventRoster|NewSalesEventRoster\\(|internal/app cmd/api|wired"
  "16|clue_id 部分唯一索引的启动调用点|func postMigrateOpportunityClueUniqueIndex|postMigrateOpportunityClueUniqueIndex\\(DB\\)|internal/pkg/db|wired"
  # ---- T-P4-06（漏斗接入商机阶段）新增两格 ----------------------------------------
  # 17a/17b 商机段的两个取数调用点。这一格守的是本卡**唯一**能把整条腿拆干的方法：把 service 里
  #     那一次 CountOpportunitiesByTimeRange 调用删掉、只留一个 `var oppCount int64`，
  #     响应仍是五段、阶段名与中文名全对（词表没动），只有商机的数字永久停在 0，
  #     而看板读起来像"线索转不动商机"。它同时躲过 §4.11 的演示表判据（没读那张表）、
  #     躲过全部 SQL 断言（没有 SQL 可断）。所以 defpat 在 ops/repository、
  #     callpat 只看 ops/service —— 定义自身不进账。
  #     **为什么拆成两格**（反向验证实测出来的，不是设计洁癖）：一个 `wired` 只要求命中 ≥1，
  #     而商机这一格在 service 里有**两个**消费点（BuildFunnel 的第五段、GetStageDetails 的
  #     详情分支）。合写成一行时"把汇总腿删干净"仍报 wired（详情那一处还在），实测 rc=0 ——
  #     那等于登记了一行永不变红的锁。故按消费点各一行；两行的 callpat 各自锚在赋值左侧的
  #     局部变量名上（同一包里两次调用文本几乎相同，不锚名字分不开）。代价：改局部变量名
  #     会让对应行报"未接线" ⇒ 是**变红**而不是变哑，红了回来改这两行即可。
  "17|漏斗汇总腿的商机取数调用点（摘掉即看板少一格真数据、且无声）|func \\(r \\*ConversionFunnelRepository\\) CountOpportunitiesByTimeRange|oppCount, oppErr := s\\.repo\\.CountOpportunitiesByTimeRange|internal/ops/service|wired"
  "17|漏斗详情腿的商机取数调用点（摘掉即 stage=opportunity 回空名 0 计数、与未知阶段无法区分）|func \\(r \\*ConversionFunnelRepository\\) CountOpportunitiesByTimeRange|count, err := s\\.repo\\.CountOpportunitiesByTimeRange|internal/ops/service|wired"
  # ---- T-P5-01（动态人群圈选）新增三格 --------------------------------------------
  # 这三格各守一把 internal/service 自己的用例看不见（或只看半边）的刀：
  #   18a 调度器的启动入口在 cmd/api。全仓 service 用例都是就地 new 一个调度器再调它，
  #       没有任何一条能回答"启动路径上有没有人调它"。摘掉这一行 ⇒ auto/schedule 两类 SOP
  #       一起静默停摆（连圈选都不会跑），而 internal/service 包**全绿**。defpat 在 service、
  #       callpat 只看 cmd/api，正是为了把"定义自身"和"测试里的调用"都挡在外面。
  #   18b 圈选器在调度器构造点的注入。摘掉 `audience:` 那一行，静态名单通道照常工作
  #       （所以 StaticCustomerIDsStillWork 那条照绿），只有声明了 audience 的 SOP 永久零开工，
  #       表现是一行 Warn，读起来像"条件写得太严"。故锚在赋值左侧的字段名上。
  #   18c 标签源取数在圈选器里的那一跳。**这一格与 18a/18b 不同类，登记时必须说清**：
  #       把那一行整个删掉会同时让 TestAudience_SelectByTag 转红（它断言 vip 那两个人必须回来），
  #       所以这一格守的不是"静默停摆"。它守的是**换路**：谁把这一跳换成圈选器里自己拼一条
  #       `WHERE name = ?`（或换成另一个批量读法），既有全部用例照绿，而 repository 那个新方法
  #       ListCustomerIDsByTag 就此变成零消费方的未接线资产 —— 那正是本台账要记的东西。
  #       顺带一条读码事实：tags 腿**没有死源判据**（segment 腿查 rfmHasAnyRow、churn 腿查 Count，
  #       tags 腿空手时只会报 `no_match:tag=…`，:154-155）⇒ 换路之后它连"源死了"都说不出，
  #       报出来的永远是"这个条件没人"，运营会去改条件，而没人会去查那条腿。
  #       与 17 同一课：`wired` 只要求命中 ≥1，所以这里刻意不写成 `New.*RepositoryWithDB\(`
  #       那种"三个源一起算一格"的宽式（RFM 那副底座另有 customer_360 的消费点，会永远命中）。
  "18|SOP 调度器的启动入口（摘掉即 auto/schedule 两类 SOP 一起静默停摆、service 包全绿）|func InitSOPScheduler|InitSOPScheduler\\(|cmd/api|wired"
  "18|圈选器在调度器构造点的注入（摘掉即 audience 型 SOP 永久零开工、只剩一行 Warn）|func NewAudienceSelectorWithDB|audience:[[:space:]]*NewAudienceSelectorWithDB\\(|internal/service|wired"
  "18|标签条件取数在圈选器里的那一跳（换成就地拼 SQL 即让新仓储读法变成零消费方资产、用例全绿）|func \\(r \\*customerTagAssignmentRepository\\) ListCustomerIDsByTag|ListCustomerIDsByTag\\(|internal/service|wired"
  # ---- T-P5-02（Active 生命周期 + 按 agent_mode 分派）新增六格 ------------------------
  # 前两格守"入口这一侧"：app 包自己的用例是**直接调** SetupAgentLifecycleRoutes /
  # NewAgentLifecycleRuntime 的，所以它们证明不了 router.Setup 里还挂着这两跳。
  #   19a 删掉 `app.InitAgentLifecycles(gormDB, engine)`：路由照挂、编译照过、app 用例照绿，
  #       而线上每一次运行都稳定回 503 —— 一个"永远没装配"的端点比没有端点更难被发现，
  #       因为它看起来是在工作的（有响应、有 JSON、有 code）。
  #   19b 删掉 `app.SetupAgentLifecycleRoutes(auth)`：运行时装配着、没有任何路由指向它，
  #       双模式重新回到本卡的起点"有实现、零调用方"。
  # 后四格是**本卡刻意没接**的兄弟字段，登记在这里防止下一个读代码的人以为它们活着 ——
  # 尤其现在 Active 真的能跑了，"这一族字段肯定都通了"是最自然的误判。
  #   19c/19d `agent.ModeOf` 与 `agent.IsActive`：唯一"消费"是同文件里 IsActive 调 ModeOf，
  #       包外零调用（callpat 只认包限定形式，包内自调不算接线）。运行期判模式走 app 侧 Resolver。
  #       为什么不并成一格：账本的字段分隔符就是 `|`，callpat 里写 `(a|b)` 会被劈成两截 ——
  #       第一版就因此把 scope 读成了半截正则，报 exit 2「scope 目录缺失」。
  #   19e `SalesRequest.AutoExecute`：**写入 5 处、读取 0 处**（实测口径：`AutoExecute bool`
  #       命中 1、`\.AutoExecute` 命中 0）。写它的都是各条入站链路，没有任何一条读它决定
  #       走不走自动回复 —— 真正的开关是 SmartCSOrchestrator 里那个进程内的 o.enableAutoReply。
  #       这是本卡顺带查出来的、比"未接线"更糟的一格：它会让人以为改这个字段就能开关自动回复。
  #   19f `AgentContext.DecisionStrategyIDs`：从 model 到 dto 这一跳 LoadContext 就没抄
  #       （defpat 命中 2 处：dto 与 runtime/types.go 各一份声明，读方 0 处），
  #       所以 Active 的"决策"只能取智能体挂的第一个可解析 SOP（见 active.go 头部）。
  "19|双模式运行时的启动装配点（摘掉即线上每次运行稳定回 503、app 用例全绿）|func InitAgentLifecycles|InitAgentLifecycles\\(|internal/router|wired"
  "19|双模式运行入口的路由登记点（摘掉即重新回到「有实现、零调用方」）|func SetupAgentLifecycleRoutes|SetupAgentLifecycleRoutes\\(|internal/router|wired"
  "19|模式判读 helper ModeOf 的包外调用方（运行期分派走 app 侧 Resolver）|func ModeOf\\(|agent\\.ModeOf\\(|"
  "19|主动模式判定 IsActive 的包外调用方（唯一消费是同文件里它调 ModeOf）|func IsActive\\(|agent\\.IsActive\\(|"
  "19|SalesRequest.AutoExecute 的读取方（写入 5 处、读取 0 处，开关其实在编排器里）|AutoExecute bool|\\.AutoExecute|"
  "19|决策策略 ID 列表的读取方（Active 因此只能取第一个可解析 SOP）|DecisionStrategyIDs \\[\\]string|\\.DecisionStrategyIDs|"
  # ---- T-P5-03（外联闸门串联 / reach_send 节点）新增四格 ------------------------------
  # 前两格守"这一族能力在启动路径上真的被接上"：本卡的行为用例全在 internal/service 里
  # 就地 new 调度器、就地 SetSOPReachSender，它们证明不了生产装配点还挂着这两跳。
  #   20a `service.InitSOPExecutionDispatcher(...)`（cmd/api 启动段）：摘掉这一行，
  #       编译照过、包内用例照绿，而**每一种**节点执行器都不再注册 —— 未登记类型走 NoopExecutor，
  #       直接报"完成"。于是 Active 出域从"发不出去"变成"发不出去且图上显示已发"，
  #       比报错更糟（同 18a 那一课，只是这次连"没跑"的日志都没有）。
  #       与 18a 分开登记是因为它们是两条独立的启动跳：InitSOPScheduler 决定"要不要跑"，
  #       InitSOPExecutionDispatcher 决定"跑起来那一步由谁执行"。
  #   20b `service.SetSOPReachSender(proactiveSvc)`（router 触达装配步）：摘掉这一行，
  #       reach_send 仍在图上、审批腿仍会挂起、人照样会在待办中心点"同意"，
  #       只有最后那一跳永久回"外发服务未装配"。本卡给它另有一道源码形状锁
  #       （internal/router/reach_sender_assembly_test.go），这一格是第二把刀：
  #       锁会被删文件绕过，台账不会。
  #   20c 生产唯一生效的退订装配在**构造器里**那一行。锚为什么必须选它而不是选
  #       `SetDoNotContact`：实测 setter 的**非测试调用点为 0**（它自己的注释就写着
  #       "测试或自定义装配时使用"），而本卡每一条退订用例都显式调 setter ⇒
  #       **摘掉构造器里那行，用例照样全绿、线上退订客户照发**。这是三判据里唯一一条
  #       "测试面完全看不见"的连线，台账是仅有的判据（17 那一课：wired 只要求命中 ≥1，
  #       所以一格只锚一个消费点，不把"setter 与构造器"并成一格）。
  #   20d 节点上的 `Tools`，本卡**查出来、刻意不接**的字段，登记在此防止误判：
  #       唯一的"消费"是 `deepCopySOPNode` 把它原样抄一份，执行器侧读取数为 **0**
  #       （实测 `\.Node\.Tools` 非测试命中 0）。这正是本卡把外发做成**新节点类型**
  #       而不是"给既有节点配一个工具"的原因 —— 后者写进图里就永不会被执行，
  #       且看起来完全像配好了。defpat 用 `Tools +\[\]string`（字段对齐是多空格，
  #       写 `Tools \[\]string` 会一格都不命中，判成 exit 2）。
  "20|SOP 节点执行器的注册链入口（摘掉即未登记类型静默按\"完成\"处置、包内用例全绿）|func InitSOPExecutionDispatcher|InitSOPExecutionDispatcher\\(|cmd/api|wired"
  "20|SOP 外发出口的装配点（摘掉即图上每一步都能走完、只有最后发送永久失败）|func SetSOPReachSender|service\\.SetSOPReachSender\\(|internal/router|wired"
  "20|退订检查在生产构造点上的装配（setter 侧零生产调用方，摘掉这行退订用例仍全绿）|func NewDoNotContactService|dnc:[[:space:]]*NewDoNotContactService\\(|internal/service|wired"
  "20|节点 Tools 字段的执行器读取方（写入靠深拷贝原样抄、读取 0 处，外发因此走独立节点类型）|Tools +\\[\\]string|\\.Node\\.Tools|"
  # ---- T-P6-01 起登记、T-P6-02 改口、T-P6-03 补齐装配的一族（21a–21d）———————————
  # 21a 盯"有没有人构造报价仓储"。scope 刻意排除 internal/repository —— 实现文件里
  #      两个构造函数的**定义**永远在，把它算成接线就从第一天起假绿（项16 的 16a 同一课）。
  #      internal/service 留在 scope 里：报价的写入方就住在那儿，而 hits() 跳过 _test.go，
  #      所以"只在测试里 NewQuoteRepositoryWithDB 了一把"不会把它翻成 wired。
  # 21b 盯"有没有人往 quotes 里写行业务数据"。T-P6-01 登记时为 UNWIRED（那一卡只交付
  #      schema 与仓储，全仓没有一个人构造过报价）；T-P6-02 交付 QuoteService 后翻
  #      wired，此后它是**防回退登记**：接线数掉回 0 = 报价生成整条腿没了。scope 排除
  #      internal/pkg/db —— 那里的 `&model.Quote{}` 是**建表登记**不是写入。
  # 两格分开是因为接线有两个断点（装配入口 / 真的产生一行报价），只盯一个会让另一个
  # 断了也没人知道。
  # T-P6-03 又补了两个断点，所以这一族今天有四格：
  # 21c `app.InitQuoteRuntime(gormDB)` —— 摘掉这一行，两条腿的全局实例恒为 nil，
  #      四条端点全部退成 503，而 **Go 用例照样全绿**：router 包里那几条"未装配回 503"的
  #      用例判的就是 nil 句柄，它分不清"本来就该 nil"和"没人装配"。这正是台账存在的理由
  #      （商机竖的 M33 那一课：service 侧单测全绿、生产装配点没人调用）。
  # 21d `setupQuoteRoutes(auth)` —— 与 21c 是两个独立断点：装配了但没挂载 = 库里有报价、
  #      HTTP 面上读不到（前端 404）；挂载了但没装配 = 端点在、每问一句回 503。
  #      两种坏法在响应面上毫无重叠，合成一格就只守得住一半。
  #      callpat 只扫 internal/router：定义与调用同包，靠 wiring 过滤掉 `func ` 那一行。
  # 读方的口径（回灌进文档的那句）：这几格只数"源码里有没有构造点"，不数库里有几行。
  # "明细一条都没有"与"明细读失败"这一对区分落在**发送出口**：T-P6-04 把明细提到开审批
  # 之前读（门序要从那几行的折扣算出来），于是读故障上抛包装错误、一条明细都没有回
  # ErrQuoteSendLinesMissing（不开待办、不认领、不外发），两种坏法在响应面上不再重叠。
  # 口径当时写的是"那是下一张卡要分的事"，本卡分掉了它 —— 但只分掉发送这一头：
  # 读端点（View / ViewAt / LatestView 共用的 buildView）对"明细一条都没有"仍照算照回
  # （Lines 为空、合计 0.00），只有读故障上抛包装错误。这不是漏项 —— 要把那条数据事故
  # 交给人工处置，人得先能把那一版的其余字段读出来。
  "21|报价仓储的装配入口（T-P6-03 起 app/quote_wiring.go 有真实构造点；接线数回到 0 = 报价两条腿都没人装）|type QuoteRepository interface|NewQuoteRepository(WithDB)?\\(|internal/app cmd/api internal/service internal/controller|wired"
  "21|报价行的生产写入点（T-P6-02 起 service 侧有真实写入方：Generate 与 Revise 各构造一版；接线数回到 0 = 报价生成整条腿没了）|type Quote struct|model\\.Quote\\{|internal/service internal/controller internal/app|wired"
  "21|报价两条腿在启动路径上的装配点（摘掉 router 那一行，端点全退 503 而 Go 用例全绿）|func InitQuoteRuntime|InitQuoteRuntime\\(|internal/router|wired"
  "21|报价 HTTP 出口的挂载点（装配了却没挂载 = 库里有报价、前端 404，与 21c 是两种坏法）|func setupQuoteRoutes|setupQuoteRoutes\\(|internal/router|wired"
  # ---- T-P7-01 账单派生腿（23a–23e）—————————————————————————————————————
  # 这一族比报价那一族多一格，因为本卡的中心事实是一句**否证**：
  # `accepted` 这个值从 T-P6-01 起就在报价值域里，而全仓非测试代码没有任何一处写它
  # （实测 QuoteRepository.UpdateStatus 的调用方只有发送腿的 draft↔sent 两次）。
  # 所以"回款域没有起点"这件事在编译、测试、响应面上全都看不出来 —— 只有台账数得出来。
  # 23a/23b 是报价族同一对断点（装配入口 / 真的产生一行账单）；
  # 23c/23d 也是（router 里那一行 Init / 那一行 mount）：摘掉 Init 是端点恒 503，
  # 而 router 包那条"未装配回 503"的用例照样绿（它判的就是 nil 句柄，分不清"本该 nil"
  # 与"没人装配"）；摘掉 mount 是库里有账单而前端 404 —— 两种坏法在响应面上不重叠，
  # 合成一格只守得住一半。
  # 23e 是本卡**故意交付而暂时没人调**的那一格：账单状态跃迁口（open→partial→paid、→voided）。
  # 判据在 T-P7-02（回款累计到位才算结清）与 T-P7-03（催收收口），今天写任何调用方
  # 都要凭空造一个"已收金额"，而那一列本卡刻意不建（求和发生在 payments 侧）。
  # defpat 锚在**方法定义**上而不是接口上：接口那一行现在也在，把它算成"定义存在"的话，
  # 方法被删掉之后这一格会退成 exit 2（报"检查形同虚设"）而不是报"接线状态漂移"。
  # callpat 用 `bills\.UpdateStatus\(`：派生服务的字段就叫 bills，等 T-P7-02 的回款累计
  # 真接上时，这一行会从 unwired 翻成 wired 并被当场数出来。
  "23|账单仓储的装配入口（摘掉 app/bill_wiring.go 那一行，派生腿恒缺件、Go 用例全绿）|type BillRepository interface|NewBillRepositoryWithDB\\(|internal/app|wired"
  "23|账单行的生产写入点（接线数回到 0 = 全系统再没有任何一处能开出一张应收）|type Bill struct|model\\.Bill\\{|internal/service internal/controller|wired"
  "23|账单派生腿在启动路径上的装配点（摘掉 router 那一行，/api/bill 永久 503 且报价 accepted 在全系统没有写入口）|func InitBillRuntime|InitBillRuntime\\(|internal/router|wired"
  "23|账单 HTTP 出口的挂载点（装配了却没挂载 = 库里有账单、前端 404，与 23c 是两种坏法）|func setupBillRoutes|setupBillRoutes\\(|internal/router|wired"
  "23|账单状态跃迁口的生产调用方（本卡只交付形状：结清判据在 T-P7-02、催收收口在 T-P7-03）|func \\(r \\*billRepo\\) UpdateStatus|bills\\.UpdateStatus\\(|internal/service|"
)

hits() {  # hits <pattern> <dir...> — 只扫 .go，跳过 _test.go
  local pat="$1"; shift
  grep -rnE --include='*.go' "$pat" "$@" 2>/dev/null | grep -v '_test\.go:' || true
}

echo "══════ 未接线资产基线（判定 A · 7 组）══════"

DEAD=0
DRIFT_NEW=""
DRIFT_LOST=""
ITEMS_WIRED=0
ITEMS_TOTAL=0

for entry in "${BASELINE[@]}"; do
  IFS='|' read -r item desc defpat callpat scope expect <<< "$entry"

  def_lines=$(hits "$defpat" "$SERVER_DIR")
  def_n=0
  [[ -n "$def_lines" ]] && def_n=$(printf '%s\n' "$def_lines" | grep -c .)

  if [[ "$def_n" -eq 0 ]]; then
    printf '  [项%s] %b定义缺失%b  %s —— defpat 未命中，基线已不可信\n' \
      "$item" "$RED" "$NC" "$desc"
    DEAD=1
    continue
  fi

  scope_dirs="$SERVER_DIR"
  if [[ -n "$scope" ]]; then
    scope_dirs=""
    for d in $scope; do
      if [[ ! -d "$SERVER_DIR/$d" ]]; then
        # scope 目录不存在时命中必然为 0，会让该项"看似未接线"实为检查失效 —— 显式失败
        printf '  [项%s] %bscope 目录缺失%b %s（%s/%s 不存在）\n' \
          "$item" "$RED" "$NC" "$desc" "$SERVER_DIR" "$d"
        DEAD=1
        scope_dirs=""
        break
      fi
      scope_dirs="$scope_dirs $SERVER_DIR/$d"
    done
    [[ -z "$scope_dirs" && $DEAD -eq 1 ]] && continue
  fi

  wiring=$(hits "$callpat" $scope_dirs \
    | grep -vE ':[0-9]+:[[:space:]]*(//|\*|/\*)' \
    | grep -vE ':[0-9]+:[[:space:]]*func ' || true)
  n=0
  [[ -n "$wiring" ]] && n=$(printf '%s\n' "$wiring" | grep -c .)

  ITEMS_TOTAL=$((ITEMS_TOTAL + 1))
  if [[ "$n" -gt 0 ]]; then
    ITEMS_WIRED=$((ITEMS_WIRED + 1))
    printf '  [项%s] %bWIRED%b    接线数=%-3s %s（def 命中 %s 处）\n' \
      "$item" "$GREEN" "$NC" "$n" "$desc" "$def_n"
    [[ "$expect" == "wired" ]] || DRIFT_NEW="$DRIFT_NEW 项$item:$desc"
  else
    printf '  [项%s] %b%s%b  接线数=0   %s%s\n' \
      "$item" "$RED" "$( [[ "$expect" == "wired" ]] && printf '回退（登记为已接线却无调用点）' || printf 'UNWIRED' )" "$NC" "$desc" \
      "$( [[ "$expect" == "wired" ]] || printf '（定义在，无人调用）' )"
    [[ "$expect" == "wired" ]] && DRIFT_LOST="$DRIFT_LOST 项$item:$desc"
  fi

  if [[ $VERBOSE -eq 1 && $n -gt 0 ]]; then
    printf '%s\n' "$wiring" | sed -e "s#$SERVER_DIR/##" -e 's/^\(.\{140\}\).*/      ↳ \1…/'
  fi
done

echo "----------------------------------------------------------------------"

if [[ $DEAD -eq 1 ]]; then
  echo -e "${RED}❌ 有登记符号的定义未命中：能力已改名或被删除，请更新本基线（exit 2）${NC}"
  exit 2
fi

if [[ -n "$DRIFT_LOST" ]]; then
  echo -e "${RED}❌ 已登记的接线被拆掉（exit 1）：$DRIFT_LOST${NC}"
  echo    "   → 恢复装配/调用点；确要下线，须同步回灌判定 A 与本基线的 expect 列，不许静默退化"
  exit 1
fi

if [[ -n "$DRIFT_NEW" ]]; then
  echo -e "${YELLOW}⚠️  与基线不符（exit 1）：$DRIFT_NEW 已接线，而登记为未接线${NC}"
  echo    "   → 回灌 docs/replan-2026-09/本项目调研.md 判定 A、AI_CORE_FEATURE_INVENTORY 短板表与对应任务卡，"
  echo    "     并把该行的 expect 改为 wired（转为防回退登记）"
  exit 1
fi

echo -e "${GREEN}✅ 与登记一致（exit 0）：${ITEMS_WIRED}/${ITEMS_TOTAL} 行已接线，其余按登记保持未接线${NC}"
echo    "   未接线能力在接线前不得对外宣称可用；接线属改变生产行为的变更，须逐条灰度。"
exit 0
