# HiveMTK 官网

HiveMTK · 私域 AI 营销操作系统的官方网站。基于 Vue 3 + Vite + vue-i18n（4 语言）+ vue-router 构建。

## 技术栈

- **框架**: Vue 3 (`<script setup>` SFCs)
- **构建工具**: Vite 8
- **路由**: Vue Router 4
- **国际化**: vue-i18n 9（中 / 英 / 日 / 阿语）
- **部署**: 线上 `https://xapptool.cn`——本地 `npm run build` 后把 `dist/` 整目录上传到服务器站点根（见「发布」章节，无发布脚本）

本站是**纯静态站**：不连数据库、不发后端 API、无登录态。**但有一个运行期外部依赖**——右下角网页客服浮标：`index.html` 末尾内联注入 `https://work.xapptool.cn/embed/marketing-chat-widget.iife.js`（连接云端 `work.xapptool.cn` 的 web 渠道智能体，`appKey ak_79Zw7HWzR5wNUnNn6QMnkw8B`）。

> 它是在服能力，**不是死代码**：2026-10-23 曾被误判「指向已下线域名」删除，线上客服整块消失。判死规约、在服依赖清单、发布后自查命令见 `../../deploy/README.md` §9（`deploy/` 在工作区根、本仓之外，绝对路径 `~/Documents/www/go/hivemtk/deploy/README.md`，不入 git）。
>
> **构建门**：`scripts/postbuild.mjs` 的 2.5 节会断言产物壳里还有 `window.MarketingChatWidgetConfig` 与 SDK 地址，缺一条就 `exit 1`——所以删掉浮标 `npm run build` 直接失败。门只保证「壳里挂着」，SDK 是否真活着仍要跑发布后自查（`deploy/README.md` §9.4）。

## 目录结构

```
website/
├── public/             # 静态资源（favicon、robots.txt、sitemap.xml、wechat.jpg）
├── scripts/            # 构建后处理脚本（postbuild.mjs：客服浮标硬门 + 按路由铺 index.html + 404 兜底）
├── src/
│   ├── components/     # Vue 组件（Navbar、Hero、Footer 等）
│   ├── composables/    # 组合式函数（useSite、useSiteContact、useToast）
│   ├── config/         # 内容配置（content.js）
│   ├── i18n/           # 国际化（4 语言模块）
│   ├── router/         # 路由配置
│   ├── views/          # 页面视图（Home、Features、Deploy、Docs 等）
│   ├── App.vue         # 根组件
│   ├── main.js         # 入口
│   └── style.css       # 全局样式
├── check_i18n.mjs      # 翻译词典完整性校验
├── dev-server.cjs      # 本机 dist 预览服务
├── vite.config.js      # Vite 配置（base = SITE_BASE = '/'，线上站点根路径）
└── package.json
```

## 启动说明

### 前置要求
- Node.js 20+
- 无其他依赖：本站不需要任何后端服务在跑

### 安装依赖

```bash
cd website
npm install
```

### 开发模式

本项目提供两种本地开发模式，**端口都是 8213**（一次只能起一种）：

1. **Vite 热更新开发**（推荐，源码修改实时生效）：
   ```bash
   npm run vite-dev
   ```
   访问 `http://127.0.0.1:8213/`。端口来自 `vite.config.js` 的 `server.port`。

2. **本地预览 dist 静态服务**（模拟线上站点根的静态托管行为，含 SPA fallback 与 404.html）：
   ```bash
   npm run dev
   ```
   启动 `dev-server.cjs`，端口在 `dev-server.cjs` 的 `const PORT = 8213` 配置。
   需先执行 `npm run build` 产出 dist/ 目录。`npm run preview` 是本命令的同义别名。

> `SITE_BASE = '/'` 是两处同步的同一字面量：`vite.config.js` 的 `base` 与 `dev-server.cjs` 的 `SITE_BASE`。线上是域名根路径托管，改托管路径须两处一起改。

### 构建生产版本

```bash
npm run build
```

产出 `dist/`，整目录上传到线上站点根。`postbuild.mjs` 在 vite build 之后补两类兜底产物：
`dist/404.html`（nginx `error_page 404 /404.html`，未知路径返回 404 状态码 + 该页，SPA 靠 `/:pathMatch(.*)*` 接管路由）
和按路由铺开的 `<route>/index.html`（让深链拿到真 200）。

## 发布（线上站点根目录，不用任何脚本）

发布只有一种方式：**本地构建 → 把 `dist/` 整目录上传到线上服务器的站点根目录**。
没有发布脚本、没有 CI 发布、不改域名、不替换任何 URL——构建产物本身就是线上要的东西。

