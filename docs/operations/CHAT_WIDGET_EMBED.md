# HiveMtk 用户端 - 嵌入式 Chat Widget 集成指南

> 私域部署下，企业如何在自己的官网嵌入客服浮标
> 关联 ADR：ADR-011（嵌入式客服 Chat Widget）

---

## 一、定位

企业有自己的官方网站，希望在右下角加一个**浮标客服入口**。点击后弹出聊天窗，与 HiveMtk 用户端对接。

**几行代码**即可嵌入，无需复杂的对接开发。

---

## 二、架构

```
┌──────────────────────────────────────────────────┐
│             企业官网（如 www.example.com）            │
│                                                  │
│  <!-- 一行 script 即可嵌入 -->                       │
│  <script src="https://chat.example.com/embed/     │
│              marketing-chat-widget.iife.js"        │
│          data-app-key="optional"                  │
│          data-position="bottom-right"             │
│          data-color="#1989fa"                     │
│  </script>                                        │
│                                                  │
│       ┌──────────────────────┐                    │
│       │ 浮标 + iframe 聊天窗  │                    │
│       │  - 右下角悬浮按钮     │                    │
│       │  - 极简白底风格       │                    │
│       │  - max-width 380px   │                    │
│       └──────────┬───────────┘                    │
│                  │                                │
└──────────────────┼────────────────────────────────┘
                   │ HTTPS / HTTP 长轮询
                   ▼
   ┌──────────────────────────────────┐
   │  user-server (HiveMtk 用户端)    │
   │  - RESTful API: /api/chat/public/*     │
   │  - WebSocket: /api/ws/visitor          │
   │  - AI 自动回复（LLM + RAG）            │
   │  - 触发转人工后接管               │
   └──────────────────────────────────┘
```

---

## 三、关键决策（ADR-011 摘要）

### 1. 部署模式：私域独立部署

- 每个企业独立部署一套完整系统（user-server + user-web + PostgreSQL）
- 数据完全隔离
- **严禁** SaaS / 多租户模式，无 `merchant_id` 字段

### 2. AppKey 机制：可选

- AppKey/ChannelID 不是必填
- 缺失时使用默认 `default` 渠道
- 中间件改为**软解析**（缺失时跳过鉴权）

### 3. WebSocket：无鉴权

- 私域部署，WebSocket 通道全部是企业自己的网站
- `/api/ws/visitor` 端点无强制鉴权

### 4. UI：极简白底

- 主色：白底 + 蓝色强调（#1989fa）
- 不使用阴影、渐变等装饰
- 移动端：max-width 380px

### 5. 附件：访客直传七牛

- 后端仅下发上传凭证，访客浏览器直传七牛 CDN
- 文件类型白名单：image / file / audio / video
- 单文件 ≤ 20MB
- Token 有效期：1 小时

### 6. 转人工：NLP 关键词自动触发

- 前端**不暴露**"转人工"按钮（永远显示"在线客服"）
- 后端基于关键词（"人工"/"真人"/"转人工"/"human"/"operator"/"客服"/"agent"/"找人"）自动触发
- 命中后跳过 AI 推理，直接走自动分配

---

## 四、嵌入步骤

### 4.1 准备浮标脚本

`embed-sdk` 是 HiveMtk 提供的网页客服浮标 SDK（独立前端工程），构建后产物在 `embed-sdk/dist/`。

由 `user-server` 在 `/embed/` 路径下服务（通过 docker-compose 挂载 `embed-sdk/dist` 到容器内的 `/app/embed-sdk-dist`）。

### 4.2 嵌入企业官网

```html
<!-- 在企业官网 </body> 前加入 -->
<script src="https://chat.example.com/embed/marketing-chat-widget.iife.js"
        data-app-key="optional-channel-id"
        data-position="bottom-right"
        data-color="#1989fa">
</script>
```

### 4.3 必传参数

| 参数 | 说明 | 默认值 |
|------|------|--------|
| `src` | 浮标脚本 URL | 必填 |
| `data-app-key` | 渠道 ID（可选，缺失时走 `default` 渠道）| `default` |
| `data-channel-id` | 直接指定 channel_id（与 appKey 二选一）| - |
| `data-api-base-url` | API 基础 URL（默认同源）| 同源 |
| `data-position` | 浮标位置 | `bottom-right` |
| `data-color` | 主色 | `#1989fa` |
| `data-title` | 聊天窗标题 | `在线客服` |
| `data-z-index` | 浮标层级 | `9999` |
| `data-offset-x` | 水平边距 px | `24` |
| `data-offset-y` | 垂直边距 px | `24` |

> 完整字段与优先级见 [embed-sdk/README.md](../../embed-sdk/README.md)。

---

## 五、用户端域名要求

- 浮标脚本 URL 域名 **必须** 与 `user-server` 部署域名一致（同源部署）
- 长轮询走同一域名（由 SDK 自动推断）
- 外部反代（CDN / 云负载均衡）需透传 `/api/chat/public/*` 与 `/api/v1/ai/chat/poll` 路径
- 跨域部署场景：详见 [embed-sdk/README.md](../../embed-sdk/README.md) "跨域部署"小节

