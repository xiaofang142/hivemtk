# Bug Fix: 客户合并保留同意与暂停

- **Slug**: customer-consent-alias（沿用上下文）
- **Fixed**: 2026-11-20
- **Assessment**: [assessment.md](assessment.md)
- **Status**: applied；冻结门禁和链 A 生产核验通过。

## Summary

客户合并在原事务内追加身份关联，不移动或复制授权事件和偏好。时间线、偏好列表及发送闸口解析同一租户内的关联身份，任何原身份仍有效的撤回或暂停继续阻断发送。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `internal/model/contact_consent.go` | 新增身份关联模型 | 租户和两端身份组成主键，两端按字典序保存；右端另有租户复合索引 |
| `internal/model/module_tables.go` | 登记关联表的模块归属 | 与原同意事件和偏好一致，归属 `reach-auto`；不增加无主表豁免 |
| `internal/repository/customer.go` | 事务内锁定客户并追加关联 | 两客户按稳定顺序加行锁；关联与既有迁移、次客户删除共用原事务 |
| `internal/repository/contact_consent_repo.go` | 解析关联身份并显式限定租户 | GORM 分层遍历去重；任一步失败返回错误，事件和偏好写入校验租户一致 |
| `internal/service/customer.go` | 使用事务内新鲜客户数据 | 保留字段和附属记录迁移；任何失败回滚身份关联，成功后才记审计 |
| `internal/service/contact_consent.go` | 汇总各原身份当前状态 | 启动迁移登记关联模型；跨身份撤回优先于暂停，暂停优先于授权 |
| `internal/service/reach_send_pipeline_steps.go` | 保留身份查询故障原因 | unavailable 在两种授权模式下均拒发，保留可重试原因 |
| 控制器与服务测试 | 新增真实 PostgreSQL 回归 | 既有接口、不可变快照、发送计数、事务回滚、并发及租户边界 |

上述路径相对于 `user-server/`。未增加路由、前端改动、依赖或 Raw/Exec 豁免。

## Conflict Rules

同一原身份仍按目的和渠道选最新事件，精确渠道偏好优先于全渠道偏好，过期暂停失效。跨身份时，任何有效撤回优先，其次任何有效暂停；另一个身份的较晚授权或恢复不能覆盖它。原身份自己的后续授权或恢复仍按既有规则生效。无有效否定时，有授权则 granted，否则 missing；独立暂停无需先存在授权事件。

时间线与偏好返回各条原始 `one_id`、ID、时间及证据。链式关联可从任一端查询；重复或反向追加同一关联幂等，已删除次客户的重复合并沿用明确错误。

## Tests Added or Updated

- `contact_consent_merge_test.go`：主次双方各为撤回或暂停的四种组合；事件和偏好前后完整快照不变，双方身份查询一致，进程内发送适配器调用为零。
- `contact_consent_merge_contract_test.go`：渠道和目的隔离、原身份恢复、链式及重复关联、四个事务故障点、并发合并、跨租户及缺上下文拒绝、写入租户不匹配拒绝。
- `controller/contact_consent_merge_test.go`：真实合并后调用原时间线和偏好 HTTP 入口；验证来源、证据、租户隔离及查询故障不伪装空列表。
- 既有同意状态机、查询故障、延迟发送后撤回、客户合并和线索识别测试同步调整模型与接口测试装配。

## Local Verification

真实 PostgreSQL 定向测试退出 0，见 [test.md](test.md) 和 [api-contract-green.log](evidence/api-contract-green.log)。所有发送器均为进程内受控测试适配器，未发送外部消息。模块归属补充后的冻结源码完整门禁全部退出 0，详见 [final3-gates.json](evidence/final3-gates.json)；源码与三份报告哈希均无漂移。

首轮完整测试发现新增关联表漏登模块归属，`TestEveryModelTableHasModuleOwner` 明确失败，其余 158 个包通过。补充登记前保留了独立[模块归属红测](evidence/module-owner-red.log)，随后模块归属、控制器、服务和仓储[四包定向回归](evidence/final2-focused.log)全部通过。当前已重新冻结源码并重跑完整门禁，没有修改扫描规则或测试基线。

## Deviations from Assessment

实现与评估契约一致。新增身份查询故障原因同时加入既有发送闸口的原因白名单，避免拒发审计丢失具体故障来源；未修改既有 missing 的发送策略。

## Delivery Boundary

链 A 于 2026-11-20 16:53:36 +08:00 发布提交 `3cb645598d8bcdee1e4e3760a1fc9b741b879eef`。独立核对运行二进制 SHA-256 `5667c7c17b96e8e01a8876f1fd1c2a0abd469ef2efa6cadb70ebe08c8d686d42`、镜像 `sha256:9e89036fa79065cf4c1228d0aebdbe4d7a7825ae121cfeda8423a2eb02ed6672`、启动时间 `2026-11-20T08:53:25.590032793Z` 与部署台账一致且容器 running；内网及公网健康均 HTTP 200/code 0。迁移表主键及右端复合索引已存在，链接数 0、无非法链接。边界前 2 条原同意事件和 1 条偏好记录的数量及 SHA-256 与迁移后完全一致；本次未在生产合并客户或发送消息。回滚点为镜像 `mtk-user-server:rollback-20261120-165136` 和二进制 `/www/wwwroot/hivemtk/user-server/user-server.bak-20261120-165136`。历史已删除且未保留身份来源的合并无法可靠回填，本次不生成推测关联。
