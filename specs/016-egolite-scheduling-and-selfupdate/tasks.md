# Tasks: ego-lite 调度闭环与 runner 自更新

**Feature**: `016-egolite-scheduling-and-selfupdate` ｜ 勾选即以「已验证」为准（无验证不算完成）

## Phase 1 数据与登记

- [x] T001 `EgoliteFlowCron` 模型（Enabled 无 default 标签防零值陷阱）+ migrate.go / module_tables.go 登记
- [x] T002 `repository_flow_cron.go`：UpsertByFlow/GetByFlow/DeleteByFlow/UpdateTimes/ListEnabled

## Phase 2 调度服务

- [x] T003 `flow_cron_service.go`：Upsert（纯读闸+表达式预检+缺省 enabled）/ Delete / Get（同响应防枚举）
- [x] T004 applySchedule 唯一出口 + register（system scope 复读 enabled + 租户回钉 + 记账）+ RestoreAll
- [x] T005 cron 工具函数同源副本（toSixField/nextRunAt/ValidateCronExpr）

## Phase 3 触发通道

- [x] T006 router：fire 闭包（EnsureOnline + 扫描门/写动作复查 + Start）+ `go RestoreAll` + cron GET/POST/DELETE 三端点
- [x] T007 `tooluse/egolite_tools.go`：`egolite_run`（flow 归属定身份/扫描门前置/写流程强制 require/market 显式 user_id/异步派发 recover）
- [x] T008 `tool_provider_wiring.go` 注册 + router `SetEgoLiteRunner` 注入（EnsureOnline + Start）

## Phase 4 runner 自更新

- [x] T009 conf `repo_dir`（解析 + --check 输出）+ `selfUpdate`（git pull --ff-only + go build 到当前二进制）挂 runUpgrade，结果事件回传，重启生效

## Phase 5 前端

- [x] T010 api/egolite.js 增 cron 三函数；Flows.vue 编辑器"定时触发"字段 + 保存同步挂载/摘除（已挂载回填，404 静默）

## Phase 6 测试与真机

- [x] T011 cron 服务测试：纯读闸/写流拒绝、显式 disabled 不翻转、二次挂载同行更新、归属防枚举、ValidateCronExpr 9 向量
- [x] T012 egolite_run 工具测试：二选一/归属定身份/扫描门前置/写流 require/market 显式 user_id/params+space 透传（channel 同步异步派发）
- [x] T013 runner `TestLoadConfRepoDir`
- [x] T014 全量回归五包（runner/egolite/tooluse/model/router）全绿；eslint 零告警
- [x] T015 真机：handOff→takeOverTaskSpace 全链打通（spaceId=11 同源、页面状态持久、finish 收口）——**F12/T5 待验证项销账**
- [x] T016 delivery.md + 索引补录 + 改进清单回写 + pathspec 提交推送
