# Tasks: ego-lite 事前阻塞写确认闸 + runner 接线杂项

**Feature**: `014-egolite-blocking-confirm-gate` ｜ 勾选即以「已验证」为准（无验证不算完成）

## Phase 1 runner 侧（`cmd/ego-runner/main.go`）

- [x] T001 conf 增加 `ego_server_name` 解析 + `--check` 输出（R2-3）
- [x] T002 `egoNodeArgs(conf)`：EgoServerName 非空时前置 `--ego-server-name=<v>`，execEgo 改用
- [x] T003 execContext 增加 decisionPath；runTask 由脚本路径派生裁决文件路径
- [x] T004 assembleScript 前导注入 `__CONFIRM_FILE__` / `__CONFIRM_TTL_SEC__`（缺省 300）/ `__awaitConfirm__` helper（D1/D3/D6）
- [x] T005 readLoop 改 RawMessage 分型解帧：task_spec / confirm{execution_id,decision} / abort（D5，修 decision 丢失）
- [x] T006 `execKeyMatches` 前缀匹配（`ego-9` ⊢ `ego-9-123`，`ego-91` ⋢ `ego-9-123`）接入 abort/confirm 两分支（R2-1）
- [x] T007 `handleConfirmFrame`：allow 写裁决文件；deny 写文件 + cancel（D2）

## Phase 2 服务端（`internal/egolite/`）

- [x] T008 `flow_service.DryRun`：按 flow.writes 派生 confirm_policy（writes 非空 → require，D7）
- [x] T009 `execution_service.WatchdogSweep`：confirm TTL 迁移成功后向在线 runner 推 abort 帧（D8）

## Phase 3 测试

- [x] T010 `confirm_gate_test.go`：组装注入契约（路径/TTL 常量/helper 定义/entry 语义不改写/TTL 缺省 300）
- [x] T011 `confirm_gate_test.go`：allow→文件内容 allow 且不取消；deny→文件 deny 且取消；错键忽略
- [x] T012 `confirm_gate_test.go`：execKeyMatches 五例（相等/前缀命中/兄弟不命中/反向不命中/空串）
- [x] T013 `confirm_gate_test.go`：egoNodeArgs 有/无 server_name 两形 + conf 解析
- [x] T014 `flow_service_test.go`：dry-run writes→require、无 writes→none
- [x] T015 回归：`go build ./...` + `go test ./internal/egolite/... ./cmd/ego-runner/...`（测试库探针口令）全绿

## Phase 4 真机 smoke 与收口

- [x] T016 本机 ego lite（0.6.1.13）跑注入协议三路径：allow 放行 / deny 停止 / TTL 自停（exit 2）
- [x] T017 删除误落产物 `user-server/ego-runner`，确认 git 状态干净（该路径不再出现）
- [x] T018 delivery.md 回写 + specs/README 与 docs/INDEX §九 补录 + 提交推送（pathspec 提交，勿碰他 lane 改动）
