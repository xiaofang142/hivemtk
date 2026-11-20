# Bug Fix Report: 推理可靠交付

- Slug: inference-reliable-delivery
- Fixed: 2026-11-20
- Assessment: ./assessment.md
- Status: implemented，定向与八项全量门禁通过，上线待独立核验

## Summary

回复和人工交接以独立持久账本记录意向、发送尝试、外部受理和确认。缺少可靠发送端或当前授权策略时落库 blocked，返回可查询的键；同步端口返回 nil 不再充当可靠确认。只有匹配当前身份及动作、包含有效回执的 confirmed 记录才能成为已发布回复。

## Changes

- 新 delivery 契约及身份测试：显式租户、稳定消息 ID、渠道账号、客户、会话、Agent 与动作共同构成键；正文是不可变指纹的一部分，不通过修改正文生成新键。
- 新 model/repository/service 与真实 PostgreSQL 测试：唯一约束、短事务租约、token 比较交换、持久发送尝试、权威回执查询、丢回执及重启恢复；数据库事务不跨网络调用。
- runtime 的编排器、执行器、消息类型及回归夹具：显式 Gateway 替代实际旧 Publish/Trigger 调用；意向与实际确认分层，重放不重复累计确认统计。
- 新 Controller 及 HTTP 测试：鉴权组内查询和重新授权重试；未确认 202、确认 200、确定拒绝 409、租户缺失 403、不可见 404、存储不可用 503，错误仍返回安全查询凭据。
- app、main、router、AutoMigrate 与响应常量：装配真实持久服务；生产不自动接入观察态工具策略或不具备原子幂等的旧外发端口。

## Tests

旧接口重复请求真实 RED：无持久交付元数据且相同消息发布两次。新增边界 RED 发现负 Agent 版本、回执键/动作不匹配及拒绝统计问题，均已最小修复。真实数据库写故障 RED 发现错误响应丢失已落库查询键，服务及 HTTP 各层修复后保留实际存储状态，绝不把内存回执冒充持久确认。

最新定向 `go test` 覆盖 delivery、runtime、service、repository、controller、app 六包，173 个叶用例通过，0 失败、0 跳过；其中 RUN03 新增 66 个，原 RUN01/RUN02 107 个保持通过。全部数据库验收仅使用本地隔离测试库和受控对端信号，未触达第三方生产发送。

## Local Verification

八项全量门禁退出码均为 0：Go 896.5s、race 1434.1s、vet 28.4s、build 24.8s、lint 27.9s、layer 0.5s、architecture 60.9s、audit 175.6s。6677 份冻结文件无漂移。lint 为 0 issues，layer 检查 1600 份非测试文件无违规；架构既有 warning 与 audit 的跨仓、覆盖率 profile、本机探针 SKIP 不能视为对应维度通过。Graphify 实际刷新失败（缺模块），没有宣称刷新成功。生产上线须另核二进制、镜像、回滚和新表约束，见正式 test.md。

## Deviations

查询凭据的故障保留、回执身份绑定及负版本拒绝是边界测试发现的同一交付契约缺口，仍在 assessment 文件范围内。新增 contracts_test.go 与各层测试未扩展产品职责。sender 必须提供原子幂等 Send 与权威 Lookup；不具备该能力的端口不能接入。租约到期旧执行者不拥有完成写权限，即使迟到调用，外部仍须按同一键原子去重。

## Boundaries

本项不提供工具跨请求幂等、不改变推理检查点身份指纹，也不宣称模拟对端就是第三方生产送达。人工交接 confirmed 仅能来自符合契约的外部受理端；handoff_to_human 本身仍只是决策。生产未装配发送/授权端时保持关闭，账本查询和重试可用；重试先查证再决定是否允许发送，不无条件重放。
