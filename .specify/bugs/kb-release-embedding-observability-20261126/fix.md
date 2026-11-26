# 修复记录

## 依据

先补真实服务行为失败用例，再做独立状态与审计补丁；不新增自动发布或重试入口。正文发布事务内记录 embedding=unconfirmed；提交后独立返回 completed/failed/unavailable/not_required，并尝试在保留租户上下文、脱离请求取消且上限 5 秒的审计上下文记录结果。审计失败返回 embedding_audit_recorded=false，不反转正文发布成功。

## 当前状态

修订前完整真实 service 包的 24 项 KB 顶层测试及 14 项子测试通过（默认 vet、无 overlay），见 bounded-memory-package-test.log。

复审发现无新正文时未落持久审计，以及补算动作未登记模型值域。新增契约测试后，模型反例失败；真实完整 service 包的 5 个状态子场景也全部失败，见 audit-contract-red.log、audit-contract-service-red.log。现已统一使用 KBAuditEmbeddingStatus，四种结果（含 not_required）均写独立审计；最终绿测与构建待隔离副本记录。

## Deviations from Assessment

必要增加模型动作常量和值域测试两个文件，与 assessment 同步；不新增表、迁移或恢复任务。并行工作区的 i18n 孤儿词条使 make audit 退出 2，该结果不归为本轮业务测试失败；隔离最新远端后的门禁另记。
