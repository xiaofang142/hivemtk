# Test: service 测试凭据 helper 递归

- **Slug**: service-test-cred-recursion-20261125
- **Status**: verified (scoped; main independent verification pending)
- **Verdict**: GREEN

## RED

- `check-e4-test-cred-red.log`：focused `TestTestCred_DefaultsAndEnvOverride` 触发 `fatal error: stack overflow`，rc=1；
- 栈帧重复 `testCred → integration_test.go:30`，约 258 万帧被省略；
- 根因是 map literal 在创建前递归调用自身。

## GREEN

- `check-e4-test-cred-green.log`：同一 focused 测试 PASS，rc=0，0.845s；
- 覆盖默认值、6 个 literal key 抽样、未知 key 回退、`old-key` 别名 env override；
- `check-e4-service-full-after-testcred.log`：full service 不再栈溢出，rc=1，246.010s，剩余 47 个 top-level 行为失败，转独立基线分类，不算本 lane 回归。

## Remaining Boundary

主智能体已审查最小 diff 与 RED/GREEN，独立 scoped verification 仍 pending；E 整体未关闭。
