# QQ 机器人渠道 Webhook 配置体验增强 — 设计决策文档

日期：2026-09-08
状态：待用户确认
关联代码：`user-server/internal/channelbot/qq/`、`user-server/internal/service/qq_account.go`、`user-server/internal/controller/qq_account.go`、`user-web/src/views/qq/account.vue`

---

## 1. 背景与问题

QQ 机器人渠道已接入（官方开放平台协议，群@/单聊入站 + AI 承接 + Ed25519 验签）。当前账号配置页要求用户**手动填写 Webhook URL**（placeholder 为 "例如：https://your-domain/api/webhook/qq/1"），存在三个问题：

1. **易填错**：URL 路径必须严格为 `/api/webhook/qq/{id}`，手打容易拼错（TG 侧曾因此专门加前缀校验，见 `ValidateTelegramWebhookURL` 注释 S3-5 修复）。
2. **无自检**：用户把 URL 粘贴到 q.qq.com 管理后台之前，无法在本系统内确认 BotSecret 是否填对、验签链路是否通。填错只能等平台推送失败排查。
3. **与 TG 体验不一致**：Telegram 账号页的 Webhook URL 是只读展示 + 一键复制（`account.vue:237`），且 URL 由后端从 `PUBLIC_BASE_URL` 自动推导。

### 平台机制约束（外部调研结论，2026-09-08 核实）

| 事实 | 来源可信度 |
|------|-----------|
| QQ 官方**没有**公开的 setWebhook 类 API；回调地址只能在 q.qq.com 管理后台（`#/developer/webhook-setting`）手动配置 | 官方文档确认 |
| botgo SDK 源码存在未公开的 `/gateway/webhook/sessions` REST API（可编程创建回调 session），但仓库两年未维护、未写入官方 wiki | 官方 SDK 源码确认，可用性存疑 |
| op13 回调验证：平台发 `{"op":13,"d":{"plain_token","event_ts"}}`，服务端用 BotSecret 派生 Ed25519 私钥签名 `event_ts+plain_token` 应答 | 官方文档确认 |
| 官方提供本地模拟验证方式：用 secret 生成签名后可自测（botgo `examples/simulate-callback-request`） | 官方 SDK 确认 |
| 回调地址要求 HTTPS，端口限 80/443/8080/8443 | 官方文档确认 |
| WebSocket 长连接与 Webhook 双通道并存；官方 2024 公告 WS 将下线但截至 2026-09 未执行 | 官方公告+文档核验 |

**结论**：Telegram 式「启动期全自动 setWebhook 对账」（`ReconcileTelegramWebhooks`）在 QQ 上**无法完整复刻**——注册那一步永远是人工的。但 URL 推导、展示、自检这三段可以全部自动化。

---

## 2. 目标与非目标

**目标**
- G1：Webhook URL 自动推导 + 展示 + 一键复制，消灭手填（公网 base 不可用时降级为校验兜底的手填）。
- G2：提供「验签自检」能力：本地验证 op13 应答链路连通、BotSecret 已填写。**明确边界：secret 是否正确只能由 q.qq.com 保存回调时平台发起的真实 op13 验证确认，本地自检不承诺这一点。**
- G3：URL 格式校验（https + 端口白名单 + 路径前缀），拦住明显错误。
- G4（二期可选，见 §5）：回环实测——服务端向推导出的公网 URL 真实 POST 一次自构造 op13 challenge 并校验应答签名，覆盖 DNS/证书/nginx/frp/路由等部署链路问题。

**非目标**
- 不做 WebSocket 接入通道（现有 webhook 架构与官方推荐方向一致，双通道并存无收益）。
- 不做启动期对账（`ReconcileQQWebhooks`）——QQ 无 setWebhook API，没有可对账的远端状态。
- 不依赖未公开的 `/gateway/webhook/sessions` API（两年未维护，风险大于收益；若官方未来正式公开可再评估）。
- 不改协议层 `channelbot/qq`（验签/op13 实现已通过单测，本次零改动或仅加只读函数）。

---

## 3. 方案对比与决策

### 方案 A：仅前端拼接 URL（最薄）

前端 JS 里用 `location.origin` 拼出 `/api/webhook/qq/{id}` 展示。

- ✅ 改动最小（约 20 行 Vue）。
- ❌ `location.origin` 是用户访问前端域名的 origin，而 webhook 要走后端公网域名（本项目拓扑：云端 nginx 静态 + frp 回本地 user-server，前端域名与后端公网回调域名可能不同）。拼出来大概率是错的，误导性比手填更强。
- ❌ 没有校验、没有自检。

**否决理由**：在 frp 部署拓扑下基本是错的答案。

