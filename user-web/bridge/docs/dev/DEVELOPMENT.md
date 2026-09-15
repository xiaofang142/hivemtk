# user-web/bridge 代码开发手册（HiveBridge 蜂桥）

> **规则级别**: ⭐⭐ 项目级开发文档
>
> 本手册面向 `user-web/bridge`（HiveBridge 蜂桥 Chrome 扩展）的二次开发者，
> 覆盖环境准备、命令、目录导航、协议契约、新功能流程、测试、构建发布与调试技巧。
>
> 关联文档：
> - 扩展设计主文档：[./../bridge.md](../../bridge.md)
> - 扩展架构总览：[./ARCHITECTURE.md](../ARCHITECTURE.md)
> - 扩展默认值清单：[./DEFAULTS.md](../DEFAULTS.md)
> - 上游服务端实现：[../../../user-server/internal/bridge/](../../../../user-server/internal/bridge)

---

## 一、环境准备

| 依赖 | 版本 | 说明 |
| --- | --- | --- |
| Node.js | ≥ 20 | 测试与构建统一使用 Node 20 LTS |
| npm | ≥ 10 | 仓库内置 `package-lock.json`，推荐 `npm install` |
| esbuild | ^0.21.5 | `npm run build` 通过 esbuild 多入口打包（MV3 content/background 场景最稳） |
| Vitest | ^1.6.0 | 单元测试，jsdom 环境 |
| Chrome | ≥ 110 | 加载已解压扩展进行联调（MV3） |
| user-server | 本地或远端 | 默认 `http://localhost:8204`（单一源：`src/core/constants.js`） |

初始化：

```bash
cd hivemtk/user-web/bridge
npm install
```

---

## 二、启动命令

| 命令 | 作用 | 备注 |
| --- | --- | --- |
| `npm run build` | esbuild 多入口打包到 `dist/` | 每次修改源码必跑，扩展加载的是 dist 产物 |
| `npm run dev` | `node scripts/build.mjs --watch` 持续构建 | 监听源码变化，刷新扩展生效 |
| `npm run test` | Vitest 单元测试（jsdom） | 覆盖 `constants.js` / `protocol` / `adapter` / `rate-limiter` / `sanitize` / `fallback` / `popup` / 三通道调度 |
| `npm run test:watch` | 监听模式 | 开发期间持续运行 |
| `npm run release` | `node scripts/release.mjs` 打 release 包 | 产物 `release/hivebridge-1.0.0.zip` |
| `npm run lint` | `eslint src test scripts --max-warnings=80` | 静态检查 |

### 2.1 本地开发联调（核心三步）

```bash
# 1. 启动 user-server（必先启动）
cd ../../user-server && go run ./cmd/api
# 验证：curl http://localhost:8204/health

# 2. 构建扩展产物
cd hivemtk/user-web/bridge
npm run dev    # 持续构建（推荐）

# 3. Chrome 加载扩展
# Chrome → chrome://extensions → 开启「开发者模式」→ 「加载已解压扩展」→ 选择 hivemtk/user-web/bridge/dist/
# 修改源码后只需点击 chrome://extensions 的「重新加载」按钮
```

### 2.2 启动后验证

```bash
# 1. 扩展 popup 打开后「测试连接」按钮
#    默认 server = http://localhost:8204（与 user-server 端口对齐）
#    应弹出「连接成功」

# 2. 打开抖音/小红书/TikTok 私信页
#    F12 → Console 看到 [bridge] ... 日志
#    F12 → Application → Service Workers → 查看 background.js 日志
```

---

## 三、端口与端点

> **核心约束**：`src/core/constants.js` 的 `DEFAULT_USER_SERVER.port` = **8204**（user-server Gin HTTP 端口）
> 这是扩展**唯一**的默认服务端地址。任何前端代码、文档、配置均不得修改此字面量，
> 除非同步修改 user-server `internal/config/ports.go` 的 `DefaultListenPort` 与本文档。

| 端口 | 服务 / 应用 | 启动入口 | 单一源常量 | 文档源 |
| --- | --- | --- | --- | --- |
| **8204** | **user-server**（扩展连接入口） | `go run ./cmd/api` | `constants.DEFAULT_USER_SERVER.port` | user-server `internal/config/ports.go` `DefaultListenPort` |
| 8207 | LLM（本地推理） | 扩展不直连 | user-server `DefaultLLMPort` | user-server docs §2.4 |
| 8208 | Embedding（本地推理） | 扩展不直连 | user-server `DefaultEmbeddingPort` | user-server docs §2.4 |
| 8209 | Rerank（本地推理） | 扩展不直连 | user-server `DefaultRerankPort` | user-server docs §2.4 |

