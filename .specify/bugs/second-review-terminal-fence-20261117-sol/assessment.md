# 延迟执行终态栅栏评估（恢复记录）

Verdict: valid / high。原评估在新测试与产品修改前建立，随后被外部删除。本文件根据已保存审核、授权及真实RED重建。

根因：MarkSent/ScheduleRetry/FinishReplay只按ID及可选令牌更新，旧相同令牌可覆盖unknown或过期执行；失败收口也填写sent_at。

决策：执行者回写必须明确租户、当前sending、非空令牌、claimed_at和有效租约，单条件UPDATE保持原接口，不持数据库锁跨网络。终态仅sent/superseded/failed/unknown；只有sent填写sent_at。旧测试未认领pending空令牌操作改为真实认领前置，不保留危险自动执行兼容。人工ResolveUnknown与过期回收另有语义，本次不改。

## Files likely to change
- `user-server/internal/repository/delayed_outbound_repo.go`
- `user-server/internal/repository/delayed_outbound_terminal_fence_test.go`
- `user-server/internal/repository/delayed_outbound_retry_lane_test.go`
- `user-server/internal/repository/delayed_outbound_lease_test.go`
- 本目录fix/test和总记录。

已有真实RED：三操作各九场景与unknown旧令牌覆盖，repository1.330秒；合法完成/改排与已取消控制通过，其它保护和失败时间语义失败。无生产队列操作。
