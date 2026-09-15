# ADR-011：嵌入式客服 Chat Widget

| 字段 | 内容 |
|------|------|
| 编号 | ADR-011 |
| 标题 | 嵌入式客服 Chat Widget |
| 状态 | ✅ Accepted |
| 决策者 | @frontend-team, @maintainer-team |
| 日期 | 2026-Q2 |
| 适用范围 | 嵌入式客服 Chat Widget 设计 |
| **实施 PR** | #218（embed-sdk v1）、#256（v1.1 优化）、#289（七牛直传）|
| **已部署环境** | dev / staging / 私有部署客户（生产 v3.12.0+，详见各客户 CHANGELOG）|

---

## 一、背景

企业客户部署 HiveMtk 用户端后，希望在自己的官方网站（`www.example.com`）右下角添加一个**客服浮标入口**，访客点击后弹出聊天窗，与 HiveMtk 的 AI 客服/人工坐席对话。

**关键约束**：

1. **私域部署基线**：每个企业独立一套系统（user-server + PostgreSQL + 推理栈），数据完全本地化（无 `merchant_id`、无 SaaS）
2. **零依赖集成**：客户官网可能用任何技术栈（Vue/React/jQuery/原生 HTML），SDK 必须是纯 JS 单文件嵌入
3. **样式隔离**：浮标不能影响官网原有样式，聊天窗不能被官网 CSS 污染
4. **跨域兼容**：企业官网域名（`www.example.com`）与 user-server 域名（`chat.example.com`）往往不同

---

## 二、决策

### 2.1 部署模式：iframe 隔离

采用 **iframe 加载聊天窗**，而不是在父页面渲染组件：

- 样式完全隔离（iframe 内部 CSS 不会污染父页）
- 跨域天然支持（只要 iframe 能加载）
- 与父页 JS 解耦（不冲突）
- 缺点：SEO 较差、消息通信稍复杂（用 postMessage）

**结论**：权衡利弊，**采用 iframe**，放弃 SPA 嵌入式组件方案。

### 2.2 AppKey 机制：软解析（可选）

最初设计 AppKey 是**强制鉴权凭证**（类似 SaaS 多租户），私域部署后改为**软解析**：

- AppKey/ChannelID **不强制必填**
- 缺失时后端走 `default` 默认 channel
- 中间件改为 `AppKeyResolve`（软解析 + 缺失放行）
- AppKey 退化为**渠道标识符**（用于日志追踪 + 未来多渠道管理）

**结论**：AppKey 软解析，符合"私域部署 = 自家用户"的定位。

### 2.3 WebSocket：无鉴权

私域部署下，WebSocket 通道全部是企业自己的网站访客：

- `/api/ws/visitor` 端点**无强制鉴权**
- 仅靠 `session_id` + `visitor_id`（localStorage 生成的 UUID）标识访客
- 风控靠**限流**（30 条/分钟/IP）+ 关键词过滤

**结论**：WebSocket 无鉴权，简化集成。

### 2.4 UI：极简白底

- 主色：`#1989fa` 蓝色（可配置）
- 浮标：圆形 56×56 px，纯白图标
- 聊天窗：380×560 px（移动端全屏）
- 不使用阴影/渐变/动画装饰
- 响应式：≤ 480px 视口自动全屏

**结论**：UI 极简，不喧宾夺主。

### 2.5 附件：访客直传七牛

- 后端仅下发上传凭证（POST `/api/chat/public/upload-token`）
- 访客浏览器**直传七牛 CDN**，文件不经过 user-server
- 文件类型白名单：image / file / audio / video
- 单文件 ≤ 20MB
- Token 有效期：1 小时

**结论**：直传 CDN，user-server 不背带宽。

### 2.6 转人工：NLP 关键词自动触发

- 前端**不暴露**"转人工"按钮（永远显示"在线客服"，避免用户焦虑）
- 后端基于关键词自动触发：
  - 中文：`人工`、`真人`、`转人工`、`客服`、`找人`
  - 英文：`human`、`operator`、`agent`、`real person`
- 命中后**跳过 AI 推理**，直接走坐席分配
- 兜底：连续 3 次 AI 答非所问也触发

**结论**：关键词触发，前端零感知。

---

## 三、架构

