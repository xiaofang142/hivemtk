# Bug Verification: 客户合并保留同意与暂停

- **Slug**: customer-consent-alias（沿用上下文）
- **Tested**: 2026-11-20
- **Assessment**: [assessment.md](assessment.md)
- **Fix**: [fix.md](fix.md)
- **Result**: verified and deployed；定向回归、冻结完整门禁及链 A 生产核验通过。

## Summary

真实客户合并、数据库仓储、原时间线与偏好 API、发送管线共同验证撤回和暂停仍生效。原始事件和偏好的完整快照保持不变，受控发送器调用为零。未以数据库模拟替代事务，也未执行外部发送。

## Reproduction Before Fix

[merge-red.log](evidence/merge-red.log) 中四个主次撤回/暂停组合均失败：合并后只查到部分事实或遗漏暂停，默认发送模式错误放行。该日志是隔离各用例表数据后的实际行为红测，不使用早期测试装配或编译错误作为复现证据。

[tenant-write-red.log](evidence/tenant-write-red.log) 另复现缺租户或系统作用域下的错租户事实写入未报错。加入显式租户一致性检查后相同契约通过。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 原症状与扩展契约 | 控制器、服务、仓储定向 Go 测试 | pass | [api-contract-green.log](evidence/api-contract-green.log) |
| 原授权状态机 | 同一原身份最新事件、渠道优先、暂停到期 | pass | 未改变原身份自己的恢复规则 |
| 合并事务 | 关联创建、主客户更新、附属更新、删除四处注入故障 | pass | 原错误返回，客户、关联、会话和事件迁移全部回滚 |
| 并发与关联组 | 并发合并、链式合并、重复和反向关联 | pass | 任一原身份可查询全部事实，关联不重复 |
| 租户边界 | 两租户相同 one_id、跨租户和缺上下文、系统作用域错租户写入 | pass | 不可读取或改写其他租户 |
| 发送查询故障 | 关联、事件、偏好故障；两模式、多渠道、备用适配器 | pass | 零外发、具体原因与可重试审计保留；恢复后重新判定 |
| 延迟发送 | 入队后撤回再处理 | pass | 继续被授权闸拦截 |
| 首轮冻结完整门禁 | lint、audit、vet、build、test-go | fail | 前四项退出 0；完整测试仅新增表无主检查失败，158 个包通过，源码无漂移 |
| 模块归属补充 | 归属扫描与四包定向回归 | pass | [module-owner-red.log](evidence/module-owner-red.log) → [final2-focused.log](evidence/final2-focused.log)，登记到既有 `reach-auto`，未改基线 |
| 最终冻结完整门禁 | lint、audit、vet、build、test-go | pass | 五项均退出 0；源码与报告哈希无漂移，见 [final3-gates.json](evidence/final3-gates.json) |
| 链 A 与生产核验 | 迁移、二进制、健康、事实保全、回滚点 | pass | 生产提交 `3cb645598d8bcdee1e4e3760a1fc9b741b879eef`；核验资料见 `api-deploy.log`、`production-*-after.json` |

定向命令在 `user-server/` 执行，使用专用 PostgreSQL `127.0.0.1:8217`：

```text
go test ./internal/controller ./internal/service ./internal/repository -run 'TestContactConsent|TestPipeline_(Consent|DeferredThenRevoked)|TestCustomerService_MergeCustomers|TestLeadMining|TestFindExistingWithRetryHonorsAttemptLimit' -count=1 -p=1 -timeout=5m -v
```

三个包均退出 0。控制器缺租户按既有错误映射返回 HTTP 400；注入查询超时返回 HTTP 500，均未返回伪装成功的空列表。

首轮完整结果及失败证据分别见 [initial-full-gates.json](evidence/initial-full-gates.json) 和 [initial-full-test-go.log](evidence/initial-full-test-go.log)。失败是新增关联表遗漏模块归属，独立重现后补入与同意事件、偏好相同的模块清单；没有新增豁免。补充后的四包定向测试同时覆盖模型归属、实际 HTTP、合并和发送契约，全部退出 0，见 [final2-focused.log](evidence/final2-focused.log)。最终冻结五项门禁全部通过，完整原始输出归档于 [final3-test-go.log](evidence/final3-test-go.log) 及同目录的 lint/audit/vet/build 日志。

## Residual Risks

- 历史已删除且没有身份映射的合并无法可靠回填，不伪造关联或授权。
- 发送验证使用本地受控适配器；未向真实客户发送消息。
- 本轮沿用现有界面和路由，通过实际 HTTP 控制器验证客户 360 数据契约，未进行浏览器界面测试。
- 本地完整回归已通过；生产核验仍需链 A 后进行。冻结源码见 [final3-frozen-source.json](evidence/final3-frozen-source.json)。

## Recommendation

完整门禁及链 A 生产核验均通过，FA-14 验收条件已满足。生产核验只检查迁移结构和既有事实摘要，没有执行生产客户合并或外发；实际业务行为由隔离的真实 PostgreSQL 和受控适配器覆盖。保留后续报告、证据，不将未执行的生产业务操作描述为验收事实。
