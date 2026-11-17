# 渠道接入手册（IM 平台接入触点清单）

> specs/024-im-seam-alignment T1203 定稿。配套机器门：`python3 scripts/check_im_touchpoints.py`
> （漏一处触点即红，CI 作业 `IM Touchpoints` 全量跑）。能力矩阵（机器生成，勿手改）见
> [channel-capabilities.md](channel-capabilities.md)；本文是**手写**的「接一个新平台要改哪几处」。

## 一、六触点清单（漏一处即被 T1201 拦下）

接一个 IM 平台，按顺序落这些触点（全部有既有先例可抄）：

| # | 触点 | 位置 | 说明 |
|---|---|---|---|
| 1 | **渠道常量** | `user-server/internal/service/webhook_channel_telegram.go`（或 model 包） | `Channel<Name> WebhookChannel = "<id>"`；桥接族渠道在 `model` + `channelgw` 登记名字串，不立 service 常量 |
| 2 | **能力 manifest** | `user-server/internal/channelcaps/manifests.go` | `Register(Manifest{...})`：Inbound/Outbound/Events/Features/OutboundFormat/MaxTextLen/**FormatEvidence**（无证据的格式声明等于猜的） |
| 3 | **账号表行** | `user-server/internal/service/webhook.go` `webhookAccountTableByChannel` | 直发渠道必须登记本地账号表（租户反查权威源）；桥接族豁免（走 integration_accounts） |
| 4 | **入站分支** | `webhookInboundCapable` switch 或专用控制器 | 通用 webhook 路由的渠道加进 switch；钉钉/公众号走专用控制器的登记豁免集 |
| 5 | **出站 sender** | 新建 `outbound_impl_<id>.go` + `outbound_sender_<id>.go` | 实现 `channelSender`；`init()` 里 `RegisterChannelSender(...)`；失败只准经 `req.Fail/FailUntracked` 上报（R5 不变量会兜底，但别依赖兜底） |
| 6 | **client 包** | `user-server/internal/channelbot/<id>/` | 平台协议 client（验签/解析/发送统一注入 `core.WithHTTPClient(httpclient.Client)`，出站 HTTP 才能被测试劫持） |

配套义务（触点之外，同样有门）：

- **事件类型**：解析器产出的事件要填 `MessageEvent.EventType`（取值必须来自 `model.EventType*`），
  并同步 `channelcaps.Manifest.Events`——`channelcaps_events_test` 钉住一致性。
- **行为冻结**：出站样例进 `outbound_golden_test.go` 的样例表（8+ 渠道 × 3 样例），
  `testdata/outbound_golden.json` 人工确认后重写。
- **路由规则**：`channel_route_rules` 免配置即放行（默认行为不变），无需为新渠道预置规则。

## 二、入站接入（curl 冒烟示例）

通用 webhook 回调入口（以 telegram 形状为例，`account_id` 为本地账号表主键）：

```bash
curl -sS -X POST "http://127.0.0.1:8080/api/webhook/telegram/<account_id>" \
  -H "Content-Type: application/json" \
  -d '{"update_id":1,"message":{"message_id":9,"date":1700000000,
       "chat":{"id":555001,"type":"private"},"from":{"id":555001,"first_name":"G"},
       "text":"hello"}}'
```

验收判据：验签失败回 4xx **不落库**；同 msg_id 重放不产生第二行
（`message_hub` 幂等）；`channel_event_audits` 能按 `event_type` 检索到这条事件。

桥接族入站不走本入口：扩展经 `POST /api/bridge/ingest` 上报（三通道契约见
`docs/bridge/BRIDGE-ARCHITECTURE-*.md`），服务端按 `channelgw` registry 能力位放行。

## 三、出站验证

```bash
# AI 回复出站走 sendOutbound → 渠道 sender；人工旁路（如需）走各渠道管理 API，
# 不要手写第二个外发权威（单一 sender 门）。
# 观测：outbound_send_failed_total{channel,category} / outbound_text_overflow_total{channel}
#       route_rule_hit_total{tenant,target}（specs/024 T1205 三指标）
```

## 四、门禁自检（接完先自己跑一遍）

```bash
python3 scripts/check_im_touchpoints.py          # 六触点矩阵
cd user-server && go test ./internal/service -run 'TestOutboundRegistry|Parity|TestOutboundBehaviorFreezeGolden' -count=1
```
