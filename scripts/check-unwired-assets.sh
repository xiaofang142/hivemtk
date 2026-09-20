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
  "5|Agent 双模式分派（passive/active）|func Resolver|lifecycle\\.Resolver\\(|"
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
  # 项9 = T-P2-04 新增：sales_events 这张表**今天在生产路径上一行都不会写**。实测三项零命中：
  # NewSalesEventStatsService 无生产构造点、注入点 SetStats( 零命中、唯一被接线的
  # FollowUpService 走 `if s.stats != nil` 保护（stats 恒 nil）。本卡给这张表加了
  # opportunity_id / quote_id 两个 LTC 预留列，若不登记，"商机事件已入库"这种话在
  # 接线前可以悄悄讲出口——正是 R-4 那条僵尸表（conversion_funnels）的原样翻版。
  "9|销售事件流的装配入口（sales_events 今日生产零写入）|func NewSalesEventStatsService|NewSalesEventStatsService\(|internal/app cmd/api internal/controller internal/router|"
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
  # 仍未接线的端（各由其卡登记，不在本基线留白即视为已闭环）：T-P5-03 外联闸门、
  # T-P6-03 报价发送、T-P9-02 知识库变更 —— 它们的 subject_type 今天没有任何生产 Submit。
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
  "16|商机仓储的装配入口（今日无人构造，接线在 T-P4-05）|type OpportunityRepository interface|NewOpportunityRepository\\(|internal/app cmd/api internal/service internal/controller|"
  "16|商机行的生产写入点（今日零，构造在 T-P4-05）|type Opportunity struct|model\\.Opportunity\\{|internal/service internal/controller internal/app|"
  "16|商机服务的装配入口（今日无人构造，路由在 T-P4-04）|func NewOpportunityService|NewOpportunityService\\(|internal/app cmd/api internal/controller internal/router|"
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
