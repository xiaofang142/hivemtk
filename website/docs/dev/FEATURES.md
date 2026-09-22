# website 功能清单

> **规则级别**: ⭐⭐ 项目级开发文档

本文档基于 `src/views/` 与 `src/router/index.js` 实际文件，列出 HiveMTK 官网所有页面、功能、引流入口、SEO 能力与统计数据。

关联文档：
- 项目总览：[../../README.md](../../README.md)
- 架构图：[./ARCHITECTURE.md](./ARCHITECTURE.md)
- 开发手册：[./DEVELOPMENT.md](./DEVELOPMENT.md)
- 代码规范：[./CONVENTIONS.md](./CONVENTIONS.md)
- 菜单规格：[../../MENU_SPEC.md](../../MENU_SPEC.md)（页面内容规格详细清单）

---

## 一、页面与视图清单

官网共 8 个视图（7 个主路由 + 1 个 404），全部由 `src/router/index.js` 注册：

| 功能 | 路由路径 | 主要视图 | 说明 |
| --- | --- | --- | --- |
| 首页 | `/` | `views/HomePage.vue` | 5 区块聚合页：HeroSection + WhyHiveMTKSection + FeaturesSection + ArchitectureDiagram + ToolchainSection + WorkflowSection。聚合核心卖点、11 项功能、6 项工程能力、6 步工作流 |
| 核心功能 | `/features` | `views/FeaturesPage.vue` | PageHeader + FeaturesSection（11 项功能卡）+ WhyHiveMTKSection（六大卖点 + 本地 vs SaaS 对比表） |
| 工程能力 | `/toolchain` | `views/ToolchainPage.vue` | PageHeader + ArchitectureDiagram（六层架构图）+ ToolchainSection（6 工程能力 + ReAct 示例 + 5 保障 + 6 类工具分组）+ WhyHiveMTKSection |
| 业务流程 | `/workflow` | `views/WorkflowPage.vue` | PageHeader + WorkflowSection（6 步流程：线索接入 → AI 谈单 → SOP 推进 → 内容生成 → 人工成交 → 复购激活） |
| 常见问题 | `/faq` | `views/FaqPage.vue` | 11 条 FAQ 问答列表（编辑杂志风），首个默认展开，支持点击展开/收起 |
| 部署指南 | `/deploy` | `views/DeployPage.vue` | 3 部署方式卡（Docker/源码/文档）+ 命令 Tab 切换 + 公开提示卡 + 联系作者卡（含微信号复制） |
| 安装文档 | `/docs` | `views/DocsPage.vue` | 12 节长文档 + 左侧锚点侧边栏（4 分组：入门/部署/使用/运维），滚动高亮 + 移动端抽屉式目录 |
| 页面未找到 | `/:pathMatch(.*)*` | `views/NotFoundPage.vue` | 404 页面，大字号 404 渐变文字 + 3 个跳转按钮（首页/文档/部署） |
| 下载（历史兼容） | `/download` | （重定向） | 302 重定向到 `/deploy`（开源版无版本下载，仅源码仓库） |

> 视图组件共 8 个文件，详见 [src/views/](../../src/views/) 目录。

---

## 二、首页功能详情

### 2.1 Hero 区（HeroSection.vue）

| 子区域 | 内容 | 数据源 |
| --- | --- | --- |
| eyebrow 标签 | 2 个：`AGPL-3.0 开源 · 私有化部署`（primary）+ `七端打透 · AI 真自主`（accent） | `hero.eyebrow` |
| 主标题 | 两段拼接，第二段渐变高亮：`让 AI 替销售,` / `从接待到逼单` | `hero.title` + `gradientIndex: 1` |
| 副描述 | 项目核心定位说明（150+ 字） | `hero.description` |
| 主 CTA | 「立即部署」→ `/deploy` | `hero.primaryCta` |
| 次 CTA | 「了解核心功能」→ `/features` | `hero.secondaryCta` |
| 源码 CTA | 「查看源码」→ `https://github.com/xiaofang142/hivemtk`（外链，`target="_blank"`） | `hero.sourceCta` |
| 4 个数据指标 | `10+` 社媒渠道打透 / `5` Telegram 核心能力 / `42` 智能体工具 / `94` 业务模块 | `hero.stats` |
| 4 张可视化卡 | AI 自动谈单 / 多账号聚合 / 销冠 SOP 智能体 / 客户 CDP | `hero.visualCards` |
| 三大差异化 | 不是管理工具 / 不是玩具 Demo / 不是 SaaS 黑盒 | `hero.differentiators` |