```
┌──────────────────────────────────────────────────┐
│          企业官网（如 www.example.com）              │
│                                                  │
│  <script src="https://chat.example.com/embed/     │
│              marketing-chat-widget.iife.js"        │
│          data-api-base-url="https://chat.example.com"│
│          data-app-key="optional">                 │
│  </script>                                        │
│                                                  │
│       ┌──────────────────────┐                    │
│       │  浮标 (position:fixed) │                  │
│       │  + iframe 聊天窗     │                     │
│       └──────────┬───────────┘                    │
└──────────────────┼────────────────────────────────┘
                   │ HTTPS / WSS
                   ▼
   ┌──────────────────────────────────┐
   │  user-server (HiveMtk 用户端)    │
   │  ────────────────────────────── │
   │  REST: /api/chat/public/*        │
   │  REST (长轮询): /api/v1/ai/chat/poll │
   │  Static: /embed/*.js + /chat/embed/*(SPA)│
   │  ────────────────────────────── │
   │  + AI (RAG + LLM)               │
   │  + 转人工 (NLP 关键词)            │
   │  + 七牛附件直传                  │
   └──────────────────────────────────┘
```

> 注：实时通道已从 WebSocket 迁移为 HTTP 长轮询（参见 `operations/AI_AGENT_PERF_API.md`）。

---

## 四、关键设计取舍

### 4.1 为什么用 IIFE 而不是 ESM

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| IIFE `<script>` | 零依赖、跨域兼容、任何技术栈 | 污染 window | ✅ 采用 |
| ESM `<script type="module">` | 现代、tree-shaking | 老浏览器不支持、必须 import | 备选 |
| npm 包 | 类型友好 | 强制依赖 npm/构建工具 | 备选 |

**实现**：Vite 库模式同时输出 IIFE + ESM 双产物，通过 `package.json` 的 `unpkg` / `jsdelivr` 字段指明 CDN 入口。

### 4.2 为什么用 iframe 而不是 Web Component

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| iframe | 样式完全隔离、跨域、成熟 | SEO 弱、消息复杂 | ✅ 采用 |
| Web Component | 真正的 DOM 嵌入 | Shadow DOM 调试难、跨域受限 | 备选 |
| 浮动 div + portal | 灵活 | 与父页 CSS 冲突风险高 | 否决 |

### 4.3 为什么 AppKey 软解析

私域部署下，每个企业只有一套系统：

- 强鉴权 → 需要管理 AppKey 生命周期 → 增加复杂度
- 软解析 → 直接用 `default` 渠道 → 极简集成
- 未来多渠道（同一企业多个客服号）再升级为强鉴权

### 4.4 为什么访客直传七牛

- user-server 不背带宽（不消耗出口流量）
- 七牛 CDN 全球加速
- 减少 user-server CPU/内存
- 缺点：必须依赖七牛（可用 MinIO/OSS 自建替代）

---

## 五、数据流

### 5.1 访客发起对话

```
访客点击浮标
  → SDK 创建 iframe (src=/chat/embed/default)
  → iframe 加载 user-web SPA
  → SPA 调用 POST /api/chat/public/sessions {app_key, visitor_meta}
  → 后端创建 customer_session,返回 session_id
  → SPA 渲染聊天界面
  → 建立 WS 连接 /api/ws/visitor?session_id=xxx
```

### 5.2 访客发消息

```
访客输入文本
  → SPA POST /api/chat/public/sessions/:id/messages
  → 后端入库 + 触发 RAG 检索
  → AI 推理（宿主机 llama-server :8207/:8208/:8209）
  → 通过长轮询推回前端
  → SPA 渲染消息
  → 若命中转人工关键词:跳过 AI,走坐席分配
```

### 5.3 父子通信

```
iframe → parent (postMessage)
  - {type:'mcw-unread', count:3}        # 未读消息数,驱动浮标红点
  - {type:'chat-widget-close'}         # 关闭按钮被点击,父页关闭 iframe

parent → iframe (postMessage)
  - {type:'mcw-config', payload:{...}} # 启动时注入配置
```

**origin 校验**：父端严格校验 `event.origin === apiBaseURL.origin`；iframe 端用具体 origin 发送，不用 `'*'`。

---

## 六、跨域策略

### 6.1 同源部署（最简）

企业官网与 user-server 同源（如 `chat.example.com` 既是官网子域也是 user-server 域名）：

```html
<script src="/embed/marketing-chat-widget.iife.js"></script>
```

无 CORS 问题，postMessage origin 自动一致。

### 6.2 跨域部署（常见）

企业官网在 `www.example.com`，user-server 在 `chat.example.com`：

```html
<script src="https://chat.example.com/embed/marketing-chat-widget.iife.js"
        data-api-base-url="https://chat.example.com">
</script>
```

