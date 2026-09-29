# embed-sdk · 客服 Web Widget 嵌入 SDK

> marketing-tools-kit 客服系统的 Web Widget 嵌入 SDK（ADR-011）。一行 `<script>` 标签即可在任意站点接入在线客服，支持浮标、聊天窗、AppKey 渠道区分、自动重连等。

## ✨ 项目功能

- **零依赖一行集成**：单 `<script>` 标签嵌入，无 npm 依赖
- **浮动按钮（Floating Button）**：右下/左下角圆形浮标，可自定义颜色 / 偏移 / 层级
- **嵌入式聊天窗（Iframe Panel）**：以 iframe 方式加载 user-web 的 `/chat/embed/:channel_ref`，样式 / 状态与浮标完全隔离
- **多渠道 AppKey 鉴权**：通过 `data-app-key` 区分不同渠道（私域部署可省略，自动用 `default`）
- **同源/跨源 API 寻址**：`data-api-base-url` 支持对接独立 API 域名
- **编程式控制**：`window.mcwInstance.open() / close() / destroy()`
- **可观测埋点**：浮标点击、聊天窗开关等事件 Console 输出
- **极简白底视觉**：不喧宾夺主，与任意站点风格融合
- **轻量级**：IIFE 构建产物 < 30KB（gzip）

## 🧱 技术栈

| 维度 | 选型 |
|---|---|
| 语言 | 原生 JavaScript（ES2018） |
| 构建 | Vite 5（库模式：IIFE + ESM 双产物） |
| 样式 | 原生 CSS（`src/styles.css`） |
| 聊天窗加载 | iframe（同源或 `data-api-base-url` 跨源） |
| 协议 | WebSocket（无鉴权，私域部署模式） |
| 自动重连 | 内建断线重连 + 离线消息补发 |

## 📁 目录结构

```
embed-sdk/
├── src/
│   ├── config.js              # 配置解析（data-* 属性 + window 全局变量）
│   ├── floating-button.js     # 浮标组件
│   ├── iframe-panel.js        # 聊天窗 iframe 容器
│   ├── widget.js              # 入口（MarketingChatWidget 类）
│   └── styles.css             # 全局样式
├── demo.html                  # 本地开发预览页
├── vite.config.js             # Vite 库模式配置
└── package.json
```

## 🚀 启动说明

### 1. 一行脚本集成（最简）

将构建产物上传 CDN 后，在目标网站 `</body>` 之前插入：

```html
<script
  src="https://your-cdn.com/marketing-chat-widget.iife.js"
  data-color="#1989fa"
></script>
```

刷新页面即可看到右下角浮标。**私域部署场景下无需 `data-app-key`**，自动用 `default` 渠道。

### 2. 多渠道 AppKey 集成

```html
<script
  src="https://your-cdn.com/marketing-chat-widget.iife.js"
  data-app-key="ak_xxxxxxxxxxxxxxxxxxxxxxxx"
  data-color="#1989fa"
  data-title="在线客服"
  data-position="bottom-right"
></script>
```

### 3. 编程式控制

```javascript
// 打开聊天窗
window.mcwInstance.open()

// 关闭聊天窗
window.mcwInstance.close()

// 完全销毁
window.mcwInstance.destroy()
```

### 4. 全局变量配置

```html
<script>
  window.MarketingChatWidgetConfig = {
    appKey: 'ak_xxx',
    apiBaseURL: 'https://api.example.com',
    color: '#ff6b35',
    position: 'bottom-left'
  }
</script>
<script src="https://your-cdn.com/marketing-chat-widget.iife.js"></script>
```

### 5. 本地开发

```bash
cd embed-sdk
npm install
npm run dev
# 启动 Vite dev server，自动打开 http://localhost:8214/demo.html
```

### 6. 构建生产产物

```bash
npm run build
# 产物输出到 dist/
#   marketing-chat-widget.iife.js  （<script> 标签引入，UMD 全局）
#   marketing-chat-widget.esm.js   （npm 安装或 import）
```

### 7. 预览构建

```bash
npm run preview
```

## ⚙️ 配置参数

### `data-*` 属性 / `window.MarketingChatWidgetConfig` 字段

