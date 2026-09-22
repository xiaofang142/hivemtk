# website 代码开发手册

> **规则级别**: ⭐⭐ 项目级开发文档

本手册面向 `hivemtk/website` 工程的开发者，覆盖环境准备、启动命令、目录导航、新增页面/多语言文案/内容的完整流程、SEO 配置、构建与部署、调试技巧。

关联文档：
- 项目总览：[../../README.md](../../README.md)
- 架构图：[./ARCHITECTURE.md](./ARCHITECTURE.md)
- 代码规范：[./CONVENTIONS.md](./CONVENTIONS.md)
- 功能清单：[./FEATURES.md](./FEATURES.md)
- 菜单规格：[../../MENU_SPEC.md](../../MENU_SPEC.md)
- 术语规范：[../../TERMINOLOGY.md](../../TERMINOLOGY.md)

---

## 一、环境准备

### 1.1 Node 与包管理器

| 项 | 要求 |
| --- | --- |
| Node.js | `>= 20.0.0`（见 `package.json` `engines`） |
| npm | `>= 9`（随 Node 20 附带） |
| 操作系统 | macOS / Linux / Windows 均可（脚本优先跨平台 Node 版本） |

> 不强制使用 pnpm/yarn。如需切换包管理器，请同步更新 `package-lock.json`。

### 1.2 安装依赖

```bash
cd hivemtk/website
npm install
```

依赖最小集（见 `package.json`）：
- 运行时：`vue@^3.5.39`、`vue-router@^4.6.4`、`vue-i18n@^9.14.4`、`@fontsource/*`（本地字体包替代 Google Fonts CDN，零三方运行时依赖）
- 构建时：`vite@^8.1.1`、`@vitejs/plugin-vue@^6.0.7`

### 1.3 配置入口（没有环境变量）

本站**不接受任何构建期环境变量**：源码里 `import.meta.env` 只读 `BASE_URL`（由 `vite.config.js` 的 `base` 注入），
因此仓库内没有 `.env.example` / `.env.development` / `.env.production`，新增 `.env` 文件也不会有任何效果。

要改的东西都有唯一代码入口：

| 想改什么 | 改哪里 |
| --- | --- |
| 文案、联系方式、外链、二维码路径 | `src/config/content.js` |
| 站点子路径（Pages 项目名） | `vite.config.js` 的 `SITE_BASE`（三处同步，见 §2.4） |
| 本机预览端口 | `vite.config.js` `server.port` + `dev-server.cjs` `PORT` |
| 发布目标 | `.github/workflows/website-pages.yml` |

历史版本曾有 `VITE_API_BASE_URL`（平台 API 基址）、`VITE_CHAT_URL`（客服 iframe 地址）、`VITE_USE_MOCK` 三个变量，
以及 `vite.config.js` 把 `/public`、`/merchant-api` 反代到 `localhost:8205` 的配置。
客服浮标与平台端联系信息接口下线后，这三个变量没有任何消费方，连同反代一起删除。

---

## 二、启动命令

`package.json` `scripts` 字段（只有这四条，没有 lint / format —— 官网未引入任何 ESLint / Prettier 配置，
质量闸门在 `deploy.sh` 与 i18n 校验脚本里）：

```json
{
  "dev": "node dev-server.cjs",
  "vite-dev": "vite",
  "build": "vite build && node scripts/postbuild.mjs",
  "preview": "node dev-server.cjs"
}
```

### 2.1 开发模式 `npm run vite-dev`

- 入口：`vite`（原生 dev server + HMR）
- 监听：`http://127.0.0.1:8213/hivemtk/`（`vite.config.js` `server.port` + `base`）
- **没有任何 API 反代**：`server.proxy` 段随客服浮标一起删除，官网运行期零后端依赖

### 2.2 预览模式 `npm run dev` / `npm run preview`

两者是同一条命令（`node dev-server.cjs`），从 `dist/` 提供静态文件，用来在本机复现 Pages 的托管行为：

