# website 代码规范

> **规则级别**: ⭐⭐ 项目级开发文档

本文档规定 `hivemtk/website` 工程的命名、目录、Vue 组件、内容配置、多语言、样式、SEO、引流入口与禁止清单。所有 PR 必须满足本规范。

关联文档：
- 项目总览：[../../README.md](../../README.md)
- 架构图：[./ARCHITECTURE.md](./ARCHITECTURE.md)
- 开发手册：[./DEVELOPMENT.md](./DEVELOPMENT.md)
- 功能清单：[./FEATURES.md](./FEATURES.md)
- 术语规范：[../../TERMINOLOGY.md](../../TERMINOLOGY.md)（最高优先级，冲突时以项目级词典为准）

---

## 一、命名规范

### 1.1 组件名

- **必须**使用 PascalCase：`HeroSection.vue` / `FooterSection.vue` / `FeaturesSection.vue`
- 文件名与导出的组件名一致：`FeaturesSection.vue` → `<FeaturesSection />`
- 组件名采用"功能 + 类型"后缀：`*Section`（区块）/ `*Widget`（浮标）/ `*Host`（容器）/ `*Diagram`（图示）/ `*Header`（页头）

### 1.2 文件名

- Vue 单文件组件：`.vue`，PascalCase
- JavaScript 模块：`.js`，camelCase（`content.js` / `useSite.js` / `platform.js`）
- 配置脚本：`.cjs`（CommonJS）/ `.mjs`（ESM），保持当前后缀
- 文档：`.md`，全大写 + 下划线分隔（`ARCHITECTURE.md` / `MENU_SPEC.md`）
- **禁止**文件名带版本后缀：`Homepage_v2.vue` / `FeaturesPage-2026.vue` 等一律禁止

### 1.3 变量名

- camelCase：`activeSection` / `mobileNavOpen` / `wechatId`
- 常量：全大写 + 下划线：`SITE` / `HOP_BY_HOP` / `STORAGE_KEY` / `PORT` / `MIME` / `LANGS`
- 私有变量：前缀 `_` 不强制，使用闭包作用域控制即可
- ref / computed 命名：`const openIndex = ref(0)` / `const groupedSections = computed(...)`

### 1.4 路由 name

- 单个单词，PascalCase：`Home` / `Features` / `Toolchain` / `Workflow` / `Faq` / `Deploy` / `Docs` / `NotFound`
- 与视图文件名对应：`FeaturesPage.vue` → `name: 'Features'`

### 1.5 路由 path

- 全小写 + 短横线分隔（如有多个单词）：`/` / `/features` / `/toolchain` / `/deploy` / `/docs`
- 历史兼容路径（如 `/download`）使用 `redirect` 字段，**不**新增视图

---

## 二、目录规范

### 2.1 目录结构

```
src/
├── api/           # API 调用封装（每端一个文件）
├── components/    # 展示组件（无路由，纯 SFC）
├── composables/   # 组合式函数（use*.js）
├── config/        # 内容配置（content.js）
├── i18n/          # 国际化（index.js / locale.js / modules/）
├── router/        # 路由配置
└── views/         # 页面视图（与路由一一对应）
```

### 2.2 文件归属规则

| 文件类型 | 归属目录 | 禁止归属 |
| --- | --- | --- |
| 页面视图 | `views/` | `components/` |
| 可复用展示组件 | `components/` | `views/` |
| 组合式函数 | `composables/` | `utils/`（不创建该目录） |
| API 调用 | `api/` | `services/`（不创建） |
| 内容配置 | `config/` | 散落在组件中 |
| 词条文件 | `i18n/modules/` | 顶层 `src/` |

### 2.3 导入路径

- 优先使用 `@/` 别名（指向 `./src`）：`import { useSite } from '@/composables/useSite.js'`
- 相对路径用于同级或子目录：`import HeroSection from '../components/HeroSection.vue'`
- **禁止**使用绝对路径 `/Users/.../src/...`

---

## 三、Vue 组件规范

### 3.1 Composition API + `<script setup>`

所有组件**必须**使用 `<script setup>` 语法，禁止 Options API。

```vue
<script setup>
import { ref, computed, onMounted } from 'vue'
import { useSite } from '@/composables/useSite.js'

const { hero } = useSite()
const openIndex = ref(0)
const isActive = computed(() => openIndex.value === 0)

onMounted(() => {
  // 初始化逻辑
})
</script>
```

### 3.2 组件顺序

