# user-web · 用户端 B 端工作台

> HiveMtk 的 Vue 3 运营工作台：统一收件箱、AI 智能体管理、客户 360°、触达排程、知识库、营销画布、数据看板都在这里操作。覆盖用户端 94 个核心业务模块。项目总览见 [仓库根 README](../README.md)。
>
> 后端服务对接 [`../user-server`](../user-server/)，嵌入聊天窗 SDK 配套 [`../embed-sdk`](../embed-sdk/)。

## ✨ 项目功能

### 客户与线索

- **OneID 客户 360°**：跨抖音 / 快手 / 小红书 / 闲鱼 / TikTok / 微信 / 短信 / 邮件身份归一化，冲突解决
- **客户事件流**：跨渠道行为统一时间线
- **RFM 分群 / 标签分层**：自定义规则 + 自动打标
- **线索管理**：线索池、跟进状态、统计、转化漏斗
- **客户旅程大屏**：阶段转化与流失节点可视化

### 智能客服

- **统一收件箱（Unified Inbox）**：所有渠道消息聚合会话
- **AI 智能体管理**：多智能体 CRUD、渠道绑定、客服挂载
- **客服工作台**：坐席状态、快捷回复库、会话标签、AI 建议、AI 拟人度 / 置信度
- **SOP 智能体**：销冠 Prompt 库自动执行
- **意图识别 / 对话记忆 / 异议处理 / 销冠画像**：AI 辅助运营
- **ChatChannel 渠道管理**：嵌入 SDK 渠道密钥、安装指引

### 多渠道触达

- **短信 / WhatsApp / Telegram / 飞书 / 企微 / 邮件** 六渠道发送与排程
- **群发任务（Bulk Messaging）**：触达管道编排
- **活码（LiveCode）**：短链 + 轮询二维码 + 统计
- **短链管理**：创建、追踪、统计
- **Webhook 对接平台端**：心跳上报、安装信息回传

### 知识库（RAG）

- **知识库工作台**：分块编辑、批量导入、OpenAPI 集成
- **统计与反馈**：检索命中率、人工反馈回流
- **RAG 产品配置**：账号 / 模型路由
- **Playground**：检索 + 生成联调

### 智能卡片

- **抖音 / 快手 / 小红书 / 闲鱼 / TikTok** 五平台卡券生成
- **卡片自动回复**：浏览器自动化接管私信
- **卡片数据统计**：曝光 / 点击 / 转化漏斗

### 营销自动化

- **营销画布（Marketing Flow）**：可视化 SOP
- **AI 内容生成**：素材库 / 模板市场 / 脚本库
- **AI 产能（AI Productivity）**：批量操作、效率看板
- **AB 实验**：多臂老虎机流量分配

### 数据看板

- **大屏数据（Dashboard Screen）**：SSE 实时推送
- **客户旅程看板 / 转化漏斗 / 自定义报表**
- **反馈学习 / 拟人度面板 / 置信度面板**：调优 AI 行为

### 系统与运维

- **系统初始化向导**：首启检测 → 管理员设置 → 业务配置
- **系统配置 / OBS 配置 / 邮件配置 / 短信配置 / LLM 路由**
- **团队与用户 / 角色权限**：RBAC 细粒度
- **备份与恢复 / 操作日志 / 安全审计**
- **标识管理 / 平台账号**：对接平台端心跳与统计

### 公域素材

- **素材库（Material Library）**：图片 / 视频 / 文档
- **模板市场（Template Market）**：可订阅行业模板

> 完整菜单 / 页面 / 路由对齐清单见 [`./MENU_SPEC.md`](./MENU_SPEC.md)。

## 🧱 技术栈

| 维度 | 选型 |
|---|---|
| 框架 | Vue 3.5（Composition API + `<script setup>`） |
| 构建 | Vite 6 |
| 路由 | Vue Router 4（WebHash 模式） |
| 状态管理 | Pinia 3 |
| UI 组件库 | Element Plus 2.9 |
| 图标 | @element-plus/icons-vue |
| HTTP | Axios 1.9（已封装 `src/utils/request.js`） |
| 样式 | Sass 1.89（SCSS） |
| 富文本 | TinyMCE 6 |
| 图表 | ECharts 6 |
| 国际化 | vue-i18n 9 |
| 测试 | Vitest 4（单元）+ Playwright（E2E） |

## 📁 目录结构

