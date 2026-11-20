# Bug Verification: 爬取持久恢复

- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Date: 2026-11-20
- Verdict: 最终完整门禁通过；待生产验收，暂不销项。

## 原因复现

[red.log](evidence/red.log) 记录修复前两个真实失败：running 状态写入失败未被返回；partially_failed 批次被拒绝重试。

## 定向回归

[recovery.log](evidence/recovery.log) 使用专用 PostgreSQL 127.0.0.1:8217，实际网络和模型边界为合成信号。覆盖：

- 请求结束/取消后，手动和重试任务仍保留 tenant 上下文。
- 两租户隔离、缺上下文拒绝、同来源并发只能一批声明成功，旧 token 拒绝覆盖新 worker。
- pending 手动任务与过期租约恢复，取消最终持久失败，来源与批次最终写入原子回滚。
- 向量/索引/QA/processing/indexed 状态写失败均真实返回；同 hash 可重试，失败变更后恢复旧内容仍重新处理。
- 重建服务模拟进程重启后重新建索引；有候选/空候选 QA 均只完成一次；QA 检查点写失败时候选事务回滚。
- 旧租约不能修改另一来源或另一批次，完成前不计成功哈希。

[migration.log](evidence/migration.log) 验证旧运行批次连续迁移两次保留状态与归属，新增空租约兼容旧任务，唯一索引按租户/来源/URL 隔离。

## 完整门禁

- 早期 `make audit` 退出 0，见 [make-audit.log](evidence/make-audit.log)。首次失败为裸协程豁免已过期，删除对应陈旧基线后重跑通过。
- 早期 `make lint` / `make vet` / `go build ./...` 退出 0，见 [gates.json](evidence/gates.json)；这些结果仅代表当时源码。
- 部分旧完整回归已主动终止；另一个中间快照完整 Go 测试退出 0，但 lint 因格式失败。这些都不代替本次冻结源码的最终门禁。
- 最终集成源码见 [frozen-source.json](evidence/frozen-source.json)。同一冻结源码的 lint、audit、vet、build、test-go 全部退出 0，每项结束后源码漂移均为空；见 [final-gates.json](evidence/final-gates.json)。
- Graphify 刷新：`ModuleNotFoundError: No module named 'graphify'`，未判通过。

审计明确跳过对端平台仓不存在时的跨仓配置比较、本机未生成的覆盖率产物和本机未启动 8204 的运行探针；跳过不等于对应维度通过。发布后另验生产健康与迁移。

## 发布

尚未发布；不得据定向测试提前销项。后续按链 A 完成二进制摘要、内外健康、默认口令拒绝、回滚点、检查点表与租约列验收后补充证据。

## 批次上下文补充回归

`TestCrawlerRecovery_EmbeddingRetainsBoundedTenantContext` 修复前退出 1：向量服务 tenant=0/false、无 deadline、取消后 ctx.Err() 为 nil。修复后连同爬取服务、控制器、检查点迁移和向量器定向回归退出 0，见 `evidence/final-focused.log`。断言取消后批次与文档均 failed，页面完成哈希仍为空。真实外部 embedding 集成用例因未配置参数中心而跳过，不计为真实模型验证。

## 最终集成定向回归

以远端 `2986a7c540b45e2031c61a3c11ac440385f9a25b` 集成上游改动，先运行 [integration-gap-red.log](evidence/integration-gap-red.log)，实际复现系统作用域、QA 失败文档终态及同步读取/processing 失败落库缺口。修复后相同断言退出 0，见 [integration-gap-green.log](evidence/integration-gap-green.log)。

随后在专用 PostgreSQL 执行知识仓储、服务、控制器和向量器扩展定向回归，退出 0，见 [integration-focused.log](evidence/integration-focused.log)。覆盖上游原有测试的全部行为、两租户及缺上下文、取消与截止时间、租约接管及旧 worker 围栏、页面和候选事务、同 hash 重试及重建服务后的恢复。普通异步导入既有测试也通过。

后续终态红测观察到索引写入后被置回 processing 仍返回 nil，见 [terminal-red.log](evidence/terminal-red.log)。终态检查接入实际路径后，完整扩展定向回归退出 0，见 [final3-focused.log](evidence/final3-focused.log)，包括未终态不得完成检查点，以及控制器后台任务保留租户、trace、有界 deadline 的断言。

审计孤儿翻译清理后，前端 `npm run lint:check`、`npm test -- --maxWorkers=2`、`npm run build` 均退出 0；110 个文件、1063 个测试通过，见 [web-gates.json](evidence/web-gates.json) 和 [web-test.log](evidence/web-test.log)。构建成功不代替生产资产核验。

完整 Go 回归随后只在架构事务守卫失败：服务事务计数 2 超过上限 1。新增删除失败回滚及租户隔离测试后，先验证原架构失败仍存在，见 [architecture-red.log](evidence/architecture-red.log)。事务移至仓储并收紧豁免后，扩展定向回归退出 0，见 [final4-focused.log](evidence/final4-focused.log)。原始错误、文档与分段回滚、文件保留、跨租户及缺租户拒绝均有断言。该轮也覆盖架构守卫有效性、FA-12 全部定向场景及 FA-13；最终完整门禁仍须以此后冻结源码的结果为准。

最终冻结轮实际结果：lint 21.81 秒、audit 113.51 秒、vet 11.20 秒、build 14.52 秒、完整 test-go 1112.72 秒，均退出 0。完整 Go 日志有 159 条包级通过记录、零包级 FAIL，见 [final-test-go.log](evidence/final-test-go.log)。非详细模式的包级输出不能证明没有内部跳过；真实外部模型等未执行维度仍以上述限制为准。各静态门的完整输出与结果 JSON 一并留存。
