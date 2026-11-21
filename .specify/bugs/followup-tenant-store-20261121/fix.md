# Bug Fix: 跟进提醒租户授权 + 统一持久存储（FA-26/27）

- **Slug**: followup-tenant-store-20261121
- **Fixed**: 2026-11-21
- **Source**: docs/未完成任务清单.md FA-26、FA-27
- **Status**: applied

## Summary

跟进提醒此前：①Reminder 与进程内 map 不带租户，所有读写忽略 ctx 租户，读口只按 ownerID、完成/取消只按 reminderID（普通销售取得他人 ID 即可改写他人待办）；②HTTP/回款钩子/AI 工具三条生产链各自 new 一份内存实例——AI/回款创建的提醒今日跟进看不到，重启即丢。现改为：行带 tenant_id 并全链按 ctx 租户过滤；HTTP 写口带操作者授权（本人或管理员）；三条链共享同一持久仓储（follow_up_reminders 表，AutoMigrate 登记）。

## Changes

| File | Change |
|---|---|
| `internal/model/followup_reminder.go` | 新增 FollowUpReminder 模型（tenant_id 复合索引前缀，rem_ 串主键） |
| `internal/pkg/db/migrate.go` | AutoMigrate 登记（含背景注释） |
| `internal/repository/followup_reminder.go` | 新增 FollowUpReminderStore 接口 + gorm 实现（scoped 租户过滤，Update RowsAffected=0 → not found 防跨租户改写） |
| `internal/service/followup_reminder.go` | Reminder 增 TenantID；store 集成（nil=内存降级仅供测试）；fetch/persist 收口；CompleteWithResultFor/CancelFor 带操作者授权（空操作者拒绝，系统内部链 Complete/Cancel 保留系统权威口）；四个读口 store 化 + ctx 租户过滤 + 读失败记日志不静默假零 |
| `internal/controller/followup.go` | Complete/Cancel 写口改 *For（contextOperatorID + role 管理员豁免；未登录 401） |
| `internal/app/order_draft_wiring.go` / `payment_wiring.go` / `tool_provider_wiring.go` | 三条生产链改 NewFollowUpServiceWithStore（同一 DB 句柄 ⇒ 同一权威存储；db 缺席回退内存并保持原旗标语义） |
| `internal/service/followup_reminder_tenant_store_test.go` | 新增：授权矩阵 + 共享存储跨实例/重启可见性 |

## Local Verification

- 专项 2 用例 + `-race` 通过（green.log / race.log）：跨租户同 ownerID 全口径隐藏存在性；同租户他人待办拒绝且零副作用；管理员豁免；本人可操作；重复完成守卫保持；实例 A 创建 → 实例 B 可见；重启（同存储新实例）行在；取消跨实例一致。
- 既有回归 `-run 'TestFollowUp|TestFollowup|TestCollection|TestClosedLoop|TestClosed_Loop'`：71 PASS / 0 FAIL（regression.log，含 closed_loop/collection/order_draft 关联链）。
- 全仓 build 通过。