`<script setup>` → `<template>` → `<style scoped>`（如有）。空行分隔各块。

### 3.3 响应式

- `ref` 用于基本类型与对象引用，访问需 `.value`（模板自动解包）
- `computed` 用于派生状态，必须返回值
- `watch` 显式声明依赖，避免深层监听 `deep: true`（性能开销大）
- 事件总线**禁止**使用，改用 props/emit 或 composables

### 3.4 Props 与 Emit

- props 必须声明类型与默认值（如 `defineProps({ kicker: { type: String, required: true } })`）
- emit 必须声明事件名（`defineEmits(['change', 'close'])`）
- 不允许隐式继承 attrs 透传到根元素，必要时 `inheritAttrs: false`

### 3.5 副作用与生命周期

- DOM 操作必须在 `onMounted` 之后
- 全局事件监听器（`window.addEventListener`）必须在 `onUnmounted` 清理
- 定时器（`setTimeout` / `setInterval`）必须成对清除（参考 `useToast.js`：新消息先 `clearTimeout` 旧句柄再重挂）

### 3.6 模板规范

- `v-for` 必须带 `:key`，且 key 唯一稳定（避免用 index）
- `v-if` 与 `v-for` 不允许同一元素（用 computed 过滤后再 v-for）
- `:class` 优先使用对象语法 `{ 'is-open': openIndex === idx }`
- `@click` 等事件处理器优先使用 named function，复杂逻辑用 inline arrow

---

## 四、内容配置规范

`src/config/content.js` 是全站文案唯一来源。所有展示文案**必须**集中在此文件，禁止散落在组件中。

### 4.1 字段命名

- 顶层导出：camelCase，与组件/区块对应（`hero` / `featuresSection` / `toolchainSection` / `workflowSection` / `faqSection` / `techSpecs` / `deploySection` / `footer` / `nav`）
- 区块头部统一字段：`tag`（标签）/ `title`（数组）/ `gradientIndex`（哪段渐变）/ `subtitle`
- 列表字段：复数形式（`features` / `toolchain` / `steps` / `faqs` / `pillars` / `stats`）
- 单项字段：`icon` / `title` / `desc` / `pain` / `solution` / `special` / `industries`
- CTA 字段：`{ text, href }` 或 `{ text, href, external }`

### 4.2 必填项

| 区块 | 必填字段 |
| --- | --- |
| `hero` | `eyebrow` / `title` / `gradientIndex` / `description` / `primaryCta` / `secondaryCta` |
| `featuresSection` | `tag` / `title` / `gradientIndex` / `subtitle` / `features[]`（每项含 `icon` / `title` / `industries` / `pain` / `solution` / `special`） |
| `toolchainSection` | `tag` / `title` / `gradientIndex` / `subtitle` / `toolchain[]` |
| `workflowSection` | `tag` / `title` / `gradientIndex` / `subtitle` / `steps[]` |
| `faqSection` | `tag` / `title` / `gradientIndex` / `subtitle` / `faqs[]`（每项含 `q` / `a`） |
| `contact` | `wechatId`（必填，引流入口不可缺失）/ `wechatQrPath`（必填）/ `email`（可选）/ `phone`（可选） |
| `nav` | `brand` / `links[]`（每项含 `label` / `href` / `type: 'route'`）/ `cta` |

### 4.3 富文本规则

- **允许**：`icon` 字段内联 SVG 字符串（由组件 `v-html` 渲染）
- **禁止**：其他字段使用 HTML 标签（如 `<strong>` / `<br>`）
- 长文本使用 `\n` 分段，由组件 `white-space: pre-line` 处理换行
- **禁止**使用 Markdown 语法

### 4.4 联系信息（contact 字段）

- `wechatId`：必填，作者微信号（引流核心入口）
- `wechatQrPath`：必填，二维码图片路径。图片放 `public/` 下，值必须写成
  `` `${import.meta.env.BASE_URL}wechat.jpg` `` 而不是 `/wechat.jpg`——Pages 把本站挂在
  `/hivemtk/` 子路径下，裸绝对路径会解析到仓库根而 404
- `email` / `phone`：可选，空字符串 `''` 时组件自动隐藏

---

## 五、多语言规范

### 5.1 全员走 i18n

所有面向用户展示的文案**必须**走 i18n，**禁止**在组件模板中硬编码中文。

### 5.2 中文为键

`content.js` 以中文原文为键（source-of-truth），`useSite.js` 递归调用 `i18n.global.t(node)` 包裹。新增文案流程：

