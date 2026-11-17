# Plan: 024-im-seam-alignment 施工计划

**范围（2026-11-17 决策）**：本 spec 只交付**接缝**（B1–B6）。平台接入已拆到 `specs/025-im-platforms/`，
两者交接面 = 本 spec 的 B1（出站注册表）+ B2（事件类型）。

## 1. 施工原则

1. **接缝先于平台**：B1–B6 期间不接任何新平台。若不先拆 `sendOutbound`，接平台只会在 663 行函数里再塞一个 case，
   每接一个平台都扩大回滚面积。
2. **纯搬迁不掺改造**：B1 只搬代码不改语义，行为冻结快照（T112）是后续所有改动的对照基线。
3. **一渠道一 commit**：T102–T109 每完成一个 sender 单独提交，回归时 `git revert` 单点即可。
4. **默认放行**：B2/B3 引入的分流与路由对存量流量零影响——新分支只在事件类型非 `message.received`
   或规则命中时生效，否则走原路径。
5. **迁移可重跑**：065–068 全部幂等；065 涉及两条唯一键替换，先在测试库跑两遍并核对行数与软删行数。
6. **外部腿与代码解耦**：本 spec 无 🔵 外部腿；真实账号验收全部在 025。

## 2. 批次计划与门禁

| 批次 | 交付物 | 进入门禁 | 出口门禁（DoD） | 估算 |
|---|---|---|---|---|
| B1 | 8 个 sender + 注册表（静态工厂表 + 实例注册表）+ parity/冻结测试 | `go build` 绿 | `sendOutbound` ≤150 行；`grep switch channel` = 0；T111+T112 绿；`TestCardDelivery` 不变 | 4–5 天 |
| B2 | `EventType` + 分流 + 审计出口 + `manifest.Events` + 交互回调（T208–T211） | B1 已合入 | 存量入站测试零改动全绿；`TestIngress_NonReceived*` 绿；回调不落消息表 | 4–5 天 |
| B3 | 规则表 + 引擎 + API | B2 已合入 | `TestIngress_NoRuleKeepsLegacyBehavior` 绿；跨租户 404；路由门禁两测试绿 | 3–4 天 |
| B4 | 渲染层 + 身份双层键 | B1 已合入（T404 依赖 T101） | `Markdown|CardDelivery` 全绿；065 在含重复数据测试库两次执行一致；两索引列序断言绿 | 3–4 天 |
| B5 | snoozed + reopen_count + 待首响查询 + 指标 | B4 已合入 | 并发重开只 +1；待首响查询 `EXPLAIN` 无 seq scan | 3 天 |
| B6 | 门禁收口（T1201–T1205） | B1–B5 已合入 | 全量门禁 + `-race` + 能力/枚举/路由三张表全绿 | 2–3 天 |

合计 **19–24 人日**（含二次审核后新增的交互回调 3 人日、双层键 +1 人日）。
平台接入另计：见 `specs/025-im-platforms/plan.md`（21–25 人日）。

## 3. 并行边界

允许并行（无共享文件）：

- B1 的 T102–T109（各 sender 独立文件）。
- B4 的 T401–T402 与 B5 的 T601 可与 B1 并行（不同包）。
- B2 的 T208（交互表）可与 T201/T202 并行（不同包），但 T209 必须等 T201 合入。

禁止并行（共享文件，必须串行）：

- `webhook.go`（能力/分发/验签/账号表四处登记）。
- `webhook_outbound.go`（T110/T404）：只在 B1/B4 内改。
- `channelcaps/manifests.go`（T201 加 `Events`、T401 加 `OutboundFormat/MaxTextLen`）。
- `model/message_event.go`、`model/ai_sales_champion.go`、`model/customer_channel.go`、`model/channel_interaction.go`。
- 迁移目录 `migrations/`（065–068 号段独占，禁临时改号）。
- `internal/pkg/db/migrate.go`（`allModels()`）。
- 路由门禁表：`route_policy_gate_test.go` 白名单与 `cross_tenant_scan_test.go` 清单。
- `internal/service/channel_card_delivery.go`：**在制品，未提交**；本 spec 全程不改该文件，只改调用侧。
- `specs/024-im-seam-alignment/tasks.md`：同一时间只允许一个人改；需要分头推进时先按 Phase 拆文件。
- `specs/024-im-seam-alignment/review.md`：二次审核只读，不改它来替代改 spec/tasks。