- 监听：`http://127.0.0.1:8213/hivemtk/`
- 行为：
  - 静态资源按 MIME 表返回，缺失文件回退 `index.html`（SPA fallback）
  - 防路径穿越：检查 `filePath.startsWith(DIST_DIR)`
  - 需先 `npm run build` 产出 `dist/`
- 与历史的差异：旧版这里还有 `/public/*`、`/merchant-api/*` 到 `PLATFORM_PROXY`（默认 `http://127.0.0.1:8205`）
  的反代与 hop-by-hop 头过滤；平台端调用链删除后整段代码已移除，`PLATFORM_PROXY` 环境变量不再生效

### 2.3 生产构建 `npm run build`

两步串行：
1. `vite build` —— 以 `base: '/hivemtk/'` 输出到 `dist/`，`manualChunks` 将 `vue` / `vue-router` / `vue-i18n` 拆为 `vendor` chunk（避免业务代码改动导致 vendor 缓存失效）
2. `node scripts/postbuild.mjs` —— 跨平台 Node 脚本，做四件事：
   - 清理 Vite 多入口默认产物 `index-*.html`（本项目是 SPA，只需一个 `index.html`）
   - 确保 `dist/favicon.svg` 存在（缺失时从 `public/favicon.svg` 复制）
   - 从 `src/router/index.js` 解析静态路由清单，为每条路由铺 `dist/<route>/index.html` 壳，让 Pages 对深链返回真 200；解析结果少于 5 条视为正则失效，当场失败
   - 复制 `dist/index.html` 为 `dist/404.html`，兜住未铺壳的路径

> 旧版第 3 步是「复制 `hivemtk/embed-sdk/dist` 到 `dist/embed`」，为客服浮标同源提供脚本。浮标删除后这一步一并删除；embed-sdk 本体仍在 `hivemtk/embed-sdk/`，供自托管者集成到自己的页面里。

### 2.4 端口与子路径对照表

> **单一源**：`vite.config.js` 的 `SITE_BASE='/hivemtk/'` + `server.port=8213`，
> 与 `dev-server.cjs` 的 `SITE_BASE` / `PORT` 必须字面一致；
> `index.html` 的 canonical/hreflang 前缀跟着同一字面量。

| 端口 | 服务 / 应用 | 启动入口 | 单一源 | 文档源 |
| --- | --- | --- | --- | --- |
| **8213** | **website**（Vite dev / dev-server.cjs） | `npm run vite-dev` 或 `npm run dev` | `vite.config.js server.port=8213` + `dev-server.cjs PORT=8213` | `vite.config.js` + `dev-server.cjs` |
| 8205 | platform-server（**可选本地组件**，官网不依赖） | `cd ../../hivemtk-platform/platform-server && go run cmd/api/main.go` | platform-server `config.DefaultServerPort` | platform-server/docs/dev/DEVELOPMENT.md §1.5 |
| 8204 | user-server（用户端本地后端，与官网无调用关系） | `cd ../user-server && go run ./cmd/api` | user-server `config.DefaultListenPort` | user-server/docs/dev/DEVELOPMENT.md §2.4 |

**约束**（禁软启动 / 禁多处硬编码）：

1. `SITE_BASE` 三处（`vite.config.js` / `dev-server.cjs` / `index.html` 的 canonical 前缀）必须字面一致；改 Pages 项目名要三处同改，`deploy.sh` 的产物门会当场验出漏改
2. `PORT=8213` 与 `vite.config.js` `server.port` 必须字面一致
3. 官网**不因任何后端端口而在启动前失败**：不存在"先把 platform-server / user-server 起起来"的前置条件

---

## 三、目录导航

`src/` 子目录作用速查：

