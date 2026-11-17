# Plan: 025-im-platforms

**Input**: `spec.md`（本目录）、`tasks.md`（本目录，含逐任务七字段规格）
**前置**: `024-im-seam-alignment` 的 B1（出站注册表）与 B2（事件类型）已交付并通过门禁
**口径**: 每个平台以「八触点齐全 + 验收四条」为完工；批次之间无代码依赖，可并行排期（同一人串行时按下表顺序）

## 一、批次与工期

| 批次 | 平台 | 任务 | 人日 | 交付物 |
|---|---|---|---|---|
| P1 | 企业微信客服 wecomcs | T701–T707 | 6–7 | `channelbot/wecomcs`、入站/出站、`wecom_cs_accounts`（069）、游标续拉 worker |
| P2 | OneBot v11 | T801–T806 | 6–7 | `channelbot/onebot`、HTTP 上报 + 反向 WS、`onebot_accounts`（070） |
| P3 | Slack | T901–T904 | 4–5 | `channelbot/slack`、Events API、线程回复、`slack_accounts`（071） |
| P4 | wecombot | T1001–T1002 | 3 | 回调解密 + 卡片事件 → `channel_interactions`、`wecombot_accounts`（072） |
| P5 | 通用 custom | T1101–T1103 | 2–3 | 平台无关契约、`custom_channel_accounts`（073）、签名回调出站 |
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
- 迁移号按批次预分配（069–073），不得临时改号：并行时号段冲突比内容冲突更难解。

## 四、回滚

| 单位 | 方式 | 数据影响 |
|---|---|---|
| 单个平台 | 关账号开关 + `webhookInboundCapable` 置 false（回调 fail-closed） | 无；已入站消息保留 |
| 枚举增量（047/063 式） | 不回滚：`ALTER TYPE ... ADD VALUE` 不可逆，保留孤立值无害 | 无 |
| 账号表（069–073） | 不回滚：表留着，代码不再引用 | 无 |
| 出站 sender | 注册表摘除 + `ErrOutboundChannelUnsupported` | 该平台回复失败可重试 |

## 五、验证分层

1. **包级**：`channelbot/<x>` 用 `httptest` 回放固定报文（验签、错误码、分页/游标）。
2. **服务级**：`webhook_channel_<x>_test.go` 走 `HandleIngressMessage`，断言落库/事件类型/审计。
3. **跨层**：`controller/webhook_*_http_test.go` 断言 4xx 不落库、租户反查 fail-closed。

## 六、与任务清单的关系

外层 `docs/未完成任务清单.md` §3.21 的 IM-6/IM-7/IM-8/IM-9 行指向本 spec；本 spec 的进度以 `tasks.md`
勾选状态为唯一真值，不再另建进度文档。