### 3.1 popup 端口默认值

- **默认**：`http://localhost:8204`（`DEFAULT_USER_SERVER.baseUrl`）
- 覆盖方式：在 popup 表单中手动修改，存储到 `chrome.storage.local`
- 跨平台：用户远程部署时改为 `https://user-server.your-domain.com` 等公网地址

### 3.2 桥接端点（HTTP 三通道）

> bridge ↔ user-server 走 **HTTP 三通道**（非 WS / 非 SSE）。详见 [./ARCHITECTURE.md §1](../ARCHITECTURE.md)。

| 端点 | 方法 | 用途 | 实现位置 |
| --- | --- | --- | --- |
| `/api/bridge/ingest` | POST | 上行消息 + 拉取同会话待发 reply | `src/core/http-ingest.js::postIngest` |
| `/api/bridge/outbox` | GET | 下行轮询：拉取待发 AI 回复 | `src/core/http-ingest.js::getOutbox` |
| `/api/bridge/outbox/ack` | POST | 标记 msg_id 为 delivered/failed | `src/core/http-ingest.js::ackOutbox` |

### 3.3 鉴权

- 默认：仅 `InitGuard`（私有化部署单用户，无 token）
- 可选：`Authorization: Bearer <token>` Header（增强部署）
- **禁止** Token 走 URL query（devtools / 浏览器历史明文泄漏）

---

## 四、目录导航

```
user-web/bridge/
├── src/
│   ├── background/                   后台 service_worker
│   │   ├── index.js                  HTTP 三通道调度 + port 路由 + 配置/状态
│   │   └── injector.js               content script 注入
│   ├── channels/                     各平台适配层
│   │   ├── douyin.js                 抖音 IM DOM 解析
│   │   ├── xhs.js                    小红书 DOM 解析
│   │   ├── tiktok.js                 TikTok DOM 解析
│   │   ├── xianyu.js                 闲鱼 DOM 解析
│   │   └── kuaishou.js               快手 DOM 解析（预留）
│   ├── content/                      content script
│   │   ├── common.js                 PollingLoop / dispatchOutbound / 解析
│   │   ├── douyin.js · xhs.js · tiktok.js · xianyu.js · kuaishou.js
│   ├── core/                         核心模块（与 platform 无关）
│   │   ├── types.js                  UnifiedMessage / UnifiedReply 数据模型
│   │   ├── channel-adapter.js        BaseAdapter + ChannelAdapter 接口
│   │   ├── base-adapter.js           MutationObserver / 去重 / 上下行封装
│   │   ├── http-ingest.js            postIngest / getOutbox / ackOutbox
│   │   ├── uplink.js                 Uplink 队列（合并窗口 + 持久化 _confirmed）
│   │   ├── downlink.js               pollDownlink + _pendingAck 重试 + SentCache
│   │   ├── polling-loop.js           下发轮询调度器
│   │   ├── circuit-breaker.js        熔断器 + 幂等键
│   │   ├── rate-limiter.js           三层风控（拟人节奏 + 令牌桶 + 会话冷却 + 去重）
│   │   ├── humanize.js               拟人化（贝塞尔鼠标 + 键入节奏）
│   │   ├── sanitize.js               XSS 防护 / 内容净化
│   │   ├── config-store.js           配置热更新
│   │   ├── selector-engine.js        选择器引擎
│   │   ├── selector-ai.js            选择器 UI 配置面板（chrome.storage 持久化）
│   │   ├── fallback.js               account_id fallback 派生
│   │   ├── offline-cache.js          离线缓存
│   │   ├── logger.js                 日志脱敏
│   │   └── constants.js              ⭐ 单一源：所有默认值（端口/URL/限速/协议）
│   └── popup/                        popup UI
│       ├── index.html
│       ├── index.js                  后端地址配置 / 状态 / 自检
│       ├── accounts.js               多账号管理
│       ├── config-io.js              配置读写
│       ├── alert-banner.js           告警横幅
│       ├── error-messages.js         错误文案友好化
│       ├── health.js                 健康度面板
│       └── emergency-stop.js         紧急停止
├── test/                             Vitest 单元测试（44 文件，674 用例）
│   ├── constants.test.js             ⭐ 验证单一源字面一致
│   ├── protocol.test.js              协议帧测试
│   ├── adapter.test.js               渠道适配器测试
│   ├── rate-limiter.test.js          限速桶测试
│   ├── rate-limiter-lru.test.js      LRU + TTL 测试
│   ├── sanitize.test.js              XSS 防护测试
│   ├── fallback.test.js              兜底测试
│   ├── http-ingest.test.js           HTTP 三通道客户端测试
│   ├── uplink.test.js                上行队列测试
│   ├── downlink.test.js              下行轮询测试
│   ├── polling-loop.test.js          轮询调度器测试
│   ├── p3d-contract.test.js          前后端契约测试
│   ├── p3h-e2e.test.js               端到端测试（需 server 可达）
│   ├── background.test.js            后台测试
│   ├── popup.test.js · popup-m2-p1.test.js  popup UI 测试
│   └── *.test.js                     各渠道 / 场景测试
├── docs/
│   ├── ARCHITECTURE.md               架构总览
│   ├── DEFAULTS.md                   ⭐ 前端默认值单一文档源
│   └── dev/DEVELOPMENT.md            本文档
├── dist/                             esbuild 构建产物（不入仓）
├── release/                          发布产物
├── scripts/
│   ├── build.mjs                     esbuild 多入口打包
│   ├── gen-icons.mjs                 零依赖生成 PNG 图标
│   ├── mock-upstream-url.mjs         上行 URL / 参数 mock 验证
│   └── release.mjs                   本机发布打包
├── manifest.json                     MV3 manifest
├── package.json
├── vite.config.js                    Vitest 配置
├── eslint.config.mjs
├── bridge.md                         设计主文档
├── RELEASE.md                        构建与发布
└── assets/                           logo / 图标
```

