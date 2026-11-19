# 017 规格验收：真实链路观测

**结论：partial，未声明 Converged 或生产生效。**

核对范围为本规格的7项功能要求、3个用户故事、4项成功标准，以及 plan/tasks 列出的生产与测试文件。以下证据来自已完成的测试，不把测试运行时间当生产响应时间。

## 要求核对

| 来源 | 实现与受控证据 | 判定 |
|---|---|---|
| FR-001、US1 | MessageTrace 可空操作时间、实测标志、版本、业务轮次与两类尝试；旧记录不反推时间。tracing/sink 与测量测试覆盖真实0、缺测和批次写入。 | 受控通过 |
| FR-002、US2 | Carrier 使用子副本；BusinessTurnID 包含完整租户/渠道/账号/会话/入站身份；临时空 TraceID 仅同一权威入站身份继承。跨范围及父不变对照通过。 | 受控通过 |
| FR-003 | 未缩短拟人输入，不改变人工锁、投诉否决、发送互斥、重试预算或 ACK 栅栏。正式完整 Go 回归通过。 | 受控通过 |
| FR-004、US2 | 初次/一次重生成及结果出口分开；入队与 ACK 在实际操作前计时；重复审计零ID不算成功入队；重领保留投递尝试。 | 受控通过 |
| FR-005、US3、SC-001 | 仓储按完整 scope/turn 聚合，缺测不进入均值/p95并返回覆盖率；仅有效 delivered 转换产生服务端接入至ACK指标。T013 五入口拒 nil/cancel/System/缺SaaS租户，Off明确默认租户。 | 受控通过 |
| FR-006、SC-003 | 凭证递归过滤，巨大载荷变合法有界JSON摘要；授权学习正文兼容。tracing 与 trace_learning 正式回归通过，未隐式调用外部分析服务。 | 受控通过 |
| FR-007、US3 | 未采集的打字/点击/锁等待保持未知；Vue 区分未知与实测0，工具按轮次和生成尝试归并。真实最新版页面复验未完成。 | partial |
| SC-002 | 操作时间不使用异步 CreatedAt；完整 tracing 回归覆盖 sink/时间边界。 | 受控通过 |
| SC-004 | T011 真实页面与 T012 最终收敛尚有未覆盖项。 | 未满足完整收敛 |

## 已完成验证

- 第三正式 `make test-go` 退出0；完整 tracing、monitor、repository、service、bridge、channelgw、trace_learning 等实际有测试包通过。
- T013 真实PG RED：五入口未知上下文崩溃/跨租户；修共享 scoped 后完整 repository **27.241秒**、monitor **1.523秒**通过。
- T013 后完整 `make lint` 为0 issues，`make audit` 退出0；此前 `go build ./...`、`go vet ./...` 通过。
- 管理端完整83文件884测试通过；定向真实 Vue mount、时序工具与九语言编译通过。

## 未覆盖与约束

- 自有浏览器 Space30 已不存在，未新建绕过；最新 TraceMonitor 页面及客户端未测分段没有生产实证。
- 管理端 lint 0错误、834告警，超过80告警预算；不能以退出0当预算通过。
- 新schema、API和UI尚未发布；不能将 ACK、任务完成或 `ai_replied` 解释为平台已收/已读。
- 本报告不替代 T011/T012，任务未全部勾选，不宣称 Converged。

## 2026-11-18 T011/T012 收口

**T011（ego 真实页面逐元素验证）→ 记录阻塞，不伪造。**

- 本机自有浏览器 Space（Space30）已不存在；按项目规则不得新建独立 profile、不得要求重新登录、
  也不得用「遍历元素 + 循环点击」的批量脚本替代逐元素人工点验（AGENTS.md 规则 -1）。
- 因此 `TraceMonitor` 最新页面的客户端实测分段（打字/点击/锁等待）仍为 `unknown`，
  本规格 FR-007 的「真实页面复验」一项继续为 partial，不写成通过。
- 解除条件：用户在 ego lite 里恢复该 Space（或指定可复用登录态的页面地址），
  再按逐元素流程补测：打开 TraceMonitor → 逐个展开单条 trace → 核对每段是「实测 0」还是「未知」。

**T012（回读 spec 逐 FR 核对）→ 已完成。**

逐条回读 `spec.md` 的 7 项功能要求、3 个用户故事、4 项成功标准，与实现和测试对账结果即上方
「要求核对」表；本轮补充的实跑证据：

```text
go test -p 1 -count=1 ./internal/repository/   → ok 212.003s（含 trace 聚合与五入口租户边界）
go test -p 1 -count=1 ./internal/monitor/      → ok 40.960s（TraceMonitor 查询与覆盖数）
```

FR-005 的「缺测不入均值/p95、返回覆盖数量」与 FR-001 的「旧记录不反推时间」由上述两包覆盖；
FR-003（计时只观察、不改发送锁/重试/ACK）由 `./internal/service/` 回归覆盖（见 021 报告同一批实跑）。

**结论不变：仍为 partial。** 剩余未覆盖项就是本规格自己写下的三条——真实页面客户端分段、
新 schema/API/UI 未发布、管理端 lint 834 告警超预算。任务已按实情勾选，不宣称 Converged。

关联：[规格](spec.md) · [计划](plan.md) · [任务](tasks.md) · [历史最终验证（partial）](../../.specify/bugs/bridge-safe-quality-20261116-sol/final-validation-20261117.md)