### 2.2 WhyHiveMTK 区（WhyHiveMTKSection.vue）—— 六大卖点

| # | key | 标题 | 数据指标 |
| --- | --- | --- | --- |
| 1 | 私域 | 完全私有化部署 | `100%` 资产自有 |
| 2 | 本地模型 | 本地 LLM 推理，不依赖云端 | `0` 次云端调用 |
| 3 | 本地数据库 | 数据落在你自己的数据库 | `0` 字节外传 |
| 4 | 数据安全 | 零出域，数据封死在域内 | `5 层` 工具调用防护链 |
| 5 | Token 节省 | 多级缓存 + 本地模型，高频问题零 Token | `0 元` 本地推理边际成本 |
| 6 | 全自动 | ReAct 智能体真自主决策 | `42` 自主工具 |

> 同区块还包含「本地部署 vs SaaS 对比表」（6 行：数据存储/AI 模型/Token 成本/数据安全/定制自由/部署门槛）。

### 2.3 FeaturesSection 区（首页 + /features 共用）

11 项核心功能卡：

| # | 功能名 | 适用行业 |
| --- | --- | --- |
| 1 | 多账号聚合中枢 | 招商加盟 / 医美连锁 / 教育培训 |
| 2 | AI 自动谈单引擎 | 高客单服务 / B2B 销售 / 本地生活 |
| 3 | 销冠 SOP 智能体 | 保险经纪 / 房产中介 / 家居定制 |
| 4 | 客户 CDP / 360° 画像 | 耐消零售 / 汽车后市场 / 健康管理 |
| 5 | 销售意向识别与打分 | 在线教培 / 家装建材 / 金融理财 |
| 6 | 全渠道触达引擎 | 电商品牌 / 内容创作者 / 跨境出海 |
| 7 | 私域自动化运营 | 快消品牌 / 母婴亲子 / 美业门店 |
| 8 | 沉睡客户激活引擎 | 会员制零售 / 医疗美容 / 健身教育 |
| 9 | 统一话术与素材中心 | 连锁门店 / 教培机构 / 医美集团 |
| 10 | 数据驾驶舱 | 集团总部 / 区域代理 / 品牌方 |
| 11 | 权限控制与合规 | 金融 / 医疗 / 政企 |

每项含 `icon` / `title` / `industries` / `pain` / `solution` / `special` 字段。

### 2.4 ArchitectureDiagram 区（首页 + /toolchain 共用）

「六层协同架构」图示：

| 层级 | 角色 | 说明 |
| --- | --- | --- |
| 1 | 接入与渠道层 | 7 类适配器统一入站，Webhook/WebSocket → InboxIngressService |
| 2 | 智能体运行时 | AgentRuntime 事件订阅 + AgentContext 加载 + 消息路由 |
| 3 | 智能体引擎 | InferenceCycle：感知 → 对齐 → 门禁 → 规划 → Bridge 执行 |
| 4 | 工具注册表 | ToolRouter 统一路由，41 工具 + 限流/重试/审计/计费装饰器链 |
| 5 | 记忆系统 | L1 短期 / L2 长期 / L3 SOP / L4 业务 四层记忆 |
| 6 | AI 算力底座 | Embedding + pgvector + RAG 引擎 + LLM 调度 + 故障切换 |

含 ReAct 智能体编排代码示例（`InferenceCycle.RunOnce` 主编排骨架）。

### 2.5 ToolchainSection 区（首页 + /toolchain 共用）

6 项工程能力卡：

| # | 能力名 | 描述 |
| --- | --- | --- |
| 1 | 消息中台 MQ | 自研消息中间件，聚合多账号消息，聚合时延 < 3s |
| 2 | 大模型路由网关 | 按场景动态路由 DeepSeek / 通义千问 / GPT-4o / 智谱 GLM |
| 3 | 私域自动化引擎 | SOP 编排、定时触达、条件分支、A/B 测试 |
| 4 | 客户 CDP | OneID 归并、360° 画像、RFM 分层与意向打分 |
| 5 | 数据驾驶舱 | 全链路指标看板与异常预警，决策周期从周级缩短到分钟级 |
| 6 | 可观测 Pipeline | 触达全链路 TraceID，限流/重试/降级/审计/计费 9 步保障 |

5 项工程保障：全链路 TraceID / 限流重试降级 / 审计与计费 / 私有化部署 / 水平扩展。

6 类工具分组：消息中台（3 工具）/ 大模型路由（3）/ 私域自动化（4）/ 客户 CDP（3）/ 数据驾驶舱（3）/ 可观测 Pipeline（3），共 19 个工具示例。

