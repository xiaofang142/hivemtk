# Bug Assessment: 架构审计确认项修复

- Slug: architecture-audit-repairs
- Created: 2026-11-20
- Source: 用户「反复深度的审计，审计结果能处理的自己直接处理掉」；外层任务清单 §3.26 AR-01～05。
- Verdict: valid
- Severity: medium

## Evidence and Root Cause

1. 分层门按目录识别嵌套层，禁止目标却用 `internal/controller` 等根路径子串。隔离树中根 service→controller 被拦，而 geo/service→geo/controller、geo/model→geo/service、content/repository→content/service 被漏检；合法 service→repository 通过。selftest 还把所有夹具的结果取并集，不能保证每枚夹具独立生效。
2. `TaskManager.goTask` 再启动 SafeGo，cron 的 SkipIfStillRunning 和 Stop 只等到启动函数返回。真实 robfig 包装器＋阻塞假任务已复现两次任务体重叠；同步控制组跳过第二次。main 的 InitCron 缺少停止/排空；每日备份丢弃传入上下文。
3. 仅 packages 变动触发 SDK 测试，没有触发主前端/桥扩展消费测试；browser_automation 同样消费共享包。lint 的无路径过滤不等价于运行消费测试。
4. app 中混放 inference、tool permission、lifecycle HTTP 参数绑定和响应。router 真实注册这些入口；这是现有实现归属问题，合法 router 依赖装配不受影响。
5. 外层架构概览只有双项目、旧 SPA 发布形态，平台可选云代理与“仅透传日志”描述冲突；README 的平台 MIT 说法与平台 AGPL LICENSE 不符。历史 CODEBASE_MAP 的旧计数本身不构成故障。

原始控制证据：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/architecture-audit-ih7yqv/`。这些证据证明本机结构/行为问题，不证明线上重复外发或数据泄漏。

## Proposed Remediation

- 分层检查按完整路径部件识别导入目标，逐夹具断言；保留合法装配、gorm model、测试文件豁免和空基线。
- cron 直接同步运行任务体，由调度器承担 goroutine、恢复和防重入；提供可取消任务上下文、幂等 Stop 与限时排空，main 显式关闭。每日备份传递上下文，保留旧调用兼容。
- 补共享目录触发范围与共享包原生测试、browser_automation 消费测试；复用现有前端/扩展 CI。
- HTTP 绑定/响应放 controller/DTO，路由映射放 router，app 保留运行时装配。保持 URL、鉴权、模块门和返回字段契约。
- 修订既有架构入口，给出跨仓、业务分组、数据流和发布映射；不改真实许可证，不以同名为由合并实体或流程引擎。

## Files likely to change

- `scripts/check-layer-boundary.py`（含自检）；`.github/workflows/{ci-bridge,ci-embed-sdk,user-server-ci}.yml`。
- `user-server/internal/pkg/cron/{cron.go,cron_test.go}` 与新增 cron 生命周期回归文件；`user-server/cmd/api/main.go`。
- 每日备份在 cron 回调中直接调用已有带 ctx 的服务方法，不编辑其他会话正在修改的 `service/backup.go`。
- `user-server/internal/app/{inference_orchestrator_wiring,agent_lifecycle_wiring}.go` 及其现有 HTTP 测试；新增对应 `controller`、`dto`、`router` 文件及行为测试；`internal/router/router.go` 的三个注册点。
- 既有外层 `docs/architecture/ARCHITECTURE_OVERVIEW.md`、唯一任务清单；内层 `docs/contribute/CODEBASE_MAP.md`；平台 `CLAUDE.md` 的外层文档归属描述（自治仓单独验证/提交）。外层 README 被 Git 忽略，仅校正本机导航，不强制纳入版本库。

## Tests to add or update

嵌套层/别名/空白导入/名称前缀误报；cron 阻塞防重入、完成后重跑、panic 恢复、关闭取消/拒绝新增、排空超时；共享消费实际单测与构建；HTTP 状态/字段和失配输入行为。先观察失败再修复，再做调用链和门禁复核。

## Risks & Considerations

工作区存在其他会话 staged/unstaged 修改，禁止覆盖或合并提交。后台任务取消是协作式，不能强杀忽略 ctx 的任务，限时关闭必须报告超时。生产发布以 release 技能的授权判断为准；本地验证与线上验证分开报告。

## Reassessment: 分层门修复后暴露的真实依赖

2026-11-20：17 枚逐文件夹具通过后，全树门首次报告 `contentguard/repository/guard.go → contentguard/service`。只为 `WordQuery/RecordQuery/SanctionQuery` 三个纯查询参数类型而反向导入，不是 SQL 或裁决逻辑需重构。扩大允许文件至 `internal/contentguard/{service/stores.go,repository/guard.go,model/query.go}`：将类型下沉 model，service 用类型别名保留调用兼容，repository 只导入 model。以全树分层门和 contentguard 全包测试验收，不添加豁免。

2026-11-20 二次复核：robfig v3.0.1 的 Skip 包装在 panic 时不会归还令牌；原 Recover 在外层，导致后续任务永久跳过。真实 AddTask 包装器回归先观察到调用次数只有 1，再将 Recover 放在 Skip 内层恢复为 2。共享 http-core 测试虽然自称 node 原生测试，却导入 Vitest，原生命令先报导入错误；将现有九例迁至 node:test / assert，保持断言语义。允许范围补充该测试、layer-boundary workflow 的夹具名称，以及 contentguard 外部测试包中的接口编译断言。