| 子目录 | 作用 | 改动频率 |
| --- | --- | --- |
| `components/` | 11 个展示组件，全部使用 `<script setup>` Composition API，纯 SFC 无副作用 | 按需扩展 |
| `composables/` | 3 个组合式函数：`useSite`（i18n 包裹文案）、`useSiteContact`（静态联系信息）、`useToast` | 极少 |
| `config/` | `content.js` 集中所有页面文案、品牌、联系方式、Hero、卖点、FAQ、TechSpecs、部署、Footer、Nav 配置 | **高频**（运营改文案只改这里） |
| `i18n/` | `index.js` 入口 + `locale.js` 工具 + `modules/` 词条文件（`common.js` / `phrases.js` / `contentExtra*.js` / `docs*.js` / `disclaimer.js`） | 增加新语言时改 |
| `router/` | `index.js` 路由表 + SEO meta 动态注入（`afterEach`） | 新增页面时改 |
| `views/` | 8 个页面视图，每个文件对应一条路由，仅做"组件组装" | 新增页面时改 |
| `App.vue` | 根组件：Navbar + main(router-view) + Footer + BackToTop + ToastHost | 极少 |
| `main.js` | 入口：本地字体 `@fontsource/*`、`style.css`、App、router、i18n、`applyDirection` 初始化 | 极少 |
| `style.css` | 全局样式 + CSS 变量设计系统（编辑杂志风 × 印章红 × 现代科技） | 调整主题时改 |

> 历史上还有 `api/platform.js`（平台端 `/public/site/contact` 封装，处理超时 / 取消 / 非 JSON 防护），
> 随客服浮标一起删除；`src/` 现在没有任何网络调用层。

---

## 四、新增页面标准流程

以"新增 `/cases` 客户案例页"为例（项目当前无此页，仅作流程演示）：

### 步骤 1：注册路由

`src/router/index.js` `routes` 数组追加：

```js
{
  path: '/cases',
  name: 'Cases',
  component: () => import('../views/CasesPage.vue'),
  meta: {
    title: '客户案例',
    description: 'HiveMTK 客户案例...',
    keywords: 'HiveMTK,客户案例,...',
  },
},
```

> 必须：`meta.title` / `meta.description` / `meta.keywords` 三件套（`router.afterEach` 会自动注入 `<head>`）。`component` 使用动态 `import()` 实现路由懒加载，避免首屏包过大。

### 步骤 2：创建 view 文件

`src/views/CasesPage.vue`：

```vue
<script setup>
import PageHeader from '../components/PageHeader.vue'
import { useSite } from '../composables/useSite.js'
const { /* 从 content.js 导出对应配置 */ } = useSite()
</script>

<template>
  <div>
    <PageHeader
      :kicker="'客户案例'"
      :title="['两段标题，', '第二段渐变高亮']"
      :gradient-index="1"
      :subtitle="'副标题'"
      :breadcrumbs="[{ label: '首页', href: '/' }, { label: '客户案例' }]"
    />
    <!-- 页面内容 -->
  </div>
</template>
```

### 步骤 3：内容配置（content.js）

`src/config/content.js` 追加导出：

```js
export const casesSection = {
  tag: '客户案例',
  title: ['两段标题，', '第二段渐变高亮'],
  gradientIndex: 1,
  subtitle: '副标题',
  // 页面所需数据
}
```

然后在 view 文件中 `const { casesSection } = useSite()` 解构使用。**所有展示文案必须走 `content.js`**，禁止在组件中硬编码中文。

### 步骤 4：菜单注册

`src/config/content.js` 的 `nav.links` 数组追加：

```js
{ label: '客户案例', href: '/cases', type: 'route' },
```

> `type` 当前仅支持 `'route'`（外链请使用普通 `<a>`）。

### 步骤 5：sitemap 更新

`public/sitemap.xml` 追加 `<url>` 节点，host 必须是 Pages 地址，含 4 语言 `hreflang`：

