# Bridge 默认值清单（DEFAULTS）

> 本文档是 `user-web/bridge` 全部默认值的**唯一文档源**。
> 代码侧单一源：`user-web/bridge/src/core/constants.js`（前端）+ `user-server/internal/bridge/handler_http.go`（后端）。
> 任何调整必须 **同步** 三处：本文档 + 代码常量 + 单元测试。

## 1. 设计原则（项目硬约束）

1. **不允许"软启动"**——任何默认值都必须能从文档溯源。
2. **不允许在多个位置重复硬编码同一个值**——所有常量集中在 `constants.js` / `handler_http.go`。
3. **不允许"兜底成空值"**——若 config 缺失，明确报错而非静默回退到默认值。
4. **新默认值必须加测试**——`test/constants.test.js` 验证与本清单字面一致。

## 2. 默认值清单

### 2.1 服务端连接（user-server）

| 字段 | 默认值 | 文档源 |
| --- | --- | --- |
| `host` | `localhost` | `user-server/docs/dev/DEVELOPMENT.md` §2.4 端口对照表（dev 本机） |
| `port` | `8204` | `user-server/cmd/api/main.go DefaultListenPort=8204` |
| `baseUrl` | `http://localhost:8204` | 上述两者组合 |
| `healthPaths` | `['/health', '/healthz', '/readyz', '/api/health']` | `user-server/internal/router/router.go` 实际注册顺序（优先 /health 含依赖检查） |
| `profile` | `dev` | `user-server/config.yaml inference.profile: dev` |

> bridge ↔ user-server 走 **HTTP 三通道**（非 WS、非 SSE）。无 `wsPath` 字段。

### 2.2 端口兜底（cmd/api/main.go）

| 常量 | 默认值 | 文档源 |
| --- | --- | --- |
| `DefaultListenPort` | `"8204"` | `user-server/docs/dev/DEVELOPMENT.md` §2.4 端口对照表 |
| `DefaultRedisPort` | `"8203"` | 同上 |

### 2.3 HTTP 桥接端点（user-server/internal/router/router.go）

| 端点 | 方法 | 中间件 | 用途 |
| --- | --- | --- | --- |
| `/api/bridge/ingest` | POST | InitGuard | 上行消息（inbound + history） + 拉取同会话待发 reply |
| `/api/bridge/outbox` | GET | InitGuard | 下行轮询：拉取待发 AI 回复 |
| `/api/bridge/outbox/ack` | POST | InitGuard | 标记 msg_id 为 `delivered` / `failed` |

约束：

- `account_id` 缺失 → 400 `account_id required`（不写 `default` 兜底）
- `channel` 不在白名单（`douyin/xiaohongshu/tiktok/xianyu/kuaishou`）→ 400 `unsupported`
- body 上限 `HTTPIngestMaxBodySize = 4MB`
- 单批消息上限 `HTTPIngestMaxMessages = 200`
- Token 走 `X-Bridge-Token` Header，除 SSE 外**禁止 URL query**
  （SSE 用 `?bridge_token=`，服务端校验后剥离并脱敏日志）

### 2.4 限速 / 风控（防封号，详见 bridge.md §7）

| 字段 | 默认值 | 设计动机 |
| --- | --- | --- |
| `accountCapacity` | 12 | 单账号每分钟 Token 桶容量（IM 风控安全阈值） |
| `accountRefillPerMin` | 12 | 单账号每分钟补充速率 |
| `minIntervalMs` | 1500 | 任意两次下行最小间隔（拟人节奏） |
| `jitterMinMs` | 800 | 发送前随机延迟下限 |
| `jitterMaxMs` | 2600 | 发送前随机延迟上限 |
| `conversationCooldownMs` | 3000 | 同会话两次回复冷却 |
| `conversationPerHour` | 40 | 同会话每小时最多 40 条回复 |
| `dedupWindowMs` | 60000 | 相同文案 60s 去重 |

- 扩展端：12/min（更严格）
- 服务端兜底（reach_adapter.go）：60/min——仅防 AI 失控洪泛

### 2.5 三通道调度参数（`BRIDGE_THREE_CHANNEL`）

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `uplinkMergeWindowMs` | 350 | 上行合并窗口（毫秒） |
| `uplinkMaxBatch` | 20 | 上行单批最大消息数 |
| `outboxPollIntervalMs` | 1500 | 下行轮询间隔 |
| `outboxBatchSize` | 50 | 下行单批最大消息数 |
| `ackFlushIntervalMs` | 500 | ack 批量 flush 间隔 |
| `sentCacheMax` | 2000 | 本地已发缓存容量（超界按最旧时间戳淘汰） |
| `sentCacheTtlMs` | 86400000 | 本地已发缓存时间界：界内命中不重发、界外视为服务端不再重投（批20d-A4）。取值必须 ≥ 服务端重推窗 |
| `sendOutboundTimeoutMs` | 20000 | 下行 send 超时 |