1. 在 `content.js` 直接用中文写文案
2. 如需其他语言翻译，把中文键加入 `i18n/modules/phrases.js`（业务文案）或 `common.js`（通用词条）
3. 4 个语言子对象（`zh` / `en` / `ja` / `ar`）必须补齐译文；vue-i18n 的取词顺序是
   当前语言 → `fallbackLocale: 'en'` → 原文键（即中文），所以缺 `en` 那一档才会真正露出中文

### 5.3 key 命名

- **不使用** `home.hero.title` 这种点分命名（项目以中文原文为键）
- key 即中文原文：`'立即部署'` / `'Docker 部署'` / `'切换语言'`
- 词典里**不存在英文 key**（2026-09-22 实测：唯一一条 `'language'` 无任何消费方，已随孤儿键清理删除；
  本节旧版举例的 `'close'` 从未作为词条存在过——`git log --all -S"'close':" -- website/src/i18n` 命中 0）

### 5.4 词条文件划分

| 文件 | 用途 | 示例 |
| --- | --- | --- |
| `common.js` | 通用 UI 词条（按钮、菜单、状态） | `'复制'` / `'返回首页'` / `'回到顶部'` |
| `phrases.js` | 业务/营销文案（页面正文） | `'Docker 部署'` / `'快速开始'` |
| `disclaimer.js` | 免责声明 | `'页脚免责声明'` |

> 词条只保留"站内确实在用"的键：2026-09-22 孤儿键清理后 `node check_i18n.mjs` 输出
> `DICT=859`、`MISSING_UNIQ=0`，即词典键与站内中文字面量一一对应
> （`TOTAL_LITERAL_KEYS=908` 是按文件重复计数的口径，同一个键在多个文件里各计一次）。
> 同一条命令还反向报 `ORPHAN_KEYS`（词典里有但没人 `t()`）与 `PARITY_INCOMPLETE`
> （某个 (文件,key) 没凑齐四语言），清单落在 `.i18n-orphans.jsonl` / `.i18n-parity.jsonl`。
> 这两项**只报告不判红**（发布前置仍只看 `MISSING_UNIQ=0`），但红了就得手工清——
> 往词典里塞没消费的键，下一次清理就会把它删掉。

### 5.5 阿拉伯语 RTL

- `applyDirection('ar')` 自动设置 `<html dir="rtl" lang="ar">`
- 新组件 CSS 优先使用逻辑属性（`margin-inline-start` / `text-align: start`）
- 复杂布局需针对 `[dir="rtl"]` 写覆盖规则

---

## 六、样式规范

### 6.1 全局样式

`src/style.css` 是全局样式与设计系统的唯一来源。包含：
- CSS 变量（`:root`）：背景层级 / 文字层级 / 品牌色 / 辅助色 / 功能色 / 边框 / 圆角 / 阴影 / 字体 / 间距 / 容器
- 全局重置（`*` / `body` / `a` / `button` / `img`）
- 容器与章节基础样式（`.container` / `.section` / `.section-header`）
- 标签样式（`.tag` / `.tag-primary` / `.tag-accent` / `.tag-ink` / `.tag-line`）
- 高亮文字（`.ink-mark` / `.ink-stroke` / `.gradient-text`）
- 按钮系统（`.btn` / `.btn-primary` / `.btn-ghost` / `.btn-outline` / `.btn-accent`）
- 卡片（`.card` / `.card-hover`）
- 装饰元素（`.seal` / `.numeral` / `.rule`）
- 无障碍（`.skip-link` / `:focus-visible`）
- 工具类（`.text-center` / `.text-muted` / `.mono` / `.serif`）

### 6.2 CSS 变量主题

主色调：印章红 `--primary: #C8392F`（HiveMTK 品牌主色），辅色：深青 `--accent: #0F4C5C`（技术沉稳）。

**禁止**在组件 `<style>` 中硬编码颜色值，必须使用 CSS 变量：

```css
/* ✅ 正确 */
.btn-primary {
  background: var(--primary);
  color: #fff;
}

/* ❌ 错误 */
.btn-primary {
  background: #C8392F;
  color: #fff;
}
```

### 6.3 scoped 与全局

- 组件样式**必须** `<style scoped>`，避免污染全局
- 需要影响子组件时使用 `:deep()` 选择器（如 `:deep(svg) { width: 16px; }`）
- 全局通用样式（`.btn` / `.card` / `.container`）写在 `style.css`，不重复定义