```xml
<url>
  <loc>https://xiaofang142.github.io/hivemtk/cases</loc>
  <lastmod>2026-09-21</lastmod>
  <changefreq>monthly</changefreq>
  <priority>0.8</priority>
  <xhtml:link rel="alternate" hreflang="zh" href="https://xiaofang142.github.io/hivemtk/cases?lang=zh" />
  <xhtml:link rel="alternate" hreflang="en" href="https://xiaofang142.github.io/hivemtk/cases?lang=en" />
  <xhtml:link rel="alternate" hreflang="ja" href="https://xiaofang142.github.io/hivemtk/cases?lang=ja" />
  <xhtml:link rel="alternate" hreflang="ar" href="https://xiaofang142.github.io/hivemtk/cases?lang=ar" />
  <xhtml:link rel="alternate" hreflang="x-default" href="https://xiaofang142.github.io/hivemtk/cases" />
</url>
```

> 深链壳文件不用手工建：`postbuild.mjs` 从路由表现场解析并铺 `dist/<route>/index.html`。
> 但 **sitemap 与路由表是两份清单**：漏加 `<url>` 不会有任何构建错误，只会让新页静默失去收录。
> `deploy.sh` 的产物门会核对「每条静态路由 + 每个 sitemap URL 都有对应壳」，方向是壳←URL，
> 反方向（路由有、sitemap 无）它拦不住，只能靠这一步。

### 步骤 6：i18n 翻译（如需）

参考 §五"新增多语言文案流程"，把新增中文键加入 `phrases.js` 各语言子对象。

### 步骤 7：自测清单

- [ ] `npm run dev` 访问 `/pricing` 页面渲染正常
- [ ] 切换 4 语言后文案正确翻译
- [ ] 浏览器开发者工具检查 `document.title` / `meta description` / `meta keywords` / `og:*` / `twitter:*` / `link rel=canonical` 均已注入
- [ ] 移动端响应式无横向滚动
- [ ] `npm run build` 无报错，`dist/` 包含 `index.html` + `assets/` + `embed/` + `favicon.svg`
- [ ] `npm run lint` 无错误

---

## 五、新增多语言文案流程

### 5.1 词条文件结构

`src/i18n/modules/*.js` 每个文件默认导出 `{ zh, en, ja, ar }` 四语言对象，键为中文原文，值为对应语言译文。

```js
// src/i18n/modules/phrases.js
export default {
  zh: {
    'Docker 部署': 'Docker 部署',
    '快速开始': '快速开始',
    // ...
  },
  en: {
    'Docker 部署': 'Docker Deployment',
    '快速开始': 'Quick Start',
    // ...
  },
  ja: { /* ... */ },
  ar: { /* ... */ },
}
```

### 5.2 新增词条

1. **首选**：直接在 `content.js` 中使用中文文案。`useSite.js` 会递归调用 `i18n.global.t(node)`，对未收录的中文键，回退显示中文（保证页面不空白）。
2. 若需要其他语言翻译，把中文键加入 `phrases.js`（业务文案）或 `common.js`（通用词条），在 4 个语言子对象中补齐译文。
3. **无需修改 `i18n/index.js`**：`import.meta.glob('./modules/*.js', { eager: true })` 自动收集所有模块。
4. 若新增独立词条分类（如 `legal.js`），按相同结构 `export default { zh, en, ja, ar }`，放入 `modules/` 目录即可自动合并。

### 5.3 切换语言入口

`Navbar.vue` 顶部下拉菜单调用：
```js
import { LANGS, setStoredLocale, applyDirection } from '@/i18n/locale.js'
function changeLocale(code) {
  locale.value = code
  setStoredLocale(code)
  applyDirection(code)  // 阿语自动 RTL
}
```

持久化在 `localStorage['website-locale']`，刷新后保留。

### 5.4 阿拉伯语 RTL 适配