### 2.6 巡检制度（patrol，content script 主动枚举左侧列表）

| 字段 | 默认值 | 文档源 |
| --- | --- | --- |
| `intervalMs` | 30000 | 轮间隔基线 |
| `jitterMs` | 30000 | 抖动窗口（30-60s） |
| `waitActiveMs` | 5000 | 等 DOM 活跃超时 |
| `throttleMs` | 1500 | 节流 |
| `switchMinMs` | 3000 | 会话间切换最小间隔 |
| `switchMaxMs` | 5000 | 会话间切换最大间隔 |
| `maxPerRound` | 6 | 单轮最多访问 6 个会话 |
| `conversationCooldownMs` | 120000 | 同一会话 120s 冷却 |
| `maxPasses` | 8 | 巡检最大轮数 |
| `maxBatchPerPatrol` | 80 | 单轮最多抓取条数 |
| `firstRunMaxBatch` | 20 | 首轮上限 |
| `firstRunWindowMs` | 60000 | 首轮时间窗 |

### 2.7 历史回填宽限期（`HISTORY_GRACE_MS`，per-channel）

| 渠道 | 宽限 | 说明 |
| --- | --- | --- |
| `douyin` | 5000 | 聊天页结构稳定 |
| `xiaohongshu` | 2000 | React 受控组件，渲染最快 |
| `tiktok` | 6000 | 海外链路 + 重 SPA |
| `xianyu` | 4000 | goofish.com IM |
| `kuaishou` | 5000 | 预留 |
| `default` | 8000 | 保守上限 |

### 2.8 UI / 测试

| 字段 | 默认值 | 说明 |
| --- | --- | --- |
| `healthCheckTimeoutMs` | 3500 | popup「测试连接」单次 fetch 超时 |
| `metaReportIntervalMs` | 5000 | content script 周期性上报当前 meta |
| `popupHealthPanelPollMs` | 5000 | popup 健康度面板轮询 |
| `popupAlertPollMs` | 10000 | popup 告警轮询 |

### 2.9 协议常量（`PROTOCOL`，与 channelgw 严格对齐）

| 字段 | 值 |
| --- | --- |
| 渠道常量 | `douyin` / `xiaohongshu` / `tiktok` / `xianyu` / `kuaishou`（无 `_web` 后缀） |
| 协议帧名 | `register` / `inbound_message` / `history` / `outbound_reply` / `pong` / `ping` / `ack` / `error` |
| 发送方类型 | `customer` / `agent` / `self` / `system` |
| 消息方向 | `inbound` / `outbound`（history 帧专用） |

### 2.10 Ack 协议 v2 常量（`BRIDGE_PROTOCOL_V2`）

| 字段 | 值 |
| --- | --- |
| `VERSION` | 2 |
| 请求字段 | `v` / `items` / `msg_ids` / `status` / `conversation_id` / `error` / `msg_id` |
| 终态 | `delivered` / `failed` |
| 响应状态 | `acked` / `failed` / `duplicate` / `not_found` / `not_in_scope` |

### 2.11 平台入口（仅 popup 快捷按钮）

| 渠道 | URL |
| --- | --- |
| 抖音 | `https://www.douyin.com/chat` |
| 小红书 | `https://www.xiaohongshu.com/chat` |
| TikTok | `https://www.tiktok.com/messages` |
| 闲鱼 | `https://www.goofish.com/im` |
| 快手 | `https://www.kuaishou.com/new-reco` |

### 2.12 安全护栏

| 字段 | 默认值 | 文档源 |
| --- | --- | --- |
| `maxReplyContentBytes` | 4096 (4KB) | `handler_http.go:43` + 前端 `SECURITY` 同值（避免服务端截断后用户看到残缺） |
| `logMaskMaxChars` | 24 | 超过此长度的字符串在 console 截断（防 PII 泄露） |

### 2.13 内容 hash（`contentHash`，回环去重钩子2）

```js
// 通道+内容（trim）→ FNV-1a 32-bit + 'mh:' 前缀
contentHash(channel, content) = 'mh:' + fnv1a(`${channel}|${content.trim()}`).hex(8)
```