### 6.4 响应式断点

统一断点（与 `style.css` 一致）：

| 断点 | 触发条件 | 用途 |
| --- | --- | --- |
| `1024px` | `max-width: 1024px` | 文档侧栏抽屉化、平板横屏 |
| `768px` | `max-width: 768px` | 移动端基础断点（章节间距、容器 padding 缩小） |
| `480px` | `max-width: 480px` | 小屏手机（FAQ 紧凑布局） |

**禁止**引入额外断点（如 `640px` / `992px`），保持一致。

### 6.5 字体

- 英文：`Fraunces`（display）/ `Manrope`（body）/ `JetBrains Mono`（mono），全部通过 `@fontsource/*` 本地加载，**禁止**使用 Google Fonts CDN
- 中文：走系统 fallback（`PingFang SC` / `Songti SC` / `Source Han` 系列）
- 字体变量：`--font-display` / `--font-body` / `--font-mono`

### 6.6 动画

- 默认 `transition: 0.2s ease`，hover 反馈不超过 `0.3s`
- 遵守 `prefers-reduced-motion`（`style.css` 已全局处理）
- **禁止**使用第三方动画库（如 `gsap` / `framer-motion`）

---

## 七、SEO 规范

### 7.1 每个页面必须有的 meta

每条路由的 `meta` 字段必须包含：
- `title`：页面标题（不带品牌后缀，由 `afterEach` 自动拼接 `| HiveMTK · 私域 AI 营销操作系统`）
- `description`：页面描述（100-200 字，含核心关键词）
- `keywords`：逗号分隔关键词列表

### 7.2 OG 与 Twitter Card

由 `router.afterEach` 自动注入：
- `og:title` / `og:description` / `og:url`
- `twitter:title` / `twitter:description`
- `link rel="canonical"`

如需 `og:image`，在 `index.html` 静态添加或在 `afterEach` 中扩展 `setMeta` 调用。

### 7.3 sitemap 更新

新增路由时**必须**同步更新 `public/sitemap.xml`：
- 追加 `<url>` 节点
- 包含 4 语言 `<xhtml:link rel="alternate" hreflang="...">`
- 设置合理的 `<lastmod>` / `<changefreq>` / `<priority>`

### 7.4 robots.txt

`public/robots.txt` 全站允许爬取，指向 sitemap：
```
User-agent: *
Allow: /
Sitemap: https://xiaofang142.github.io/hivemtk/sitemap.xml
```

**禁止**设置 `Disallow:` 限制核心页面。

---

## 八、引流入口规范

官网核心目标之一是引流，所有引流入口**不可缺失**：

### 8.1 GitHub 仓库

- URL：`https://github.com/xiaofang142/hivemtk`
- 出现位置：Navbar（桌面 + 移动）、FooterSection 顶部 `repoLinks`、FooterSection 底部 `openSourceLinks`
- 必须使用 `target="_blank" rel="noopener noreferrer"`

### 8.2 Gitee 仓库

- URL：`https://gitee.com/xhpmayun/hivemtk`
- 出现位置：Navbar（桌面 + 移动）、FooterSection 顶部 `repoLinks`、DeployPage 部署方式卡、DocsPage 安装文档正文
- 同样使用 `target="_blank" rel="noopener noreferrer"`

### 8.3 微信二维码

- 二维码图片：`public/wechat.jpg`（路径配置在 `contact.wechatQrPath`，渲染于 FooterSection 第 4 列，`v-if="wechatQrURL"`）
- 微信号：`contact.wechatId`（默认 `xiao142000`，唯一事实源是 `content.js`）
- 「复制微信号」按钮必须存在（DeployPage 联系作者卡），使用 `navigator.clipboard.writeText`

### 8.4 邮箱与电话

- 来自 `contact.email` / `contact.phone`
- 空字符串时组件自动隐藏（不显示空标签）
- 出现位置：FooterSection 第 4 列（条件渲染）

### 8.5 开源项目入口（Footer 第 3 列）

必须包含：
- AGPL-3.0 开源协议（外链 Gitee LICENSE）
- 贡献指南（外链 Gitee CONTRIBUTING.md）
- 免责声明（外链 Gitee DISCLAIMER.md）

**禁止**移除或合并到其他列。

### 8.6 联系信息数据源