`applyDirection('ar')` 会设置 `<html dir="rtl" lang="ar">`。CSS 需使用逻辑属性（`margin-inline-start` / `padding-inline-end` 等）或针对 `[dir="rtl"]` 写覆盖规则。`style.css` 已全局支持 `text-align: inherit` 等基础 RTL 行为，复杂布局组件需自查。

---

## 六、内容编辑流程（content.js 配置驱动）

`src/config/content.js` 是全站文案的唯一来源（source-of-truth）。组件只做渲染，不做内容决策。

### 6.1 顶层导出速查

| 导出名 | 用途 | 主要消费方 |
| --- | --- | --- |
| `brand` | 品牌名、全称、slogan、Logo 文本 | Navbar、Footer、Hero |
| `contact` | 静态联系信息（微信、邮箱、电话、二维码路径） | useSiteContact 兜底、Footer、Deploy |
| `chat` | 客服 iframe 配置（url、title） | CustomerServiceWidget |
| `experience` | 在线体验地址、账号密码 | DeployPage、README |
| `hero` | Hero 首屏（eyebrow、title、description、CTA、stats、visualCards、differentiators） | HeroSection |
| `whyHiveMTK` | 六大卖点（pillars）+ 本地 vs SaaS 对比表 | WhyHiveMTKSection |
| `featuresSection` | 11 项核心功能卡（icon/title/industries/pain/solution/special） | FeaturesSection |
| `toolchainSection` | 6 项工程能力 + 六层架构 + ReAct 示例 + 5 项保障 + 6 类工具分组 | ToolchainSection |
| `workflowSection` | 6 步工作流 | WorkflowSection |
| `faqSection` | 11 条 FAQ（q/a） | FaqPage |
| `techSpecs` | 8 项技术规格 | FeaturesPage/ToolchainPage |
| `deploySection` | 部署方式卡片 | DeployPage（部分） |
| `footer` | 页脚配置（resourceLinks、contactCol、copyright、ICP 等） | FooterSection（部分自有数据） |
| `nav` | 顶部导航（brand、links、cta） | Navbar |

### 6.2 字段命名约定

- **数组字符串**：title 字段统一为数组 `['第一段，', '第二段']`，配合 `gradientIndex` 指定哪段渐变高亮（如 `1` 表示第二段）
- **CTA 对象**：`{ text, href }` 或 `{ text, href, external }`，`external: true` 表示外链
- **stats / pillars / features / faqs**：统一为数组，每项为对象，字段名 `value` / `label` / `desc` / `sub` 等
- **icon**：直接内联 SVG 字符串（避免引入图标库依赖），由组件 `v-html` 渲染
- **industries**：行业列表，最多 3 项（features 卡片）

### 6.3 富文本规则

- 仅允许 HTML 内联在 `icon` 字段（SVG），其他字段保持纯文本
- 长描述使用 `\n` 分段时由组件处理换行
- 不支持 Markdown，需要换行/加粗时由组件模板控制

### 6.4 编辑流程

1. 修改 `content.js` 对应字段
2. `npm run vite-dev` 实时预览（改的是纯 JS 配置，HMR 直接生效）
3. 切换 4 语言验证译文（未翻译回退中文）
4. 提交前跑门禁：`node check_i18n.mjs`（要求 `MISSING_UNIQ=0`）+ `bash deploy.sh --preflight-only`

---

## 七、SEO 配置

### 7.1 路由 meta（核心）

每条路由必须配置 `meta.title` / `meta.description` / `meta.keywords`。`router/index.js` 的 `afterEach` 钩子会自动注入：

- `document.title` = `${meta.title} | HiveMTK · 私域 AI 营销操作系统`
- `<meta name="description">`
- `<meta name="keywords">`
- `<meta property="og:title">` / `<meta property="og:description">` / `<meta property="og:url">`
- `<meta name="twitter:title">` / `<meta name="twitter:description">`
- `<link rel="canonical">`

### 7.2 sitemap.xml

