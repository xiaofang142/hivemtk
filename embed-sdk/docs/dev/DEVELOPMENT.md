# embed-sdk 代码开发手册

> **规则级别**: ⭐⭐ 项目级开发文档

> 本手册面向 embed-sdk 的二次开发者，覆盖环境准备、命令、目录导航、集成方式、新功能流程、测试、构建发布与调试技巧。
> 关联文档：[../../README.md](../../README.md)、[./ARCHITECTURE.md](./ARCHITECTURE.md)、[./CONVENTIONS.md](./CONVENTIONS.md)、[./FEATURES.md](./FEATURES.md)、[../../../docs/operations/CHAT_WIDGET_EMBED.md](../../../docs/operations/CHAT_WIDGET_EMBED.md)

---

## 一、环境准备

| 依赖 | 版本 | 说明 |
|---|---|---|
| Node.js | ≥ 18（推荐 20 LTS 或 22+） | 22+ 内置 `WebSocket`，可直接运行 `test/ws.test.mjs` |
| npm | ≥ 9 | 跟随 Node |
| Vite | `^5.0.0`（devDependency） | 仅开发期依赖，不进入产物 |
| 浏览器 | 现代 Chrome / Firefox / Safari | 调试 demo.html 使用 |

初始化：

```bash
cd /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/embed-sdk
npm install
```

`package.json` 仅有 `vite` 一个 `devDependencies`，无任何运行时依赖，保证 SDK 产物纯净。

---

## 二、启动命令

| 命令 | 作用 |
|---|---|
| `npm run dev` | 启动 Vite dev server，自动打开 `http://localhost:8214/demo.html` |
| `npm run build` | Vite 库模式构建，输出 `dist/marketing-chat-widget.{iife,esm}.js` |
| `npm run preview` | 预览构建产物 |
| `npm run test` | 运行 `test/unit.test.mjs` 单元测试（需先 `npm run build`） |
| `npm run test:boundary` | 运行 `test/boundary.test.mjs` 边界测试 |
| `npm run test:ws` | 运行 `test/ws.test.mjs`，需 user-server 监听 8204 端口 |
| `npm run test:all` | 串联执行 `unit` + `boundary` |
| `npm run test:e2e` | 运行 `bash test/e2e.sh`，端到端验证（user-server + SDK + CORS + WS） |

### 2.1 端口对照表

> **核心约束**：`src/config.js` 的 `DEFAULTS.apiBaseURL` = **`script 同源` 或 `window.location.origin`**（不写死端口）
> 但默认行为上**通过 user-server 提供 demo**（见 `vite.config.js` 的 `server.port=8214`），dev 模式下 demo.html 的 `{apiBaseUrl}` 会被自动替换为 `http://localhost:8204`（user-server 端口）

| 端口 | 服务 / 应用 | 启动入口 | 单一源 | 文档源 |
| --- | --- | --- | --- | --- |
| **8214** | **embed-sdk Vite dev**（demo.html 托管） | `npm run dev` | `vite.config.js server.port=8214` | `vite.config.js:25` |
| **8204** | **user-server**（demo.html 实际请求目标） | `cd ../user-server && go run ./cmd/api` | user-server `config.DefaultListenPort` | user-server/docs/dev/DEVELOPMENT.md §2.4 |
| 8207 | LLM（user-server 本地推理） | user-server `make inference-host-up` | user-server `config.DefaultLLMPort` | user-server docs §2.4 |
| 8208 | Embedding | user-server `make inference-host-up` | user-server `config.DefaultEmbeddingPort` | user-server docs §2.4 |

**前端启动约束**（禁软启动 / 禁多处硬编码）：

1. SDK 集成后**不绑定端口**——`apiBaseURL` 走 `data-api-base-url` 属性 / `window.MarketingChatWidgetConfig.apiBaseURL` / script 同源
2. demo.html 端口 8214 是 Vite dev 端口（仅 demo），不影响 SDK 产物
3. WS 端点由 user-server 端注册：`/api/ws/visitor`（`service_routes.go`）—— SDK 端**禁止写绝对 WS URL**