```
user-web/
├── public/                       # 静态资源
├── src/
│   ├── api/                      # 业务 API（按域拆分，70+ 模块）
│   ├── components/               # 公共组件（含各平台卡片预览）
│   ├── constants/                # 业务常量（渠道 / 状态 / 类型）
│   ├── i18n/                     # 多语言（见下方 i18n 说明）
│   │   ├── locales/              # 语言包：zh / en / ar / ja
│   │   ├── index.js              # i18n 实例与挂载
│   │   └── locale.js             # 语言切换 / 持久化
│   ├── layout/                   # 主布局 Layout.vue
│   ├── router/
│   │   ├── index.js              # 路由主文件（懒加载）
│   │   └── modules/              # 60+ 业务模块路由
│   ├── stores/                   # Pinia stores（app / user / permission / material）
│   ├── styles/                   # 全局样式（index / reset / variables）
│   ├── types/                    # 类型声明（components.d.ts）
│   ├── utils/                    # request / config / initHelper / 地图 / iconMap
│   └── views/                    # 页面（按域组织）
│       ├── chat/embed/           # 公开嵌入聊天窗
│       ├── KnowledgeWorkspace/   # 知识库工作台
│       └── ... 60+ 业务页面
├── tests/
│   ├── unit/                     # Vitest 单元测试
│   └── e2e/                      # Playwright E2E
├── docs/                         # 前端开发文档（ARCHITECTURE / CONVENTIONS / DEVELOPMENT / FEATURES）
├── .env.development              # 开发环境变量
├── .env.production               # 生产环境变量
├── vite.config.js                # Vite 配置（含代理 / 分包 / 别名）
├── vitest.config.js              # Vitest 配置
├── playwright.config.js          # Playwright 配置
├── MENU_SPEC.md                  # 菜单 / 页面 / 路由对齐清单
└── package.json
```

## 🌐 i18n 多语言

- 框架：`vue-i18n 9`（Composition API）
- 入口：`src/i18n/index.js`（实例化并挂载到 `app.use(i18n)`）
- 语言切换：`src/i18n/locale.js`，提供 `setLocale / getLocale`，语言选择持久化到 `localStorage`
- 切换组件：`src/components/LanguageSwitcher.vue`
- 语言包：`src/i18n/locales/`
  - `zh.json` —— 简体中文（默认）
  - `en.json` —— 英文
  - `ar.json` —— 阿拉伯语（RTL 适配中）
  - `ja.json` —— 日文
- 业务文案统一走 `$t('key')` 或 `t('key')`，禁止在模板硬编码中文 / 英文字符串
- i18n 覆盖度统计：`/i18n/dashboard` 路由下的「i18n 看板」

## 🚀 启动说明

### 前置要求

- Node.js 20+
- npm 10+ / pnpm 8+ / yarn 4+（任选）
- 后端 [user-server](../user-server/) 已启动（默认 `http://localhost:8204`）

### 1. 安装依赖

```bash
cd user-web
npm install          # 或 pnpm install / yarn install
```

### 2. 配置环境变量

开发环境默认从 `.env.development` 读取：

```bash
# .env.development
VITE_API_BASE_URL=https://hiveuserapi.xapptool.cn
```

如需联调本地后端，复制 `.env.example` 覆盖：

```bash
cp .env.example .env.development
# 然后修改 VITE_API_BASE_URL=http://localhost:8204
```

Vite 已配置 `/api` 反代到 `http://localhost:8204`，开发态直接请求 `/api/...` 即可。

### 3. 启动开发服务器

```bash
npm run dev
# 默认端口 8211（端口被占用时自动递增）
# 浏览器访问 http://localhost:8211
```

### 4. 构建生产版本

```bash
npm run build
# 产物输出到 dist/ 目录
```

构建会自动按 `vue / elementPlus / utils` 三个 chunk 拆包，详见 `vite.config.js`。

### 5. 预览生产构建

```bash
npm run preview
# 启动静态服务预览 dist/
```

### 6. 单元测试

```bash
npm run test               # 单次运行
npm run test:watch         # 监听模式
npm run test:coverage      # 覆盖率报告
```

### 7. E2E 测试（Playwright）

```bash
# 安装浏览器（仅首次需要）
npx playwright install

# 运行全部 E2E
npm run test:e2e

# UI 模式
npm run test:e2e:ui

# 查看报告
npm run test:e2e:report
```

## ⚙️ 环境变量

| 变量 | 说明 | 默认 |
|---|---|---|
| `VITE_API_BASE_URL` | 后端 API 基础 URL | dev: 线上 demo；生产：相对路径或独立域名 |

## 🛠 常用命令

```bash
# 开发
npm run dev

# 构建
npm run build

# 预览
npm run preview

# 单元测试
npm run test
npm run test:watch
npm run test:coverage

# E2E 测试
npm run test:e2e
npm run test:e2e:ui
```

## 📝 开发规范