`public/sitemap.xml` 列出 7 个核心 URL（`/`、`/features`、`/toolchain`、`/workflow`、`/deploy`、`/docs`、`/faq`），每个 URL 含 4 语言 `hreflang` 与 `x-default`。新增页面时务必同步追加 `<url>` 节点。

`lastmod` 集中为 `2026-07-24`，`changefreq` 按内容更新频率：首页/部署/文档 `weekly`，功能页 `monthly`。
全部 `<loc>` / `hreflang` 的 host 固定为 `https://xiaofang142.github.io/hivemtk/`；换成别的 host
（例如自建域名或误写旧站域名）会被 `deploy.sh` 的旧域名门与深链门当场拦下。

### 7.3 robots.txt

```
User-agent: *
Allow: /

# HiveMTK 官网 sitemap
Sitemap: https://xiaofang142.github.io/hivemtk/sitemap.xml
```

### 7.4 meta 标签

`index.html` 中应包含基础 meta（lang、charset、viewport、og:site_name 等）。动态 meta 由路由钩子覆盖。如需添加全局 meta（如 `og:image`、`twitter:card`），可：
- 静态：在 `index.html` 直接写
- 动态：在 `router.afterEach` 中追加 `setMeta(...)` 调用

---

## 八、构建与部署

### 8.1 本地构建

```bash
cd hivemtk/website
npm run build
```

产物结构（`bash deploy.sh` 跑完后的 `dist/`，以实际清单为准）：
```
dist/
├── index.html              # SPA 入口
├── 404.html                # Pages 深链兜底（index.html 的副本）
├── favicon.svg
├── wechat.jpg              # public/ 资产（联系方式二维码）
├── robots.txt
├── sitemap.xml
├── assets/
│   ├── vendor-[hash].js    # vue + vue-router + vue-i18n
│   ├── [Page]-[hash].js    # 按路由懒加载的业务 chunk
│   └── index-[hash].css
├── features/index.html     # ↓ 每条静态路由一份 SPA 壳，让深链返回真 200
├── toolchain/index.html
├── workflow/index.html
├── faq/index.html
├── deploy/index.html
├── docs/index.html
└── download/index.html
```

### 8.2 发布到 GitHub Pages

唯一发布路径：推到 GitHub 远端 `upstream`（`xiaofang142/hivemtk`）的 `master`，
`.github/workflows/website-pages.yml` 命中 `website/**` 后自动构建上线。

```bash
cd hivemtk/website
bash deploy.sh            # 预检 + npm ci + 构建 + 六类产物门（与 CI 同一条命令）
cd ..
git status                # 确认只包含本次要发的改动
git add website           # 明确指定路径，不用 git add -A
git commit -m "..."       # 提交信息规范见仓库 CONTRIBUTING.md
git push upstream master  # 触发 website-pages workflow
```

> 只推 Gitee（remote `gitee-upstream`）**不会**更新官网：Pages workflow 只认 GitHub 的 push 事件。

前置一次性开关（否则 configure-pages 步骤直接报错）：

```bash
gh api -X POST repos/xiaofang142/hivemtk/pages -f build_type=workflow
```

`deploy.sh` 的六类产物门（`--verify-only` 可单跑已构建的 `dist/`）：
`base` 前缀、SPA 兜底（`404.html`）、深链 200 壳（路由表 × sitemap 双源交叉核对）、
站内裸绝对路径、旧域名回流、publicDir 资产（`wechat.jpg`）。

### 8.3 自托管静态站（可选）

产物对托管方无要求：把 `dist/` 交给任意静态服务器、开启 SPA fallback（nginx `try_files $uri $uri/ /index.html`）
即可，**无需反代任何 API**。唯一必须跟着改的是站点前缀：`vite.config.js` / `dev-server.cjs` 的 `SITE_BASE`
与 `index.html` 的 canonical/hreflang、`public/robots.txt`、`public/sitemap.xml` 里的 host 要一起换，
否则 Pages 版地址会被带偏。

