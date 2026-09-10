# Brain 产品级规格（BRAIN_PRODUCT_SPEC）

> 日期：2026-09-10。承接 AGENT_BASE_PROOF.md：内核已对齐业界骨架，本规格把「demo 级」补成「产品级」。
> 依据：①业界机制调研（browser-use MessageManager/重试分类学/成本分层；Midscene 缓存/报告/重试配置）②对 service/ 目录的 8 项代码审计。
> 每项规格带验收标准（V 编号），实现后逐条打勾。

## 审计结论（demo → 产品的真实差距）

| # | 短板 | 现状 | 产品级要求 |
|---|------|------|-----------|
| 1 | 输入无预算 | snapshot 原样进 prompt；truncate 按字节可切断 UTF-8 | token 预算 + UTF-8 安全截断 + 逐条限长 |
| 2 | 重试不分类 | 固定 3 次重试，401/内容拒绝同样重试 | 429/5xx/网络重试；4xx 快败 |
| 3 | 成本无计量熔断 | 仅 plan token 落库；judge/summary 消耗不记；无上限 | session 级聚合 + 预算熔断 |
| 4 | 状态不可恢复 | reflectState 纯内存；resume=从头跑 | （P3，本轮记录不动——依赖断点续跑基建） |
| 5 | 数据竞争 | cmdSeq++ 多 session 并发无锁 | session 局部计数 |
| 6 | LLM 参数可注入 | LLM 可下发 retry_count=100 | 服务端 clamp |
| 7 | judge 形同虚设 | fail-open 且拒绝不落库 | 拒绝落库；连续 fail-open 升级拒绝 |
| 8 | 可观测缺口 | judge/耗时/自愈动作不落库；failed 无总结 | 全事件落库 |

## P0（本轮必做）

### P0-1 输入预算与 UTF-8 安全
- `truncateRunes(s, n)`：按 rune 截断，替代按字节 truncate（保留 truncate 为字节版供旧调用）。
- `budgetInput(goal, snapshot, st)`：prompt 组装前统一预算——snapshot 上限 48k rune（超出截断并追加「[快照已截断]」标注）；memory≤600 rune；PrevEvaluation≤400 rune；history 每条≤200 rune（组装时裁剪）。
- LLM schema 的字数建议在 prompt 中保持，但代码侧硬限。
- **V1**：构造 100k rune 快照调用 planOnce，prompt 长度 ≤ 预算，末尾有截断标注；中文多字节不产生乱码。

### P0-2 cmdSeq 数据竞争
- Executor.cmdSeq 删除，改 `appendCommandLog` 由调用方传 session 内局部 seq（executeStepWithRetry 维护 `seq int`）。
- **V2**：`go test -race` 并发两 session 落日志无 race；seq 各自单调。

### P0-3 LLM 重试错误分类
- 新增 `isRetryableLLMError(err)`：包含 `429/500/502/503/504/rate limit/timeout/deadline/connection` → 可重试；包含 `401/403/invalid_api_key/content_policy/无 JSON content` 中的 401/403 类 → 不重试快败。
- plan 循环：不可重试错误立即返回（不再烧 3 次）；可重试维持退避。
- **V3**：单测覆盖分类函数；mock 401 场景只调 1 次。

### P0-4 LLM 参数 clamp
- executeBrain 解析 steps 后 clamp：`retry_count = min(max(0, rc), 3)`；`retry_backoff_ms = min(max(100, b), 10000)`；continue_on_error 保留但 fail 计数超 `brainMaxFailures(5)` 强制终止。
- **V4**：单测 clamp 边界；恶意 retry_count=100 实际 ≤3。

## P1（本轮次优先）

### P1-1 成本聚合与预算熔断
- BrowserLLMPlan 增加 session_id 列（v3.40.0 迁移）；plan/judge/summary 三处 token 全部落库。
- Executor 持 session 级 token 计数（累计 plan+judge），`brainTokenBudget = 200_000`（env 可调 `BRAIN_TOKEN_BUDGET`）；超限终止会话，error_msg=`Token 预算耗尽（N）`。
- **V5**：注入超小 budget 跑 Brain，会话在超限时 failed 且 error_msg 明确。

### P1-2 judge 强化
- judge 结果（approve/reason/token）追加落 browser_command_log（direction=judge）；连续 2 次 fail-open 后视为未通过（拒绝放行），日志告警。
- **V6**：judge 拒绝场景日志可查；fail-open 连续 2 次不再放行。

## P2（本轮顺带）

- **P2-1** 自愈动作落库：重开 tab 的 openTab 写 command_log（direction=event, action=open_tab_recovery）。
- **P2-2** failed 会话也走 SummarizeSession（失败归因总结）。
- **P2-3** plan 落库失败升级为 error 日志（带 task/session 上下文）。
- **V7**：故障注入（杀 Host 后自愈）后 command_log 可见 recovery 事件。

## P3（记录，本轮不做）

- 断点续跑：reflectState 持久化 + 从 plans/steps 重建状态（依赖事件日志重放基建，M4）。
- 快照注入防护：隔离用户页面文本为「数据段」标记（需 prompt 层 A/B 验证效果）。
- detectBlockedIfFatal 的 snapshot 复用（与下一轮 plan 共享快照，省一半）。
- 分层模型路由（贵模型规划/便宜模型定位）：待 llm_routing 增加场景后配置。

## 不做（明确否决）

- 真实 token 计数器（tiktoken）：业界 browser-use 自己都是字符级 TODO 桩——字符预算已达标，不引入分词依赖。
- 泄漏 goroutine 强杀：Go 无法安全杀 goroutine，HTTP client 180s 自然终结已够。
