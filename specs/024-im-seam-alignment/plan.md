# Plan: 024-im-seam-alignment 施工计划

## 1. 施工原则

1. **接缝先于平台**：B1–B5 全部是接缝，期间不接任何新平台。理由：若不先拆 `sendOutbound`，
   接 wecomcs 会在 663 行函数里再塞一个 case，B6 之后每接一个平台都会扩大回滚面积。
2. **纯搬迁不掺改造**：B1 只搬代码不改语义，行为冻结快照（T112）是后续所有改动的对照基线。
3. **一渠道一 commit**：T102–T109 每完成一个 sender 单独提交，出现回归时 `git revert` 单点即可。
4. **默认放行**：B2/B3 引入的分流与路由，对存量流量必须零影响——所有「新分支」只在事件类型非
   `message.received` 或规则命中时生效，否则走原路径。
5. **迁移可重跑**：065–067 全部幂等；065 涉及唯一键替换，先在测试库跑两遍并核对行数。
6. **外部腿与代码解耦**：T707/T806/T904 只做真实账号验收，失败不回退代码（fixture 测试已覆盖逻辑）。

## 2. 批次计划与门禁

| 批次 | 交付物 | 进入门禁 | 出口门禁（DoD） | 估算 |
|---|---|---|---|---|
| B1 | 8 个 sender + 注册表 + parity/冻结测试 | `go build` 绿 | `sendOutbound` ≤150 行；`grep switch channel` = 0；T111+T112 绿；`TestCardDelivery` 不变 | 4–5 天 |
| B2 | `EventType` + 分流 + 事件总线 + manifest.Events | B1 已合入 | 存量入站测试零改动全绿；`TestIngress_NonReceived*` 绿 | 2 天 |
| B3 | 规则表 + 引擎 + API | B2 已合入 | `TestIngress_NoRuleKeepsLegacyBehavior` 绿；跨租户 404 | 2 天 |
| B4 | 渲染层 + 身份键 | B1 已合入（T404 依赖 T101） | `Markdown|CardDelivery` 全绿；065 在含重复数据测试库两次执行一致 | 2–3 天 |
| B5 | snoozed + 计数 + 待首响查询 + 指标 | B4 已合入 | 并发重开只 +1；待首响查询索引命中（`EXPLAIN` 无 seq scan） | 2 天 |
| B6 | wecomcs 端到端 | B1–B4 已合入 | 8 触点齐全 + 4 条验收（验签失败/幂等/群私聊/租户） | 3–4 天 |
| B7 | OneBot v11（HTTP + 反向 WS） | B6 已合入 | CQ 码转义用例；断线重连用例；真实账号验收 | 3–4 天 |
| B8 | Slack + wecombot | B7 已合入 | 签名时间窗拒绝；thread 回复；AES 解密向量 | 3 天 |
| B9 | custom 入口 + 门禁收口 | B6 起任意时刻可做 | `check-channel-onboarding.py` 全量通过 | 2 天 |

合计约 30–34 个工作日（不含真实账号等待）——二次审核按批重算：
B1 4–5、B2 3–4、B3 3–4、B4 2–3、B5 3、B6 5、B7 4–5、B8 3、B9 2–3。
原估 21–26 与 62 个任务的体量不符（单 wecomcs 的「游标 + 新表 + 验签 + 出站 + 5 条测试」就难低于 4 天）。

⚖️ **待决策（范围拆分）**：若工期必须压回 20 人日以内，建议把 B6–B9 拆到 `025-im-platforms`，
本 spec 只交付 B1–B5 + 门禁；接缝先行、平台各自独立发布，避免 spec 长期停在「定稿但在制」。
**未拍板前本文档仍按 9 批整体施工口径维护。**

**每个平台批次的隐性必做项**（漏一项会被门禁或线上拒绝，逐行复核后补入）：

| 必做项 | 文件/命令 | 漏掉的后果 |
|---|---|---|
| 渠道常量进 pg enum | `migrations/047_pg_enums.sql` + `063_*` 式 `ADD VALUE IF NOT EXISTS` | 离线 SQL 部署拒绝未知 `message_hub.platform`；`check-enum-consistency.sh` 红 |
| 账号模型登记 | `internal/pkg/db/migrate.go` `allModels()` | `check_model_migration.py` 红；AutoMigrate 不建表 |
| 租户解析登记 | `webhookAccountTableByChannel`（`webhook.go:393`） | 回调 404 fail-closed，消息永远进不来 |
| 能力/提示一致 | `webhookInboundCapable` + `webhookInboundRejectHints` | `TestWebhookInbound_RejectHintsStayConsistentWithCapability` 红 |
| 验签分支 | `Verify`（`webhook.go:663`） | 伪造回调可入库 |
| 能力表 | `channelcaps` manifest + `capregistry` | 能力文档漂移测试红 |
| 拒绝路径不落库 | 每平台一条 4xx 用例 | 占位行污染收件箱 |
| 口令存储与出口脱敏 | 新账号表 + controller 脱敏 helper | 平台凭据明文落库且随列表接口返回 → 只读 DB 账号即可伪造回调（现网先例：`model/wecom.go:13-17` 明文 + `controller/wecom_secret_mask.go` 脱敏；加密先例：`internal/secrets/aesgcm.go`） |
| 长连接/游标生命周期 | 各平台 worker | wecomcs 游标不落库＝永远拉同一段；OneBot WS 不挂 start/stop＝泄漏连接与 goroutine（`check-naked-goroutine.sh`） |