### 2.6 WorkflowSection 区（首页 + /workflow 共用）

6 步业务流程：

| # | 步骤 | 描述 |
| --- | --- | --- |
| 1 | 线索接入与识别 | 多渠道线索统一接入，AI 实时识别意向等级并自动打标签、更新画像 |
| 2 | AI 自动谈单 | 智能体 7×24 承接咨询，完成寒暄、探需、异议处理与逼单邀约 |
| 3 | SOP 智能推进 | 按客户阶段自动执行对应 SOP 分支，把普通销售带入销冠节奏 |
| 4 | 内容自动生成 | 销冠人设的朋友圈文案、海报、短视频脚本一键生成并定时发布 |
| 5 | 人工介入成交 | 高意向客户由真人销售 1v1 推进成交，中低意向客户进入自动培育池 |
| 6 | 复购与激活 | 沉睡客户触发自动激活 SOP，成交客户进入复购旅程，形成增长闭环 |

---

## 三、文档页功能（/docs）

`views/DocsPage.vue` 含 12 节长文档 + 左侧锚点侧边栏（4 分组）。

| 分组 | 锚点 ID | 标题 |
| --- | --- | --- |
| 入门 | `#quickstart` | 快速开始 |
| 入门 | `#architecture` | 架构与分工 |
| 入门 | `#requirements` | 系统要求 |
| 部署 | `#docker-deploy` | Docker 部署（推荐） |
| 部署 | `#source-deploy` | 源码部署 |
| 部署 | `#frp-deploy` | FRP 私域穿透 |
| 部署 | `#config` | 配置说明 |
| 使用 | `#modules` | 功能模块（25 张模块卡） |
| 使用 | `#auto-reply` | 自动回复配置 |
| 使用 | `#rag` | RAG 知识库 |
| 运维 | `#troubleshoot` | 故障排查 |
| 运维 | `#faq` | 常见问题（7 条部署 FAQ） |

功能特性：
- 左侧 sticky 侧边栏，按 4 分组渲染 12 锚点
- 滚动监听 `activeSection` 自动高亮当前节
- 移动端（`<= 1024px`）切换为抽屉式目录，「查看目录/关闭目录」按钮切换
- 路由 hash 直接定位：`/docs#docker-deploy` 自动滚动到对应章节
- 25 张模块卡覆盖 62 业务模块：邮件营销 / 短信营销 / 多平台卡片 / 社群管理 / 短链活码 / 线索与客户 / 数据分析 / 内容创作 / 系统管理 / 团队协作 / AI Agent 智能体 / LLM 路由网关 / 客服会话 / 坐席看板 / 销冠 SOP / 客户 CDP / 标签分层 / 触达运营 / RAG 知识库 / 模型计量 / 资产包市场 / 用户黑名单 / 心跳与安装 / 嵌入式聊天窗 / 数据驾驶舱

---

## 四、FAQ 页功能（/faq）

`views/FaqPage.vue` 渲染 11 条 FAQ：

| # | 问题 |
| --- | --- |
| 1 | 和传统 SCRM / 企微 SaaS 有什么区别？ |
| 2 | 支持哪些大模型？ |
| 3 | 数据安全如何保障？ |
| 4 | 需要技术团队才能部署吗？ |
| 5 | 七端是哪七端？ |
| 6 | 和现有 CRM / 订单系统能打通吗？ |
| 7 | AI 自动回复会不会被平台封号？ |
| 8 | 开源协议是什么？商用有什么限制？ |
| 9 | 如何开始使用？ |
| 10 | 支持多语言 / 出海业务吗？ |
| 11 | 后续会持续更新吗？ |

功能特性：
- 编辑杂志风章节头（杂志顶栏 + tag-line + 大字标题 + 计数器）
- 手风琴式展开/收起，首个 FAQ 默认展开
- 大字号编号（01-11）+ 印章红主题色
- 响应式：移动端紧凑布局

> 注：当前 FAQ 列表为静态展示，未提供分类筛选（数据量 11 条无需筛选）。如未来扩展到 30+ 条，可在 `faqSection` 增加 `categories` 字段并启用分类 tab。

---

## 五、部署页功能（/deploy）

`views/DeployPage.vue`：

