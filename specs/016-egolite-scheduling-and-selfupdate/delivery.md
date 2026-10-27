# Delivery: ego-lite 调度闭环与 runner 自更新

**Feature**: `016-egolite-scheduling-and-selfupdate` ｜ **Delivered**: 2026-10-28 ｜ **Spec**: `./spec.md`

## 结论

改进清单 C 组剩余可代码化项全部交付：**C12-S10 流程 cron 触发**（调度职责闭环）、
**C12-S11 `egolite_run` AI 工具**（调度入口第二通道）、**C10 余量 runner 自更新**、
**C1 handoff→resume 真机验证（F12/T5 销账，无需真人即可编程接管）**。

## 改动文件

| 层 | 文件 | 内容 |
|---|---|---|
| model | `internal/model/egolite.go` | `EgoliteFlowCron`（Enabled 无 default 标签——BrowserCronTrigger 同款 GORM 零值陷阱规避） |
| model/db | `internal/model/module_tables.go`、`internal/pkg/db/migrate.go` | 新表两处登记（前缀归属测试自动覆盖） |
| repo | `internal/egolite/repository_flow_cron.go`（新） | UpsertByFlow/GetByFlow/DeleteByFlow/UpdateTimes/ListEnabled |
| service | `internal/egolite/flow_cron_service.go`（新） | Upsert（**纯读闸**：writes 非空拒绝）/applySchedule 唯一出口/register（system scope 复读 enabled+租户回钉）/RestoreAll/ValidateCronExpr 向量面 |
| tooluse | `internal/aiagent/agent/tooluse/egolite_tools.go`（新） | `egolite_run`：flow 归属定身份+扫描门前置+写流程强制 require+market 显式 user_id+异步派发 recover |
| 装配 | `internal/router/egolite_routes.go` | fire 闭包（EnsureOnline+流程复查+Start）+ RestoreAll + cron 三端点 + SetEgoLiteRunner 注入 |
| 装配 | `internal/app/tool_provider_wiring.go` | RegisterEgoLiteTools（高危写工具，走既有 risk gate 链） |
| runner | `cmd/ego-runner/main.go` | conf `repo_dir` + `selfUpdate`（git pull --ff-only + go build 到当前二进制，重启生效，结果事件回传） |
| 前端 | `user-web/src/api/egolite.js`、`ego/Flows.vue` | cron 三 API + 编辑器"定时触发"字段（保存时同步挂载/摘除，已挂载回填） |
| 测试 | `flow_cron_service_test.go`、`egolite_tools_test.go`、`confirm_gate_test.go`(增) | 纯读闸/零值陷阱回归/归属防枚举/9 向量；工具面 6 断言组（channel 同步异步派发）；repo_dir 解析 |
| spec | `specs/016-egolite-scheduling-and-selfupdate/`（新） | 四件套 |

## 检查命令与结果

```
go build ./...                                       → OK
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=<容器口令> \
  go test ./cmd/ego-runner/... ./internal/egolite/... \
          ./internal/aiagent/agent/tooluse/ \
          ./internal/model/... ./internal/router/...
  → 五包 ok（tooluse 3.5s / model 2.0s / router 25.5s）
npx eslint（Flows.vue + api/egolite.js）              → 零 error 零 warning
```

## 真机验证（ego lite 0.6.1.13）——F12/T5 销账

handOff→resume 全流程**无需真人点击**即闭环：

```
round1: taskSpace("hivemtk-handoff-*") → example.com → handOff()
        → {"spaceId":11, ...} {"phase":"round1-handed-off"}
round2: listTaskSpaces() → found ownership=agentDelegatedToUser id=11
        → takeOverTaskSpace(11) → 同 spaceId=11、p1 页面状态持久（URL 一致）
        → finish({keep:[]}) → {"ok":true}
```

## 关键语义（写代码时实测纠偏）

- EgoliteFlow **无软删列**（与 browser_tasks 不同）——工具的 raw 查询不带 deleted_at。
- 测试与异步派发用 channel 同步（goroutine 时序不假设）。
- cron 挂载是**纯读特权**：fire 闭包在触发时复查扫描门与 writes（流程挂载后被改红/补
  writes 的窗口被关闭），与 Upsert 闸形成双保险。

## 遗留项

- C3（法律评估，外部）；C5/C6（platform 联动，决策门控）；C7 余量 Windows 包；
  C8/C9（隐私评审/G-E7 前置）；C13/C14/C15/C18（P3 排期）——与改进清单一致。
- cron 触发执行目前无重投（跳过+日志）——与"runner 离线跳过不落行"同口径，如需
  补投机制另立 spec（M4 面）。
