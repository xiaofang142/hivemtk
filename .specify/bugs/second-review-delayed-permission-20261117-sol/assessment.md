# 延迟投递双路径权限评估

Verdict: valid；Severity: high。范围对应二次审核R1/R2，并封闭同表离线回扫旁路。

## 证据和决策

临时真实PG审核已复现：夜间重放在人工锁存在时仍创建出站；较新delivered答案存在时仍补投旧内容。离线回扫操作同一张reach_delayed_outbound表，却只有pending状态抢占，没有当前租约或人工/旧答案复核。

不拼接会话ID、不持数据库锁跨网络、不关闭免打扰、不主动释放积压。两路径共用只读投递资格：按明确租户、平台、账号、接收客户查询唯一有效会话；缺失、模糊、故障、人工/DNC或人工锁不可确认均不发。已确认同租户平台账号会话的较新delivered非审计答案才作废旧答案；pending/failed/系统/审计或别账号不能作为已回复证据。

不新增会话字段或猜测旧队列归属。离线回扫签发与主链路同源随机令牌/租约，执行前再次确认当前DB载荷与共用权限；所有回写通过R3严格栅栏。保留卡片交主链路、原重试上限及退避，不赋予unknown自动重放能力。旧离线方法签名需要令牌时明确迁移全部调用方，不保留无令牌执行者回写。

## Files likely to change

- `user-server/internal/service/webhook_outbound.go`
- `user-server/internal/service/bridge_offline_replay.go`
- 新 `user-server/internal/service/delayed_replay_permission.go`
- 新 `user-server/internal/service/delayed_replay_permission_test.go`
- 新 `user-server/internal/repository/customer_session_replay.go`
- 新 `user-server/internal/repository/message_hub_replay.go`
- `user-server/internal/repository/bridge_offline_replay_repo.go`（仅延迟任务查询/认领，孤儿结算不变）
- `user-server/internal/repository/delayed_outbound_repo.go`（仅指定任务同源租约认领）
- `user-server/internal/service/webhook_outbound_quiet_result_test.go`
- `user-server/internal/service/webhook_outbound_replay_lease_test.go`
- `user-server/internal/service/webhook_outbound_send_retry_test.go`
- `user-server/internal/service/bridge_offline_replay_test.go`
- 本评估的fix/test及重建总计划。

## 验证

真实专用PG先复现两路径人工接管、较新回答仍投递，保留正常AI会话合法投递对照。再覆盖缺/多会话、DNC、锁读故障、跨租户/账号/渠道答案、审计/失败答案、租约与两扫描者竞争。明确更新旧合法夹具的真实会话/租户/认领前提，不删除原断言或用全部拒绝伪造通过；无生产发送或队列写入。