- 同一文本在不同会话哈希一致（不含 `conversationID`）→ 服务端 `GetByMsgID` 可跨会话去重 patrol 回声；
- 必须用 UTF-8 字节哈希（与 Go `[]byte(s)` 对齐），不可用 JS `charCodeAt` 直接哈希多字节字符；
- 算法必须前后端逐字节一致，否则回环防护断裂。

## 3. 调整流程

1. **改一处**：`constants.js` 或 `handler_http.go` / `http_reply_buffer.go` / `cmd/api/main.go` 的常量
2. **改两处**：本文档（数值 + 文档源）
3. **改三处**：相关单元测试（`test/constants.test.js` / `internal/bridge/*_test.go`）
4. **跑测试**：
   ```bash
   cd user-web/bridge && npx vitest run
   cd user-server && go test -race -count=1 ./internal/bridge/...
   ```

## 4. 反模式（禁止）

| 反模式 | 后果 | 正确做法 |
| --- | --- | --- |
| `serverUrl || 'http://localhost:8204'` | 静默兜底，掩盖配置错误 | 缺失时明确报错 |
| 多处硬编码 `1024` | 改一处忘另一处 | 全部从 const 取 |
| 文档和代码不一致 | 排查时无法信任文档 | 测试验证一致性 |
| 加默认值不写文档源 | 后续维护者无法调整 | 每个常量都附"文档源"注释 |
| HTML 中写死端口/URL（如 `placeholder="http://localhost:8204"`） | 改 constants.js 后 UI 不变 | 由 JS 加载时从 constants 设置 placeholder |
| `setInterval(fn, 5000)` 直接写 5000 | 改 constants.js 后行为不变 | 从 `UI_DEFAULTS.metaReportIntervalMs` 取 |
| Token 走 URL query | devtools / 浏览器历史明文泄漏 | 走 `X-Bridge-Token` Header（SSE 例外：`?bridge_token=`，服务端剥离 + 脱敏） |

## 5. 测试矩阵

| 测试文件 | 验证 |
| --- | --- |
| `test/constants.test.js` | 所有前端常量与本文档字面一致 |
| `test/protocol.test.js` | 协议帧名/字段名 |
| `test/http-ingest.test.js` | URL 构造 / Header / 重试 |
| `internal/bridge/handler_http_p0_test.go` | 三个端点 400 / 200 行为 |
| `internal/bridge/handler_http_p3d_test.go` | ack 详细状态码 / 跨账号探测 |
| `internal/bridge/handler_http_ack_test.go` | ack 终态 delivered / failed |
| `internal/bridge/handler_http_metrics_test.go` | 指标埋点 |
| `internal/bridge/http_reply_buffer_test.go` | 容量 / 超时 / 拉取匹配 |
| `internal/bridge/reach_adapter_test.go` | bridge 渠道入缓冲 / 非 bridge 委托 |
| `internal/bridge/channel_test.go` | 渠道常量 |
| `internal/bridge/defaults_test.go` | 默认值字面对齐 |

## 6. 同步检查（grep audit）

```bash
# 扩展端：禁止在 src/ 出现裸端口字面量（除 constants.js 注释外）
cd hivemtk/user-web/bridge
grep -rn "8204\|8207\|8208\|8209\|8232" src/ --include="*.js" | grep -v "constants.js"

# 服务端：禁止在非 ports.go / config 出现裸端口字面量
cd hivemtk/user-server
grep -rn '"8204"\|"8203"\|"8205"\|"8206"\|"8207"\|"8208"\|"8209"\|"8232"\|8202' cmd/ internal/ --include="*.go" \
  | grep -v "ports.go" \
  | grep -v "inference_load_test.go" \
  | grep -v "main.go" \
  | grep -v "//.*端口" \
  | grep -v "DEVELOPMENT.md" \
  | grep -v "testdb.go"     # 测试默认值，与 CI 一致
# 应只返回端口字面量在 ports.go / 注释 / 文档字符串
```

## 7. 软启动禁用清单

下列行为是典型"软启动"，**全部禁止**：

- `serverUrl || 'http://localhost:8204'` 兜底
- `account_id || 'default'` 兜底
- Token 走 URL query（devtools / 浏览器历史明文泄漏）
- `setInterval(fn, 5000)` 硬编码 5000（应从 `UI_DEFAULTS` 取）
- `placeholder="http://localhost:8204"` 硬编码（应从 constants 动态设置）
- 多处硬编码同一数字（如 `4*1024`）
