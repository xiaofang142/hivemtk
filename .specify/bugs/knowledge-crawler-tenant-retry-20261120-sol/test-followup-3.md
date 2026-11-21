# FA-12 孤儿批次队列饥饿验证

- 日期：2026-11-20
- Assessment：`assessment-followup-3.md`
- Fix：`fix-followup-3.md`
- Result：verified（本项原始症状在授权合成 PG 经实际 scanner/ResumeBatches 复现并消除）。
- 交付状态：**DONE_WITH_CONCERNS**。本项功能验证通过；全仓裸 goroutine 门存在既有命中，图谱刷新环境缺模块，均如实保留。FA-12 仍待父任务独立核验及原 quality reviewer 复审，未宣称 QUALITY_APPROVED。

## RED → GREEN

| 实跑 | 结果 | 证据 |
| --- | --- | --- |
| 补丁前3个新顶层测试 | 可编译的行为 RED，exit 1；三次 scanner 后孤儿 failed=0/105、有效批次仍 pending；40001 被误报不存在；并发认领/完成写=0/0 | `evidence/followup-3-orphan-red.log` |
| 补丁后4个新顶层测试含 race | 全部 PASS，exit 0；第一扫描清前100，第二扫描处理剩余5并执行有效批次，第三扫描维持结果；并发认领/完成写=1/1 | `evidence/followup-3-orphan-green-race.log` |
| 完整 FA-12 专项含 race、真实PG/OS恢复 | 40个顶层 PASS（29 service + 11 cron），0 FAIL，exit 0 | `evidence/followup-3-regression-race.log` |

OrphanFinishRecovery 是补丁后的补充测试，不将其描述为独立先红。前三项原始失败均在修改生产代码前记录。历史 assessment/fix/test 与 followup-2 报告没有覆盖。

## 覆盖和实际入口

- 105个队首孤儿均通过真实 DeleteSource 软删其源；包括 pending、started_at过期 running、started_at=NULL且created_at旧的 running，各35条。真实 KBSourceDueScanner → ResumeBatches → RunBatch → repository 查询/认领/完成 SQL，未直接调用 crawlOne，未更改 fixture 绕过产品路径。
- 通过 service.ListBatches 查询105条 failed 的 error_msg 和活跃批次，共106行；确认孤儿错误包含“爬取源不存在”，finished_at非空。
- 后续有效批次首次扫描 pending，第二次到达执行并落 sitemap 抓取失败终态，证明已越过固定100条窗口。未扩大 LIMIT 或更改排序。
- 两租户：租户7扫描/直接执行均不能修改租户8孤儿；租户8自己的扫描可终结它。软删源的活跃 running 经扫描及直接 RunBatch 都保持原 started_at、running、无finished_at。既有 CompletionFence 同时验证跨租户完成写和旧令牌拒绝。
- 临时查询错误：真实合成 PG 的 DO 显式抛出 SQLSTATE 40001，由 Query callback 单次注入源读取路径；正常源/软删源 × pending/过期running四组合均保持原状态/started_at、无finished_at/error_msg，RunBatch错误链可 errors.As 为原pgconn.PgError。下一次实际 ResumeBatches 可重新处理；确认不存在后才终结。此处模拟临时查询错误分支，没有宣称制造真实负载下的序列化冲突。
- 两 worker 对同一孤儿并发 RunBatch，真实数据库成功认领和完成写均恰好1次；新增并发使用 SafeGo。
- 孤儿完成故障：真实PG返回40001或在完成UPDATE前替换令牌，均返回可识别错误并保持running、不覆盖新started_at。随后仅把本测试行started_at调旧，再经ResumeBatches完成。清理ctx带租户7/trace、无SystemScope且有10秒内deadline。
- 既有OS kill/recover仍通过：本轮 killed pid=80737，另一恢复进程pid=80755，经既有scanner恢复持久批次。未停止或重启数据库服务。

## 命令

以下Go命令工作目录为内层 `user-server/`；日志保存实际命令和退出码。DSN仍仅指向父任务授权的临时集群。

```bash
export FA12_PG_DSN='host=/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/fa12-pg-u0tmyd4x port=55412 user=fa12_runner dbname=fa12_synthetic sslmode=disable'
export GIN_MODE=release

# 补丁前实跑，exit 1
go test ./internal/pkg/cron -run '^TestFA12Recovery_Orphan' -count=1 -v -timeout=60s

# 补丁后实跑，exit 0
go test ./internal/pkg/cron -race -run '^TestFA12Recovery_Orphan' -count=1 -v -timeout=90s
go test ./internal/aiagent/knowledge/service ./internal/pkg/cron -race -run '^Test(FA12|ProcessContent_|ProcessDocumentByID_|ProcessDocumentAsync_StateFailureStopsQA|CrawlerProcessingContext_)' -count=1 -v -timeout=120s
go build ./...
go vet ./...
```

内层仓根实际门禁：

| 命令 | 结果 | evidence日志 |
| --- | --- | --- |
| `python3 scripts/check-layer-boundary.py` | exit 0，扫描1604个生产Go文件，新增违规0 | `followup-3-layer.log` |
| `bash scripts/check-architecture.sh` | exit 0；保留既有webhook_wecomcs_puller.go缺interface warning | `followup-3-architecture.log` |
| `bash user-server/scripts/check-gorm-only.sh` | exit 0 | `followup-3-gorm.log` |
| `git diff --check` | exit 0 | `followup-3-diff.log` |
| `bash user-server/scripts/check_naked_goroutine.sh` | exit 1，命中全仓既有生产代码；本轮未新增裸goroutine，未改他人文件消除此门 | `followup-3-goroutine.log` |
| `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | exit 1，ModuleNotFoundError: No module named 'graphify' | `followup-3-graph.log` |

build/vet日志为 `followup-3-build.log`、`followup-3-vet.log`，均exit 0。所有门禁的command/cwd/exit/output汇总在 `evidence/followup-3-gates.json`。`evidence/followup-3-source-sha256.json` 记录最终源码hash；与followup-2 manifest比较，仅本轮列出的两个源码/测试文件改变，其余所列源码保持原hash。

## 未验证边界

仅授权私有Unix socket的PostgreSQL16.14、fa12_synthetic、fa12_runner，每测试唯一schema，真实tenancyidx callbacks和三张合成表；没有连接业务库、使用NewTestDB、安装pgvector、停止集群或删除目录。

有效批次到达执行终态使用既有SSRF拒绝loopback这一确定性失败，未验证真实站点→embedding→pgvector→QA成功链。失效通过修改自己的合成行模拟，未等待真实35分钟。持续DB故障仍可能使failed无法持久化，保留running供既有过期恢复；本轮证明的是单次故障后可恢复。

遵守用户限定，本轮未跑会使用其它测试数据库入口的全仓数据库套件、未发布。未派生Agent、未创建worktree、未暂存/提交/推送；HEAD仍2986a7c54，外层FA-12清单未改。交父任务独立核验和quality复审。