---

## 六、嵌入 SDK 自动行为

- 页面加载完成后自动注入右下角浮标按钮
- 用户点击后弹出 iframe 聊天窗（最大宽度 380px）
- iframe 内容由 user-server 提供（`/chat/embed/:channel_ref` 路径，channel_ref = appKey / channelId / `default`）
- 父子页面通过 `postMessage` 通信（带 origin 校验）
- 访客身份由 localStorage 生成的 UUID 跟踪

---

## 七、聊天窗功能

| 功能 | 说明 |
|------|------|
| 实时消息 | HTTP 长轮询 |
| 消息历史 | localStorage 缓存 + API 拉取 |
| 快捷回复 | 内置常见问题模板 |
| 文件上传 | 七牛直传，支持图片/文档/音频/视频 |
| 转人工 | 关键词触发（前端不显示按钮）|
| 离线消息 | 客服不在线时留言，下次访客打开仍可见 |

---

## 八、SDK 集成模式

### 8.1 自动注入（默认）

页面加载后自动注入浮标，无需额外配置。

### 8.2 编程式控制

```javascript
// 打开聊天窗
window.mcwInstance.open()

// 关闭聊天窗
window.mcwInstance.close()

// 完全销毁
window.mcwInstance.destroy()
```

### 8.3 全局变量配置

```html
<script>
  window.MarketingChatWidgetConfig = {
    appKey: 'ak_xxx',
    apiBaseURL: 'https://api.example.com',
    color: '#ff6b35',
    position: 'bottom-left'
  }
</script>
<script src="https://chat.example.com/embed/marketing-chat-widget.iife.js"></script>
```

> 配置优先级：`data-*` 属性 > `window.MarketingChatWidgetConfig` > 内置默认值。

---

## 九、移动端适配

- 聊天窗 max-width: 380px
- 浮标位置避免与浏览器工具栏重叠
- 自动检测移动设备尺寸

---

## 十、安全

- 访客消息 HTML 转义（防 XSS）
- WebSocket 无鉴权（私域部署，访客是企业自己的用户）
- 速率限制：30 条消息/分钟/IP
- iframe 通信用 postMessage + origin 校验

---

## 十一、CDN 部署

为提高全球访问速度，可把 `embed-sdk/dist/` 单独部署到 CDN（仅静态 JS 资源，**API 与 WebSocket 仍走 user-server 域名**）：

```bash
# embed-sdk 构建后
npm run build

# 上传到 CDN
aws s3 sync dist/ s3://cdn.example.com/embed/ \
  --cache-control "public, max-age=31536000"
```

企业官网改为引用 CDN（注意 `data-api-base-url` 必须指向真正的 user-server 域名）：

```html
<script src="https://cdn.example.com/embed/marketing-chat-widget.iife.js"
        data-api-base-url="https://chat.example.com">
</script>
```

> CDN 部署属于**跨域**场景，需确保 user-server 的 `CORS_ALLOW_ORIGINS_USER` 已放行 `cdn.example.com` 的请求（其实静态资源不受 CORS 限制，但 postMessage 时 origin 校验会用到 user-server 域名）。

---

## 十二、相关代码

- `embed-sdk/src/` - SDK 源代码
- `user-server/internal/router/chat_routes.go` - `/api/chat/public/*` 与 `/api/ws/visitor` 路由
- `user-server/internal/router/embed_static_routes.go` - `/embed/*` 静态服务 + `/chat/embed/*` SPA 路由（默认查找 `../embed-sdk/dist`，可由 `EMBED_SDK_DIST` 环境变量覆盖）
- `user-server/internal/controller/chat_public.go` - `GetUploadToken` 实现七牛直传凭证下发（同时支持阿里云/腾讯云/AWS/本地存储，见 `internal/dto/obs_config.go`）
- `user-server/internal/service/chat_visitor.go` - 访客聊天服务（`VisitorChatService`）
- `user-web/src/views/chat/embed/Index.vue` - 嵌入聊天窗 SPA 入口
- `migrations/004_customer_session.sql` - customer_sessions 表（platform 字段记录来源渠道）

---

## 十三、相关文档

- SDK 完整说明：[../../embed-sdk/README.md](../../embed-sdk/README.md)
- 完整 ADR：[../architecture/adr/ADR-011-chat-widget-embed.md](../architecture/adr/ADR-011-chat-widget-embed.md)
- FRP 私域部署指南：[../architecture/FRP私域部署指南.md](../architecture/FRP私域部署指南.md)
- RAG 自动回复架构：`RAG_AUTO_REPLY_UNIFIED_ARCHITECTURE.md`（未编写）
- 部署架构：[../architecture/部署方案_用户端.md](../architecture/部署方案_用户端.md)
- 用户端部署手册：[MERCHANT_DEPLOYMENT.md](MERCHANT_DEPLOYMENT.md)