## 4. 回滚点

| 批次 | 回滚单位 | 回滚动作 | 数据影响 |
|---|---|---|---|
| B1 | 单 sender commit | `git revert <sha>` | 无 |
| B2 | 事件类型 commit | revert 后 `EventType` 字段保留（空值即旧行为） | 无（新事件不入库） |
| B2 | 交互回调 commit | revert 后 `channel_interactions` 表闲置 | 无（表留着，不写不读） |
| B3 | 规则表 migration 066 | revert 代码 + 保留表（空表无副作用） | 无 |
| B4-T501 | 065 迁移 | 迁移器只有 up；回滚 = 反向换名（旧 3 列键 → 换名回 `idx_oneid_channel`，删 `idx_channel_identity`） | 软删行可恢复（`deleted_at → NULL`）；反向换名前需先合并同一 `(one_id, channel, account_id)` 多行，否则建旧键冲突 |
| B5 | 067 迁移 | 新增列可空，revert 代码后列闲置 | 无 |
| B6 | 门禁/文档 | revert 文档与测试 | 无 |

## 5. 验证策略

三层：

1. **单元**：注册表、结果不变量、匹配引擎、渲染函数、sender 参数（fake integration）。
2. **服务级**：`HandleIngressMessage` 全链路（事件类型分流、审计出口、交互幂等、路由默认放行）。
3. **并发**：`go test -race ./internal/service/ ./internal/channelbot/...`（注册表装配、snooze worker、交互清理 worker）。

不做的验证：不做跨平台一致性压测（无证据需求）、不做 UI 自动化批量回归（规则 -1 禁止批量点击脚本）。

## 6. 风险与缓解（落地动作）

| 风险 | 落地动作 | 判据 |
|---|---|---|
| 搬迁引入隐性差异 | T102 前先生成 golden 快照（用旧实现跑），搬迁后比对 | T112 绿 |
| sender 既不成功也不失败（静默丢消息） | 注册表加结果不变量：`!Sent` 必须已调用过 `Fail/FailUntracked` | `TestSenderOutcomeMustBeExplicit` |
| 唯一键替换锁表 | 065 换名法 + `CONCURRENTLY`；部分索引 `WHERE deleted_at IS NULL`；去重单独事务 | 迁移在 500 万行测试集 < 60s，期间无写阻塞 |
| 事件分流改坏入站 | `message.received` 保持原函数路径，仅新增前置 switch | 存量入站测试断言不变 |
| 总线丢事件（队列满即丢） | 成员/机器人事件同步写审计（幂等键），总线只旁路 | `TestIngress_MemberEvent_AuditWritten` |
| 路由规则把回复吞掉 | `discard` 必须写审计；`IngressResult.RouteRuleID` 可查 | `route_rule_hit` 日志可检索 |
| 交互回调重复答复 | 唯一键 `(tenant_id, channel, interaction_id)` + `MarkAnswered` 幂等 | `TestIngress_InteractionCallback_DuplicateIsNoop` |

## 7. 与既有任务清单的关系

- 本 spec 的批次在 `docs/未完成任务清单.md` §3.21 登记为 IM-1～IM-5（可本机推进）；平台项 IM-6～IM-9 指向 025。
- 「需证据触发」四项（Inbox 实体 / Redis 未读 / 模板全量 / 翻译）不登记为任务，只在 spec §四 备查。
- 已在 `2f658e7b` 交付的两项（channelcaps、生命周期三列）不重复登记，作为本 spec 前提。
