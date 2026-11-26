# Plan: 025-im-platforms

**Input**: `spec.md`（本目录）、`tasks.md`（本目录，含逐任务七字段规格）
**前置**: `024-im-seam-alignment` 的 B1（出站注册表）与 B2（事件类型）已交付并通过门禁
**口径**: 每个平台以「八触点齐全 + 验收四条」为完工；批次之间无代码依赖，可并行排期（同一人串行时按下表顺序）

## 一、批次与工期

| 批次 | 平台 | 任务 | 人日 | 交付物 |
|---|---|---|---|---|
| P1 | 企业微信客服 wecomcs | T701–T707 | 6–7 | `channelbot/wecomcs`、入站/出站、`wecom_cs_accounts`（070）、游标续拉 worker |
| P2 | OneBot v11 | T801–T806 | 6–7 | `channelbot/onebot`、HTTP 上报 + 反向 WS、`onebot_accounts`（071） |
| P3 | Slack | T901–T904 | 4–5 | `channelbot/slack`、Events API、线程回复、`slack_accounts`（072） |
| P4 | wecombot | T1001–T1002 | 3 | 回调解密 + 卡片事件 → `channel_interactions`、`wecombot_accounts`（073） |
| P5 | 通用 custom | T1101–T1103 | 2–3 | 平台无关契约、`custom_channel_accounts`（074）、签名回调出站 |
| 合计 | | 22 任务 | 21–25 | |

估算依据：024 的 `sendOutbound` 单分支搬迁实测（T101–T112 共 4–5 人日 / 8 分支）→ 每平台「客户端 + 入站 + 出站 + 测试」
约 2–3 人日，WS/Slack 各 +1 人日，账号表与枚举增量 +0.5 人日。

## 二、进出口门禁

**进（开工前必须为真）**：
1. 024 B1 已合入：`RegisterChannelSender` 存在且 `TestEveryInboundChannelHasSender` 绿；
2. 024 B2 已合入：`MessageEvent.EventType` 存在、`NormalizeEvent` 归一为 `message.received`；
3. 目标平台账号已申请（🔵 仅「真实回放」需要；账号未到位也可开工，用 fixture 完成 B 面）。

**出（每平台完工判据，全绿才算完）**：
1. 八触点齐全（见 `spec.md` §二）；
2. 验收四条：验签失败 4xx 不落库 / 同 msg_id 双投不重复 / 私聊+群聊 fixture 回放 / `*ChannelError` 分类正确；
3. 全量门禁：`go build ./... && go vet ./...`、`go test ./internal/service/ ./internal/channelbot/...`、
   `check-layer-boundary.py`、`check_model_migration.py`、`check-enum-consistency.sh`；
4. `channelcaps` 文档已重新生成（`UPDATE_GOLDEN=1`），`TestCapabilityDocFreshness` 绿；
5. 🔵 真实账号回放记录写进 `delivery.md`（无账号则标「未回放」且该平台不得宣称上线）。

## 三、并行边界

- 各平台包（`channelbot/<x>`、`webhook_channel_<x>.go`、`outbound_sender_<x>.go`）互不共享，可并行。
- **共享文件禁并行**（必须串行合入，否则冲突/漂移）：
  `internal/service/webhook.go`（capable/dispatch/verify/账号表四处）、`internal/service/webhook_ai.go`、
  `internal/model/message_event.go`、`migrations/047_pg_enums.sql`、`internal/pkg/db/migrate.go`、
  `internal/channelcaps/manifests.go`、`internal/channelgw/registry.go`。
- 迁移号按批次预分配（070–074；2026-11-18 勘误：原 069–073，069 已被 024 的 `channel_interactions` 占用，
  见 §七 M-1），不得临时改号：并行时号段冲突比内容冲突更难解。

## 四、回滚

| 单位 | 方式 | 数据影响 |
|---|---|---|
| 单个平台 | 关账号开关 + `webhookInboundCapable` 置 false（回调 fail-closed） | 无；已入站消息保留 |
| 枚举增量（047/063 式） | 不回滚：`ALTER TYPE ... ADD VALUE` 不可逆，保留孤立值无害 | 无 |
| 账号表（070–074） | 不回滚：表留着，代码不再引用 | 无 |
| 出站 sender | 注册表摘除 + `ErrOutboundChannelUnsupported` | 该平台回复失败可重试 |

## 五、验证分层

1. **包级**：`channelbot/<x>` 用 `httptest` 回放固定报文（验签、错误码、分页/游标）。
2. **服务级**：`webhook_channel_<x>_test.go` 走 `HandleIngressMessage`，断言落库/事件类型/审计。
3. **跨层**：`controller/webhook_*_http_test.go` 断言 4xx 不落库、租户反查 fail-closed。

## 六、与任务清单的关系

外层 `docs/未完成任务清单.md` §3.21 的 IM-6/IM-7/IM-8/IM-9 行指向本 spec；本 spec 的进度以 `tasks.md`
勾选状态为唯一真值，不再另建进度文档。

## 七、首切片前杂项（2026-11-18 调研收口登记）

来源：024 交付后的 LangBot/Chatwoot 吸收项三方核对（要吸收／已在计划／已实现）。小项随 P1
切片前或批次间隙清掉，不单独立 spec：

