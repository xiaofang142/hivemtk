# R-01 发布后的向量补算状态丢失

## 根因与现象

`KBReleaseService.PublishPending` 已提交正文及版本后调用 `embedApplied`。补算错误只写进程日志；API 的 PublishResult 和已有发布审计没有补算状态，调用方无法区分正文生效与向量完成。既有 `EmbedAndPersistChunks` 不会替该调用记录持久重试；独立 reindex 为现有人工恢复入口。

## 修复选择与反证

- 保留发布成功，不把提交后的补算故障当发布失败，避免重放已成功的正文发布。
- 复用现有发布审计，在发布事务内注明补算待确认，再用独立补算审计记录结果；不新增任务表或不受控后台重试。
- 发布结果增加独立 embedding_status 与 embedding_audit_recorded。补算失败/未装配为需要人工 reindex 的状态；审计失败明确返回未记录，不能假称恢复线索已落盘。
- 仅退役/无新正文为 not_required；新正文且无 embedder 为 unavailable，不能假称完成。
- 请求取消后，用保留租户上下文的有界 cleanup context 保存结果；不把正文或原始供应商错误放入审计。
- 本次修复可观测性及恢复线索，不声称已具备自动补算调度闭环。

## Files likely to change

1. `user-server/internal/service/kb_release.go`
2. `user-server/internal/repository/kb_release.go`
3. `user-server/internal/service/kb_release_test.go`
4. `user-server/internal/model/kb_release.go`
5. `user-server/internal/model/kb_release_test.go`

复审补充：无新正文也必须保存 `not_required`，避免持久记录永远停在 `unconfirmed`；补算审计统一使用模型登记的 `embedding_status` 动作，状态保存在 `NewValue`，因此必要扩展模型常量与值域契约测试，不新增表或迁移。

## 验收

先用真实服务和窄仓储替身验证失败：失败/成功/未装配/无新正文/审计失败的响应与记录各自准确；正文只提交一次，失败不返回发布错误；取消请求仍能尝试保存审计。随后运行该服务相关测试、构建与门禁。提交及生产验证另记实际结果。