### 方案 B（推荐）：TG 对齐 — 后端推导 + URL 校验 + 本地自检接口

完全复用 TG 侧已验证的三件套模式，QQ 化改造：

1. **URL 推导**：后端 `deriveQQWebhookURL(accountID)` 已存在（`qq_account.go:261`，从 `config.GetPublicBaseURL()` 推导）但**没有任何调用方**——本次接上：List/Get VO 增加 `webhook_url_suggested` 字段；`Create/Update` 时**仅当请求未填且存量为空**才自动落推导值（显式 URL 永远优先，与 TG `ResolveTelegramWebhookURL` 优先级语义对齐：账号表显式值 > PUBLIC_BASE_URL 推导）；推导值自身若过不了 `ValidateQQWebhookURL`（如 base 带非白名单端口）则**不落值**，前端退化为可编辑 + 警示，避免只读死锁。
2. **URL 校验**：新增 `ValidateQQWebhookURL(raw)`（仿 `ValidateTelegramWebhookURL`）：必须 https、host 非空、path 前缀 `/api/webhook/qq/`、**端口 ∈ {80,443,8080,8443}**（QQ 官方白名单，比 TG 多这条）。
3. **自检接口**：`POST /api/qq/accounts/:id/verify-callback`，内部：
   - 校验 BotSecret 非空；
   - 用现有 `GenerateCallbackTestSignature`（qq.go:346，当前已被线上 op13 真实验证链路 `HandleQQCallbackChallenge` 复用）对模拟的 `plain_token/event_ts` 生成 op13 应答，确认签名生成链路 OK；
   - **能力边界（重要）**：本地自检只能证明「验签链路连通 + secret 非空」——`DerivePrivateKey` 对任意非空 secret 都能成功签名，secret 是否填对**只能由平台保存回调时发起的真实 op13 验证确认**。前端文案必须如实表述，不得宣称"自检通过 = secret 正确"。
   - 返回 `{plain_token, signature}`（与平台 op13 应答体字段名一致）。
   - 这正是 botgo 官方示例 `simulate-callback-request` 的本地自测姿势。
4. **前端**：仿 TG `account.vue:237`——Webhook URL 展示（推导值优先，存量手填值次之；`PUBLIC_BASE_URL` 未配置或推导值校验不过时输入框**退化为可编辑**并警示）+ 复制按钮；加「自检验签」按钮调 verify-callback；BotSecret 输入框旁提示"填 q.qq.com 下发的 BotSecret"。

- ✅ 复用全部已验证件（derive/validate/genSignature 三者 TG/QQ 侧均有现成实现或同构模式）。
- ✅ 用户手工步骤压缩到最少且不可错：复制 → 粘贴到 q.qq.com → 平台自动 op13 验证通过。
- ✅ 五层架构干净：controller 加只读推导+校验函数、service 加 verify-callback 方法、router 加一条映射。
- ⚠️ `PUBLIC_BASE_URL` 未配置时推导值为空——降级为允许手填（校验兜底），与 TG 行为一致。

### 方案 C：方案 B + 未公开 session API 自动注册

在 B 之上，调用 botgo 未公开的 `POST /gateway/webhook/sessions` 自动注册回调。

- ✅ 唯一能做到"接近 TG 全自动"的路线。
- ❌ 接口未公开、未文档化、botgo 仓库两年未维护，随时可能变更或封禁（OpenAPI 调用还要求出口 IP 白名单）。
- ❌ 违反本项目「不接脆弱依赖」纪律（对比：tech-research 期间对未文档化接口一律回避）。

**否决理由**：收益（省一次粘贴）不抵风险（接口消失导致功能静默失效 + 白名单运维负担；且本项目 go.mod 无 botgo 依赖，采纳 C 意味着新增脆弱外部依赖）。若官方正式公开该 API，届时在 B 基础上增量加入即可——B 的数据模型完全兼容。

### 决策

**采纳方案 B**。理由：在平台机制硬约束（无 setWebhook API）下，B 已把自动化空间吃满，剩余一步（粘贴到平台）是物理不可消除的；C 的风险收益比不成立。

---

## 4. 详细设计（方案 B）

### 4.1 后端

**service 层**（`internal/service/qq_account.go` + 新文件 `internal/service/qq_webhook_bootstrap.go`）：

```go
// qq_webhook_bootstrap.go
const QQWebhookURLPathPrefix = "/api/webhook/qq/"
var QQWebhookAllowedPorts = map[int]bool{80: true, 443: true, 8080: true, 8443: true}

// ValidateQQWebhookURL 校验：https + host + path 前缀 + 端口白名单（QQ 官方约束）
func ValidateQQWebhookURL(raw string) error

// SuggestQQWebhookURL(publicBase string, accountID uint) string —— 提取现有 deriveQQWebhookURL 逻辑为 service 导出函数，controller 复用
```

