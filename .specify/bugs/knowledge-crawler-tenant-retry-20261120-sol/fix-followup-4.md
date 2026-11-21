# FA-12 Cron 重复建单修复

- 日期：2026-11-20
- Assessment：`assessment-followup-4.md`
- Status：applied；同一 FA-12 继续修订，交父任务独立核验及原 quality reviewer 复审。

## 修复

每个 due source 在 `KBSourceDueScanner` 创建 cron 批次前调用 `CrawlerService.HasPendingOrRunningBatch`。该 service 门面先 fail-closed 验证租户，再从已剥除 SystemScope、保留 trace 的租户 ctx 调用 Repository。Repository 通过 GORM 只读查询限定 source_id 和 pending/running；tenant callbacks 注入租户谓词。

- 存在 pending/running（不论是否在恢复页、是否活跃）→ skip，不创建新批次。
- 查询错误 → 原错误包装后从 scanner 返回，终止本轮，不继续创建其它新批次。
- 未找到 → 保持原有 CreateBatch/RunBatch 行为。
- 原有 `recovered` source 集合仍有效，覆盖本轮恢复后已变成终态的源，避免同轮重复新建。

不改 LIMIT 100、恢复顺序、批次 CAS/完成令牌，不加 schema、全局源锁或跨进程 exactly-once 保证。

## 文件

- `user-server/internal/aiagent/knowledge/repository/kb_crawler.go`：新增租户限定 pending/running 查询。
- `user-server/internal/aiagent/knowledge/service/kb_crawler.go`：增加租户 fail-closed service 门面。
- `user-server/internal/pkg/cron/kb_crawler_cron.go`：每个 due source 建批前查询；查询错误 fail closed。
- `user-server/internal/pkg/cron/kb_crawler_recovery_test.go`：复用 synthetic PG fixture 新增恢复页外批次、活跃任务/租户与暂态查询错误真实 scanner 测试。
- 本目录新增本 followup-4 assessment/fix/test 和 `evidence/followup-4-*`；未覆盖先前报告。

## RED→GREEN

修复前用实际 `KBSourceDueScanner`（包括内部真实 `ResumeBatches`）运行新增测试：目标合法 due source 的 pending 批次 id=101 在恢复前100之外；第一次扫描 source batch count 从1增至2。活跃 running 源也从1增至2。错误测试在第二次 batch 查询注入真实 PostgreSQL 40001；修复前没有存在性查询，此时命中已建新批次的 RunBatch 读取，scanner 仍返回 nil。证据 `evidence/followup-4-red.log`，exit 1；不能把这个 RED 描述成命中了修复前不存在的 lookup。

修复后 3 个新顶层 scanner 测试含 `-race` 通过：恢复页外既有批次不增加记录，后续 scanner 消费原批次且总数仍为1；活跃 running 保持、不同租户的同 source_id 合成行不影响本租户判断；存在性查询40001被返回且 created_batches=0。证据 `evidence/followup-4-green-race.log`，exit 0。

扩展组合后的最终专项回归为43个顶层测试通过、0跳过、race无报告；build/vet/layer/architecture/GORM/diff全部exit 0。全仓裸goroutine门exit 1，命中63处，与followup-3相比无增减；架构门有既有wecomCSService interface warning。图谱刷新exit 1：缺少graphify模块。完整命令与输出见 `evidence/followup-4-gates.json`，源文件哈希与followup-3比较仅Repository/Service/cron/cron测试四个目标文件发生变化。

## Deviations / 限制

- 无超出 assessment 的实现范围。评估计划中的105个前置批次简化为100个前置批次加目标第101条，仍明确覆盖总量>100、目标在首个恢复页以外。
- 最初误先草拟了生产补丁，随即完整移除本轮草稿，实际记录 RED 时运行的仍是修复前产品路径；之后重新实现最小补丁并取得 GREEN。不把这一草稿顺序描述为严格从未先写实现的 TDD。
- 初次 RED 用例的队尾目标为 pending/AutoResync=true；后续 expired-running/AutoResync=nil 和 Service SystemScope/缺租户检查属补充覆盖，不声称这些组合逐一先红。
- 查询与 CreateBatch 之间仍非原子操作，不承诺跨进程同时创建 exactly-once；本项按要求不加全局锁。
- 真实站点成功抓取/embedding/pgvector 不在本测试证明范围；合成有效任务以 SSRF 拒绝 loopback 形成持久 failed 终态。
- 遵守父任务协作边界：未使用 NewTestDB、未连业务 DB、未停止/删除集群，未修改外层清单、未 stage/commit/push、未进入 FA-13。