## 3. 并行边界

允许并行（无共享文件）：

- B1 的 T102–T109（各 sender 独立文件）。
- B4 的 T401–T402 与 B5 的 T601 可与 B1 并行（不同包）。
- B9 的 T1101 与 B6/B7 并行（custom 独立入口）。

禁止并行（共享文件，必须串行）：

- `webhook.go`（T703/T803/T902/T1002/T1103 都要改登记）：按批次串行，每次改前先看 `git diff` 有无他人改动。
- `webhook_outbound.go`（T110/T404）：只在 B1/B4 内改，平台批次只新增 sender 文件，不碰主函数。
- `channelcaps/manifests.go`（T705/T805/T903 都要加）：按批次串行。
- `model/ai_sales_champion.go` / `model/customer_channel.go`：迁移批次内独占。
- `migrations/047_pg_enums.sql`（每个新平台都要加枚举值）：按批次串行，且必须同时提 `063` 式增量脚本。
- `internal/pkg/db/migrate.go`（`allModels()`，每个新账号模型都要登记）：按批次串行。
- 路由门禁表：`route_policy_gate_test.go` 的白名单与 `cross_tenant_scan_test.go` 的清单（T305 起新增路由均涉及）。
- `internal/service/channel_card_delivery.go`：**在制品，未提交**；本 spec 全程不改该文件，只改调用侧（T403 若需要改内部实现，先由作者提交）。
- `specs/024-im-seam-alignment/tasks.md`：同一时间只允许一个人改（1587 行单文件，多批次并行必冲突；如需分头推进，先按批拆文件）。
- `specs/024-im-seam-alignment/review.md`：二次审核只读，不改它来替代改 spec/tasks。

## 4. 回滚点

| 批次 | 回滚单位 | 回滚动作 | 数据影响 |
|---|---|---|---|
| B1 | 单 sender commit | `git revert <sha>` | 无 |
| B2 | 事件类型 commit | revert 后 `EventType` 字段保留（空值即旧行为） | 无（新事件不入库） |
| B3 | 规则表 migration | revert 代码 + 保留表（空表无副作用） | 无 |
| B4-T501 | 065 迁移 | 迁移器只有 up，无 `--down`；回滚 = 反向换名：`CREATE UNIQUE INDEX CONCURRENTLY` 旧 3 列 → `DROP INDEX CONCURRENTLY` 4 列 → `RENAME`（AutoMigrate 不会自动改/删索引） | 软删行可恢复（`deleted_at → NULL`）；反向换名前需先合并同 (one_id, channel) 多账号行，否则建旧键会冲突 |
| B5 | 067 迁移 | 新增列可空，revert 代码后列闲置 | 无 |
| B6+ | 平台包 | 移除 `channelcaps` manifest + `dispatchToChannel` case 即下线，账号数据保留 | 无 |

## 5. 验证策略

三层：

1. **单元**：注册表、匹配引擎、渲染函数、sender 参数（fake integration）。
2. **fixture 回放**：每平台一组真实回调 JSON（脱敏），断言「事件类型/入库行数/回复调用」。
3. **真实账号（🔵）**：每平台 1 私聊 + 1 群聊，记录消息 id 与回复 id；失败只登记不阻塞代码合入。

不做的验证：不做跨平台一致性压测（无证据需求）、不做 UI 自动化批量回归（规则 -1 禁止批量点击脚本）。

## 6. 风险与缓解（落地动作）

| 风险 | 落地动作 | 判据 |
|---|---|---|
| 搬迁引入隐性差异 | T102 前先生成 golden 快照（用旧实现跑），搬迁后比对 | T112 绿 |
| 唯一键替换锁表 | 065 用换名法：`CREATE UNIQUE INDEX CONCURRENTLY v2` → `DROP INDEX CONCURRENTLY` 旧键 → `RENAME` 复用原名；去重单独事务 | 迁移在 500 万行测试集 < 60s，且期间无写阻塞 |
| 事件分流改坏入站 | `message.received` 保持原函数路径，仅新增前置 switch | 存量入站测试断言不变 |
| 路由规则把回复吞掉 | `discard` 必须写审计；`IngressResult.RouteRuleID` 可查 | `route_rule_hit` 日志可检索 |
| 平台回调验签与 `Verify` 混用 | 每平台独立 `Verify<X>`，`Verify` 只分发 | `TestWebhookVerify_Dispatch` |
| OneBot WS 连接泄漏 | 连接表 + 断开即删 + 心跳超时 | `TestOnebotWS_ReconnectReplacesConn` |

## 7. 与既有任务清单的关系

- 本 spec 的 P0 项（B1/B2/B3/B6）在 `docs/未完成任务清单.md` §3.21 登记为可本机推进项。
- 「需证据触发」四项（Inbox 实体 / Redis 未读 / 模板全量 / 翻译）不登记为任务，只在 spec §四 备查。
- 已在 `2f658e7b` 交付的两项（channelcaps、生命周期三列）不重复登记，作为本 spec 前提。