### 4.1 关键文件作用

| 文件 | 作用 |
| --- | --- |
| `src/core/constants.js` | **单一源**：端口 / URL / 限速 / 协议 / 安全护栏。**所有模块从这里 import，禁止就地写数字** |
| `src/core/types.js` | `UnifiedMessage` / `UnifiedReply` 数据模型 + `contentHash` + `parseUnifiedReply` |
| `src/core/http-ingest.js` | `postIngest` / `getOutbox` / `ackOutbox` 三个端点客户端 + Token Header 构造 |
| `src/core/uplink.js` | 上行队列：合并窗口 + 持久化 `_confirmed` + 失败重试 |
| `src/core/downlink.js` | 下行轮询 + `_pendingAck` 重试（10 次 / 1s→60s 退避 / 24h TTL）+ SentCache |
| `src/core/rate-limiter.js` | 三层风控：拟人节奏（1.5s 最小间隔 + 800-2600ms 抖动）+ 令牌桶（12/min）+ 会话冷却（3s）+ 60s 去重窗口 |
| `src/core/humanize.js` | 拟人化：贝塞尔鼠标轨迹 + 键入节奏 |
| `src/core/sanitize.js` | XSS 防护：剥离控制字符 + 截断 4KB（与服务端 `maxReplyContentBytes` 对齐） |
| `src/background/index.js` | 后台 service_worker：HTTP 三通道调度 + port 路由 + 配置/状态 |
| `src/content/common.js` | PollingLoop / dispatchOutbound / 解析（content 入口共用） |
| `src/popup/index.js` | popup UI：后端地址配置 / 状态 / 自检 / 健康度面板 |

---

## 五、协议契约（与服务端一一对应）

> **强约束**：扩展端 `PROTOCOL` / `BRIDGE_PROTOCOL_V2` 枚举必须与 `user-server/internal/channelgw` 严格对齐。
> 任何新增/修改必须同步两端 + 文档（`bridge.md` §4 + `docs/DEFAULTS.md` §2.9/2.10）。

```javascript
// src/core/constants.js
export const PROTOCOL = Object.freeze({
  CHANNELS: { DOUYIN: 'douyin', XHS: 'xiaohongshu', TIKTOK: 'tiktok', XIANYU: 'xianyu', KUAISHOU: 'kuaishou' },
  FRAME: {
    REGISTER: 'register',
    INBOUND: 'inbound_message',
    HISTORY: 'history',
    OUTBOUND: 'outbound_reply',
    PONG: 'pong',
    PING: 'ping',
    ACK: 'ack',
    ERROR: 'error',
  },
  SENDER: { CUSTOMER: 'customer', AGENT: 'agent', SELF: 'self', SYSTEM: 'system' },
  DIRECTION: { INBOUND: 'inbound', OUTBOUND: 'outbound' },
});
```

---

## 六、新功能标准流程

### 6.1 新增前端常量（端口/URL/限速）