| 子区域 | 内容 |
| --- | --- |
| Hero 区 | 标签「部署指南」+ 标题「5 步完成部署 / 开箱即用」+ AGPL-3.0 开源澄清提示 |
| 部署方式卡（3 张，可点选切换） | Docker 一键部署（推荐）/ 源码部署 / 完整安装文档 |
| 命令片段区 | Tab 切换 Docker / 源码，展示对应 shell 片段，「复制」按钮写剪贴板 |
| 公开提示卡 | AGPL-3.0 开源，零授权门槛；源码与文档在 GitHub / Gitee 公开托管 |
| 联系作者卡 | 微信号（`useSiteContact` 动态加载）+「复制微信号」按钮 + 1v1 部署指导 |

Docker 命令片段关键步骤：
1. 克隆开源仓库 `git clone https://gitee.com/xhpmayun/hivemtk.git`
2. 准备环境变量 `cp .env-example .env`
3. 一键安装与启动 `make install` + `make up`
4. 访问 `http://localhost:8204`（默认账号 `admin` + `.env` 中的 `PLATFORM_ADMIN_PASSWORD`）

源码命令片段关键步骤：
1. 克隆仓库
2. 启动后端 `cd user-server && go build` → 端口 8204
3. 启动前端 `cd user-web && npm run dev` → 端口 5173

---

## 六、多语言切换功能

| 功能 | 说明 |
| --- | --- |
| 支持语言 | 4 种：简体中文 (`zh`) / English (`en`) / 日本語 (`ja`) / العربية (`ar`) |
| 切换入口 | `Navbar.vue` 顶部下拉菜单 |
| 持久化 | `localStorage['website-locale']` |
| 自动检测 | 首次访问跟随 `navigator.language`（en/ja/ar 之一），默认 `zh` |
| RTL 适配 | 阿拉伯语自动设置 `<html dir="rtl" lang="ar">` |
| 回退策略 | 未翻译短语回退到英语（`fallbackLocale: 'en'`），再回退到中文原文键 |
| 词条文件 | 10 个（`src/i18n/modules/`）：`common.js` 通用 + `phrases.js` 业务 + `contentExtra.js`/`contentExtra2-5.js` 分批 + `docs.js`/`docs2.js` 文档页 + `disclaimer.js` 免责声明 |
| 词条总数 | 3444 条译文 = 861 个 (文件, key) 词条对 × 4 语言（跨文件去重后 859 个唯一 key）。2026-09-22 孤儿键清理前：3816 条 / 953 个唯一 key |
| 自动收集 | `import.meta.glob('./modules/*.js', { eager: true })`，新增语言模块无需改入口 |

---

## 七、SEO 功能

| 功能 | 实现位置 | 说明 |
| --- | --- | --- |
| 路由 meta 注入 | `src/router/index.js` `afterEach` | 每个路由的 `meta.title` / `meta.description` / `meta.keywords` 动态注入 `<head>` |
| `document.title` | `router.afterEach` | 格式：`${meta.title} \| HiveMTK · 私域 AI 营销操作系统` |
| meta description | `setMeta('name', 'description', ...)` | 100-200 字页面描述 |
| meta keywords | `setMeta('name', 'keywords', ...)` | 逗号分隔关键词 |
| Open Graph | `setMeta('property', 'og:title/og:description/og:url', ...)` | 社交分享卡片 |
| Twitter Card | `setMeta('name', 'twitter:title/twitter:description', ...)` | Twitter 分享 |
| canonical | `setCanonical(url)` | 每页唯一规范 URL |
| sitemap.xml | `public/sitemap.xml` | 7 URL × 4 hreflang + x-default，含 lastmod / changefreq / priority |
| robots.txt | `public/robots.txt` | `User-agent: *` / `Allow: /` / 指向 sitemap |
| favicon | `public/favicon.svg` | 站点图标，postbuild 脚本确保存在 |
| hreflang | sitemap.xml + `<html lang>` | 4 语言 alternate 链接，避免重复内容惩罚 |

sitemap 覆盖 7 个核心 URL（lastmod 集中为 2026-07-24）：

| URL | priority | changefreq |
| --- | --- | --- |
| `/` | 1.0 | weekly |
| `/features` | 0.9 | monthly |
| `/toolchain` | 0.8 | monthly |
| `/workflow` | 0.8 | monthly |
| `/deploy` | 0.9 | weekly |
| `/docs` | 0.9 | weekly |
| `/faq` | 0.7 | monthly |

---

## 八、引流入口功能

官网作为引流入口，所有引流渠道**必须**完整可用：

