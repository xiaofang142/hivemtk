# Delivery: ego-lite 事前阻塞写确认闸 + runner 接线杂项

**Feature**: `014-egolite-blocking-confirm-gate` ｜ **Delivered**: 2026-10-27 ｜ **Spec**: `./spec.md`

## 结论

全部交付。写操作确认闸从「乐观执行 + 可杀死」升级为「**事前阻塞 + 可杀死**」，
并修复 4 处「接口通了但跑起来断」的接线断裂（第三批同类问题，全部是单测绿但真机必挂的路径）。

## 改动文件

| 层 | 文件 | 内容 |
|---|---|---|
| runner | `user-server/cmd/ego-runner/main.go` | ① conf + `ego_server_name`（`egoNodeArgs` 前置 `--ego-server-name=`，E3）；② execContext + decisionPath，裁决文件与脚本同目录（每执行独立 MkdirTemp，无残留面）；③ assembleScript 前导注入 `__CONFIRM_FILE__` / `__CONFIRM_TTL_SEC__`（缺省 300）/ `__awaitConfirm__` helper（动态 import node:fs/promises，500ms 轮询，只认显式 allow）；④ readLoop 改 RawMessage 分型解帧（修 confirm 帧 decision 解帧即丢）；⑤ `execKeyMatches` 前缀匹配（`ego-<row>` ⊢ `ego-<row>-<ms>`，修 abort/confirm 帧被静默丢弃）；⑥ `handleConfirmFrame`：allow 写裁决文件 / deny 写文件+cancel / 未知值不动作（fail-close） |
| runner 测试 | `user-server/cmd/ego-runner/confirm_gate_test.go`（新） | 8 组：注入契约（路径/TTL/helper/entry 语义不改写）、TTL 缺省、execKeyMatches 六例、allow/deny/错键/未知 decision 四裁决路径、egoNodeArgs 两形、conf 解析 |
| 服务端 | `user-server/internal/egolite/flow_service.go` | DryRun 按 flow.writes 派生 confirm_policy（非空 ⇒ require，客户端不可关闭；决策 4 同口径） |
| 服务端 | `user-server/internal/egolite/execution_service.go` | WatchdogSweep confirm-TTL 分支迁移成功后向在线 runner 推 abort 帧（TTL=deny 落到进程） |
| 服务端测试 | `user-server/internal/egolite/flow_service_test.go` | `TestDryRunConfirmPolicyDerivedFromWrites`（读流 none / 写流 require） |
| spec | `specs/014-egolite-blocking-confirm-gate/`（新） | spec/plan/tasks/delivery 四件套 |
| 索引 | `specs/README.md`、`docs/INDEX.md` §九 | 014 立项补录 |
| 杂项 | `user-server/ego-runner`（误落的 9MB 编译产物） | 删除，git 状态不再出现 |

## 检查命令与结果

```
go build ./...                                    → OK
go vet ./cmd/ego-runner/                          → 无告警
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=<容器口令> \
  go test ./cmd/ego-runner/... ./internal/egolite/... \
          ./internal/model/... ./internal/router/...
  → ok cmd/ego-runner / ok internal/egolite / ok internal/model / ok internal/router (22.7s)
```

测试库口令按「无固定权威源」纪律以宿主机 TCP 探针裁决：容器 env 新于 .env（8232 口令探针失败，
8202 容器口令探针通过）。

## 真机 smoke（ego lite 0.6.1.13 / 内嵌 Node v24.22.1，helper JS 从 main.go 原样抽取）

| 路径 | 驱动 | 结果 |
|---|---|---|
| allow | confirm 打印后 1.2s 落 `{"decision":"allow"}` | 阻塞在写提交点 → 放行 `write_performed`，exit 0 |
| deny | 1s 后落 `{"decision":"deny"}` | `ok:false confirm_denied_or_timeout`，exit 2 |
| TTL | 无裁决文件，TTL=2s | 恰 2s 自停，`ok:false`，exit 2 |
| （附带）| 脏场残留 deny 文件 | 首次轮询立即拒绝——fail-close 顺带得证 |

## 遗留项（后续轮，登记于 spec §三）

- 跨实例官方包通道（验签代码已备未接主链路）；handoff→resume 全流程真机验证（F12，
  需真人配合 ego UI）；遥测 S8 / cron S10 / aiagent 工具 S11 / 安装器 R2 / Windows T11 /
  沙箱审核机 T12 / 运行时收敛全量 T13 / SSE T18。
- `--awaitConfirm__` 契约已在 preamble 自述 + spec §五；发布台/编辑器的作者须知文案
  可在 U1 表单模式轮次一并补进前端提示。