- **页面** 放 `src/views/`，按业务域分子目录（如 `customer360/`、`aiAgent/`）
- **公共组件** 放 `src/components/`
- **API** 统一放 `src/api/`，按域命名（如 `customer.js`、`knowledge.js`）
- **状态** 按模块拆 Pinia store，放 `src/stores/`
- **样式** 使用 Sass，遵循 BEM；变量定义在 `src/styles/variables.scss`
- **i18n** 所有面向用户的文案走 `vue-i18n`，语言包在 `src/i18n/locales/`（`zh / en / ar / ja`）
- **常量** 业务常量统一放 `src/constants/`
- **图标** 优先使用 `@element-plus/icons-vue`
- **路由** 模块路由放 `src/router/modules/`，主路由自动懒加载

## 🌐 浏览器嵌入客服窗

工作台内置公开聊天窗，可被第三方站点嵌入。后端 `/api/v1/chat-public/*` 提供 AppKey 鉴权的公开对话能力，前端路由负责渲染聊天窗 UI：

- 路由：`/chat/embed/default`（默认渠道）
- 路由：`/chat/embed/:channel_ref`（指定渠道）
- 嵌入 SDK 配套：[../embed-sdk](../embed-sdk/)
- 嵌入接入文档：[../docs/operations/CHAT_WIDGET_EMBED.md](../docs/operations/CHAT_WIDGET_EMBED.md)

```html
<iframe src="https://your-domain/#/chat/embed/default" style="width:0;height:0;border:0;"></iframe>
```

## 🔗 与 embed-sdk 集成

`user-web` 的公开聊天窗（`src/views/chat/embed/`）是 [embed-sdk](../embed-sdk/) 的承载页面：

1. 商户在 B 端「ChatChannel 渠道管理」创建渠道，获得 `channel_ref` 与 `AppKey`
2. 第三方站点引入 `embed-sdk` 构建产物（`<script src=".../embed-sdk.umd.js">`），SDK 会在页面右下角注入浮按钮 + iframe 面板
3. iframe 指向 `https://your-domain/#/chat/embed/:channel_ref`，前端通过 `chatPublic` API 调用后端 `/api/v1/chat-public/*`，使用 AppKey 鉴权（无需用户登录）
4. 会话消息、AI 回复、转人工全部通过 user-web ↔ user-server 的 WebSocket / HTTP 通道完成

开发联调时，可直接在浏览器访问 `http://localhost:8211/#/chat/embed/default`，配合 `.env.development` 指向本地后端进行调试。

## 🐳 Docker 集成

仓库根 `hivemtk/docker-compose.yml` 中以构建产物方式集成：

- 构建阶段：`Dockerfile` 执行 `npm install && npm run build`
- 运行阶段：由 同源托管 `dist/` 静态资源 + `/api` 反代到 `user-server:8204`

## 📷 截图

统一收件箱 / AI 智能体 / 营销画布 / 客户 360° / 知识库 RAG 五张核心页面截图已收录在仓库根 [`docs/screenshots/`](../docs/screenshots/),亦嵌入根 [README](../README.md#界面一览) 与 [英文 README](../README.en.md#screenshots)。

| 统一收件箱 | AI 智能体 |
|---|---|
| ![统一收件箱](../docs/screenshots/screenshot-unified-inbox.png) | ![AI 智能体](../docs/screenshots/screenshot-ai-agent.png) |
| **营销画布** | **客户 360°** |
| ![营销画布](../docs/screenshots/screenshot-marketing-canvas.png) | ![客户 360°](../docs/screenshots/screenshot-customer-360.png) |
| **知识库 RAG** | |
| ![知识库 RAG](../docs/screenshots/screenshot-knowledge-rag.png) | |

> _待补：登录页 / 工作台首页 / 数据大屏 等页面截图。_

## 📚 关联文档

- 仓库根 [README](../README.md)
- 菜单 / 路由清单 [./MENU_SPEC.md](./MENU_SPEC.md)
- 架构图 [../docs/architecture/ARCHITECTURE_DIAGRAM.md](../docs/architecture/ARCHITECTURE_DIAGRAM.md)
- 嵌入聊天窗接入 [../docs/operations/CHAT_WIDGET_EMBED.md](../docs/operations/CHAT_WIDGET_EMBED.md)
- 嵌入 SDK [../embed-sdk](../embed-sdk/)
- 后端服务 [../user-server](../user-server/)

## 📄 许可证

本项目以 **GNU Affero General Public License v3.0（AGPL-3.0）** 发布，详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。

- 任何对本项目的修改与网络服务提供均须开源衍生代码（AGPL-3.0 第 13 条）
- 商业闭源集成 / 二次分发请先联系商务获取授权

商务合作 / 技术支持：jideilvluoqun@gmail.com
