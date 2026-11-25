# Test: Webhook 死信并发 replay 双投

- **Slug**: webhook-dead-letter-replay-idempotency-20261125
- **Status**: verified (main independently verified 2026-11-25; E logically closed, no commit)
- **Verdict**: GREEN

## RED

- `check-e4-hook-dead-letter-tests.log`：并发 replay 收到 2 次外部请求；顺序 replay/500 失败路径通过。
- `check-e4-webhook-repo-red.log`：repository seam 未实现，编译失败（预期 RED）。
- `check-e4-webhook-service-red.log`：并发单投、稳定 delivery-id、stale recovery、crash recovery 四项失败。

## GREEN

- `check-e4-webhook-repo-final.log`：repository CAS **3/3 PASS**，rc=0，0.739s。
  - claim 一次、lease 内二次 claim 拒绝；
  - stale token 不能覆盖新 claim；
  - 过期 replaying 恢复、attempts cap。
- `check-e4-webhook-hook-tests-final.log`：hook/dead-letter **18/18 PASS**，rc=0，1.368s。
  - 并发 worker 恰好一次 receiver hit；
  - success 幂等、500 保持 dead_letter；
  - stale/fresh lease、attempts cap；
  - tenant-scoped 与 system-scope；
  - tenant 0 replay/list fail-closed；
  - candidate/completion DB 错误可见；
  - disabled subscription 可见失败且零外发；
  - 稳定 delivery-id，crash 后复用同 ID；
  - 成功/死信持久化失败结构化日志。
- `check-e4-e-contract-fixtures-green-final.log`：E3 fixture migration **11/11 PASS**，rc=0，2.394s。

## Full-service Baseline Classification

HEADs：user `d264d3104`、platform `13016b717`、assetdpo `f6b07837`、sagex `10ccdb702`。

- clean `d264d3104` + 仅 testCred blocker fix：**51 top-level FAIL**，rc=1，237.574s（`check-e4-service-d264-baseline.log`）。
- webhook 修复后、E fixture 前：**47 top-level FAIL**，rc=1，246.010s；current-only = 0；baseline-only = 4 个 webhook replay/stable-ID 测试。
- E fixture 修复后：**30 top-level FAIL**，rc=1，226.408s（`check-e4-service-full-after-e-fixtures.log`）；current-only = **0**；baseline-only = 21：
  - 4 个 webhook 测试；
  - 11 个本轮选定 E-contract 测试；
  - 6 个共享 CS01 fixture 的 MD05 测试。
- 因此剩余 30 项全部是 clean baseline 已有失败，不是本 lane 回归；未继续追逐无关生产失败。

## Test-only Fixture Notes

- `bridge_observation_test.go` 仅改 canonical account seeding、复用同一测试 DB、custom-edition context；外部既有内容保留。
- `outbound_golden_test.go` 当前 diff 仅外部 scanner 的 `golden` 字符串拆分，未修改、排除在本 lane 报告外。
- 选定 GREEN 日志仍出现背景 scanner teardown 的 `sql: database is closed`；断言全部通过。该噪声涉及全局后台 scanner 生命周期，未做广泛重构，按已知负路径 teardown noise 保留。

## Delivery Guarantee Boundary

系统保证并发 claim 单投、条件完成与 at-least-once 恢复；crash-after-delivery 会复用同一 `X-Hivemtk-Delivery-ID` 重发。端到端 exactly-once 取决于 receiver 去重，不作此宣称。

## Final Stable-Worktree Gates

- full bridge：179/179，rc=0，7.990s（`check-e4-final-bridge-full-r2.log`）；
- full channelgw：**122 top-level tests：121 passed / 1 skipped（`TestWSTransport_NilPipeline`）**，rc=0，7.829s（`check-e4-final-channelgw-full.log`）；
- hook/dead-letter：rc=0，1.633s（`check-e4-final-hook-tests.log`，18/18）；
- bridge vitest：108/108 files，1328 passed / 7 skipped（`check-e4-final-bridge-vitest.log`）；
- `go build ./...`：rc=0（`check-e4-final-build.log`）；
- `go vet ./...`：rc=0（`check-e4-final-vet.log`）；
- layer boundary：rc=0，1758 files / 0 violations（`check-e4-final-layer.log`）；
- architecture：rc=0，仅既有 `wecomCSService` interface warning（`check-e4-final-architecture.log`）。

主智能体独立最终复验：`main-e-final-verification.txt`，08:34:20–08:35:42，overall_rc=0；E 整体逻辑关闭（不 commit）。真实外部/UI 仍由 F 边界阻塞。