### `node demo.html` 调试方式

`demo.html` 不能直接 `node demo.html` 运行（它是 HTML 文件）。正确的本地调试方式：

1. **方式 A（推荐）**：`npm run dev` 后访问 `http://localhost:8214/demo.html`
2. **方式 B**：先 `npm run build`，再用任意静态服务器（`python3 -m http.server 8080`）从 embed-sdk 根目录提供服务，访问 `http://localhost:8080/demo.html`，此时 demo.html 会加载 `./dist/marketing-chat-widget.iife.js`
3. **方式 C**：让 user-server 在 8204 端口运行后，demo.html 中的 `{apiBaseUrl}` 会被自动替换为 `http://localhost:8204`

> demo.html 内置 6 种场景（基础接入、多渠道、跨域部署、CDN 部署、编程式控制、全局变量配置），点击「激活此场景」会销毁旧 widget 并按新配置重新加载 SDK。

---

## 三、目录导航

```
embed-sdk/
├── src/
│   ├── config.js              # 配置解析（data-* / window 全局 / query / 默认值）
│   ├── floating-button.js     # FloatingButton 浮标类
│   ├── iframe-panel.js        # IframePanel 聊天窗容器类
│   ├── widget.js              # 入口 MarketingChatWidget 主类
│   └── styles.css             # .mcw-floating-btn / .mcw-iframe 样式
├── test/
│   ├── unit.test.mjs          # 单元测试（vm 沙箱 + DOM mock）
│   ├── boundary.test.mjs      # 边界情况测试
│   ├── ws.test.mjs            # WebSocket 端到端测试（需 user-server）
│   └── e2e.sh                 # 完整 e2e 脚本（curl + node）
├── demo.html                  # 6 场景演示页
├── vite.config.js             # Vite 库模式配置
└── package.json
```

### `src/` 每个文件的作用

| 文件 | 行数级别 | 核心导出 | 作用 |
|---|---|---|---|
| `widget.js` | ~237 行 | `MarketingChatWidget`（默认 + 命名导出） | 入口编排：调用 `parseConfig`、创建 `FloatingButton` + `IframePanel`、绑定 `message` 监听、触发 `onReady/onOpen/onClose/onUnread/onMessage`；自动实例化并挂到 `window.mcwInstance` |
| `config.js` | ~182 行 | `parseConfig` / `DEFAULTS` | 读取 `data-*`、`window.MarketingChatWidgetConfig`、query 参数；按优先级合并；自动推导 `apiBaseURL` 与 `allowedOrigins` |
| `floating-button.js` | ~140 行 | `FloatingButton` | 创建 56×56 圆形浮标 `div`，挂载到 `document.body`；提供 `mount/unmount/setOpen/setUnread`；点击切换 `mcw-open` class 并切换 SVG icon |
| `iframe-panel.js` | ~273 行 | `IframePanel` | 创建 iframe 加载 `/chat/embed/:channel_ref`；`show/hide/destroy/getChannelRef`；发送 `mcw-config`；接收 `chat-widget-close`；移动端 `visualViewport` 适配 |
| `styles.css` | ~10 行 | - | `.mcw-floating-btn:hover/active` 缩放阴影动画、`.mcw-iframe` opacity 过渡 |

---

## 四、集成方式

### 4.1 `<script>` 标签引入（最简，IIFE）

```html
<script
  src="https://your-cdn.example.com/embed/marketing-chat-widget.iife.js"
  data-app-key="ak_xxx"
  data-api-base-url="https://chat.example.com"
  data-color="#1989fa"
  data-position="bottom-right"
  data-title="在线客服"
  data-welcome="您好,请问有什么可以帮您?"
  data-lang="zh-CN"
  data-z-index="9999"
  data-offset-x="24"
  data-offset-y="24"
></script>
```

刷新页面后自动出现右下角浮标，`window.mcwInstance` 可编程控制。

### 4.2 ES Module 引入