1. 修改 `src/core/constants.js`，确保新值有**文档源**（DEVELOPMENT.md 或 DEFAULTS.md 链接）
2. 同步 `docs/DEFAULTS.md` 表格
3. 在 `test/constants.test.js` 追加断言（验证字面与文档源一致）
4. 如跨包（如 user-server 端口）：同步 `user-server/internal/config/ports.go`
5. 跑 `npm run test` 验证

### 6.2 新增渠道（如快手私信，已预留）

1. 在 `src/channels/` 新增 `<platform>.js`，实现 DOM 解析与统一消息转换
2. 在 `src/core/channel-adapter.js` 注册新渠道（如未自动注册）
3. 在 `src/content/` 新增 `<platform>.js` content script
4. 修改 `manifest.json`：
   - `content_scripts` 添加 `matches: ["https://*.<platform>.com/*"]` + `js: ["content-<platform>.js"]`
   - `host_permissions` 已有 `http://*/*` / `https://*/*`，无需新增
5. 在 `src/popup/index.html` 添加「打开 <platform>」入口
6. 跑 `npm run test && npm run build` 验证

### 6.3 调整限速/风控参数

1. 修改 `src/core/constants.js` 的 `RATE_LIMIT_DEFAULTS` 数值
2. 同步 `docs/DEFAULTS.md` §2.4 限速风控表
3. 在 `test/rate-limiter.test.js` 验证行为变更
4. 跑 `npm run test` 验证

### 6.4 修改协议字段（强约束）

1. 同步修改 `user-server/internal/channelgw` 与 `src/core/types.js`
2. 同步 `bridge.md` §3 领域模型 + §4 协议
3. 同步 `docs/DEFAULTS.md` §2.9/2.10 协议常量
4. 同时更新 `user-server/internal/bridge/handler_http.go` 解析逻辑
5. 跑 `go test ./internal/bridge/...` + `npm run test` 验证
6. 跑 `p3d-contract.test.js` 验证前后端契约

---

## 七、测试运行

### 7.1 单元测试

```bash
npm run test        # 一次性运行
npm run test:watch  # 监听模式
```

**测试覆盖范围**（`test/` 目录，44 文件 / 674 用例）：

| 测试文件 | 覆盖模块 |
| --- | --- |
| `constants.test.js` | 端口/URL/限速/协议常量与 DEFAULTS.md 字面一致 |
| `protocol.test.js` | FRAME / SENDER / DIRECTION 帧类型 |
| `http-ingest.test.js` | URL 构造 / Header / 重试 / 三通道客户端 |
| `uplink.test.js` | 上行队列（合并窗口 / 持久化 / 重试） |
| `downlink.test.js` | 下行轮询 / `_pendingAck` / SentCache |
| `downlink-pending-ack-p0_9.test.js` | _pendingAck 退避（14 用例） |
| `polling-loop.test.js` | 轮询调度器生命周期 |
| `p3d-contract.test.js` | 前后端契约 |
| `p3h-e2e.test.js` | 端到端（需 server 可达） |
| `adapter.test.js` / `adapter-history.test.js` | 渠道适配器（DOM → UnifiedMessage） |
| `douyin-*.test.js` / `xhs-*.test.js` / `xianyu.test.js` | 各渠道 DOM 解析与发送 |
| `chat-page-convid.test.js` | /chat 路由 + 会话 ID 提取 |
| `rate-limiter.test.js` / `rate-limiter-lru.test.js` | 三层风控 + LRU |
| `circuit-breaker.test.js` / `circuit-breaker-p3.test.js` | 熔断器 + 幂等键 |
| `humanize.test.js` | 拟人化（贝塞尔 + 键入节奏） |
| `sanitize.test.js` | XSS 防护（控制字符 / 4KB 截断） |
| `fallback.test.js` | account_id fallback 派生 |
| `background.test.js` | 后台路由 |
| `popup.test.js` / `popup-m2-p1.test.js` | popup UI 交互 |

---

## 八、构建与发布

### 8.1 本地构建

```bash
npm run build
# 产物：dist/background.js, dist/content-{douyin,xhs,tiktok,xianyu,kuaishou}.js,
#       dist/popup.{html,js}, dist/manifest.json, dist/icons/*.{png}
```

### 8.2 发布打包

```bash
npm run release
# 产物：release/hivebridge-<version>.zip
# 内容：dist/ 全部 + assets/logo.svg + LICENSE
```

### 8.3 Chrome 加载

1. 打开 `chrome://extensions`
2. 开启「开发者模式」
3. 「加载已解压扩展」→ 选择 `dist/` 目录
4. 修改源码后点击「重新加载」即可

### 8.4 加载前必做真机校准