```bash
# 1) 本地构建（产物落在 website/dist）
cd website
npm ci
npm run build            # vite build + postbuild（按路由铺 <route>/index.html、生成 404.html）

# 2) 上传：整目录覆盖线上站点根（--delete 清掉旧 hash 资源，避免残留文件堆积）
#    服务器别名 mtk-prod 见 ~/.ssh/config（101.35.252.112:57122，root）
#    --exclude=.user.ini：宝塔站点根那个文件带 immutable 属性，--delete 删它必报 EPERM
rsync -az --delete --exclude='.user.ini' -e 'ssh -p 57122' dist/ mtk-prod:/www/wwwroot/hivemtk/website/

# 3) 冒烟（状态码 + 客服浮标是否还在页里）
curl -s -o /dev/null -w '%{http_code}\n' https://xapptool.cn/
curl -s -o /dev/null -w '%{http_code}\n' https://xapptool.cn/pricing/
# 浮标自查：每个路由壳应命中 2 次（配置块 URL + 注入脚本 URL），详情见 deploy/README.md §9.4
curl -s https://xapptool.cn/ | grep -c 'marketing-chat-widget'   # 期望 2
```

**服务器侧（一次性配置，与日常发布无关）**

| 项 | 值 |
|---|---|
| 站点根目录 | `/www/wwwroot/hivemtk/website`（`dist/` 内容平铺在此，无子目录） |
| Web 服务器 | 宝塔 nginx，站点配置 `/www/server/panel/vhost/nginx/xapptool.cn.conf` |
| HTTPS / SSL | 宝塔证书 `/www/server/panel/vhost/cert/xapptool.cn/fullchain.pem` + `privkey.pem`，443 已启用，HTTP 自动跳 HTTPS（证书在宝塔续期，不随发布变动） |
| SPA 兜底 | nginx `error_page 404 /404.html`；未知深链由 `postbuild` 生成的 `404.html` 渲染 |
| 数据库 | 同机 docker 容器 `mtk-postgres`（127.0.0.1:8202）、`mtk-redis`（127.0.0.1:8203）——**官网是纯静态站，运行期不连数据库**，这些容器服务于用户端 / 平台端后端 |

构建期没有任何环境变量与域名替换：`base` 固定为根路径 `/`（`vite.config.js` 的 `SITE_BASE`），
所以 `dist/` 可以直接铺到站点根，不需要在上传前 sed 任何东西。

## 配置口径

本站**没有任何构建期环境变量**：源码里 `import.meta.env` 只读 `BASE_URL`（Vite 由 `base` 注入，与 `.env` 无关），
因此仓库内不存在 `.env.example` / `.env.development` / `.env.production`，也不需要。

- 翻译词典：`src/i18n/modules/`（完整性由 `check_i18n.mjs` 校验，`MISSING_UNIQ=0` 才算绿）
- SEO：`index.html` 的 canonical/hreflang/JSON-LD、`public/robots.txt`、`public/sitemap.xml`

## 内容配置

所有页面文案集中在 `src/config/content.js`，修改该文件即可更新：
- 品牌信息（`brand`）
- 联系方式（`contact`）
- Hero 首屏内容（`hero`）
- 核心功能（`featuresSection`）
- 工程能力（`toolchainSection`）
- 工作流（`workflowSection`）
- 常见问题（`faqSection`）
- 技术规格（`techSpecs`）
- 部署指南（`deploySection`）
- 页脚（`footer`）
- 导航（`nav`）

## 开源协议

本项目以 **GNU Affero General Public License v3.0（AGPL-3.0）** 发布，详见 [../LICENSE](../LICENSE) 与 [../NOTICE](../NOTICE)。

- 任何对本项目的修改与网络服务提供（例如 SaaS / 云端 / API / 托管实例）均须按 AGPL-3.0 第 13 条向使用该服务的所有用户免费提供其修改后的完整对应源代码（Corresponding Source），且同样以 AGPL-3.0 开源
- 商业闭源集成 / 二次分发请先联系商务获取授权

商务合作 / 技术支持：`jideilvluoqun@gmail.com`

## 所在仓库

本站源码是 **hivemtk 仓库的 `website/` 目录**（不是独立仓库），线上发布 = 本地构建产物上传到服务器站点根：

- **主仓库（Gitee）**: [hivemtk](https://gitee.com/xhpmayun/hivemtk)
- **源码（GitHub）**: [xiaofang142/hivemtk](https://github.com/xiaofang142/hivemtk)（官网 = `website/` 目录）
- **平台端仓库（可选组件）**: [hivemtk-platform](https://gitee.com/xhpmayun/hivemtk-platform) —— 用户端默认 `PLATFORM_ENABLED=false`，不部署平台端也能完整运行