```js
import MarketingChatWidget from 'marketing-chat-widget'

const widget = new MarketingChatWidget({
  apiBaseURL: 'https://chat.example.com',
  appKey: 'ak_xxx',
  color: '#ff6b35',
  events: {
    onReady: (info) => console.log('ready', info),
    onMessage: ({ type, payload }) => console.log(type, payload)
  }
})
widget.init()
```

### 4.3 全局变量配置（无法修改 HTML 时）

```html
<script>
  window.MarketingChatWidgetConfig = {
    apiBaseURL: 'https://chat.example.com',
    appKey: 'ak_xxx',
    color: '#ff6b35',
    position: 'bottom-left',
    welcome: 'Hi, how can I help you?',
    lang: 'en-US',
    width: 400,
    height: 600,
    events: {
      onReady: function (info) { console.log('ready', info) }
    }
  }
</script>
<script src="https://your-cdn.example.com/embed/marketing-chat-widget.iife.js"></script>
```

### 4.4 配置参数总表

| 字段 | data-* 别名 | 默认值 | 说明 |
|---|---|---|---|
| `appKey` | `data-app-key` | `''` | 渠道 AppKey；私域部署可省略 |
| `channelId` | `data-channel-id` | `''` | 直接指定 channel_id（与 appKey 二选一） |
| `apiBaseURL` | `data-api-base-url` | script 同源 / `window.location.origin` | API 基础 URL |
| `position` | `data-position` | `bottom-right` | `bottom-right` \| `bottom-left` |
| `color` | `data-color` | `#1989fa` | 浮标主色（hex） |
| `title` | `data-title` | `在线客服` | 聊天窗标题（iframe `title` 属性） |
| `welcome` | `data-welcome` | `您好,请问有什么可以帮您?` | 欢迎语（透传到 iframe） |
| `lang` | `data-lang` | `zh-CN` | `zh-CN` \| `en-US` |
| `visitorIdKey` | `data-visitor-id-key` | `mtk_visitor_id` | localStorage 中访客 UUID 的 key |
| `zIndex` | `data-z-index` | `9999` | 浮标 / iframe 层级 |
| `offsetX` | `data-offset-x` | `24` | 水平边距 px |
| `offsetY` | `data-offset-y` | `24` | 垂直边距 px |
| `width` | `data-width` | `380` | 聊天窗宽度（移动端自动全屏） |
| `height` | `data-height` | `560` | 聊天窗高度（移动端自动全屏） |
| `allowedOrigins` | - | 自动推导 | postMessage origin 白名单 |
| `events` | - | `{}` | 事件回调集合 |

**解析优先级**：`window.MarketingChatWidgetConfig` > query 参数 > `data-*` 属性 > 内置默认值（见 `config.js`）

---

## 五、新增聊天功能的标准流程

以「新增一个『快捷回复』按钮，点击发送预设消息」为例：

### 5.1 在 `widget.js` 注册（可选）

如果新功能需要父端介入（例如事件透传），在 `_bindMessageListener` 中新增 case：

```js
// src/widget.js
if (data.type === 'mcw-quick-reply') {
  this._fireEvent('onMessage', { type: 'mcw-quick-reply', payload: data.payload })
  return
}
```

> 大部分聊天 UI 功能（按钮、列表、输入框）都在 iframe 内的 user-web SPA 实现，**不需要修改 embed-sdk**。仅当需要父端联动（如浮标红点、宿主埋点）时才动 SDK。

### 5.2 在 `iframe-panel.js` 渲染（如需 iframe 容器层支持）

`IframePanel` 通常只负责 `mcw-config` 透传与 `chat-widget-close` 处理。新增配置字段时：

1. 在 `IframePanelOptions` typedef 增加字段
2. 在 `constructor` 中读取并赋默认值
3. 在 `show()` 的 `mcw-config` payload 中透传给 iframe

```js
// src/iframe-panel.js
this.quickReplies = options.quickReplies || []
// 在 show() 的 postMessage 中:
payload: { ..., quickReplies: this.quickReplies }
```

### 5.3 与 user-server 协议对齐

