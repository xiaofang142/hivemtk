# Implementation Plan: ego-lite 事前阻塞写确认闸 + runner 接线杂项

**Feature**: `014-egolite-blocking-confirm-gate` ｜ **Created**: 2026-10-27 ｜ **Spec**: `./spec.md`

## 技术路线（一句话）

裁决权仍在服务端（唯一事实源不变），runner 只增加一条**本机文件握手**：
confirm 事件行上报后，脚本轮询执行级临时目录里的 `confirm-decision.json`，
服务端 confirm 帧（allow/deny）经 runner 落成该文件——把既有的
「乐观执行 + 可杀死」升级为「事前阻塞 + 可杀死」，协议面零改动（帧型不变、事件行不变）。

## 握手时序（DF-5 的事前版）

```
脚本写提交点
  └─ console.log({"type":"confirm",...})            → runner → 服务端 waiting_confirm → 前端弹窗
  └─ 轮询 __CONFIRM_FILE__（500ms 间隔，deadline=confirm_ttl_sec）
        ├─ 服务端 allow → confirm 帧{decision:allow} → runner 写 {"decision":"allow"}
        │     └─ 脚本读到 allow → 返回 true → 继续写 → done(exit 0) → completed
        ├─ 服务端 deny → confirm 帧{decision:deny} → runner 写 {"decision":"deny"} + cancel()
        │     └─ 脚本读到 deny 返回 false，或进程直接被杀 → 终态 stopped（服务端已迁移）
        └─ TTL 到期 → 脚本轮询自停返回 false（exit 2）；服务端看门狗 stopped + 推 abort 帧（双保险）
```

收敛性论证：四个动作方（脚本自停、runner kill、服务端看门狗、用户裁决）任意一个先到，
状态机都收敛到终态；`finishFromDone` 对已终态执行只落事件行不迁移（幂等既有语义）。

## 设计决策

| # | 决策 | 理由 |
|---|---|---|
| D1 | 裁决文件放 `MkdirTemp` 执行目录，路径由 preamble 注入常量 | 每执行独立目录：无跨执行残留/伪造面；env 不可穿透（F13），注入文本是唯一通道 |
| D2 | deny = 写文件 + cancel 双保险 | 文件让脚本有机会打出 `ok:false` 审计行；cancel 保证 kill 不依赖脚本自律 |
| D3 | 轮询只认 `decision==="allow"` 为放行，其余一律拒 | fail-close：文件损坏/半写/未知值都不得放行 |
| D4 | 键匹配用前缀规则 `ego-<row>` ⊢ `ego-<row>-<ms>`，反向与兄弟行不命中 | 服务端下行键（EgoExecKey）与 dispatch 键的自然关系；`ego-91` 不命中 `ego-9`（强制 `-` 分隔） |
| D5 | readLoop 改 RawMessage 分型解帧 | taskSpec 结构体解帧天然丢非成员字段（decision 实测丢失路径） |
| D6 | `__awaitConfirm__` 注入 preamble、脚本显式调用 | 不改写 entry 语义（组装纪律：包装不得改写脚本内容）；未调用者退回乐观+杀死 |
| D7 | dry-run writes→require 在 flow_service 派生 | 与 market 分支「资产声明写动作⇒强制 require」同口径，客户端不可关闭 |
| D8 | TTL 看门狗迁移成功后推 abort 帧 | fail-close：到期即死。conn 不在线时静默（脚本自停兜底） |

## 分层落点

| 层 | 文件 | 改动 |
|----|------|------|
| runner | `cmd/ego-runner/main.go` | conf + EgoServerName；execEgo 参数拼装 `egoNodeArgs`；execContext + decisionPath；assembleScript 注入 `__CONFIRM_FILE__/__CONFIRM_TTL_SEC__/__awaitConfirm__`；readLoop RawMessage 分型解帧 + handleConfirmFrame + execKeyMatches |
| runner 测试 | `cmd/ego-runner/confirm_gate_test.go`（新） | spec.md §四-2 的 a–e、g |
| 服务端 | `internal/egolite/flow_service.go` | DryRun 派生 confirm_policy |
| 服务端 | `internal/egolite/execution_service.go` | WatchdogSweep confirm-TTL 分支推 abort 帧 |
| 服务端测试 | `internal/egolite/flow_service_test.go` | dry-run writes→require / 无 writes→none |
| 杂项 | `user-server/ego-runner`（误落产物） | 删除 |

## 风险与回退

- 握手是**纯增量**：未调用 `__awaitConfirm__` 的存量资产/流程行为不变（乐观+杀死）。
- confirm 帧形状未变（`{"type":"confirm","spec":{...}}`），旧 runner（读 taskSpec 丢 decision）
  收 allow 帧本就无副作用、收 deny 帧靠 cancel 仍生效——前后向兼容。
- 真机 smoke 不经 server/WS，直接以 `ego-browser nodejs` 验证注入 JS 的可行性
  （E5 已证动态 import 与轮询可用；不触碰用户浏览器 UI）。
