# Tasks: ego-lite 运行时监督与收口杂项

**Feature**: `015-egolite-runtime-supervision` ｜ 勾选即以「已验证」为准（无验证不算完成）

## Phase 1 契约冲突修复（specs/014 遗留）

- [x] T001 `__awaitConfirm__` 契约去 process.exit（EGO-R013 冲突）：deny ⇒ 跳过写 + ok:false 审计行 + 正常结束；preamble 注释与 specs/014 spec §五 同步

## Phase 2 服务端

- [x] T002 `dispatch` 下发 `domains`（draft←flow / market←目录，下发时刻快照）+ `parseDomains`
- [x] T003 `BroadcastUpgrade` + `POST /api/egolite/admin/runner/upgrade`（RequireAdminMiddleware）
- [x] T004 扫描门 EGO-R032（warn）：全文无结构化事件输出

## Phase 3 runner

- [x] T005 taskSpec.Domains + preamble `__DOMAINS__` 注入（空数组=零域全拒）
- [x] T006 域守卫 JS：`__domainAllowed__` / `__gotoChecked__` / `__fetchChecked__`
- [x] T007 `runnerEntryBlockers` 二次静扫（7 条镜像规则，只扫 entry）+ assemble fail-close
- [x] T008 `domainAllowed` / `auditEventURL` + execEgo 统一 `handleEvent`（越域 ⇒ error 事件 + cancel）
- [x] T009 `runUpgrade` / `egoUpgradeArgs` + readLoop `upgrade` 帧（异步执行不阻塞读循环）

## Phase 4 安装器（macOS）

- [x] T010 `install.sh`（编译/conf 不覆盖/LaunchAgent/--check）+ `uninstall.sh`（conf/日志保留）
- [x] T011 README：conf 全键说明（含 ego_server_name）+ 官方升级口径

## Phase 5 前端

- [x] T012 Flows.vue 编辑器脚本契约 alert（常量/confirm 用法/不要 process.exit/域守卫/事件输出）
- [x] T013 Assets.vue 运行面板契约 alert（写确认 + 域白名单）
- [x] T014 EgoRunnerInstallGuide.vue：conf 补 `ego_server_name` 可选行 + install.sh 指引 + 官方升级说明

## Phase 6 测试与真机

- [x] T015 runner 新增 6 组单测（runtime_guard_test.go）全绿
- [x] T016 服务端新增 2 组单测（parseDomains / EGO-R032）全绿
- [x] T017 三个 Vue 文件 eslint 零 error 零 warning；install/uninstall `bash -n` 通过
- [x] T018 真机：同名 space 跨进程续跑（spaceId=10 一致 + 页面状态持久 + finish 收口）
- [x] T019 全量回归：`go build ./...` + ego-runner/egolite/model/router 四包 `go test` 全绿
- [x] T020 delivery.md + specs/README、docs/INDEX §九 补录 + 内部改进清单回写 + pathspec 提交推送
