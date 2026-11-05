# GEO 智能优化模块使用指南

> **GEO（Generative Engine Optimization，生成式引擎优化）** 是针对 AI 搜索引擎（如 ChatGPT Search、Perplexity、Google SGE 等）的内容优化方法论。本模块将 GEO 全流程工具化，覆盖从关键词挖掘到内容发布闭环。

---

## 核心工作流

```
审计体检 → 品牌/产品六问 → 关键词蒸馏 → 内容创作 → 多模型验证 / 双端监测
   ↑                                                        ↓
   └──────── 月度复查对比 ←── 承接页（FAQ/留资/首响）←── 平台发布
```

---

## 功能模块

### 1. 关键词蒸馏 (`/geo-tools/keyword-mining`)

将种子词扩展为高价值、分类清晰、覆盖多意图的关键词集。

| 功能 | 说明 |
|------|------|
| **关键词挖掘** | 基于品牌 + 种子词，LLM 生成 20+ 个高价值关键词，覆盖对比/评测/购买等意图 |
| **语义扩展** | 基于现有关键词，LLM 生成同义/场景/长尾扩展词 |
| **话题聚类** | 将关键词按语义聚类为话题组，辅助内容规划 |

### 2. 内容创作 (`/geo-tools/content-creative`)

按关键词 + 品牌 + 优势生成 SEO/GEO 优化的文章。

| 功能 | 说明 |
|------|------|
| **AI 生成** | 支持品牌声音、字数、风格控制 |
| **内容评分** | 从结构完整性、品牌自然度、权威性、数据支撑四维度百分制评分 |
| **文章优化** | 基于品牌优势改写现有内容 |
| **E-E-A-T 增强** | 提升经验/专业/权威/可信信号 |
| **Schema 生成** | 自动生成 JSON-LD 结构化数据标记 |
| **独特性检测** | 检测内容重复率 |

### 3. 多模型验证 (`/geo-tools/verification`)

模拟多个 AI 搜索引擎验证品牌提及情况。

| 功能 | 说明 |
|------|------|
| **AI 搜索验证** | 模拟 ChatGPT/Perplexity/SGE 对指定查询的回答，检测品牌是否被提及 |
| **负面监控** | 自动生成负面查询，评估负面提及风险等级 |

### 4. 数据报表 (`/geo-tools/reports`)

查看 GEO 运营数据。

| 功能 | 说明 |
|------|------|
| **汇总报表** | 关键词数 / 文章数 / 验证次数 / API 调用量 |
| **ROI 分析** | 投入产出比（API 成本 vs 品牌曝光提升） |
| **API 成本** | 按 LLM 提供商 / 模型统计 Token 消耗与费用 |

### 5. 配置优化 (`/geo-tools/config`)

品牌信息与 LLM 配置管理。

| 功能 | 说明 |
|------|------|
| **品牌配置** | 品牌名 / 优势 / 竞品 / 域名 / 语言 |
| **配置优化** | LLM 分析当前配置，给出优化建议 |

### 6. 平台同步 (`/geo/platform/*`)

将内容发布到外部平台。

| 功能 | 说明 |
|------|------|
| **平台管理** | 当前 5 个平台定义：GitHub README / GitHub Blog / WordPress / 通用 Webhook / Medium（手动） |
| **真实发布** | GitHub（Contents API）、WordPress（XML-RPC `wp.newPost`）、通用 Webhook（JSON + 可选 HMAC）为 `real_api` |
| **手动发布** | Medium 无开放写接口，只提供手动复制；知乎/掘金/CSDN 等未内置，不做“假装已发布”的记录 |

账号配置：

- GitHub：AppKey = owner，Secret = Personal Access Token（repo 权限）
- WordPress：AppKey = 用户名，Secret = 应用密码，额外配置 `{"endpoint":"https://站点/xmlrpc.php"}`
- Webhook：Secret = 可选 HMAC 密钥，额外配置 `{"url":"https://接收端"}`

### 7. 审计中心 (`/geo-tools/audit-center`)

| 功能 | 说明 |
|------|------|
| **站点体检** | rubric v3（112 分，含产品定位/人群/代际差异/版本时效），支持单页或 sitemap 全站（默认 10 页、上限 20） |
| **内容体检** | 抓 URL 正文/meta/JSON-LD，跑 content-v2 因子并落库 |
| **复查对比** | 同 rubric 版本才可比；输出新增/修复/回退；报告可下载 HTML/CSV |
| **月度任务** | `site_audit` 每月 1 日跑启用站点，分数下降 > 8 写告警 |

