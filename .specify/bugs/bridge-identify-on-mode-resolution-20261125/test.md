# Bug Verification: tenant-on identify_code fallback

- **Slug**: bridge-identify-on-mode-resolution-20261125
- **Tested**: 2026-11-25T03:20:22+08:00
- **Result**: verified（scoped；主智能体独立复验 pending）

## RED → GREEN

| Case | RED | GREEN |
|------|-----|-------|
| valid channel-level identify_code | ambiguous | PASS |
| alias channel | ambiguous | PASS |
| WS identify_code | ambiguous | PASS |
| channel fallback query error | ambiguous / error swallowed | PASS, original error visible |
| disabled-only channel code | 未覆盖 | PASS，显式出站校验拒绝 |

日志：`check-e4-on-mode-red-r2.log`、`check-e4-on-mode-disabled-green.log`。

## Preserved Contracts

- off-mode identity-only，不绑定租户、不改写 request；
- 缺码/无效码 fail-closed；
- account/channel mismatch 与已绑定不同租户拒绝；
- edition gate 保留；
- bartoken/account_id 未恢复为权威。

## Boundary

未 commit/push、未部署、未外发、未 UI。E 整体仍未关闭。