| 引流入口 | 出现位置 | 数量 | 数据源 |
| --- | --- | --- | --- |
| GitHub 仓库 | Navbar（桌面 + 移动）、Footer 顶部 `repoLinks`、Footer 底部 `openSourceLinks` | 3 处 | 硬编码 `https://github.com/xiaofang142/hivemtk` |
| Gitee 仓库 | Navbar（桌面 + 移动）、Footer 顶部 `repoLinks`、DeployPage 部署方式卡、DocsPage 安装文档 | 4+ 处 | 硬编码 `https://gitee.com/xhpmayun/hivemtk` |
| 微信二维码 | Footer 第 4 列 | 1 处 | `useSiteContact.wechatQrURL` ← `content.contact.wechatQrPath` |
| 「复制微信号」按钮 | DeployPage 联系作者卡 | 1 处 | `navigator.clipboard.writeText(wechatId)` + Toast 反馈 |
| 邮箱 | Footer 第 4 列（条件渲染） | 1 处 | `useSiteContact.email`，空字符串自动隐藏 |
| 电话 | Footer 第 4 列（条件渲染） | 1 处 | `useSiteContact.phone`，空字符串自动隐藏 |
| AGPL-3.0 协议外链 | Footer 第 3 列 `openSourceLinks` | 1 处 | `https://gitee.com/xhpmayun/hivemtk/raw/master/LICENSE` |
| 贡献指南外链 | Footer 第 3 列 `openSourceLinks` | 1 处 | `https://gitee.com/xhpmayun/hivemtk/blob/master/CONTRIBUTING.md` |
| 免责声明外链 | Footer 第 3 列 `openSourceLinks` | 1 处 | `https://gitee.com/xhpmayun/hivemtk/blob/master/DISCLAIMER.md` |
| 源码入口 | HomePage Hero `sourceCta` | 1 处 | `content.hero.sourceCta`（GitHub 仓库外链） |

引流入口数据源：**只有 `content.js` 一层**。历史版本的「平台 API 覆盖 + 占位值探测 + `VITE_CHAT_URL` /
`VITE_API_BASE_URL` 环境变量」三条动态通路，随客服浮标 `CustomerServiceWidget.vue` 与 `src/api/platform.js`
一起删除（承载它们的服务器已停服，保留即死链）；改联系方式＝改 `content.js` 后 push，见
[./ARCHITECTURE.md](./ARCHITECTURE.md) §五。

---

## 九、总计统计

| 维度 | 数量 | 说明 |
| --- | --- | --- |
| 页面视图文件 | 8 个 | `HomePage / FeaturesPage / ToolchainPage / WorkflowPage / FaqPage / DeployPage / DocsPage / NotFoundPage` |
| 注册路由 | 9 条 | 7 主路由 + 1 重定向（`/download` → `/deploy`）+ 1 通配 404 |
| 展示组件 | 11 个 | `ArchitectureDiagram / BackToTop / FeaturesSection / FooterSection / HeroSection / Navbar / PageHeader / ToastHost / ToolchainSection / WhyHiveMTKSection / WorkflowSection` |
| 组合式函数 | 3 个 | `useSite / useSiteContact / useToast` |
| i18n 词条 key | 908 站内字面量（按文件重复计数）/ 859 词典键 | `node check_i18n.mjs` 实跑输出口径（`TOTAL_LITERAL_KEYS=908 DICT=859 MISSING_UNIQ=0`，2026-09-22 孤儿键清理后；清理前 `DICT=952`） |
| i18n 译文 | 3444 条 | 861 个 (文件, key) 词条对 × 4 语言，逐条由 `src/i18n/modules/*.js` import 后实测计数（清理前 955 对 / 3816 条） |
| 引流入口 | 8 类 | GitHub ×3 + Gitee ×4+ + 微信二维码 ×1 + 复制微信号 ×1 + 邮箱 ×1 + 电话 ×1 + 协议/贡献/免责 ×3 + 源码入口 ×1 |
| sitemap URL | 7 个 | `/` / `/features` / `/toolchain` / `/workflow` / `/deploy` / `/docs` / `/faq` |
| SEO meta 字段 | 8 个/页 | title + description + keywords + og:title + og:description + og:url + twitter:title + twitter:description + canonical |
| 核心功能卡 | 13 项 | featuresSection.features |
| 工程能力卡 | 6 项 | toolchainSection.toolchain |
| 工作流步骤 | 6 步 | workflowSection.steps |
| FAQ 条目 | 12 条 | faqSection.faqs |
| 六大卖点 | 6 项 | whyHiveMTK.pillars |
| 文档章节 | 12 节 | DocsPage 12 个 `<section id="...">` |
| 文档模块卡 | 25 张 | DocsPage `#modules` 区覆盖 62 业务模块 |

---

最近更新日期: 2026-07-26
