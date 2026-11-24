# Bug Verification: bridge 显式出站账号校验与 replay 租户反查 seam

- **Slug**: outbound-explicit-account-validation-20261125
- **Tested**: 2026-11-25T02:42:30+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified（E3 scoped；主智能体已于 2026-11-25 独立复验，证据 `main-e3-verification-20261125.txt`；E 整体未关闭）

## Summary

actual worktree 已完成显式账号 fail-closed、registry-owned 账号解析、`BridgeAccountTenantRepository` replay seam 与 10 个 capability 夹具迁移。E3 scoped 定向回归与 build/vet/layer/architecture 四道门均通过；未 commit/push、未外发、未 UI。

## RED → GREEN Evidence

| Gate | RED | GREEN |
|------|-----|-------|
| 双文件架构闸 | `check-e3-architecture-red-actual.log`：rc=1，命中 webhook 直查能力位、`:508` 直查 DB、`:652-664` 直查 model/GORM | `check-e3-targeted-green-actual.log`：rc=0 |
| 显式账号行为矩阵 | `check-e3-explicit-account-red-actual.log`：rc=1，不存在/禁用/错渠道/跨租户/edition 禁止/channel+account 仓储错误均暴露错误行为 | 同 final targeted 日志：rc=0 |
| 10 个 capability 用例 | `check-e2-capability-clean-head-all.log`：真正未修改 HEAD 上 10 FAIL / 3 PASS，全部 `capability_mapping_incomplete` | canonical 夹具迁移后，同 final targeted 日志内原 13 个 `TestSendOutbound_Bridge_*` 全 PASS |
| A dispatch 回归 | 已有 A staged 错误传播补丁 | 同 final targeted 日志：5 个错误传播用例 + 2 个 constructor 用例 + SelectionMatrix（8 subtests）全 PASS |
| repository reverse lookup | 新 seam 测试随实现补齐 | no-row / tenant0 / one-tenant / ambiguous / query-error 全 PASS |
| replay 租户仲裁 | 新 replay 测试随实现补齐 | 未命中回退、错误/歧义不回退、无归属拒绝、错误/歧义立即 abandon 全 PASS |

## Final Targeted GREEN

日志：`check-e3-targeted-green-actual.log`

命令覆盖：

```text
go test ./internal/repository ./internal/service \
  -run '^(TestBridgeAccountTenantRepository_ResolveTenantIDMatrix|TestOutboundAccountValidation_NoDirectDataAccess|TestSendOutbound_Bridge_|TestChannelDispatchService_|TestResolveDelayedReplayTenant_Arbitration|TestReplayDelayedOutbound_TenantLookupFailureAbandons)' \
  -count=1 -v
```

实际读数：

- `internal/repository`：PASS，rc=0；
- `internal/service`：PASS，rc=0；
- 原 bridge 13 用例：13/13 PASS（10 个迁移夹具 + 3 个既有 PASS）；
- 新增显式账号矩阵：有效/空账号 + 8 个 fail-closed subtests 全 PASS；
- A dispatch：8 个顶层用例全 PASS（含 5 个错误传播、2 个 constructor、SelectionMatrix 8 subtests）；
- replay：5 个仲裁 subtests + 2 个 abandon subtests 全 PASS；
- 架构闸：PASS。

## Four Full Gates

| Gate | Log | Result |
|------|-----|--------|
| `go build ./...` | `check-e3-build-actual.log` | PASS，rc=0（02:42:22） |
| `go vet ./...` | `check-e3-vet-actual.log` | PASS，rc=0（02:42:12） |
| `python3 scripts/check-layer-boundary.py` | `check-e3-layer-boundary-actual.log` | PASS，rc=0，1757 files / 0 新增跨层违规 |
| `bash scripts/check-architecture.sh` | `check-e3-architecture-gate-actual.log` | PASS，rc=0，仅既有 `wecomCSService` interface warning |

## Capability Baseline Classification

真正未修改 HEAD（`head-snapshot-934825c2d/`）上：

- `^TestSendOutbound_Bridge_`：13 个用例，10 FAIL / 3 PASS；
- 10 个失败全部为 `capability_mapping_incomplete`；
- 根因是旧夹具使用外部字符串账号且未播种 canonical `Channel/ChannelAccount`，不是 capability 生产缺口；
- 迁移后 final targeted GREEN 中 13/13 PASS，capability 门未放宽。

## Residual Boundaries

- 本 verdict 只覆盖 E3 scoped 修复，不覆盖 unrelated full-service 存量失败。
- 未跑完整 `go test ./internal/...`、bridge/channelgw 全包、bridge vitest、UI、真实外发或生产验收。
- E 整体仍有 identify_code bridge/channelgw/JS 迁移工作，进度见 temp `acceptance-progress.md`；本 lane 不自行关闭 E。
- 所有修改留在 actual worktree，未 commit/push。

## Recommendation

E3 scoped 本地验收已由主智能体独立复验（`main-e3-verification-20261125.txt`）；本 bug lane 可关闭，但 E 整体协议迁移与完整验收仍未关闭。

## E4 Addendum — identify_code 协议迁移与完整验收（2026-11-25）

**Verdict: partial / implemented, main verification pending.** E 整体未关闭。

### Completed

- bridge 全包从 54 个 top-level FAIL 降到 6 个；剩余均为已确认生产缺口，未改测试掩盖：
  - on-mode identify_code 解析缺租户上下文（3）；
  - 缺参/超大 body 校验晚于租户解析（3）。
- channelgw CS02 已迁移为单租户 canonical Channel/ChannelAccount + identify_code fixture，4/4 PASS，恶意 extra account_id 仍被丢弃。
- channelgw 全包仅剩 3 个 CS06 基线失败；未修改 HEAD 同样 3/3 超时，已隔离为独立 assess-only lane。
- bridge vitest：108/108 文件通过，1328 passed / 7 skipped；旧 `X-Bridge-Token` 断言已迁到 `X-Channel-Identify-Code`，relay 边界与鉴权断言未放宽。
- hook dead-letter：持久化、列表、顺序幂等 replay、失败保留死信均 PASS；并发 replay 双投已复现并开 assess-only lane。

### E4 Evidence

- `check-e4-bridge-full-after-migration.log`：6 个生产缺口 FAIL。
- `check-e4-channelgw-full-after-migration.log`：仅 3 个 CS06 基线 FAIL。
- `check-e4-cs06-clean-head-baseline.log`：未修改 HEAD 3/3 FAIL。
- `check-e4-bridge-vitest-full-green.log`：108 files / 1328 passed / 7 skipped。
- `check-e4-hook-dead-letter-tests.log`：3 PASS / 1 concurrent replay FAIL（预期暴露缺陷）。
- `check-e4-diag-identify-resolution-on.log`：on-mode 无租户 ctx 解析出空 ChannelCode。

### New Assess-only Lanes

- `.specify/bugs/bridge-identify-on-mode-resolution-20261125/assessment.md`
- `.specify/bugs/bridge-param-validation-order-20261125/assessment.md`
- `.specify/bugs/cs06-pipeline-fixture-drift-20261125/assessment.md`
- `.specify/bugs/webhook-dead-letter-replay-idempotency-20261125/assessment.md`

### Boundary

未 commit/push、未部署、未外发、未 UI。E 整体保持开放，等待主智能体对 E4 及上述 assess-only lane 的独立复验/裁决。
