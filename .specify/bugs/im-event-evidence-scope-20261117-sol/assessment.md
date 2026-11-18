# IM事件证据的租户、账号与失败边界

Verdict: valid / high。

## 已确认根因

024接缝现将平台事件审计与交互行的tenant_id取自消息Extra，而非请求权威租户。两表幂等唯一域不含账号，跨账号同名事件可能被吞。服务端审计或交互写失败仍可能返回Accepted=true，违背同步落库证据要求。交互答复状态查询也未限定账号。

## 论证与决策

- 不改普通消息、AI、平台认证、夜间保护或发送次数。平台事件租户只取已确认ctx；拒绝nil、取消、SystemScope及SaaS未绑定上下文。单租户无绑定仅显式默认租户。
- 保留现有字符串tenant_id数据库类型，以ctx租户的十进制字符串写入，消息Extra不作为授权。
- 审计与交互幂等均包含账号。新增正式迁移更新旧唯一索引，不仅修改模型标签而假定旧索引已变。
- 交互登记与其审计在同一仓储事务提交；证据写入失败不宣称接受，不发布后续总线事件。成员/机器人事件也必须审计成功。重复事件仍可幂等接受。
- 交互答复/查询显式限定tenant、channel、account和interaction ID，迁移唯一Telegram调用与原测试。编辑/撤回的会话范围另立根因评估，不在本轮静默扩大。

## Files likely to change

- `user-server/internal/service/inbox_ingress_platform_events.go`
- `user-server/internal/service/webhook_channel_telegram.go`：仅交互答复状态调用
- `user-server/internal/repository/channel_event_audit.go`、`channel_interaction.go`、新`channel_event_scope.go`
- `user-server/internal/model/channel_event_audit.go`、`channel_interaction.go`
- 新`user-server/internal/migration/migrations/channel_event_identity_migration.go`及测试、`initial_schema.go`注册
- 新`user-server/internal/service/inbox_ingress_event_evidence_test.go`
- 原`inbox_ingress_platform_events_test.go`、`inbox_ingress_interaction_test.go`：更新无证据不能Accepted和账号参数，保不产生消息/不触发AI的断言
- 本目录fix/test及总计划/发现/进度

## 验证范围

真实专用PG先复现：Extra伪租户、同账号重投与跨账号同ID、审计/交互故障、缺依赖/非法ctx。修复后保留正向无消息无AI、原交互答复幂等，另验证迁移旧索引、失败事务回滚。仅127.0.0.1:8217，密码不输出；不操作生产事件或队列，不修改Git索引。
