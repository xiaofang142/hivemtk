# Fix: service 测试凭据 helper 递归

- **Slug**: service-test-cred-recursion-20261125
- **Status**: applied（test-only）
- **File**: `user-server/internal/service/integration_test.go`

## Change

仅将 `parts` map literal 中 6 个 self-referential key 从 `testCred("...")` 改为字面量：

- `old_key` / `old_secret`
- `keep_key` / `keep_secret`
- `new_key` / `new_secret`

`HIVEMTK_TEST_CRED_*` env override、字符数组占位值、未知 key 回退 `test_key` 均保持不变。未改生产代码或 integration setup。

## Boundary

- HEAD `d264d3104177587a87dd3c88e33fbe898536e688`；
- 修改前 staged/worktree SHA256：`f380719fb8d1968ac9f604f57b2dfdea46e4ff6181d6d7758bd4f97782a268d7`；
- 未 commit/push/deploy/外发/UI。
