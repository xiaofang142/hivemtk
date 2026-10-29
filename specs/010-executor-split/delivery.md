# Delivery Report: executor.go 拆分（A1 纯重构）

**Feature**: `010-executor-split` ｜ **交付日期**: 2026-10-24 ｜ **Spec/Plan/Tasks**: 同目录

## 一句话

1901 行的 `service/executor.go` 按职责拆成 8 个同包文件，函数内容逐字节一致、公开 API 零改动；
7 处「读 executor.go 源码」的静态锁测试同步改到拆分后的文件上（断言不变），并为 6 个此前无直接测试的
纯函数补上不连 DB 的单元用例。

## 交付物清单

| 类型 | 文件 | 行数 | 说明 |
|---|---|---|---|
| 修改 | `service/executor.go` | 1901 → 253 | 只保留核心类型、结构体、构造与装配注入 |
| 新增 | `service/executor_stop.go` | 49 | 停止/中断注册表 |
| 新增 | `service/executor_confirm.go` | 258 | D7 人工确认闸门全套 |
| 新增 | `service/executor_session.go` | 448 | cleanupSessionTab + ExecuteSession + executeBrain |
| 新增 | `service/executor_step.go` | 232 | executeStepWithRetry（双发闸/预算/派生写闸门编排） |
| 新增 | `service/executor_blockdetect.go` | 103 | detectBlockedIfFatal 拦截检测 |
| 新增 | `service/executor_dispatch.go` | 406 | dispatchStep + recordResultPayload + mergeExtract + finalizeComment |
| 新增 | `service/executor_helpers.go` | 229 | 纯函数与小型工具（历史压缩/错误分类/步骤参数/睡眠等） |
| 新增 | `service/executor_split_test.go` | 6 用例 | 补无直接测试的纯函数 |
| 修改 | 7 个 `*_test.go` | 各 1–2 行 | 静态源码锁的“文件定位”更新（断言不变） |
| 文档 | `specs/010-executor-split/{spec,plan,tasks,delivery}.md` | — | 四件套 |

## 拆分前后数据

| 指标 | 拆前 | 拆后 |
|---|---|---|
| 文件数 | 1 | 8 |
| executor.go 行数 | 1901 | 253 |
| 最大单文件 | 1901 | 448（executor_session.go） |
| 总行数（内容） | 1901 | 1901（逐字节一致，+77 行仅为每文件 package/import 头） |
| 函数/类型签名改动 | — | 0 |

## 测试命令与结果（干净工作树 HEAD `c9b2f265` 下实测）

| 命令 | 结果 |
|---|---|
| `go build ./...` | ✅ 通过 |
| `go vet ./internal/browser_automation/...` | ✅ 通过 |
| `gofmt -l internal/browser_automation/` | ✅ 空输出（干净） |
| `go test ./internal/browser_automation/... -run '^$'` | ✅ 全部包测试文件编译通过 |
| `go test ./internal/browser_automation/service/ -run '<纯函数+静态锁 regex>' -v` | ✅ 48+ 顶层用例全绿（含新增 6 用例），0 FAIL |
| `go test ./internal/browser_automation/... -count=1 -timeout 40m`（全量，**真 PG**） | ✅ **rc=0 全绿**：controller 8.0s / dto 0.5s / platform 0.5s / repository 29.5s / **service 241.9s**（135 个 DB 用例已实跑，0 FAIL）——2026-10-29 复跑，见下方测试库说明 |

**测试库说明**（2026-10-29 更新）：此前卡点是**测试进程连的库口令与容器实例不一致**（SASL 28P01），
不是代码缺陷。取证方式：以容器实例自身的口令注入环境变量后复跑——
`POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=$(docker exec mtk-postgres printenv POSTGRES_PASSWORD) go test ./internal/browser_automation/...`
⇒ 五个包全绿、rc=0（service 241.9s）。仓内 `.env` 的口令与运行中的容器实例不一致属环境漂移，
不影响本结论；该做法只用于取证，不写入脚本、不落盘密钥。

**隔离验证说明**：主工作树的 `go build ./...` 在交付时被**并发 lane 的未提交 WIP** 阻断——
`internal/repository/observability_dashboard.go`（新增，非本泳道）与既有 `dashboard_stats.go` 重复声明
`FunnelStageRow`。本泳道铁律禁止触碰 `internal/browser_automation/` 之外文件，故未修、未动；
改用 `git worktree add` 在 HEAD 干净树上复制本目录做上述全量验证（该树不含任何 lane 的 WIP）。

## 哪些行为被保守保留、未动

1. **函数体/注释/常量/结构体字段逐字节一致**：抽取脚本内置拼接一致性校验（8 段按原顺序拼回 == 原文件），
   校验通过才落盘；拆分只是「搬」，不是「改」。
2. **公开 API 与函数签名零改动**：`NewExecutor`、`ExecuteSession`、`Set*`、`SignalStop`、`SignalConfirm`
   等对外方法签名与原来完全一致；未导出类型（parsedStep/confirmGate/StepParams…）同包共享，不导出。
3. **静态锁的断言强度一字不减**：7 处测试只把 `readSrc(t, "executor.go")` / `os.ReadFile("executor.go")`
   改成读拆分后对应文件；`TestConfirmGatePrecedesIrreversibleSend` 改为按固定顺序拼接 4 文件后，
   prep→gate→send、awaitConfirmGate×2、waitForConfirm×1、register→frame→wait、派生闸门先于命令帧
   等全部判据保留原样。
4. **不清理历史逻辑**：看到「可优化但非本卡范围」的代码一律不碰（例如某些可简化的错误拼接）。

## 决策点（详见 plan.md）

- **D1** 同包多文件而非子包：大量未导出类型跨函数共享，拆子包必然扩大 API 面。
- **D2** 8 文件粒度：只拆 4 文件仍留 500+ 行文件，不解除巨型压力。
- **D3** 函数零改动：唯一可验证手段是「逐字节一致」，任何顺手优化都会破坏它。
- **D4** 静态锁测试改「文件定位」不改「断言」。
- **D5** 补测 6 个无直接测试的纯函数（taskPlatformID/sendErrText/lastN/historyActionOf/
  auditEventPayload/sleepInterruptible），而非为每个函数重复造测试。

## 遗留项（如实）

1. **主树 `go build ./...` 当前红**：并发 lane 在 `internal/repository` 的 WIP（
   `observability_dashboard.go` 与 `dashboard_stats.go` 重复声明 `FunnelStageRow`）所致，
   **非本泳道改动**；本泳道已用隔离工作树证明自身改动全绿。等该 lane 收口后主树应自然转绿。
2. ~~135 个 DB 用例未实跑（需真库）~~ **已销账（2026-10-29）**：真 PG 下 `./internal/browser_automation/...` 全绿 rc=0，
   service 包 241.9s 跑完 135 个 DB 用例、0 FAIL（命令与取证方式见上表）。
3. ~~文档行号陈旧~~ **已销账（2026-10-29）**：处置方式是「标注历史锚点」而不是改数字——
   历史日志/评估里的 `executor.go:<行号>` 是拆分前 revision 的注码，回改等于伪造当时证据。
   `user-server/docs/architecture/model-list.md` 头部已加文件定位提示（`service/executor.go` 现指同包八文件那一组），
   `docs/architecture/DATABASE_SCHEMA_DEEP_DIVE.md` 对应历史叙述处已补一句行号锚点说明。