`QQService` 增加：

```go
// VerifyCallbackSelfCheck 本地 op13 自检：模拟平台验证请求，返回签名应答。
// 失败条件：BotSecret 未配置 / 签名生成异常。
func (s *QQService) VerifyCallbackSelfCheck(ctx context.Context, accountID uint) (*QQCallbackSelfCheckResult, error)
// QQCallbackSelfCheckResult{PlainToken, EventTS, Signature string}
```

**controller 层**（`internal/controller/qq_account.go`）：

- VO 增加 `webhook_url_suggested`（`deriveQQWebhookURL` 接到 service 导出函数）。
- Create/Update：仅当请求未填且存量为空时自动落推导值（推导值先过校验）；非空 URL 走 `ValidateQQWebhookURL`，Update 失败写回 `last_error_msg` 并 400，Create 失败直接 400（记录未落库无处写错误状态）。
- 新端点 `POST /:id/verify-callback` → `svc.VerifyCallbackSelfCheck`，守卫 `guardChannelAccountOwnership`。

**router**：`setupQQRoutes` 内现有 RegisterRoutes 模式追加一条 `g.POST("/:id/verify-callback", ...)`，零新路由组。

### 4.2 前端（`user-web/src/views/qq/account.vue` + `src/api/qqBot.js`）

- `qqBot.js` 加 `verifyCallback(id)`。
- 表单 Webhook URL 项仿 TG：展示优先级 `webhook_url(存量) || webhook_url_suggested(推导)`；存量存在且 base 可用时 disabled + 复制按钮；**base 未配置或推导值校验不过时始终可编辑**，附警示文案（端口白名单 80/443/8080/8443）。
- 表单加「自检验签」按钮：调 verify-callback，成功 toast 展示签名前 16 位，并弹出指引文案（"到 q.qq.com → 开发者 → 回调配置 粘贴 URL，平台将自动完成 op13 验证"）。
- 列表页 Webhook 列已存在，无改动。

### 4.3 数据模型

零迁移。复用 `qq_accounts` 现有 `webhook_url / webhook_secret / webhook_enabled / last_error_msg` 字段（`model/feishu.go:115 QQAccount`）。

### 4.4 错误处理

| 场景 | 行为 |
|------|------|
| `PUBLIC_BASE_URL` 未配置 | VO `webhook_url_suggested=""`，前端退化为手填 + 校验兜底 |
| `PUBLIC_BASE_URL` 已配置但端口不在白名单（或经 GetPublicBaseURL 升级 https 后仍不合法） | 推导值**不自动落库**，前端可编辑 + 警示 |
| BotSecret 未配置 | 自检接口 400："请先填写 q.qq.com 下发的 BotSecret" |
| URL 校验失败 | 400；Update 场景写 `last_error_msg`，Create 场景仅 400 |
| 自检通过但平台验证失败 | 不在本系统可控范围；前端指引文案明确分界（本地自检只证链路连通，secret 正确性以平台 op13 为准） |

### 4.5 测试

- `service` 单测：`ValidateQQWebhookURL` 全分支（仿 `telegram_webhook_test.go`）；`VerifyCallbackSelfCheck` 有/无 secret。
- `controller` 单测：Create/Update 自动落 URL 与校验拒绝；verify-callback 成功/失败（仿 `webhook_qq_fullchain_test.go` 现有口径）。
- 回归：现有 `channelbot/qq` + `service` QQ 测试全绿；`go build ./...` + `vite build` 通过。

---

## 5. 风险与遗留

- **风险**：QQ 官方若正式公开 session API，可在本设计上增量加方案 C（数据模型兼容），届时另开决策。
- **二期增强（G4）**：回环 op13 实测——服务端向推导出的公网 URL 真实 POST 自构造 challenge 并校验应答签名，能在粘贴平台前暴露 DNS/证书/反代/frp/路由/secret 未配等部署问题。打的是自家端点，不违反"无 setWebhook API"约束；本次不做（涉及公网回环探测的频控与超时策略，值得单独设计）。
- **遗留修正**：TG 侧 URL 来源优先级实为「账号表显式 URL > PUBLIC_BASE_URL 推导」（`ResolveTelegramWebhookURL`）；X-Forwarded 请求头回退是 controller 侧 `deriveTelegramWebhookURLWithBase` 的行为。QQ 侧本次只做「显式 > PUBLIC_BASE_URL」两级，与 frp 拓扑下唯一可靠来源保持一致。
- **部署提醒**（写进自检指引文案）：服务器出口 IP 需加入 q.qq.com 管理端白名单才能调用 OpenAPI 发消息。
