# Assessment Follow-up: 批次边界必须到达向量服务

- Date: 2026-11-20
- Parent: [assessment.md](assessment.md)
- Verdict: valid；继续原 FA-12 修复车道。

最终复核发现，同步爬取入口虽然向分片器和索引传递批次 context，`Vectorizer.EmbedBatch` 内部重新创建 `context.Background()`。向量服务因此丢失租户、批次截止时间和取消信号，三十分钟执行边界不能约束该阶段。

新增真实 PostgreSQL 服务回归 `TestCrawlerRecovery_EmbeddingRetainsBoundedTenantContext`，从 `RunBatch` 进入，只替换网络和向量服务为明确合成信号。修复前退出 1，分别观察到 tenant=0/false、无 deadline、取消后 ctx.Err() 为 nil。批次与文档失败落库断言正常，证明缺口在向量调用边界。

最小修复为新增兼容的 `EmbedBatchContext`，同步爬取处理使用它。旧 `EmbedBatch` 入口及普通异步导入调用保持原行为。最终门禁必须在源码冻结后重新运行；此前两轮完整回归已主动终止，不能计为通过。

## 与已合入修复的集成复核

独立集成目录以双远端共同提交 `2986a7c540b45e2031c61a3c11ac440385f9a25b` 为基线，保留该提交的同步失败与普通异步导入行为。爬虫旧测试依赖不支持事务的数据库回调，改为专用 PostgreSQL 夹具，保留原测试名及断言意图，并覆盖新检查点事务。

集成红测实际发现三个缺口：可信系统扫描未收窄为来源租户；QA 或候选持久化失败时文档仍为 indexed；同步文档读取或 processing 写入失败时未保留 failed。见 [integration-gap-red.log](evidence/integration-gap-red.log)。修复分别在服务入口收窄系统上下文、在检查点失败事务中同步写文档 failed、复用有界失败落库入口。普通缺租户请求仍拒绝，原异步上传函数未改。

对应定向红测转绿，见 [integration-gap-green.log](evidence/integration-gap-green.log)。扩展集成回归同时覆盖上游既有测试、租约恢复和 FA-13，见 [integration-focused.log](evidence/integration-focused.log)。源码冻结清单见 [frozen-source.json](evidence/frozen-source.json)；完整门禁和生产发布仍以各自最终证据为准。

## 终态检查接入与最终门禁复核

最终 lint 指出两个仅被测试调用的旧辅助函数。新增真实 PostgreSQL 回归将索引写入后的文档置回 processing，先实际观察到页面入库仍返回 nil，见 [terminal-red.log](evidence/terminal-red.log)。现将已有终态检查接入同步处理之后、QA 与完成哈希之前；未达 indexed 时保留 failed 和空完成哈希。移除旧脱离请求辅助函数及仅验证该函数的测试，控制器实际手动同步/重试回归改为同时断言请求取消后租户、trace、有效截止时间均保留。

扩展定向回归再次通过，见 [final3-focused.log](evidence/final3-focused.log)。另仅修正继承文件 `order_change_request.go` 的一行 gofmt 对齐。审计发现的唯一孤儿翻译是已由轮数文案替代、没有消费者的分钟文案，已从九种语言删除；没有放宽基线或改变页面交互。前端完整 lint、110 文件/1063 测试与构建均退出 0。

## 完整回归发现的事务边界

完整 Go 回归的唯一失败为 `TestArchServiceNoNewGormDB`：集成基线中的文档仓储接口与删除回调使知识服务事务计数为 2，超过原上限 1。先保留该失败并补充删除文档的真实 PostgreSQL 回滚、文件保留和租户隔离断言，见 [architecture-red.log](evidence/architecture-red.log)。未提高架构豁免上限。

删除文档及其分段现由仓储 `DeleteWithChunks` 在同一事务内完成；服务只在事务成功后处理文件和内存索引。移除服务暴露的 GORM 回调接口，将对应事务豁免从 1 收紧为 0。删除成功、未找到、分段或文档删除失败回滚、跨租户及缺租户拒绝均通过；架构守卫及其有效性测试也通过，见 [final4-focused.log](evidence/final4-focused.log)。爬取和重建回归同时通过，普通异步导入路径未改。
