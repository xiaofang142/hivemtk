# Test: bridge channel hint 负例迁移

- **Slug**: bridge-channel-hint-test-drift-20261125
- **Status**: verified (scoped; main independent verification pending)
- **Verdict**: GREEN

## RED

`check-e4-final-bridge-full.log`：旧断言期望 channel hint 不一致返回 `ErrBridgeTenantAmbiguous`，实际 `err=nil`，rc=1。

## GREEN

- `check-e4-channel-hint-test-green.log`：新契约与 account mismatch 负例 **2/2 PASS**，rc=0，1.223s。
- `check-e4-final-bridge-full-r2.log`：`go test ./internal/bridge -count=1` **179/179**，rc=0，7.990s。

## Boundary

仅测试断言迁移；identify_code 权威、canonical channel 防误路由、account mismatch fail-closed 均保持。未 commit/push/deploy/外发/UI。
