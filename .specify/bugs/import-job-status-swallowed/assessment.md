# Bug Assessment: 客户导入作业权威状态写被吞，作业永久卡住且无日志

- **Slug**: import-job-status-swallowed
- **Created**: 2026-11-25
- **Source**: pasted text（本次会话审计扫描 + 源码精读，无 URL）
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

审计角度 `data-integrity`（永动审计循环下一轮，state.json round 77 / index 5）扫描写库吞错时命中
`user-server/internal/service/customer_import_job.go` 的 `Execute`：
4 处权威状态写（`running` 标记、读文件失败、空 CSV、最终状态）一律 `_ = s.repo.Update/UpdateStatus(...)`。
同型缺陷已在 R76 error-handling 轮被判为真实 P2（`knowledge_merchant_external.go` 的 job Create 失败仍返回 jobNo），
本次按证据复用而非重新论证设计意图。

## Symptom

`Execute` 在 DB 状态写失败（连接抖动/锁冲突/磁盘满等）时仍继续执行导入或返回成功报告；
作业行的 `status` 停在旧值（`pending` 或 `running`），用户轮询永远拿不到终态，
再次执行又被 `if job.Status == "running"` 拒绝，形成**既无结果又不能重试**的死锁；全程零日志，运维不可观测。

## Reproduction

1. 构造 `CustomerImportJobService`，注入 `repo.Update`/`UpdateStatus` 恒返 `errors.New("db down")` 的 fake repo（其余正常）。
2. 用合法 job（`Status="pending"`，文件存在且非空）调用 `Execute(ctx, jobID)`。
3. **现状**：函数照常遍历 CSV、返回 `Status="completed"` 的报告，而 DB 中作业状态仍为 `pending`/`running`；日志无任何错误。
   **预期**：权威状态写失败即中止（不产生误导性的执行成功），错误显式返回给调用方。
4. 变体：`running` 状态写失败后，第二次 `Execute` 因内存/DB 中状态未被标 `running` 而可并发重入；
   `update` 去重策略下两路执行交错，出现双重更新。

[NEEDS CLARIFICATION: 无——本报告为源码可证的行为，不依赖真实生产故障。]

## Suspected Code Paths

- `user-server/internal/service/customer_import_job.go:306` — `job.Status="running"; _ = s.repo.Update(ctx, job)`，执行前抢占状态写失败被吞。
- `user-server/internal/service/customer_import_job.go:310` — 读文件失败后 `_ = s.repo.UpdateStatus(..., "failed", ...)`，失败凭据落库失败被吞。
- `user-server/internal/service/customer_import_job.go:315` — 空 CSV 同上。
- `user-server/internal/service/customer_import_job.go:389` — 最终报告 `_ = s.repo.UpdateStatus(...)`，终态写失败仍向 controller 返回成功。
- `user-server/internal/repository/customer_import_job.go:60-84` — `UpdateStatus`/`Update` 返回真实 GORM error，并非恒 nil，吞错有实际后果。
- `user-server/internal/controller/customer_import_job.go:117-126` — controller 以 `Execute` 返回的 report 回 `response.Success`，无独立状态回读，会让 UI 显示"执行完成"而列表里作业仍是 pending。

## Root Cause Hypothesis

`Execute` 把作业状态机当作"best-effort 缓存刷新"处理，但这些写是该作业唯一的权威状态源；
历史写法用 `_ =` 规避错误分支，导致失败路径静默、状态机与实际执行脱节。
Confidence: **high**（源码路径闭合，接口契约明确返回 error，无其他状态回写点）。

## Proposed Remediation

**Preferred**：把 4 处吞错改为显式错误传播：

1. `running` 状态写失败 → 立即 `return nil, fmt.Errorf("标记导入任务执行中失败: %w", err)`，不进入 CSV 处理。
2. 读文件/空 CSV 的失败状态写失败 → 主错误仍返回，但用 `logger.Errorf` 记录"原始失败 + 状态写失败"双凭据（不可再静默）。
3. 最终状态写失败 → `return report, fmt.Errorf("写入导入结果失败: %w", err)`；report 仍返回供调用方参考，但 controller 会把错误回 400，UI 不再假绿。
4. 补 `logger.Errorf` 到每处失败分支，日志带 jobID 与错误。

**Alternatives**：
- 改为 `UpdateStatus` 带 CAS（仅 pending→running）以彻底消除并发重入 —— 需扩接口与仓储、影响面大，本次不做，登记为后续边界。
- 全局重试状态写 —— 属基础设施改造，不在本次范围。

**Files likely to change**：
- `user-server/internal/service/customer_import_job.go`
- `user-server/internal/service/customer_import_job_test.go`

**Tests to add or update**：
- fake repo 注入 `failUpdate bool` / `failUpdateStatus bool`，4 组用例：
  running 写失败即中止且不读 CSV；读文件失败 + failed 状态写失败返回原始错误且日志有记录；空 CSV 同型；最终状态写失败返回 report+error。
- 正常路径回归：状态写成功时既有行为不变。

## Risks & Considerations

- controller 由 200 变 400：这是正确的语义修正，但需确认前端对 `execute` 失败有提示（既有 `response.Error` 链已覆盖）。
- 吞错改报错会让原本"DB 偶发抖动仍完成导入"的行为变成"立即失败"——这是有意的 fail-closed，符合数据完整性优先。
- 不改其他 30+ 处同类吞错，避免扩大 blast radius；其余由审计循环逐项处理。
- 不新增 schema/migration、不改 API 契约形状。

## Open Questions

- [NEEDS CLARIFICATION: 无。若 fix 阶段发现根因与评估不符，按 §2.2 停手并写 Deviations。]
