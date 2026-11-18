# R3 修复记录

[评估](assessment.md)的恢复来源保持。

- 三执行者回写复用`replayUpdate`，要求明确租户、当前sending、非空令牌、认领时间及数据库实际时钟下有效租约。
- `MarkSent`只确认下游交接；改排清租约并递增原次数，不重置已用预算。
- `FinishReplay`仅允许规定终态，只有sent填`sent_at`；失败、作废或未知不伪造交付时间。
- 原直接对pending空令牌改排的测试迁移为真实到期认领。人工`ResolveUnknown`及过期回收语义未修改。

真实RED repository 1.330秒；27场景和同令牌unknown覆盖回归通过。未重放生产积压。
