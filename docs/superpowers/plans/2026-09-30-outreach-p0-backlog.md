# 自动触达 P0 未完成功能清单

> 日期 2026-09-30 ｜ 状态：进行中 ｜ 前置：`docs/superpowers/specs/2026-09-30-outreach-product-design.md` + `docs/superpowers/plans/2026-09-30-outreach-p0.md`

## 进度汇总

| 指标 | 值 |
|------|-----|
| 总进度 | ~50% |
| 已完成 | 3/5 chunks（Chunk1 完整、Chunk2 完整、Chunk3 基础层） |
| 未完成 | Chunk3 接线 + Chunk4 + Chunk5 |
| 剩余工时 | 约 5-8 小时 |
| 双远端 | gitee-upstream + upstream 均已推送 |

## 已完成（已提交推送双远端）

| Chunk | 内容 | 提交 |
|-------|------|------|
| Chunk1 | JEV TYPE_TEXT（CopyText 任务文案 + 可输入目标选择 + 空文案回退） | `8bc08f94` |
| Chunk2 | 单主 Profile 健康监护（熔断落库 + 启动门 + 人工恢复 + v3.49） | `1e5eeaf7` |
| Chunk3 部分 | helpers + 字段 + setter（model/repo/service 三件套） | `773b9f1e` |

## 未完成

### Chunk3 剩余：触达去重接线

- [ ] `executeStepWithRetry` 加 `pageURL` 尾参（3 调用点：653 显式传 `""`、926 brain 传 pageURL 现货、test57 传 `""`）
- [ ] `dispatchStep` 加 `outreachCtx` 尾参（唯一调用方 1116）
- [ ] 检查点：`writeKey` 后 `guardResubmit` 前，`resolveOutreachDedupe` 命中 → `finishStep skipped` + 审计帧绿返
- [ ] 插入点：`verified` 分支 `recordOutreachDedupeSend`（warn-only）
- [ ] 迁移 `v3.50.0`：建 `browser_outreach_dedupe` 表（四元组唯一键）+ 测试 + `initial_schema.go` 注册
- [ ] routes 装配：`WithDB` + `SetOutreachDedupeRepository` setter

### Chunk4：活动预算 + 时间线可观测

- [ ] task 模型加活动预算字段
- [ ] dispatcher 调用点预算扣减
- [ ] 审计帧每步模型/token/延迟
- [ ] 门禁：`POSTGRES_TEST_PORT=8232` + 口令跑 `browser_automation` 与 `llm` 包

### Chunk5：触达回执留存

- [ ] DONE 收口：截图 + 帖子链接 + 文案快照入库
- [ ] 前端：任务详情回执展示（`user-web/src/api/browserAutomation.js` 对应接口）

## 硬约束（续）

- 规则0：双远端直推（gitee-upstream + upstream，禁 PR，推送前 fetch 确认 fast-forward）
- 规则0b：禁子 Agent 串行
- 规则1：自动提交推送
- 规则2：问题当轮修
- 五层架构：Router → Controller → Service → Repository → Model
- Executor 进程级单例禁内存计数（熔断/去重状态必须落库）
