# Implementation Plan: ego-lite 调度闭环与 runner 自更新

**Feature**: `016-egolite-scheduling-and-selfupdate` ｜ **Created**: 2026-10-28 ｜ **Spec**: `./spec.md`

## 技术路线（一句话）

调度与既有域**同构不同源**：cron 照抄 `browser_cron_triggers` 模式（TaskManager 注册 +
applySchedule 唯一出口 + RestoreAll system scope 恢复）但物理独立成 `egolite_flow_crons`；
AI 工具照抄 `SetBrowserTaskRunner` 注入式接线；runner 自更新挂在既有 upgrade 帧之后，
零新协议。

## 设计决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | cron 只挂**纯读流程**（writes 空），service 层 fail-close 拒绝 | 决策点 6：ego 腿全自动写长期受限；无人值守写流程会在 waiting_confirm 卡到 confirm TTL 判停。挂载闸 + fire 时复查（流程挂载后被改红/补 writes 双保险） |
| D2 | `Enabled` 不声明 gorm default:true，缺省语义在 Upsert 入参 `*bool` | BrowserCronTrigger 已实证的 GORM 零值陷阱（显式停用被列默认值翻转成 true） |
| D3 | 触发前置 runner 在线（EnsureOnline），离线跳过不落行 | Start 落 created 行会堆积占满并发闸（≤2）；跳过+日志是唯一不产生垃圾状态的选择 |
| D4 | fire 闭包装配在 router，service 只持 `EgoFlowCronFire` 函数 | 与 tooluse.SetBrowserTaskRunner 同款解耦；service 不反向依赖装配面 |
| D5 | cron 工具函数（toSixField/nextRunAt/ValidateCronExpr）在 egolite 包内同源副本 | 禁跨域 import browser_automation；漂移由 ValidateCronExpr 向量测试钉死 |
| D6 | egolite_run 工具：flow 通道归属=流程创建者（DB 行定身份）；asset 通道必须显式 user_id | browser_open_task 同款；market 资产是全局目录，没有"创建者"可默认，执行绑定到该用户本机 runner |
| D7 | 工具异步派发 + `tenant.BackgroundWithTenant(ctx)` + recover | browser_open_task 同款；执行可达数分钟不阻塞工具调用 |
| D8 | runner 自更新仅 `repo_dir` 显式配置时启用；`git pull --ff-only` + `go build` 到当前二进制；重启生效 | 运行中进程不被替换（无原子自替换）；ff-only 防覆盖本机改动；未配置=零回归 |
| D9 | flow raw 查询不带 deleted_at 过滤 | EgoliteFlow 无软删列（与 browser_tasks 不同）——实测纠偏 |

## 分层落点

| 层 | 文件 | 改动 |
|----|------|------|
| model | `internal/model/egolite.go` | `EgoliteFlowCron`（+gorm import） |
| model | `internal/model/module_tables.go` | egoLiteOwnedTables += egolite_flow_crons |
| db | `internal/pkg/db/migrate.go` | allModels += EgoliteFlowCron |
| repo | `internal/egolite/repository_flow_cron.go`（新） | UpsertByFlow/GetByFlow/DeleteByFlow/UpdateTimes/ListEnabled |
| service | `internal/egolite/flow_cron_service.go`（新） | Upsert/Delete/Get/applySchedule/register/RestoreAll + cron 工具函数副本 |
| tooluse | `internal/aiagent/agent/tooluse/egolite_tools.go`（新） | `egolite_run` 工具 + SetEgoLiteRunner 注入点 |
| 装配 | `internal/router/egolite_routes.go` | fire 闭包 + RestoreAll goroutine + cron 三端点 + SetEgoLiteRunner 注入 |
| 装配 | `internal/app/tool_provider_wiring.go` | RegisterEgoLiteTools |
| runner | `cmd/ego-runner/main.go` | conf `repo_dir` + `selfUpdate`（git pull --ff-only + go build）挂 runUpgrade 之后 |
| 前端 | `user-web/src/api/egolite.js`、`ego/Flows.vue` | cron 三 API + 编辑器"定时触发"字段（保存时同步挂载/摘除，404 静默） |
| 测试 | `flow_cron_service_test.go` / `egolite_tools_test.go` / `confirm_gate_test.go`（增） | ①②③ + ValidateCronExpr 向量 + 工具面 + repo_dir |

## 风险与回退

- TaskManager 全局单例注册测试任务：测试结束不显式 Remove（进程级测试库同生命周期，
  注册面无泄漏语义）；生产由 unregister/Delete/Disable 收口。
- 自更新失败（网络/编译）只影响该次升级，旧二进制继续跑——下次升级重试，无半更新状态。
- egolite_run 是高危写工具，走既有 risk gate（agent whitelist/approval decorator）链路。
