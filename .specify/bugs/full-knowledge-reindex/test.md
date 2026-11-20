# Bug Verification: 知识库全量重建

- Assessment: [assessment.md](assessment.md)
- Fix: [fix.md](fix.md)
- Date: 2026-11-20
- Verdict: 定向及最终完整门禁通过；待生产验收，暂不销项。

## 修复前复现

专用 PostgreSQL 上运行新增回归，最初完整重建对缺失文件仍返回 nil，1001 条夹具只触达首批 1000 条。补充回归还证明回填创建时间的并发新增可进入本轮、最终整行保存覆盖并发标题修改。对应红测见 `evidence/initial-red.log` 与 `evidence/cursor-finalization-red.log`。

尝试次数字段缺失时，失败后与重试后的 JSON 查询分别得到 `reindex_attempts=<nil>`，期望 1 与 2；先确认运行时失败，再新增模型字段与原子计数。

## 定向回归

`go test ./internal/aiagent/knowledge/repository ./internal/aiagent/knowledge/service ./internal/aiagent/knowledge/controller -run 'TestKnowledgeDocumentRepository_Reindex|TestReindex|TestRebuildIndex|TestKnowledgeWorkspace.*Reindex' -count=1 -p=1 -timeout=5m -v`

补计数后执行退出 0，见 `evidence/attempts-focused-green.log`：

- `initial=1001 attempted=1001 distinct=1001 failed=1001`；均为缺失源的合成文档，验证完整覆盖及失败持久化。
- `attempts=101 indexed=99 failed=2 deferred=2 first_error=first embedding failure`；后续文档继续处理，两个并发新增延后。
- 失败后首次尝试为 1，修复向量或写入依赖后重试为 2；向量写入、最终状态写入失败均可查询并重试。
- 缺失与空源、失败状态写入错误、可读源同步完成、并发标题修改、冻结分页及租户隔离通过。
- 单文档/全量 × 成功/缺失源四个真实 PostgreSQL 控制器场景通过；HTTP 成功返回前持久向量和 indexed 状态已经存在。

测试仅连接专用 PostgreSQL 127.0.0.1:8217；向量为合成夹具或 loopback HTTP 响应，不代表外部服务验收。

最终集成定向回归退出 0，见 [integration-focused.log](evidence/integration-focused.log)。除上述场景外，覆盖计数/完成写入跨租户和缺租户拒绝，以及四个接口场景的累计次数断言。1001 条与并发新增场景再次得到相同计数。

爬取终态检查接入后再次运行同一组扩展回归，退出 0，见 [final3-focused.log](evidence/final3-focused.log)。1001 条的 attempted、distinct、failed 均为 1001；并发场景仍为 101 次、99 indexed、2 failed、2 deferred。此后按冻结清单执行最终完整门禁。

文档删除事务边界收紧后，FA-12/FA-13、删除回滚与租户隔离、架构守卫有效性再次联合通过，见 [final4-focused.log](evidence/final4-focused.log)。1001 条和并发场景仍得到相同计数；此轮后的源码冻结清单作为最终完整门禁依据。

## 完整门禁与发布

此前部分静态门禁退出 0；计数字段加入前启动的完整 Go 回归曾读取到不一致的源码版本，已主动终止。另一中间快照完整 Go 测试退出 0，但 lint 因格式失败，均不作为最终验证。最终集成源码见 [frozen-source.json](evidence/frozen-source.json)。该冻结轮 lint 21.81 秒、audit 113.51 秒、vet 11.20 秒、build 14.52 秒、完整 test-go 1112.72 秒，全部退出 0；每项结束后源码漂移均为空，见 [final-gates.json](evidence/final-gates.json)。

完整 Go 日志有 159 条包级通过记录、零包级 FAIL，见 [final-test-go.log](evidence/final-test-go.log)。包级非详细输出不用于声称没有内部跳过；外部模型、第三方实际运行及审计环境缺项不计通过。

Graphify 刷新失败：`ModuleNotFoundError: No module named 'graphify'`。未将环境缺项记为通过。

生产发布尚未执行。需记录链 A 的提交、二进制摘要、回滚点、双健康检查、默认口令拒绝及 `reindex_attempts` 迁移证据后再关闭清单。