`useSiteContact.js` 只读 `content.js` 的 `contact` 字段：站点发布到 Pages 后没有任何后端可查，
联系信息**只有一个事实源**。历史版本的「平台 API 优先 + 静态兜底」双源策略（含 9 条占位值模式探测）
随平台端降级一起删除。

**禁止**直接在组件中硬编码微信号 / 邮箱 / 电话。所有联系信息必须走 `useSiteContact`。

---

## 九、禁止清单

### 9.1 架构禁止

- **禁止**依赖 反向代理容器（项目已删除 `Dockerfile` / 反向代理层配置，唯一发布目标是 GitHub Pages）
- **禁止**依赖后端服务运行（`src/api/` 已整块删除；运行期零 XHR/fetch，详见 ARCHITECTURE.md §五）
- **禁止**引入 SSR/SSG（保持纯客户端 SPA）
- **禁止**引入状态管理库（无 Vuex/Pinia，使用 composables + 模块级单例）
- **禁止**引入 UI 组件库（如 Element Plus / Ant Design Vue），样式完全自研

### 9.2 文件与命名禁止

- **禁止**文件名带版本后缀：`Homepage_v2.vue` / `FeaturesPage-2026.vue`
- **禁止**使用 `temp` / `backup` / `old` / `new` 前缀（如 `old_Navbar.vue`），用 git 历史管理
- **禁止**在 `src/` 顶层散落文件（除了 `App.vue` / `main.js` / `style.css`）

### 9.3 内容禁止（详见 TERMINOLOGY.md）

- **禁止**使用「AI 私域销冠」「营销智能体套件」「销冠系统」等旧品牌名（统一为 `HiveMTK`）
- **禁止**使用「一键部署」「3 分钟安装」（统一为「4 步完成部署」，与 `content.js` 及 4 语言译文一致）
- **禁止**使用「立即试用」「免费注册」「开通账号」「获取授权码」CTA（仅允许「立即部署 / 克隆仓库 / 获取源码」）
- **禁止**使用「版本下载」「离线包」「安装包」（开源版仅 git clone）
- **禁止**使用「无 License」「无任何授权」暗示没有授权义务（统一为「基于 AGPL-3.0 开源」）；
  但「无商户授权 / 无授权码」是**允许且应当**的表述——产品已无商户授权环节，见 TERMINOLOGY.md §二
- **禁止**在 Footer 使用「版本下载 / 帮助中心 / 关于我们 / 博客」（仅允许「产品功能 / 快速导航 / 开源项目 / 开源仓库 / 联系作者」）

### 9.4 代码禁止

- **禁止**在组件中硬编码展示文案（必须走 `content.js`）
- **禁止**在组件中硬编码颜色（必须用 CSS 变量）
- **禁止**使用 Google Fonts CDN（必须用 `@fontsource/*` 本地加载）
- **禁止**使用 `console.log` 提交生产代码（debug 后必须删除）
- **禁止**在 `git add` 时使用 `git add -A` / `git add .`（应明确指定文件，避免提交 `.env` / 凭据）
- **禁止**引入新运行时依赖（当前仅 `vue` + `vue-router` + `vue-i18n` + `@fontsource/*`）

### 9.5 引流入口禁止

- **禁止**移除 GitHub / Gitee 仓库链接
- **禁止**移除微信二维码与「复制微信号」按钮
- **禁止**在 `contact.wechatId` 填写占位值（如 `your-wechat-id`）

### 9.6 术语检查命令

提交前在 `website/` 目录执行；它是**评审辅助命令**，命中要读上下文（否定式的「无授权码 / 无版本下载」是
正当宣传，`用户端` 是规范词），三类存量的逐条判读口径见
[../../TERMINOLOGY.md](../../TERMINOLOGY.md) §四。零命中硬门是仓根的 `scripts/check-no-xapptool.sh`（旧域名回流）。

```bash
cd website
grep -rn "用户端\|商家端\|客户端\|激活码\|免费注册\|一键部署\|授权码\|注册开户\|联系开通\|版本下载\|离线包\|安装包\|无 License\|无授权\|AI 私域销冠\|营销智能体套件\|获取商户标识\|前往下载" src/
```

发现禁用别名时必须修改后再合并。详见 [../../TERMINOLOGY.md](../../TERMINOLOGY.md)。

另有全仓防回流闸（在 `hivemtk/` 目录执行）拦截已下线线上域名字面量：

```bash
bash scripts/check-no-xapptool.sh   # 命中即非零退出
```

---

最近更新日期: 2026-07-26