- 新增的配置字段（如 `quickReplies`）需同步在 user-web SPA `/chat/embed/:channel_ref` 入口中读取并渲染
- 涉及后端的能力（如服务端预设快捷回复模板）需在 user-server `internal/router/chat_routes.go` 与 `chat_visitor_service.go` 中扩展 REST API
- 新增 postMessage 消息类型时，**两侧 type 字符串必须完全一致**，并文档化于 [./ARCHITECTURE.md §4.1](./ARCHITECTURE.md)

### 5.4 测试

- 在 `test/unit.test.mjs` §8「关键关键字在源码中存在」追加新关键字断言
- 在 `test/boundary.test.mjs` §11「多种 postMessage 消息类型」追加新 type
- 更新 `demo.html` 演示新功能

---

## 六、测试运行

### 6.1 单元测试 `test/unit.test.mjs`

- 用 Node `vm` 模块创建沙箱，加载 `dist/marketing-chat-widget.iife.js`
- 轻量级 DOM mock（`makeElement` + `documentMock` + `windowMock`）
- 覆盖 10 个章节：SDK 加载、默认配置、FloatingButton、IframePanel、跨域 origin、事件回调、JSDoc 完整性、关键关键字、demo 场景、docs 索引

```bash
npm run build    # 必须先构建产物
npm run test
```

### 6.2 边界测试 `test/boundary.test.mjs`

13 个章节覆盖异常输入：

1. allowedOrigins 白名单边界（非法 origin / 协议不同 / 端口不同 / 大小写）
2. 消息体格式异常（null / undefined / 字符串 / 数字 / 数组 / 空对象）
3. 超大消息体（1MB / 10MB）
4. allowedOrigins 配置异常（null / undefined / 空数组 / 非数组）
5. 多次 mount / destroy 循环
6. 用户事件回调异常不导致 SDK 崩溃
7. URL 解析边界（畸形 apiBaseURL / `javascript:` 协议）
8. config.js SSR 守卫完整性
9. query 参数非法值
10. 多次 init 不重复挂载
11. 多种 postMessage 消息类型
12. 快速 open/close 循环（50 次）
13. 危险协议 origin 处理

```bash
npm run test:boundary
```

### 6.3 WebSocket 测试 `test/ws.test.mjs`

- 需要 Node 22+ 内置 `WebSocket`
- 需要 user-server 监听 `localhost:8204`
- 测试 `/api/ws/visitor` 端点：缺参返回 400、合法参数握手成功、不存在 channel 异步校验

```bash
# 前置：启动 user-server
cd ../user-server && ./bin/server &
cd ../embed-sdk
npm run test:ws
```

### 6.4 端到端 `test/e2e.sh`

11 个 section 综合验证：

0. 前置检查（user-server 健康、SDK 产物存在、node 已安装）
1. user-server 关键端点（`/api/health`、`/chat/embed/default`、`/embed/marketing-chat-widget.iife.js`）
2. SDK 端点内容（含 `MarketingChatWidget`）
3. CORS 跨域配置
4. WebSocket 端点存活
5. demo.html 6 场景覆盖
6. SDK 源码 JSDoc 完整性（`@typedef McwConfig/McwEvents/FloatingButtonOptions/IframePanelOptions`）
7. 跨域 origin 校验逻辑
8. FRP 模板生成
9. 文档一致性（`docs/INDEX.md` / `CHAT_WIDGET_EMBED.md` / `FRP私域部署指南.md`）
10. 单元测试
11. WebSocket 实测

```bash
bash test/e2e.sh
```

---

## 七、构建与发布

### 7.1 Vite 库模式构建

`vite.config.js` 关键配置：

```js
build: {
  lib: {
    entry: resolve(__dirname, 'src/widget.js'),
    name: 'MarketingChatWidget',
    formats: ['iife', 'esm'],
    fileName: (format) => `marketing-chat-widget.${format}.js`
  },
  outDir: 'dist',
  emptyOutDir: true,
  rollupOptions: { output: { extend: true, exports: 'named' } },
  minify: 'esbuild',
  sourcemap: false,
  target: 'es2018'
}
```

### 7.2 产物

