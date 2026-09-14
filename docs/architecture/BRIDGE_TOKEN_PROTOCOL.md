# Bridge 通道凭证发放与轮换协议（v3 设计稿）

> 对应实现：`user-server/internal/middleware/bridge_ingress_guard.go`
> 状态：服务端已就绪（`BRIDGE_INGEST_TOKEN` / `BRIDGE_INGEST_AUTH=off`），本文档定义扩展端的密钥获取与轮换流程。

## 一、目标

浏览器扩展（抖音/快手/小红书/闲鱼/TikTok 五渠道）调用 user-server 桥接通道
（`/api/bridge/ingest|outbox|outbox/ack|outbox/sse`）时的身份凭证机制，
封堵匿名读取外发消息与伪造入站消息触发 AI 管线的攻击面。

## 二、凭证模型（阶段一：静态共享 token，已实现）

```
┌─────────────┐  1. 用户在管理端「渠道接入」页生成 token      ┌─────────────┐
│  user-web   │ ──────────────────────────────────────────▶ │ user-server │
│             │     POST /api/bridge/token (admin)           │  BRIDGE_    │
│  展示一次    │ ◀────────── 返回 token（仅此一次明文）────── │ INGEST_TOKEN│
└─────────────┘                                              └─────────────┘
        │ 2. 用户复制粘贴到扩展设置页（Chrome storage.local）
        ▼
┌─────────────┐  3. 每次请求携带 X-Bridge-Token               ┌─────────────┐
│ 浏览器扩展  │ ──────────────────────────────────────────▶ │ user-server │
└─────────────┘     常量时间比对；失败 401                      └─────────────┘
```

- **存储**：token 仅存服务端 env（重启不丢）；扩展侧存 `chrome.storage.local`。
- **传输**：全程 HTTPS；SSE 端点同样校验（EventSource 不支持自定义 header 时，
  允许 `?bridge_token=` query 一次性传递，服务端等价校验并保证日志脱敏）。
- **格式**：32 字节 crypto/rand → base64url（43 字符），无前缀。

### 服务端行为矩阵
| BRIDGE_INGEST_TOKEN | BRIDGE_INGEST_AUTH | 行为 |
|---|---|---|
| 已配置 | * | 强制校验 X-Bridge-Token |
| 未配置 | off | 放行 + 启动 WARN（仅联调） |
| 未配置 | 未设置 | 全部请求 503 |

## 三、阶段二路线：per-account AppKey/AppSecret（规划）

单商户多扩展/多操作员时升级为每账号独立凭证：

1. 表 `bridge_accounts` 增加 `app_key`(公开标识) + `app_secret_hash`(bcrypt)。
2. 签名升级：`X-Bridge-Key` + `X-Bridge-Timestamp` + `X-Bridge-Signature`
   = hex(HMAC-SHA256(app_secret, METHOD+"\n"+PATH+"\n"+TS+"\n"+SHA256(body)))。
   与平台端 merchant-api HMAC 同构，复用验签代码骨架（含 ±300s 窗口与
   验签后置防重放缓存——吸取 Round6 平台端投毒 DoS 教训）。
3. 发放：管理端为每个 bridge_account 生成 secret 一次下发；
   扩展端设置页粘贴，同阶段一交互。

## 四、轮换

| 场景 | 流程 |
|---|---|
| 泄露疑似 | 管理端「重置 token」→ 新 token 下发 → 各扩展设置页更新 → 旧 token 立即失效（env 单值模型天然即时生效需重启；阶段二 per-account 为热生效） |
| 例行轮换(180d) | 与 FIELD_ENCRYPTION_KEY 轮换节奏对齐，见 docs/operations/secret_rotation.md |

阶段一双值灰度：支持 `BRIDGE_INGEST_TOKEN` + `BRIDGE_INGEST_TOKEN_PREV`
双读一个发布周期，扩展端滚动更新零中断。

## 五、验收清单

- [x] 无 token 请求 → 401/503（默认 fail-closed）
- [x] 错误 token → 401，常量时间比较
- [x] 正确 token → 放行（ingest/outbox/ack/SSE 四端点）
- [x] 管理端生成/重置 UI（`user-web/src/views/bridge/TokenManagement.vue`，路由 `/bridge/token`，接 `/api/bridge/token/status|reset`）
- [x] SSE query-token 透传与日志脱敏（`bridge_ingress_guard.go` 支持 `?bridge_token=` 并在校验通过后从 RawQuery 剥离；`bridge_helpers.go` `maskTokenBridge` 脱敏记录，单测对齐扩展端格式）
- [x] 双值灰度窗口（guard 同时接受 `BRIDGE_INGEST_TOKEN` + `BRIDGE_INGEST_TOKEN_PREV`；`BridgeTokenService.Reset` 轮换时旧值自动转入 PREV）
