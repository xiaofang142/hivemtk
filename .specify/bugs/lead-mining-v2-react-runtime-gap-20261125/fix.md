# Bug Fix: Lead-mining v2 ReAct runtime is installed but not operational

- **Slug**: `lead-mining-v2-react-runtime-gap-20261125`
- **Fixed**: 2026-11-25
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

按 assessment §10 的批准切片，把线索发掘 ReAct 改为服务自有、严格文本协议、零原生 Tools 的有界只读取证链，并接入生产 resolver、frozen history scope、typed outcome 与判定持久化。Agent persona/provider/model 注入按裁决不在本 lane 实现。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/service/lead_react.go` | modified | 严格 Action/Final Answer 文本协议；`Tools=nil`；query-only JSON；3 轮/8 秒上限；工具白名单、typed outcome、有界 prompt/observation |
| `user-server/internal/service/lead_react_task.go` | added | 从 frozen `leadHistoryScope` 构造可信任务；canonical ID + verified aliases；raw exact-sourceRef fallback；缺租户/SystemScope/非法 scope fail-closed |
| `user-server/internal/service/lead_react_resolver.go` | added | 生产历史线索与最小 Customer360 会话摘要 resolver；缺失依赖 fail-visible；raw fallback 禁用 Customer360 |
| `user-server/internal/service/dispatcher_judge.go` | added | 生产 judge 先执行 ReAct，再执行 structured judge；传播 typed outcome/degraded |
| `user-server/internal/service/lead_mining.go` | modified | 生产任务注入、ReAct judge 装配、`ReactOutcome` 持久化；降级写入 `Degraded=true`、`EvidenceComplete=false` |
| `user-server/internal/repository/lead_mining_judgement.go` | modified | tenant/platform/account-alias 历史查询；group → DM → sender fallback；有界 limit；显式补写 `evidence_complete=false` |
| `user-server/internal/repository/customer_session.go` | modified | account-alias 最小会话摘要与全范围状态计数；缺租户/SystemScope fail-closed |
| `user-server/internal/router/router.go` | modified | Router 只装配 service-owned resolver；缺失依赖记录错误并保留 fail-visible hook |
| `user-server/internal/service/lead_react_test.go` | modified | 文本协议、NoFC/NativeFC zero-Tools、strict args、边界、raw fallback、frozen scope、持久化等回归 |
| `user-server/internal/service/lead_react_resolver_test.go` | added | 真实 PostgreSQL 下账号/租户隔离、最小摘要、nil DB、全量状态计数 |
| `user-server/internal/service/dispatcher_judge_test.go` | added | ReAct 先于 structured judge、缺 frozen task、raw/degraded 持久化、legacy fallback |
| `user-server/internal/repository/lead_react_scope_test.go` | added | canonical+alias、跨账号/跨租户、group/DM/sender fallback、SystemScope 拒绝 |

## Shared-file Provenance

外部冻结基线来自 `lead-history-external-stable-monitor118.txt`。本 lane 只对共享 `lead_mining.go` 做了一处定点合并；其余五个共享文件与 monitor118 字节一致。

| Shared file | monitor118 external | ReAct merged | Result |
|-------------|---------------------|--------------|--------|
| `service/lead_history_scope.go` | `09876f0454b3dd6ee9c93089a574a16a452083373f1eeaf6724fbe2bd40f5c09` | same | unchanged |
| `service/lead_history_scope_test.go` | `e4a54e23911e1ceae8d43ee6b988640ca5ed315cf3975295913b5cb14f8cd0f9` | same | unchanged |
| `service/lead_judge_context.go` | `071b6c040bb16231bc52b06506251c76d810497501741d4ec66d458698e4fb7c` | same | unchanged |
| `service/lead_mining.go` | `094ba9bbfe46973ace2e9d5c02767999277f5e897982106b570782f49d929bfc` | `a0ad6d4c74bcc94e0ec7eeab3612ce640cf3df96237f0aa489857cf833ae43b3` | one ReAct hunk |
| `repository/message_hub_inbox.go` | `35f1ff7f00c69b5f677fed98afa646d2985e995c6f4145d27518b524e504aa71` | same | unchanged |
| `repository/message_hub_history_scope_test.go` | `79a513d079961c8c0722190951dfe775f554cc40aa455ba9d6cebe688188f381` | same | unchanged |

字节级证明：

```text
094ba9bbfe46973ace2e9d5c02767999277f5e897982106b570782f49d929bfc  lead-mining-monitor118-reconstructed.go
a0ad6d4c74bcc94e0ec7eeab3612ce640cf3df96237f0aa489857cf833ae43b3  user-server/internal/service/lead_mining.go
```

`diff -u lead-mining-monitor118-reconstructed.go user-server/internal/service/lead_mining.go` 只有一处 hunk：

```diff
-	ctx = withLeadReactTask(ctx, buildLeadReactTask(hub, jctx))
+	ctx = withLeadReactTask(ctx, buildLeadReactTask(ctx, hub, jctx, scope))
```

因此 `a0ad6d4c…` 完全由本 lane 在上述外部冻结文件上定点合并产生。外部 `historyScope`、account-aware debounce key、rule-target canonical scope、group/DM/sender history boundary 均未被覆盖或回退。

## Diff Highlights

- ReAct 不再依赖 shared `react_adapter.go` 或原生 ToolCalls；生产 dispatcher 请求强制 `Tools=nil`、`ToolChoice=""`。
- `LeadReactTask` 不含 tenant；tenant 只从 `context.Context` 读取，缺租户、零租户、SystemScope 在 resolver/LLM 前拒绝。
- canonical scope 使用 canonical account ID 与 verified alias set；raw fallback 只保留 exact `sourceRef`，Customer360 不查询且整轮 degraded。
- 模型参数只接受 `{"query":"..."}`；未知字段、畸形 JSON、空/超长 query 均拒绝。
- 持久化只保存工具观察与固定原因，不保存模型 Thought 或内部错误原文。

## Tests Added or Updated

- `lead_react_test.go`：NoFC/NativeFC 生产 dispatcher zero-Tools、strict args、dictionary unsupported、round/timeout、empty resolver、unknown tool、bounded task、canonical/raw/invalid frozen scope。
- `lead_react_resolver_test.go`：真实 PostgreSQL 账号/租户隔离、最小 Customer360 白名单、nil DB、状态统计覆盖全部会话。
- `dispatcher_judge_test.go`：ReAct 调用顺序、typed outcome、missing frozen task、raw/degraded 持久化、legacy structured fallback。
- `lead_react_scope_test.go`：canonical+alias 历史与会话、跨账号/跨租户拒绝、group/DM/sender fallback、SystemScope 拒绝。

## Local Verification

- `go build ./...` → rc=0（14:38:19，外部 CRM mid-edit 前）。
- `go vet ./...` → rc=0（14:38:32，外部 CRM mid-edit 前）。
- `python3 scripts/check-layer-boundary.py` → rc=0，1764 files / 0 new violations。
- `bash scripts/check-architecture.sh` → rc=0，仅既有 `wecomCSService` interface warning。
- `bash user-server/scripts/check-gorm-only.sh` → rc=0。
- `bash user-server/scripts/check_naked_goroutine.sh` → rc=1，62 处既有基线命中；本 lane scoped hits=0，未扩修。
- Scoped contract → rc=0，31 top-level + 15 subtests PASS，0 FAIL/SKIP。
- Scoped serial matrix `-count=3` → rc=0，93 top-level + 45 subtests PASS，0 FAIL/SKIP。
- Repository full → rc=0，1733 test events PASS，56.416s。
- Router/service full → 14:48:23–14:55:06 已完整执行，rc=1：router 4 个命名顶层失败、service 20 个命名顶层失败，均为既有基线；ReAct 失败事件 0。service 相对 12:10 基线 current-only=0、common=20、baseline-only=6（六项 lead-mining 转绿）；router 失败集合与 no-ReAct 基线完全相同。
- `git diff --check` 与 `git diff --cached --check` → rc=0，无输出。

完整证据日志位于 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/recent-task-audit-20261125/`，关键文件包括 `check-lead-react-{text-only-red-r2,strict-args-red-r1,adaptation-red-r3,adaptation-tenant-red-r1,boundary-red-r1,boundary-green-r3,repository-adaptation-green-r2,service-adaptation-green-r2,service-contract-final-r2,serial-matrix-r3,build-final,vet-final,layer-final,architecture-final,affected-full-final}.log`。

## Deviations from Assessment

- 按 §10 Main Adjudication 采用 text-only ReAct，不实现 native ToolCalls/shared adapter 改造。
- 按 §10 移除 `query_dictionary` 白名单；当前无安全词库来源。
- Agent persona/SystemPrompt/provider/model/version 注入按裁决延后到独立 lane。
- 14:48 全包已补齐 26-failure 基线比较：该时点无新增顶层失败，六项旧 lead-mining 失败转绿；但 18:13 发现三个共享文件在运行后漂移，旧结果不能关闭当前工作树回归。
- 早期一次未显式注入本地 dispatcher 的探针曾进入全局 provider 选择路径，该日志已作废；最终 GREEN 证据均使用显式 `httptest` 本地 provider，不依赖该次运行。

## Follow-ups

- 主智能体在外部 build 停止、三个共享文件哈希稳定后，以新哈希重跑 repository/router/service 全包并复核 current-only/baseline-only；14:48 旧哈希结论不得复用。
- Agent 配置注入另开 lane。
- 本 lane 不 commit/push/deploy、不发真实外发、不跑 UI。