| 产物 | 用途 | 引入方式 |
|---|---|---|
| `dist/marketing-chat-widget.iife.js` | `<script>` 标签 | UMD 全局 `window.MarketingChatWidget` + `window.mcwInstance` 自动实例化 |
| `dist/marketing-chat-widget.esm.js` | npm 包 / 现代打包工具 | `import MarketingChatWidget from 'marketing-chat-widget'` |

### 7.3 集成到 user-web 静态托管

`user-web/public/` 会将构建产物作为静态资源托管：

```bash
# 在仓库根 hivemtk/ 下
make sdk-build
# 等价于 cd embed-sdk && npm install && npm run build
```

### 7.4 CDN 发布

```bash
npm run build
aws s3 sync dist/ s3://cdn.example.com/embed/ \
  --cache-control "public, max-age=31536000"
```

### 7.5 版本管理

- `package.json#version` 当前 `1.0.0`
- 主版本升级必须保持 `window.mcwInstance` / `window.MarketingChatWidget` API 兼容（ADR-011 §10.2）
- 新字段一律可选，默认值向前兼容
- 弃用字段提前 3 个 minor 版本 warning

---

## 八、调试技巧

### 8.1 demo.html 本地调试

- `npm run dev` 启动 Vite，自动打开 `http://localhost:8214/demo.html`
- demo.html 顶部有「占位符」面板，可动态修改 `apiBaseUrl` / `channelId` / `primaryColor`
- 每个场景卡片下方有「激活此场景」按钮，会销毁旧 widget 并按新配置重新加载 SDK
- 场景 5「编程式控制」提供 `open() / close() / destroy()` 按钮和实时事件日志

### 8.2 浏览器 DevTools

- **Console**：SDK 在 `_fireEvent` 异常时输出 `[MarketingChatWidget] event xxx error:`，可定位用户回调崩溃
- **Elements**：浮标 DOM 为 `<div class="mcw-floating-btn">`，iframe 为 `<iframe class="mcw-iframe">`
- **Application → Local Storage**：查看 `mtk_visitor_id`（访客 UUID）
- **Network → WS**：选中 iframe 请求，切到「Messages」tab 查看 WebSocket 帧（注意 iframe 内的 WS 在 iframe 上下文）

### 8.3 跨 iframe 调试

DevTools 顶部 frame selector 切换到 iframe 上下文：

```
top
└── chat.example.com/chat/embed/default
```

切换后可在 iframe 内执行 `console.log(window.location)` / `localStorage.getItem('mtk_visitor_id')` 等。

### 8.4 WebSocket 帧分析

- Chrome DevTools → Network → 筛选 `WS` → 选中 `/api/ws/visitor` 请求
- 切换到「Messages」tab 查看双向消息（绿色为接收，白色为发送）
- 关注 `seq` / `ack` / `event` 字段（具体格式由 user-web SPA 与 user-server 协议定义）

### 8.5 postMessage 调试

在宿主页面 Console 执行：

```js
window.addEventListener('message', (e) => {
  console.log('[postMessage]', e.origin, e.data)
})
```

可观察 iframe → 父端的所有消息（包括 `mcw-config` 回声、`mcw-unread`、`chat-widget-close`）。

### 8.6 强制重新加载 SDK

```js
// 销毁当前实例
window.mcwInstance.destroy()
// 移除旧 script
document.getElementById('mcw-loader')?.remove()
// 重新加载
const s = document.createElement('script')
s.id = 'mcw-loader'
s.src = './dist/marketing-chat-widget.iife.js'
document.body.appendChild(s)
```

### 8.7 查看 SDK 实际生效的配置

```js
window.mcwInstance.config
// {
//   appKey, channelId, apiBaseURL, position, color, title, welcome,
//   lang, visitorIdKey, zIndex, offsetX, offsetY, width, height,
//   allowedOrigins: [...], events: {...}
// }
```

### 8.8 调试 origin 白名单

```js
// 查看当前白名单
window.mcwInstance.config.allowedOrigins
// 例如：['http://localhost:8204', 'http://localhost:8214']

// 临时增加白名单（调试用）
window.mcwInstance.config.allowedOrigins.push('https://test.example.com')
```

---

最近更新日期: 2026-07-26