### 8. 双端监测 (`/geo-tools/dual-engine`)

| 功能 | 说明 |
|------|------|
| **AI 端** | 探针次数/品牌提及/引用数，按引擎与 `mode`（simulated/real/manual）分列 |
| **Google 端** | 导入 GSC 性能 CSV（Top queries / Top pages），展示点击/展现/CTR/平均排名 |
| **口径** | AI 提及与 Google 点击不混算；无日期列标 `gsc_csv_undated`，不冒充精确日粒度 |

### 9. 承接链接 (`/geo-tools/capture-links`)

| 功能 | 说明 |
|------|------|
| **公开落地页** | `GET /api/public/geo/l/:code`，访客可看标题/关键词、去目标链接 |
| **FAQ** | `POST .../ask` 复用知识库 RAG；单链接每日 200 次上限 |
| **留资** | `POST .../lead` 写统一线索池（类型 18），单链接每日 100 条上限；有 OneID 时回填归因链 |
| **首响** | 按 `clues.one_id → customer_sessions` 统计；没有会话显示“未接入”，不展示零延迟 |

---

## 快捷入口（页面内导航）

| 页面 | 可跳转至 |
|------|----------|
| 关键词蒸馏 | 数据增强（历史关键词回流） |
| 内容创作 | 文章优化 / Schema 生成 / 独特性检测 |
| 多模型验证 | 负面监控 / 验证历史 |
| 配置优化 | 工作流模板 / 知识库管理 |

---

## 隐藏功能（通过 URL 直接访问）

| 路径 | 功能 |
|------|------|
| `/api/geo/workflow/workflows` | 工作流管理 |
| `/api/geo/kb/documents` | 知识库管理 |
| `/api/geo/resources/*` | GEO 资源推荐 |
| `/api/geo/keyword-enhance/*` | 关键词数据增强 |
| `/api/geo/techconfig/*` | robots.txt / sitemap.xml 生成 |

---

## API 速查

所有接口返回格式：`{ code: 0, data: ..., message: "ok" }`

### 配置

```
GET    /api/geo/config                获取配置
GET    /api/geo/reports/summary       GEO 汇总报表
GET    /api/geo/reports/roi           ROI 分析
GET    /api/geo/reports/api-costs     API 成本报表
```
> 注意：当前 `PUT /api/geo/config` 与 `POST /api/geo/config/optimize` 暂未在路由中开放；配置修改走 `geo_brand_configs` 表直接读写（见 `internal/geo/repository/config.go`）。

### 关键词

```
POST   /api/geo/keywords/mine         挖掘关键词
POST   /api/geo/keywords/expand       语义扩展
POST   /api/geo/keywords/cluster      话题聚类
GET    /api/geo/keywords/list         关键词列表
DELETE /api/geo/keywords/:id          删除关键词
```

### 内容

```
POST   /api/geo/content/generate      生成内容
POST   /api/geo/content/optimize      优化内容
POST   /api/geo/content/score         内容评分
POST   /api/geo/content/eeat          E-E-A-T 增强
POST   /api/geo/content/schema         生成 Schema
POST   /api/geo/content/uniqueness     独特性检测
GET    /api/geo/content/list           文章列表
GET    /api/geo/content/:id            文章详情
```

### 验证

```
POST   /api/geo/verification/verify            AI 搜索验证
POST   /api/geo/verification/negative          负面监控
GET    /api/geo/verification/results/:article_id  验证结果
```

### 知识库

```
GET    /api/geo/kb/documents           文档列表
POST   /api/geo/kb/documents           保存文档
GET    /api/geo/kb/documents/:id       文档详情
DELETE /api/geo/kb/documents/:id       删除文档
GET    /api/geo/kb/search?q=           关键词检索
POST   /api/geo/kb/ask                 RAG 问答
```

### 工作流

```
GET    /api/geo/workflow/workflows           工作流列表
POST   /api/geo/workflow/workflows           创建工作流
GET    /api/geo/workflow/workflows/:id       工作流详情
PUT    /api/geo/workflow/workflows/:id       更新工作流
DELETE /api/geo/workflow/workflows/:id       删除工作流
POST   /api/geo/workflow/workflows/:id/run   执行工作流
GET    /api/geo/workflow/workflows/:id/executions  某工作流执行历史
GET    /api/geo/workflow/templates           模板列表
POST   /api/geo/workflow/templates           创建模板
```

