# HiveMTK 官网

HiveMTK · 私域 AI 营销操作系统的官方网站。基于 Vue 3 + Vite + vue-i18n（4 语言）+ vue-router 构建。

## 技术栈

- **框架**: Vue 3 (`<script setup>` SFCs)
- **构建工具**: Vite 8
- **路由**: Vue Router 4
- **国际化**: vue-i18n 9（中 / 英 / 日 / 阿语）
- **部署**: GitHub Pages（`https://xiaofang142.github.io/hivemtk/`）

本站是**纯静态站**：运行期对任何后端零依赖，不含 API 调用、iframe 客服浮标或登录态。

## 目录结构

```
website/
├── public/             # 静态资源（favicon、robots.txt、sitemap.xml、wechat.jpg）
├── scripts/            # 构建后处理脚本（postbuild.mjs：404 兜底 + 按路由铺 index.html）
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
├── deploy.sh           # 唯一构建入口：预检 + npm ci + build + Pages 产物门
├── dev-server.cjs      # 本机 dist 预览服务
├── vite.config.js      # Vite 配置（base = SITE_BASE = '/hivemtk/'）
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
   访问 `http://127.0.0.1:8213/hivemtk/`。端口来自 `vite.config.js` 的 `server.port`。

2. **本地预览 dist 静态服务**（模拟 Pages 的静态托管行为，含 SPA fallback）：
   ```bash
   npm run dev
   ```
   启动 `dev-server.cjs`，端口在 `dev-server.cjs` 的 `const PORT = 8213` 配置。
   需先执行 `npm run build` 产出 dist/ 目录。`npm run preview` 是本命令的同义别名。

> `SITE_BASE = '/hivemtk/'` 是三处同步的同一字面量：`vite.config.js` 的 `base`、`dev-server.cjs`、`index.html` 的 canonical/hreflang 前缀。改站点子路径必须三处一起改。

### 构建生产版本

```bash
npm run build
```

产出 `dist/`，由 GitHub Pages 托管。`postbuild.mjs` 在 vite build 之后补两类 Pages 兜底产物：
`dist/404.html`（Pages 无 rewrite，未知路径返回 404 状态码 + 该页，SPA 靠 `/:pathMatch(.*)*` 接管路由）
和按路由铺开的 `<route>/index.html`（让深链拿到真 200）。

## 发布（GitHub Pages）

构建入口只有一个脚本，CI 与本地跑同一条命令：

```bash
cd website
bash deploy.sh                # 预检 + npm ci + build + Pages 产物门
bash deploy.sh --preflight-only   # 只跑预检，不构建
```

推送到 `master` 且改动命中 `website/**` 时，`.github/workflows/website-pages.yml` 自动执行同一脚本并把
`website/dist` 发布到 Pages。一次性前置（仓库设置里开一次即可）：

```bash
gh api -X POST repos/xiaofang142/hivemtk/pages -f build_type=workflow
```

未开启时 workflow 的 configure-pages 步骤会直接报错，不会静默发布空站。

## 配置口径

本站**没有任何构建期环境变量**：源码里 `import.meta.env` 只读 `BASE_URL`（Vite 由 `base` 注入，与 `.env` 无关），
因此仓库内不存在 `.env.example` / `.env.development` / `.env.production`，也不需要。

- 翻译词典：`src/i18n/modules/`（完整性由 `check_i18n.mjs` 校验，`MISSING_UNIQ=0` 才算绿）
- SEO：`index.html` 的 canonical/hreflang/JSON-LD、`public/robots.txt`、`public/sitemap.xml`（host 统一为 Pages 地址）

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

本站源码是 **hivemtk 仓库的 `website/` 目录**（不是独立仓库），Pages 站点由该仓库的 GitHub 端发布：

- **主仓库（Gitee）**: [hivemtk](https://gitee.com/xhpmayun/hivemtk)
- **发布源（GitHub）**: [xiaofang142/hivemtk](https://github.com/xiaofang142/hivemtk) → `https://xiaofang142.github.io/hivemtk/`
- **平台端仓库（可选组件）**: [hivemtk-platform](https://gitee.com/xhpmayun/hivemtk-platform) —— 用户端默认 `PLATFORM_ENABLED=false`，不部署平台端也能完整运行
