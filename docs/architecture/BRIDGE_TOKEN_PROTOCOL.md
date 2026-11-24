# 渠道识别码协议（原 Bridge 通道凭证协议，v4 / 一次性替换后）

> 对应实现：`user-server/internal/middleware/bridge_ingress_guard.go`、
> `user-server/internal/service/channel_identify.go`、
> `user-server/internal/repository/channel_identify_code.go`。
> 状态：自 US4（T058）起读路径一次性切换到渠道识别码；旧 Bridge token 已退役
> （T061 停双写、T063 删 KV 字段）。**没有长期兼容期**——旧客户端请求 0% 被接受。

## 一、目标

浏览器扩展（抖音/快手/小红书/闲鱼/TikTok 五渠道）调用 user-server 桥接通道
（`/api/bridge/ingest|outbox|outbox/ack|outbox/sse`）时的身份凭证机制，
封堵匿名读取外发消息与伪造入站消息触发 AI 管线的攻击面。

## 二、凭证模型（渠道唯一识别码，已实现）

```
┌─────────────┐  1. 管理员在「渠道识别码管理」页为渠道生成/重置识别码  ┌─────────────┐
│  user-web   │ ───────────────────────────────────────────────────▶ │ user-server │
│             │     POST /api/identify-codes/{channel_id}/rotate      │ channel_     │
│  展示/复制   │ ◀────────── 返回识别码明文（可再查看）───────────── │ identify_    │
└─────────────┘                                                       │ codes        │
        │ 2. 用户复制粘贴到扩展设置页（Chrome storage.local）           └─────────────┘
        ▼
┌─────────────┐  3. 每次请求携带 X-Channel-Identify-Code              ┌─────────────┐
│ 浏览器扩展  │ ───────────────────────────────────────────────────▶ │ user-server │
└─────────────┘     候选源 = channel_identify_codes；失败 401          └─────────────┘
```

- **存储**：识别码权威记录在 `channel_identify_codes`（全局唯一，可重置，version+1）；
  同时在 `channel_accounts.identify_code` 冗余一列供按码反查租户/账号。
  扩展侧存 `chrome.storage.local`。
- **传输**：全程 HTTPS；SSE 端点（`EventSource` 不支持自定义头时）允许
  `?identify_code=` 查询一次性传递，服务端校验通过后从 RawQuery 剥离并保证日志脱敏。
- **旧凭证**：`X-Bridge-Token` / `?bridge_token=` / KV `bridge_ingest_token(_prev)` /
  环境变量 `BRIDGE_INGEST_TOKEN(_PREV)` 全部退役（T063 删除 KV 键）；
  旧头不再被读取，旧值即使从任意头进来也换不到桥接权限。

### 服务端行为矩阵
| channel_identify_codes 有效行 | BRIDGE_INGEST_AUTH | 行为 |
|---|---|---|
| 有 | * | 强制校验 X-Channel-Identify-Code（恒时比较） |
| 无 | off | 放行 + 启动 WARN（仅联调） |
| 无 | 未设置 | 全部请求 503 fail-closed |

## 三、三条消息路径统一（FR-031）

bridge HTTP、统一 API 上报、hook/轮询/SageX 三条入口保留为适配器，仅做归一化投递：
消息统一落 `InboundMessage` 收件箱，租户/账号归属统一走 `ChannelResolver.ResolveByIdentifyCode`
（识别码全局唯一 → 反查租户/账号；歧义 fail-closed 拒绝，不回落默认租户）。

## 四、重置与在途窗口（T061）

- 重置（`/api/identify-codes/{channel_id}/rotate`）生成新码、version+1、更新 `reset_at`。
- 旧码进入 `prev_code` + `prev_valid_until` 短暂窗口（默认 60s，env
  `CHANNEL_IDENTIFY_GRACE_SECONDS` 可调），只覆盖"重置瞬间仍在飞行"的请求；
  窗口过后立即失效。这不是与旧 Bridge token 的兼容期。
- Phase2 的双写（把识别码写进旧 Bridge token KV）已停用——旧 KV 已无读方，
  连续重置不再挤占 `_prev` 位。

## 五、验收清单

- [x] 无识别码请求 → 401/503（默认 fail-closed）
- [x] 错误识别码 → 401，常量时间比较
- [x] 正确识别码 → 放行（ingest/outbox/ack/SSE 四端点）
- [x] SSE query-token 透传与日志脱敏（改名为 `?identify_code=`，校验后从 RawQuery 剥离）
- [x] 旧 X-Bridge-Token / 旧 KV 键全部移除（0% 接受）
- [x] CORS 允许头改为 X-Channel-Identify-Code（跨源扩展预检不失败）
- [x] 重置在途窗口按 version+1 处理，不保留长期兼容期
