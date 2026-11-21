# FA-12 第二轮验证

- 日期：2026-11-20
- 实现状态：**DONE_WITH_CONCERNS**。
- 本轮五条源码 finding 已修订并有行为证据；完整生产端到端验证仍为 partial。未进入质量审查。
- 下列日志均位于本目录 evidence/，原报告/证据保留。

## RED → GREEN 证据

| 修订前实跑 | 实际结果 | 证据 |
| --- | --- | --- |
| service followup 初始五项 | 4 项行为 FAIL：失败游标推进、取消后 running、内部 deadline 返回 nil、默认 QA 缺服务仍计成功；QA 状态故障用例原已 PASS | followup-2-service-red.log |
| 真实 controller / scanner / 并发 / OS 恢复入口 | 均 FAIL：手动 tenant=0/trace 空；retry 无执行；持久任务不消费；PG 两 worker 认领=2；新 OS 进程未恢复 running | followup-2-recovery-red.log |
| QA final 与真实租户 UPDATE 0行 | FAIL：页面计 newPages=1，两个仓储方法均未返回错误 | followup-2-zero-row-red.log |
| RunBatch→真实 Vectorizer→embedding fixture | FAIL：tenant=0、trace 空、bounded=false，批次 partially_failed 而非内部超时 failed | followup-2-embedding-red.log |
| 仅补 embedding context 后，再测取消写 | FAIL：batch=failed，但 doc=processing，失败收尾写用了已取消 ctx | followup-2-timeout-cleanup-red.log |
| scanner 上游携带 SystemScope | FAIL：恢复/源扫描继承系统豁免 | followup-2-scan-scope-red.log |
| 最终专项 | 36 个顶层测试 PASS，0 FAIL，含 -race | followup-2-final-focused-race.log |

本轮上述缺陷均先获得可编译的行为 RED 再作对应生产修订。CompletionFence、内部预算 seam、limiter、processing context 是扩展验证，未将它们全部描述为独立的先红测试。前轮导入实现的历史 TDD 不合规仍见原报告。

## 真实 PostgreSQL / OS 证据

仅使用授权私有 Unix socket 的 PostgreSQL 16.14 / fa12_synthetic / fa12_runner，连接前校验 host、port、user、dbname；连接后检查服务器版本、current_database/current_user、inet_server_addr() IS NULL。按测试唯一 schema 创建 source/batch/doc 三张表并注册真实 tenancyidx callbacks；不替换 SQL 执行回调、不使用 NewTestDB、不 drop database。回调用于观察上下文、并发屏障、认领后子进程挂起，SQL 本身真实执行。

- ControllerSyncAndRetry：实际 gin 路由请求返回并取消后，后台批次仍带租户 7 与原 trace，通过真实仓储进入运行/失败终态及源结果更新。
- ScannerPersistentQueue：pending、过期 running、NULL started_at 且旧 created_at 均恢复；新 running 保持原 started_at；租户 8 在扫描租户 7 时保持 pending，再由租户 8 的扫描消费。
- ScannerRemovesSystemScope：恢复/源查询不能继承系统豁免。
- ConcurrentClaim：两个 worker 均先读到 pending，随后同时竞争；真实 PostgreSQL 成功认领次数=1。
- DocumentTenantAndMissingUpdate：两租户同 URL 查询各得自己的失败文档；跨租户状态 UPDATE 0 行返回错误；无租户查询 fail closed。
- CompletionFence：旧 started_at 与其它租户完成写均 0 行；正确令牌能完成。
- ProcessRestart：子进程 RunBatch 持久 running 后阻塞；父测试 SIGKILL，再启动新子进程，经真实 KBSourceDueScanner 消费该持久批次。最终日志记录 killed pid=45779，恢复 pid=45782，批次变为 failed 且 finished_at 非空。父测试 pid=45750。不是仅重新构造 service。

OS 验证边界：kill 后人为将该合成行 started_at 改为 1h 前，以免等待 35min；恢复后的抓取被现有 SSRF 防护拒绝，验证的是持久消费/失败终态，不代表 pgvector/生产站点全链成功。PostgreSQL 进程未重启，集群/目录由父任务保留管理。

## 可复算命令

工作目录为内层 user-server/；以下 DSN 只指向本轮授权临时集群。父任务停止该集群后需由其重新提供合成环境，不能替换为业务连接。

```bash
export FA12_PG_DSN='host=/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x port=55412 user=fa12_runner dbname=fa12_synthetic sslmode=disable'
export GIN_MODE=release

# 对应最初修订前分别实跑；退出 1 的行为 RED 见上表
go test ./internal/aiagent/knowledge/service -run '^TestFA12Followup_' -count=1 -v
go test ./internal/pkg/cron -run '^TestFA12Recovery_' -count=1 -v -timeout=90s

# 最终退出 0：29 个 service 顶层测试 + 7 个真实PG/OS顶层测试
go test ./internal/aiagent/knowledge/service ./internal/pkg/cron -race -run '^Test(FA12|ProcessContent_|ProcessDocumentByID_|ProcessDocumentAsync_StateFailureStopsQA|CrawlerProcessingContext_)' -count=1 -v -timeout=120s

# 单独复算 OS 进程恢复；测试本身负责派生/杀死自己创建的子进程
go test ./internal/pkg/cron -run '^TestFA12Recovery_ProcessRestart$' -count=1 -v -timeout=30s

go build ./...
go vet ./...
```

实际本轮单独 OS 命令未重复执行；最终专项命令包含同一个测试及其两个 OS 子进程。所有实际命令、cwd、退出码见 followup-2-final-gates.json / followup-2-gates.json，RED 日志亦保存对应命令。

## 其它检查

| 检查 | 实跑结果 / 证据 |
| --- | --- |
| 最终 build / vet | 均 exit 0，followup-2-final-build.log / final-vet.log |
| 最终 layer boundary | exit 0，0 新违规，followup-2-final-layer.log |
| 最终 GORM-only | exit 0，followup-2-final-gorm.log |
| architecture | exit 0，有既有 webhook_wecomcs_puller.go warning，followup-2-architecture.log |
| 相邻 crawler/vectorizer/retrieval 单元 | 18 个顶层 PASS，followup-2-adjacent.log |
| 相邻 service 单元 | 15 个顶层 PASS，followup-2-service-adjacent.log |
| git diff --check（相关文件） | exit 0，followup-2-diff-check.log |
| Graph refresh | exit 1，缺 Python graphify 模块，followup-2-graph.log |
| 源码 SHA256 | followup-2-source-sha256.json |
| 全仓 DB 测试 / 实际 embedding / pgvector / 生产发布 | 本轮未跑；不将本合成集群用于其它 DB helper |

持续故障测试确认 QA processing/final 写及 failed 收尾都失败时仍返回错误、计数/hash 不成功；不能声称 DB 不可用时 failed 也已持久。QA 候选与 final 状态非同一事务，重试仍可能重复候选。完整限制见 fix-followup-2.md。

未新增生产 schema/migration，未连接业务库，未停止/删除临时集群，未暂存或提交/推送；原 staged/在途工作保留。