---

## 九、调试技巧

### 9.1 联系信息 / 二维码不显示

官网没有任何后端可查，这类问题一律是配置或产物问题，按顺序查：

1. `content.js` 的 `contact` 字段是否为空串（空串时组件用 `v-if` 直接隐藏，不报错）
2. `wechatQrPath` 是否带 `${import.meta.env.BASE_URL}` 前缀；不带前缀时 Pages 上会解析到仓库根而 404
3. `dist/wechat.jpg` 是否存在：`bash deploy.sh --verify-only` 会当场红。曾因根 `.gitignore` 的裸 `*.jpg`
   模式命中 `website/public/wechat.jpg`，文件在本地有、在 CI 的 checkout 里没有
4. 深链页面上的资源是否 404：`index.html` 里的引用必须全部带 `SITE_BASE` 前缀，
   `deploy.sh` 的「站内绝对路径」门专拦裸 `/` 引用

### 9.2 i18n 翻译缺失

未翻译的中文键会回退显示中文（fallback 顺序：当前 locale → `en` → 原中文键）。如需查找缺失：
```bash
node check_i18n.mjs | tail -1   # TOTAL_LITERAL_KEYS=… DICT=… MISSING_UNIQ=0 才算绿
```

### 9.3 SEO meta 验证

打开浏览器开发者工具 → Elements → `<head>`，检查：
- `document.title` 是否为 `${meta.title} | HiveMTK · 私域 AI 营销操作系统`
- `meta[name="description"]` / `meta[name="keywords"]` 是否更新
- `link[rel="canonical"]` 是否指向当前 URL
- `meta[property="og:*"]` 是否完整

或用 Lighthouse SEO 审计一键检查。

### 9.4 路由 hash 定位

`router/index.js` 的 `scrollBehavior` 配置了 hash 锚点跳转（如 `/docs#docker-deploy`）。访问带 hash 的 URL 会自动滚动到对应 `<section id="...">` 上方 90px。

### 9.5 移动端调试

- Chrome DevTools 设备模拟：iPhone 12 / iPad / 自定义尺寸
- 移动端断点：`@media (max-width: 768px)`（见 `style.css`）
- 文档页侧边栏在 `<= 1024px` 自动切换为抽屉式
- 真机调试用 `npm run vite-dev`：它的 `server.host: true` 会监听局域网地址，手机访问
  `http://<本机IP>:8213/hivemtk/`（`/hivemtk/` 前缀不能省）。`dev-server.cjs` 写死
  `listen(PORT, '127.0.0.1')`，只服务本机。

### 9.6 构建产物验证

```bash
npm run build && npm run dev
# 访问 http://127.0.0.1:8213/hivemtk/
```

产物齐全性不要手工比对，交给门：

```bash
bash deploy.sh --verify-only    # 只校验现有 dist/
```

`dist/` 应包含：`index.html`、`404.html`、`favicon.svg`、`wechat.jpg`、`assets/`、`robots.txt`、
`sitemap.xml`，以及每条静态路由一个 `<route>/index.html`。如发现多份 `index-*.html`，说明 postbuild 清理失败，
检查 `scripts/postbuild.mjs`。

### 9.7 Vite 配置

`vite.config.js` 关键项：
- `base: SITE_BASE`（`'/hivemtk/'`）—— Pages 项目子路径，全站资源前缀的唯一来源
- `resolve.alias['@']` → `./src`（用 `@/...` 导入）
- `server.port: 8213` + `server.host: true`
- **无 `server.proxy`**：历史版本的 `/public` → `http://localhost:8205` 反代已随平台端调用链删除
- `build.sourcemap: false`（生产不输出 sourcemap）
- `build.chunkSizeWarningLimit: 1000`（避免 vendor chunk 警告）
- `rollupOptions.output.manualChunks` —— 把 vue 全家桶拆为 `vendor` chunk

---

最近更新日期: 2026-09-21