需要：
- user-server 的 `CORS_ALLOW_ORIGINS_USER` 放行 `https://www.example.com`
- SDK 自动推断 API base URL 为 `data-api-base-url` 指定的 origin
- postMessage origin 严格校验，避免 XSS 攻击

### 6.3 CDN 部署

静态资源走 CDN（`cdn.example.com`），API 仍走 user-server：

```html
<script src="https://cdn.example.com/embed/marketing-chat-widget.iife.js"
        data-api-base-url="https://chat.example.com">
</script>
```

属于跨域场景，注意事项同 6.2。

### 6.4 FRP 穿透（NAT 内网）

user-server 在内网，云端 frps/frpc 暴露公网域名 `chat.example.com`：

```
访客 → chat.example.com(云端)→ frp 隧道 → 本地 user-server:8204
```

详见 [FRP私域部署指南.md](../FRP私域部署指南.md)。

---

## 七、SDK API（公开方法）

```javascript
// 启动：自动执行（解析 script 标签 + 注入浮标）
<script src="..."></script>

// 编程式控制
window.mcwInstance.open()      // 打开聊天窗
window.mcwInstance.close()     // 关闭
window.mcwInstance.destroy()   // 销毁（移除 DOM + 清理监听）

// 暴露类（高级用户）
window.MarketingChatWidget     // 构造函数
```

---

## 八、配置项总览

| 字段 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `appKey` | ❌ | - | 渠道 AppKey |
| `channelId` | ❌ | - | channel_id（与 appKey 二选一） |
| `apiBaseURL` | ❌ | 同源 | API 基础 URL |
| `position` | ❌ | `bottom-right` | 浮标位置 |
| `color` | ❌ | `#1989fa` | 浮标主色 |
| `title` | ❌ | `在线客服` | 聊天窗标题 |
| `zIndex` | ❌ | `9999` | 浮标层级 |
| `offsetX` | ❌ | `24` | 水平边距 |
| `offsetY` | ❌ | `24` | 垂直边距 |
| `width` | ❌ | `380` | 聊天窗宽度 |
| `height` | ❌ | `560` | 聊天窗高度 |

**优先级**：`data-*` 属性 > `window.MarketingChatWidgetConfig` > query 参数 > 内置默认值

---

## 九、风险与边界

### 9.1 已知风险

| 风险 | 影响 | 缓解 |
|------|------|------|
| iframe 加载慢 | 首屏延迟 | SDK 显示"加载中"占位 + 异步加载 |
| WebSocket 断线 | 消息丢失 | 客户端 localStorage 缓存 + 重连补发 |
| CORS 配置错误 | 接口 403 | 部署检查脚本 + 文档强提示 |
| 跨域 postMessage 被拒 | 无法通信 | 严格 origin 校验 + 文档说明 |
| FRP 隧道掉线 | 客服不可用 | frpc 自动重连 + 健康检查 + 告警 |

### 9.2 边界

- **不做**：OAuth/SSO 集成（私域部署 = 自家用户）
- **不做**：多语言切换（i18n 由 user-web 负责）
- **不做**：浮标图标定制（避免品牌冲突）
- **不做**：移动端原生 SDK（Web 优先）

---

## 十、迁移路径

### 10.1 未来升级点

- **多渠道管理**：每个企业可创建多个 channel（不同客服号），走强鉴权
- **Web Component 化**：作为 iframe 之外的备选方案
- **访客身份透传**：通过 `data-visitor-id` 关联企业 CRM 用户
- **事件埋点 SDK**：浮标点击 / 聊天窗打开 / 转人工等事件标准化回调

### 10.2 兼容策略

- SDK 主版本升级必须保持 `window.mcwInstance` / `window.MarketingChatWidget` API 兼容
- 新字段一律可选，默认值向前兼容
- 弃用字段提前 3 个 minor 版本 warning

---

## 十一、相关文档

- [CHAT_WIDGET_EMBED.md](../../operations/CHAT_WIDGET_EMBED.md) - 嵌入集成指南
- [FRP私域部署指南.md](../FRP私域部署指南.md) - FRP 穿透完整指南
- [embed-sdk/README.md](../../../embed-sdk/README.md) - SDK 完整说明
- [部署方案_用户端.md](../部署方案_用户端.md) - 整体部署架构
- `RAG_AUTO_REPLY_UNIFIED_ARCHITECTURE.md`（未编写） - AI 自动回复架构

## 修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-Q2 | @frontend-team | 初版 Chat Widget 架构 |
| v1.1 | 2026-08-16 | audit-agent | 增补"实施 PR"和"已部署环境"字段 |