详见 [./../bridge.md §10](../../bridge.md)：

- 抖音：私信列表 / 消息气泡 / 输入框 / 发送按钮真实生效
- 小红书：`.xhs-im-conv-item` / `.chat-item` / `[contenteditable]` 输入框真实生效
- TikTok：DraftEditor 输入框 / 发送动作（Enter vs 飞机按钮）真实生效
- `account_id` 派生稳定
- 受控输入框填值不被框架拦截
- 会话切换时历史回填进入 `user-server`（`history` 帧，不触发 AI）
- 拟人节奏不触发平台风控

---

## 九、调试技巧

### 9.1 popup 无法连接

- **症状**：popup 点击「测试连接」提示失败
- **排查**：
  ```bash
  # 1. 确认 user-server 在 8204 端口运行
  curl http://localhost:8204/health

  # 2. 确认扩展 popup 的 server URL 与 user-server 端口一致
  #    单一源：src/core/constants.js DEFAULT_USER_SERVER.baseUrl
  ```

### 9.2 content script 未注入抖音页

- **症状**：打开 douyin.com 私信页，控制台无 `[bridge]` 日志
- **排查**：
  - `chrome://extensions` → HiveBridge → 检查「已启用」开关
  - `chrome://extensions` → 「检查视图」是否有报错
  - F12 → Console → 输入 `window.__bridgeAdapter` 查看是否注入

### 9.3 三通道轮询异常

- **症状**：后台日志显示下行拉取失败 / 上行无响应
- **排查**：
  - 抓取 `chrome://extensions` → Service Worker → Console 日志
  - 搜索 `[bridge:http]` / `[bridge:downlink]` / `[bridge:uplink]`
  - 确认 `BRIDGE_THREE_CHANNEL.outboxPollIntervalMs` 不被误改

### 9.4 限速拦截

- **症状**：AI 回复没发出去
- **排查**：
  - F12 → Console → 搜索 `[rate-limiter]`，查看被哪个规则拦截
  - 检查 `RATE_LIMIT_DEFAULTS` 参数是否需要调整

### 9.5 强制重置扩展状态

```javascript
// 在 popup F12 Console 执行
chrome.storage.local.clear()
// 然后 chrome://extensions → 「重新加载」
```

---

## 十、跨包对齐清单

> **任何修改以下字段都必须同步两端**（扩展端 ↔ user-server 端）：

| 字段 | 扩展端 | user-server 端 |
| --- | --- | --- |
| user-server 端口 | `constants.DEFAULT_USER_SERVER.port` | `internal/config/ports.go` `DefaultListenPort` |
| 桥接 ingest 端点 | `src/core/http-ingest.js::INGEST_PATH` | `internal/router/router.go` `bridgeWS.POST("/bridge/ingest", ...)` |
| 桥接 outbox 端点 | `src/core/http-ingest.js::OUTBOX_PATH` | `internal/router/router.go` `bridgeWS.GET("/bridge/outbox", ...)` |
| 桥接 ack 端点 | `src/core/http-ingest.js::OUTBOX_PATH/ack` | `internal/router/router.go` `bridgeWS.POST("/bridge/outbox/ack", ...)` |
| 最大回复字节 | `SECURITY.maxReplyContentBytes=4KB` | `internal/bridge/handler_http.go` `maxReplyContentBytes=4KB` |
| 协议帧名 | `PROTOCOL.FRAME.*` | `internal/bridge/frames.go` `Frame*` |
| 协议结构 | `src/core/types.js` | `internal/channelgw`（HTTP/WS 共用单源） |
| 渠道常量 | `PROTOCOL.CHANNELS.*` | `internal/bridge/channel.go` |
| 健康检查路径 | `DEFAULT_USER_SERVER.healthPaths` | `internal/router/router.go` 实际注册 |
| Ack 协议 v2 常量 | `BRIDGE_PROTOCOL_V2` | `internal/model/bridge_protocol.go` `BridgeAckStatus*` |

---

## 十一、相关文档

- [./../bridge.md](../../bridge.md) — 设计主文档（背景 / 架构 / 数据流 / 限速 / 协议）
- [./ARCHITECTURE.md](../ARCHITECTURE.md) — 架构总览
- [./DEFAULTS.md](../DEFAULTS.md) — 前端默认值清单（与 constants.js 字面对齐）
- [./../RELEASE.md](../../RELEASE.md) — 构建与发布
- [../../../user-server/docs/dev/DEVELOPMENT.md](../../../../user-server/docs/dev/DEVELOPMENT.md) — user-server 启动文档

---

最近更新日期: 2026-08-18
