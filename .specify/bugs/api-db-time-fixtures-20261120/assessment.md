# Bug Assessment: PostgreSQL时间夹具精度

- Created: 2026-11-20
- Verdict: valid
- Severity: low（测试确定性）

干净环境全量go test中TestFollowFlush_MarkDueByConversation与TestInboxConversation_UpdateLastMessage_AdvancesActivity失败，实际持久化时间仅比time.Now纳秒夹具少亚微秒部分。PostgreSQL仅保留微秒，测试拿纳秒原值精确Equal使合法行为误红。

修复限于夹具：将future与t0在写库前Truncate(time.Microsecond)，保留原精确Equal断言、不改业务逻辑、不放宽时间比较。

Files likely to change:
- user-server/internal/repository/delayed_outbound_flush_test.go
- user-server/internal/repository/message_hub_inbox_lifecycle_test.go

验证：完整repository包重跑，保留初次全量失败证据；其余完整Go测试继续。