### 平台同步

```
GET    /api/geo/platform/platforms    支持的平台列表
GET    /api/geo/platform/accounts     账号列表
POST   /api/geo/platform/accounts     新增账号
DELETE /api/geo/platform/accounts/:id 删除账号
POST   /api/geo/platform/publish      发布到平台
GET    /api/geo/platform/records      发布记录
```

### 审计 / 双端 / 承接

```
POST   /api/geo/audit/site                   站点体检（scope=page|site）
POST   /api/geo/audit/content                内容体检（给 URL 自动抓正文）
GET    /api/geo/audit/runs                   体检历史
GET    /api/geo/audit/runs/:id               详情
GET    /api/geo/audit/runs/:id/report        报告（format=html|csv）
GET    /api/geo/audit/compare                同 rubric 两次对比

POST   /api/geo/search-console/import        导入 GSC CSV/JSON
GET    /api/geo/reports/dual-engine          AI/Google 双端报表

POST   /api/geo/capture/links                创建承接链接
GET    /api/geo/capture/links                列表
GET    /api/geo/capture/links/:id            详情 + 首响
DELETE /api/geo/capture/links/:id            删除

GET    /api/public/geo/l/:code               公开落地页（HTML；?format=json）
POST   /api/public/geo/l/:code/ask           公开 FAQ（每日配额）
POST   /api/public/geo/l/:code/lead          公开留资（每日配额）
```

### 资源 / 配置 / 指标

```
GET    /api/geo/resources/agents        AI Agent 推荐
GET    /api/geo/resources/tools         工具推荐
GET    /api/geo/resources/papers        论文 / 指南
GET    /api/geo/resources/communities   社区推荐
GET    /api/geo/resources/summary       资源汇总
GET    /api/geo/resources/search        资源搜索
POST   /api/geo/techconfig/robots       生成 robots.txt
POST   /api/geo/techconfig/sitemap      生成 sitemap.xml
POST   /api/geo/techconfig/llms-txt     生成 llms.txt（AI 爬虫索引）
POST   /api/geo/metrics/analyze         内容质量分析
GET    /api/geo/keyword-enhance/analyze 关键词表现分析
POST   /api/geo/keyword-enhance/enhance 数据增强关键词
```

---

## 技术栈

| 层 | 技术 |
|----|------|
| 后端 | Go + Gin + GORM + PostgreSQL |
| LLM | 复用 hivemtk 全局 LLM Dispatcher（支持 6+ 厂商：local / OpenAI 兼容 / DeepSeek / Anthropic / Google / 自托管；含场景路由 + 缓存 + 故障转移） |
| 前端 | Vue 3 + Element Plus + Pinia + ECharts |
| 存储 | PostgreSQL（**16 张 geo_* 表**：workflow / query_chain / keyword / crawler_visit / content / knowledge / verification / config 等，AutoMigrate 自动建表） |
| 工作流 | 自研 DAG 引擎（条件判断 + 步骤跳转 + **5 种内置执行器**：content_generate / content_score / eeat_enhance / fact_density_enhance / verify） |

---

## 常见问题

**Q: 工作流执行需要配置 LLM 吗？**
A: 是的。工作流中的 `content_generate`、`content_score`、`eeat_enhance`、`fact_density_enhance`、`verify` 步骤都需要 LLM。未配置时，这些步骤会返回 fallback 结果（固定文本/默认分数）。

**Q: 知识库 RAG 最多支持多少文档？**
A: 当前实现使用 PostgreSQL 内存检索（关键词匹配），适合几百篇以内的品牌知识库。如需更大规模，建议后续接入 pgvector 向量检索。

**Q: 平台同步支持哪些平台？**
A: 真正走 API 的只有 GitHub（Contents API）、WordPress（XML-RPC）和通用 Webhook；Medium 只支持手动复制。知乎/掘金/CSDN 等未内置，页面不会伪造发布记录。

**Q: 关键词数据增强怎么工作的？**
A: 它分析历史验证结果中的查询，统计每个关键词的提及率，提取高价值关键词，然后用 LLM 生成增强建议。需要先运行过 AI 搜索验证功能积累数据。
