# FA-12 Cron 重复建单验证

- 日期：2026-11-20
- Assessment：`assessment-followup-4.md`
- Fix：`fix-followup-4.md`
- Result：verified（复审 finding 的原始 scanner 行为经授权 synthetic PG 实际重现，修复后目标场景与暂态 DB 错误路径均通过）。
- FA-12 仍待父任务独立验证和原 quality reviewer 复审；不是质量最终批准，不进入 FA-13。

## RED→GREEN 证据

| 阶段 | 结果 | 证据 |
| --- | --- | --- |
| 修复前，3个新增真实 scanner 顶层测试 | exit 1：恢复页外 target pending 的批次数 1→2；active running 也新建到2；第二次batch读取注入40001时，新批次已创建且scanner返回nil | `evidence/followup-4-red.log` |
| 修复后，3个新增真实 scanner 顶层测试含 race | exit 0：既有pending批次不重复建单且后续消费；活跃running保留；租户隔离成立；lookup SQLSTATE 40001返回并创建0批次 | `evidence/followup-4-green-race.log` |
| FA-12 专项完整race回归（扩展组合后） | exit 0：43个顶层测试通过，0跳过，无race报告；包括原有孤儿、并发CAS、完成栅栏、独立进程恢复测试 | `evidence/followup-4-regression-race.log` |

新增 recovery-page 测试 fixture 创建100个前置pending批次，其源通过真实软删除；目标 source 是 status=active、site_verified=true、auto_resync=true、last_crawl_at=nil。目标 source 自身 pending 批次 id=101，真实 scanner 首次 Resume 恰只得到前100个。测试第一轮断言 source 批次数仍为1；随后再跑实际 scanner，断言原 id=101 批次变终态且批次数仍为1。修复前日志显示首次 count=2。没有通过改 helper 绕开产品路径；所有入口均为 `KBSourceDueScanner`。

活跃 running 测试确认 active source 有 started_at 新鲜的批次时 scanner 不创建第二批，原 started_at/status 不变。租户隔离测试构造 tenant 8 同 source_id pending 合成行：tenant 7 的查询不应读到它，允许按 tenant 7 due 条件创建 tenant 7 自己的一批；tenant 8 行保持 pending。错误测试在队列读取后，对 source-open-batch lookup 注入真实 PG DO SQLSTATE 40001，断言 errors.As 可识别 pgconn.PgError 且 source batch count=0。

扩展组合另覆盖页外expired-running/AutoResync=true、pending/AutoResync=nil；从数据库回读校验AutoResync确为指定值。Service直接查询验证上游SystemScope被剥除、无租户被拒绝。该组合和额外边界属补充验证，初次RED日志针对pending/true、活跃running和临时错误。草稿顺序偏差如实见fix-followup-4，不追溯宣称全流程严格test-first。

错误注入按batch查询次序定位：第一次为恢复页，第二次在修复后为存在性查询；修复前不存在这个查询，第二次实际为新批次的RunBatch读取。因此该RED证明修复前先建批次且scanner吞错，GREEN才证明新lookup错误在CreateBatch之前返回、创建数为0。

## 环境与范围

运行使用用户指定 `FA12_PG_DSN`，helper 校验 PostgreSQL16.14、socket、fa12_synthetic/fa12_runner；每测试唯一 schema，仅三张合成表、真实 tenancyidx callbacks。不使用 testutil.NewTestDB，不触碰业务数据库，不停止或删除集群目录。QA/向量链未参与。

## 命令与门禁

工作目录 `user-server/`：

```bash
export FA12_PG_DSN='host=/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x port=55412 user=fa12_runner dbname=fa12_synthetic sslmode=disable'
export GIN_MODE=release
go test ./internal/pkg/cron -run '^TestFA12Recovery_(DueSourceExistingBatchBeyondRecoveryPage|DueSourceOpenBatchTenantScope|DueSourceLookupErrorFailsClosed)$' -count=1 -v -timeout=90s
go test ./internal/pkg/cron -race -run '^TestFA12Recovery_(DueSourceExistingBatchBeyondRecoveryPage|DueSourceOpenBatchTenantScope|DueSourceLookupErrorFailsClosed)$' -count=1 -v -timeout=90s
go test ./internal/aiagent/knowledge/service ./internal/pkg/cron -race -run '^Test(FA12|ProcessContent_|ProcessDocumentByID_|ProcessDocumentAsync_StateFailureStopsQA|CrawlerProcessingContext_)' -count=1 -v -timeout=150s
go build ./...
go vet ./...
```

内仓门禁使用实际命令、cwd、exit 与完整输出保存于 `evidence/followup-4-gates.json`。

| 检查 | 本轮实跑结果 |
| --- | --- |
| go build ./... / go vet ./... | 均exit 0 |
| layer boundary | exit 0，跨层违规0 |
| architecture | exit 0；既有wecomCSService interface warning |
| GORM-only / git diff --check | 均exit 0 |
| naked goroutine | exit 1；63处命中，与followup-3逐行比较无新增/删除 |
| graphify刷新 | exit 1，ModuleNotFoundError: No module named 'graphify' |

未运行 `go test ./internal/...` 全仓测试：本任务禁止使用NewTestDB/连接业务DB，验证限定于上述专项测试与合成PostgreSQL；不能据此宣称后端全套测试通过。没有为全仓既有门禁或缺失工具修改无关文件。

## 剩余边界

查询和 CreateBatch 之间存在时间窗；未引入全局锁或唯一约束，不能声称跨进程严格 exactly-once。数据库不可用时本轮停止创建，返回错误并等待后续 cron 重试。真实 PostgreSQL 错误由 SQLSTATE 40001 人工注入；未声称生产负载下实测冲突。未实际等待批次租约过期；有效批次执行到既有 SSRF loopback 拒绝终态，不证明生产网站、embedding、pgvector 或 QA 成功链。

工作区其他文件变更均属此前/其它 in-flight 工作，保留不覆盖。本轮只触及 assessment/fix/test-followup-4、相关 evidence，以及 Repository/Service/cron 三个生产文件和已有 cron 集成测试文件。HEAD 保持不变，无 stage/commit/push，无外层清单修改。

最终交付状态：DONE_WITH_CONCERNS。HEAD仍为 `2986a7c54`；`followup-4-source-sha256.json`与当前目标源文件一致。与followup-3清单比较仅四个目标文件变化；后续出现的order_change_request及inbox assignment相关工作区变化未由本任务操作。父任务独立核验及原reviewer复审仍是最终验收边界。