| # | 项 | 内容 | 验收 |
|---|---|---|---|
| M-1 | 迁移号勘误 | 本 spec 三份文档账号表迁移号 069–073 → 070–074 → **071–075**（2026-11-18 终审：024 三审新增 070_channel_evidence_identity 占用 070）（069 已被 024 `channel_interactions` 占用；本次勘误已同步 spec/plan/tasks，tasks.md 原有的 wecomcs=068、custom=070 错号一并修正） | 五张账号表号一一对应：wecomcs=071／onebot=072／slack=073／wecombot=074／custom=075，`grep -rn '迁移 06[89]' specs/025-im-platforms/` 仅剩勘误注记 |
| M-2 | channelcaps 附件上限 | 对齐 Chatwoot P2-3：`Manifest` 增附件数量/大小上限字段，21 条 manifest 逐条附「代码取证锚点」，入站校验落点在渠道客户端（不在渲染层） | 字段 + 锚点 + 1 条防漂移一致性测试 |
| M-3 | 细粒度内容归一 | 对齐 Chatwoot `WebhookContentNormalizer`：`RenderForChannel` 已收口主语义（plain 去 MD/卡片降级），补 CommonMark 硬换行反斜杠清理、代码块外分隔线转义 | 对齐细节行为的单测 |
| M-4 | webhttp/ima 粗细能力矩阵归位 | 024 delivery §七遗留 | `TestCapabilityDocFreshness` 绿 |
| M-5 | `wecom_integration.go` `if false ||` 死条件清理 | 024 review2 §C 留观项 | grep 无死条件 |

M-2/M-3 与平台批共享 `internal/channelcaps/manifests.go` 与渲染文件，按 §三 串行纪律执行。

**状态（2026-11-18）**：M-2/M-3/M-4 已完成（M-1/M-5 随 024 交付完成）。

- M-2：`Manifest` 新增 `MaxAttachments`/`MaxAttachmentBytes`/`AttachmentEvidence` + `AttachmentLimitOf`；
  21 条 manifest 逐条登记（桥接渠道经 `bridge()` 闭包共享），`AttachmentEvidence` 全部非空；
  防漂移测试 `TestEveryManifestDeclaresAttachmentLimit`（负值/空证据红，入站媒体渠道的 0 上限必须解释）
  \+ `TestAttachmentLimitOf`；能力表增「附件数/附件上限/附件取证」三列并由 `TestCapabilityDocFreshness` 钉死。
  校验落点仍是渠道客户端（inbox_ingress / channelbot），本次只声明不拦截。
- M-3：`RenderForChannel` 在格式转换前调 `normalizeOutboundText`：代码围栏外清硬换行行尾反斜杠
  （奇数个才清，`C:\\tmp` 这类成对反斜杠不动）、还原 `\---`/`\***`/`\___` 转义分隔线、
  去尾随空行；围栏内逐字保留。单测 `TestRender_NormalizesCommonMarkHardBreaks` /
  `TestRender_RestoresEscapedThematicBreak` / `TestRender_KeepsCodeFenceVerbatim` / `TestRender_TrimsTrailingBlankLines`。
- M-4：`TestNonMessagePlatformsCarryOnlyConnectorCaps` 钉住「不在 channelcaps 的平台只能有
  sms/email/kb.sync 能力」，并要求 ima/webhttp 作为 kb.sync 样本存在；能力表头注明
  ima/webhttp 是知识库连接器、无 IM 消息路径。
需证据触发类吸收项（typing／流式四细节／邮件归属／聚合窗口不立项）不进本表，已补登 024 spec §四。

**平台批交付状态（2026-11-18）**：T701–T706、T801–T805、T901–T903、T1001–T1002、T1101–T1103 已全部落地
（19 项勾选），T707/T806/T904 三项 🔵 真实账号验收保留未勾——本轮无真实账号，已用 httptest 回放 +
固定签名向量完成模拟信号验收，待有账号后按 tasks.md 验收清单补跑。

- 客户端：`channelbot/wecomcs`（token 缓存 + sync_msg 游标 + send_msg + 回调验签）、
  `channelbot/onebot`（四类 post_type + CQ 转义/切块 + HTTP 调用）、
  `channelbot/slack`（v0 签名 + 5 分钟窗 + postMessage/conversations.open）、
  `channelbot/wecombot`（43 位 AESKey → AES-256-CBC + 签名 + markdown 推送）。
- 入站：通用 webhook 五渠道全部接线（Verify/ParsePayload/dispatch + 账号表 + 租户反查）；
  Slack `url_verification`、wecomcs GET 验证在入队前短路；wecomcs 回调触发增量拉取，
  游标每页落 `wecom_cs_accounts.sync_cursor`，另有 60s 兜底 ticker。
- OneBot 反向 WS：`GET /ws/onebot/:account_id`，Bearer token 先鉴权后升级；同 self_id 重连顶旧连接，
  读超时 90s 自动注销；出站优先 WS、失败/未连接回落 HTTP。
- 出站：五个 sender 全部登记 `RegisterChannelSender`；wecomcs 窗口错误映射 `CategoryWindowExpired`，
  custom 4xx 不可重试 / 5xx+网络可重试。
- custom 入站由「枚举占位」转为真适配器：`webhookInboundRejectHints` 的 custom 条目与旧断言已删除。
- 测试：`channelbot/{wecomcs,onebot,slack,wecombot}` 回放测试 + service 纯函数/签名/拉取测试
  （不依赖 POSTGRES_TEST_PASSWORD）；DB 全链路用例沿用既有门禁按环境跳过。