| 字段 | 必填 | 默认值 | 说明 |
|---|---|---|---|
| `appKey` / `data-app-key` | ❌ | - | 渠道 AppKey（B 端"客服渠道"管理获取）；**私域部署可省略**，省略时自动用 `default` 渠道（ADR-011：AppKey 软解析） |
| `channelId` / `data-channel-id` | ❌ | - | 直接指定 channel_id（与 appKey 二选一） |
| `apiBaseURL` / `data-api-base-url` | ❌ | 同源 | API 基础 URL，如 `https://api.example.com` |
| `position` / `data-position` | ❌ | `bottom-right` | `bottom-right` \| `bottom-left` |
| `color` / `data-color` | ❌ | `#1989fa` | 浮标主色（hex） |
| `title` / `data-title` | ❌ | `在线客服` | 聊天窗标题 |
| `welcome` / `data-welcome` | ❌ | `您好,请问有什么可以帮您?` | 欢迎语（透传到 iframe） |
| `lang` / `data-lang` | ❌ | `zh-CN` | `zh-CN` \| `en-US` |
| `visitorIdKey` / `data-visitor-id-key` | ❌ | `mtk_visitor_id` | localStorage 中访客 UUID 的 key |
| `zIndex` / `data-z-index` | ❌ | `9999` | 浮标层级 |
| `offsetX` / `data-offset-x` | ❌ | `24` | 水平边距（px） |
| `offsetY` / `data-offset-y` | ❌ | `24` | 垂直边距（px） |
| `width` / `data-width` | ❌ | `380` | 聊天窗宽度（px），移动端自动全屏 |
| `height` / `data-height` | ❌ | `560` | 聊天窗高度（px），移动端自动全屏 |
| `allowedOrigins` | ❌ | 自动推导 | postMessage origin 白名单；留空则自动 = [apiBaseURL, window.location.origin] |
| `events` | ❌ | `{}` | 事件回调集合（onReady/onOpen/onClose/onUnread/onMessage） |

### 解析优先级

`window.MarketingChatWidgetConfig` 全局变量 > query 参数 > `data-*` 属性 > 内置默认值

## 🎨 浮标视觉

- 极简白底圆形按钮（56×56 px）
- 客服气泡 SVG 图标
- 悬浮轻微阴影变化
- 完全可定制颜色 / 位置

## 🌐 聊天窗加载

聊天窗以 iframe 形式加载 user-web 的路由：

```
${apiBaseURL}/chat/embed/{channel_ref}#/chat/embed/{channel_ref}
```

- `channel_ref` 优先用 `channelId`，其次 `appKey`，最后 `default`
- iframe 模式保证浮标与宿主页面样式完全隔离
- 跨域情况下通过 `postMessage` 传递消息（保留扩展点）

## 🔌 与后端通信

| 接口 | 用途 |
|---|---|
| `POST /api/chat/public/sessions` | 创建会话（AppKey 鉴权） |
| `POST /api/chat/public/sessions/{id}/messages` | 发送消息 |
| `GET /api/chat/public/sessions/{id}/messages` | 拉取历史消息 |
| `WS /api/ws/visitor` | 实时消息推送（私域部署无鉴权） |

## 🛠 常用命令

```bash
npm run dev        # 启动开发服务器
npm run build      # 构建生产产物
npm run preview    # 预览构建产物
```

## 📦 集成到 user-web 构建

`user-web/public/` 会将构建产物 `marketing-chat-widget.iife.js` 作为静态资源托管：

```bash
# 在仓库根 hivemtk/ 下
make sdk-build     # 等价于 cd embed-sdk && npm install && npm run build
```

## 📚 关联文档

- 仓库根 [README](../README.md)
- user-web 嵌入路由 [../user-web/src/views/chat/embed/Index.vue](../user-web/src/views/chat/embed/Index.vue)
- 用户端后端 [../user-server](../user-server/)
- 部署详细文档 [../docs/operations/CHAT_WIDGET_EMBED.md](../docs/operations/CHAT_WIDGET_EMBED.md)
- 部署方案 [../docs/architecture/部署方案_用户端.md](../docs/architecture/部署方案_用户端.md)

## 📄 许可证

本项目采用 [GNU Affero General Public License v3.0（AGPL-3.0）](../LICENSE) 发布,与用户端主仓库同协议。详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。
