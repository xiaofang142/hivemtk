<div align="center">

# 🐝 HiveMtk · 开源私域营销 AI 操作系统

### ReAct 智能体 · 多端社媒触达 · 数据零出域 · 本地私有化部署

**为成长型团队打造：把销冠能力复制给团队里每一个普通人**

</div>

<div align="center">

[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev) [![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vue.js&logoColor=white)](https://vuejs.org) [![Docker](https://img.shields.io/badge/Docker-24+-2496ED?logo=docker&logoColor=white)](https://www.docker.com) [![PostgreSQL 15+](https://img.shields.io/badge/PostgreSQL-15+-336791?logo=postgresql&logoColor=white)](https://www.postgresql.org) [![License AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE) [![Gitee](https://img.shields.io/badge/Gitee-xhpmayun%2Fhivemtk-C71D23?logo=gitee)](https://gitee.com/xhpmayun/hivemtk) [![GitHub](https://img.shields.io/badge/GitHub-xiaofang142%2Fhivemtk-181717?logo=github)](https://github.com/xiaofang142/hivemtk)

</div>

---

## 项目简介

**HiveMtk**（读音 /ˈhaɪv ɛm ti keɪ/，"Hive"+"Marketing Toolkit"）是一套**面向中文私域运营场景的开源 AI 营销操作系统**。它把"多端社媒触达、ReAct 自主智能体、本地知识库与零出域数据安全"四件事在同一个仓库里做透，目标用户是 5–50 人的成长型团队、合规敏感行业（金融/医疗/政企）以及希望摆脱 SaaS 厂商锁定的运营团队。

HiveMtk 不是"给大模型套个壳"，更不是把流程写死的自动化脚本。系统内置一套**能感知 → 规划 → 调工具 → 反思**的 ReAct 自主智能体，从消息入站到回复出站，自己想办法把事办成。覆盖**获客 → 触达 → 转化 → 复购**全链路营销场景，完整功能列表与已知限制如实披露，见 [docs/marketing-features/README.md](docs/marketing-features/README.md)。

核心特性一句话：**🌐 多端打通 · 🤖 ReAct 自主智能体 · 🔒 100% 私域零出域 · 📦 开箱即用业务模块 · ⚡ 5 分钟一键起**。

**关键技术指标**：
- 94 个营销功能模块（SCRM + AI + CDP + 营销自动化）
- 10+ 社媒/沟通渠道统一接入（抖音/快手/小红书/闲鱼/TikTok/企微/邮件/Telegram/WhatsApp/短信）
- 1024 维向量检索（pgvector HNSW + bge-m3 + bge-reranker-v2-m3）
- 本地推理栈：llama.cpp (Qwen2.5) + TEI (Embedding/Rerank)
- AGPL-3.0 网络 copyleft，私有部署无需开源，仅 SaaS 服务需开源

---

## 📑 目录

- [项目简介](#项目简介)
- [关键词与适用场景](#关键词与适用场景)
- [快速体验](#快速体验)
- [一句话定位](#一句话定位)
- [为什么选择 HiveMtk](#为什么选择-hivemtk)
- [核心能力](#核心能力)
  - [多端触达](#1-多端触达一个工作台全管)
  - [AI 智能体](#2-ai-范式react-自主智能体不是写死的工作流)
  - [数据安全](#3-数据安全100-私域对话不出客户内网)
  - [GEO 智能优化](#4-geo-智能优化模块ai-搜索获客闭环)
- [与同类项目对比](#与同类项目对比)
- [架构总览](#架构总览)
- [功能模块](#功能模块)
- [技术栈](#技术栈)
- [仓库结构](#仓库结构)
- [快速开始](#快速开始)
- [常用命令](#常用命令)
- [文档导航](#文档导航)
- [路线图](#路线图)
- [常见问题 FAQ](#常见问题-faq)
- [合规与免责](#合规与免责)
- [联系与社区](#联系与社区)
- [镜像仓库](#镜像仓库)
- [License](#license)

---

## 关键词与适用场景

### 核心关键词（GitHub / Gitee 搜索）

**产品定位**：SCRM 私域营销 · 私域增长 AI · 私域智能体 · customer service AI · 营销自动化 · sales copilot · CDP 客户数据平台

**AI 能力**：ReAct agent · LLM · RAG 检索增强 · hybrid search · ai agent · react-agent

**技术栈**：Go · Vue · PostgreSQL · pgvector · Redis · Docker · llama-cpp · bge-m3 · qwen

**部署形态**：self-hosted · on-premise · private deployment · 数据零出域 · AGPL-3.0

### 适用场景（典型行业）

| 场景 | 关键能力 | 典型行业 |
|------|---------|---------|
| 私域获客 → AI 自动谈单 | 多端线索接入 + ReAct 智能体承接咨询 + 异议处理 + 逼单邀约 | 招商加盟 / 医美 / 教培 / B2B |
| 企微 SCRM 多账号聚合 | 多账号统一收件箱 + 客户资产沉淀 + 离职继承 | 连锁门店 / 品牌方 |
| AI 客服 / RAG 知识库 | 混合检索 + 智能客服 + 7×24 自动回复 | 电商 / SaaS / 售后 |
| 营销自动化 SOP | 销冠 SOP 可视化编排 + RFM 分层 + 流失预警 + 沉睡激活 | 会员制零售 / 母婴 / 美业 |
| 跨境出海多渠道触达 | TikTok + WhatsApp + 邮件 + Telegram 统一管理 | 跨境电商 / 出海品牌 |
| 合规私有化部署 | 本地推理栈 + 零出域 + 行级权限 + 审计存档 | 金融 / 医疗 / 政企 |

**技术选型对比**（为技术决策者提供参考）：

| 维度 | HiveMtk | Dify / FastGPT | 商业 SCRM | 企微专用 SCRM |
|------|---------|----------------|-----------|---------------|
| 核心定位 | 私域营销 AI OS | 通用 LLM 应用平台 | SaaS 服务 | 企微深度集成 |
| 部署模式 | 100% 私有化 | 自托管 / SaaS | 仅 SaaS | SaaS / 私有 |
| AI 能力 | ReAct 智能体 + 工具集 | Workflow 编排 | 基础客服机器人 | 简单 RAG |
| 渠道覆盖 | 10+ 端 | 无内置 | 1-3 端 | 仅企微 |
| 数据主权 | 完全本地 | 可选本地 | 云端 | 云端/本地 |
| 开源协议 | AGPL-3.0 | Apache-2.0 | 闭源 | 部分开源 |

---

## 快速体验

> ⚠️ **演示环境为公共示例，演示数据任何人可访问，请勿上传真实业务数据。**
> 演示地址以 [Gitee Releases](https://gitee.com/xhpmayun/hivemtk/releases) 或 [GitHub Releases](https://github.com/xiaofang142/hivemtk/releases) 公告为准。

| 项目 | 值 |
|------|-----|
| 体验地址 | https://hiveuser.xapptool.cn/（以官方公告为准） |
| 登录账号 | `admin` |
| 登录密码 | `Seed@123456` |
| API 文档 | 部署后访问 `http://<host>:8204/swagger/index.html` |

---

## 一句话定位

> **开源私域营销 AI 操作系统**:把多端社媒触达、ReAct 自主智能体、零出域数据安全三件事**同时做透**。

我们不是给大模型套个壳,更不是把流程写死的自动化脚本。HiveMtk 内置一套**能感知 → 规划 → 调工具 → 反思**的 ReAct 自主 AI 智能体,从消息入站到回复出站,自己想办法把事办成。覆盖**获客 → 触达 → 转化 → 复购**全链路营销场景,完整功能模块开箱即用,另附 GEO 智能优化模块打通 AI 搜索获客闭环。

```bash
# ⚡ 3 步 5 分钟跑起来
git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk
make install   # 复制 .env 模板 + 构建前后端 + 下载模型 + 拉起数据层与推理栈（docker-compose.yml 随仓库提供）
vim .env       # 改 4 个密钥:POSTGRES_PASSWORD / REDIS_PASSWORD / JWT_SECRET / PLATFORM_ADMIN_PASSWORD
make dev       # 启动 user-server 热更新 → http://localhost:8204
```

⭐ **Star / Watch 一下,跟项目一起成长** · 💬 **[联系与社区](#联系与社区)**

---

## 为什么选择 HiveMtk

### 痛点对比

| 痛点 | SaaS SCRM | 套壳 AI | 自动化脚本 |
|------|-----------|---------|-----------|
| 数据出域 | ❌ 数据在云端 | ⚠️ 调用云端 API | ✅ 本地 |
| AI 自主性 | ❌ 规则引擎 | ❌ 单轮问答 | ❌ if-else 写死 |
| 渠道覆盖 | ⚠️ 1-3 端 | ❌ 无 | ⚠️ 单平台 |
| 可定制深度 | ❌ 黑盒 | ⚠️ 受限 | ✅ 高 |

### 解决方案

HiveMtk 把**私域部署 + 真 AI 智能体 + 多端打通**三件事同时做透:

- **数据封死在域内**:所有对话、知识库、向量化、检索增强全程在客户内网完成,零外网可跑
- **AI 真自主**:ReAct 循环(感知→规划→调工具→反思),不是写死的工作流
- **多端一工作台**:抖音/快手/小红书/闲鱼/TikTok/微信/邮件/短信 统一管理

### 适合谁

- 🎯 **成长型团队**:想自主可控、多端运营、强 AI 辅助的 5-50 人小团队
- 🏛️ **合规敏感行业**:金融/医疗/政务等要求数据不出域的场景
- 🔧 **自主可控追求者**:希望深度定制、二次开发、避免厂商锁定的团队

---

## 核心能力

### 1. 多端触达:一个工作台全管

| 渠道 | 触达 | 智能卡片 | 自动回复 | RAG 客服 | 备注 |
|------|------|---------|---------|---------|------|
| 抖音 | ✅ | ✅ | ✅ | ✅ | Chrome 扩展桥接,含直播/私信 |
| 快手 | ✅ | ✅ | ✅ | ✅ | Chrome 扩展桥接,含直播/私信 |
| 小红书 | ✅ | ✅ | ✅ | ✅ | Chrome 扩展桥接,含私信/评论 |
| 闲鱼 | ✅ | ✅ | ✅ | ✅ | Chrome 扩展桥接,二手商品场景 |
| TikTok | ✅ | ✅ | ✅ | ✅ | Chrome 扩展桥接,海外矩阵 |
| 微信 / 企业微信 | ✅ | - | ✅ | ✅ | 含社群/朋友圈 |
| 邮件 | ✅ | - | ✅ | ✅ | SMTP/163/QQ |
| Telegram | ✅ | - | ✅ | ✅ | **5 大能力**: Webhook/Polling 双模式 · 群线索挖掘 · 主动 DM 触达 · 群消息分析 · 入群管控 |
| WhatsApp | ✅ | - | ✅ | ✅ | Cloud API + 模板消息 |
| 短信 | ✅ | - | - | - | 阿里云/腾讯云/华为云 |

> **桥接架构说明**:抖音/快手/小红书/闲鱼/TikTok 五端经 **Chrome 扩展( Bridge 客户端)+ 你自己的登录态浏览器** 收发消息——无需无头浏览器。扩展所在浏览器在线时,入站私信自动进入统一收件箱,AI 生成的回复经扩展在真实会话中发出。

#### Telegram 五大核心能力（跨境出海/私域增长利器）

Telegram 不是又一个"接入渠道"——它是 HiveMtk 内置的 **群增长 + 主动获客** 完整闭环:

| # | 能力 | 实现 | 价值 |
|---|------|------|------|
| 1 | **Bot 收发（Webhook + Polling）** | BotFather 创建 → 系统自动注册 Webhook；无公网 IP 时自动降级 Polling | 双模式兜底,永不丢消息 |
| 2 | **群线索自动挖掘** | 监听群所有发言 → 中英双语关键词 + 联系方式信号打分（0-100）→ score≥40 入库为商机 | 群里每句"多少钱/怎么买/求购"自动变线索,静默运行零刷屏 |
| 3 | **高意向主动 DM 触达** | score≥60 触发 → 冷却期 60 分钟（Redis SetNX 去重）→ 调用 Bot 发送个性化私信模板 | 把群里沉默的意向客户拉到 1v1 私聊,转化率提升 3-5 倍 |
| 4 | **群消息分析与智能回复** | 群消息全量入库 → 智能体自动识别 @机器人 / 关键词触发 → RAG 知识库精准回复 | 7×24 承接群咨询,把群变成永不打烊的销售前台 |
| 5 | **入群管控（Gate）** | 方案 A `join_request`（私密群申请制）/ 方案 B `mute_unlock`（公开群禁言验证）→ 用户通过 Bot `/start <token>` 激活 → 自动放行/解禁 | 防垃圾广告 + 强制用户与 Bot 建立首交互,打通主动触达前置条件 |

> **Telegram 架构亮点**:Bot 协议直连（非 Chrome 扩展）,入站经 Webhook/Polling 双模式消费 → Message Hub 统一入库 → LeadMiner 静默打分 → 高意向 DMOutreach 自动发起。解决了 Telegram Bot 不能主动向未交互用户私聊的官方限制——Gate 管控链强制用户先 `/start`,后续就能自由 DM 触达。

统一 CDP 客户视图,一份资料全渠道触达;统一消息中心,会话/工单/留言一处看完。

### 2. AI 范式:ReAct 自主智能体,不是写死的工作流

- **ReAct 循环**:感知 → 规划 → 调工具 → 反思,智能体自主决策
- **工具装饰器链**:Permission → Retry → Timeout → RateLimit → Audit 五层装饰器逐层包裹,另有熔断器/死信队列/循环守卫
- **混合检索 RAG**:粗排(pgvector HNSW 向量 + BM25 关键词,RRF 融合) + 精排(bge-reranker-v2-m3) + 可选查询改写(HyDE / MultiQuery)
- **多智能体协作**:被动应答智能体 + 主动触达智能体
- **AI 销冠**:话术模板 + RAG + 自动跟进,全流程辅助坐席
- **可视化工作流**:营销自动化编辑器,零代码搭建 SOP

**对比传统"工作流"**:工作流是 if-else 写死的,撞到预设之外的情况就崩;智能体是自己想办法的,遇到没见过的场景也能组合工具搞定。

> 工具注册逻辑以源码为准：[user-server/internal/aiagent/agent/tooluse/](user-server/internal/aiagent/agent/tooluse/)（代码即清单）。

### 3. 数据安全:100% 私域,对话不出客户内网

- **本地 AI 推理栈**:llama.cpp(Qwen2.5)+ TEI(bge-m3 + bge-reranker-v2-m3),三个 OpenAI 兼容服务(LLM/Embedding/Rerank)跑在客户内网
- **数据零出域**:所有对话、知识库、向量化、检索增强**全程在客户内网完成**,零外网可跑
- **FRP 私域穿透**:访客从公网进,数据经隧道回本地,云端**不落一条对话**
- **合规友好**:满足等保、数据出境管控、私有化部署基线
- **可选云端 LLM**:要更强的模型?把 `LLM_BASE_URL` 改成 DeepSeek/OpenAI 即可,Embedding/Rerank 仍强制本地

### 4. GEO 智能优化模块（AI 搜索获客闭环）

> **GEO（Generative Engine Optimization，生成式引擎优化）**：面向 ChatGPT Search、Perplexity、Google SGE 等 AI 搜索引擎的内容优化方法论。SEO 争的是搜索结果排名，GEO 争的是 **AI 答案里的"席位"**——让品牌在被大模型引用作答时被提及、被正面表述。

完整指南见 [user-server/docs/geo-module-guide.md](user-server/docs/geo-module-guide.md)。

```
品牌配置 → 关键词蒸馏 → 内容创作 → 多模型验证 → 平台同步发布
                ↑                             ↓
           数据增强 ← 历史验证数据 ←──────────┘
```

六大能力：

| 能力 | 介绍 |
|------|------|
| **关键词蒸馏** | 种子词扩展为高价值关键词集（对比/评测/购买意图） |
| **内容创作** | 按品牌声音/字数/风格约束生成,四维百分制评分 |
| **E-E-A-T + Schema** | 注入经验/专业/权威/可信要素,自动生成 JSON-LD 标记 |
| **多模型验证** | 用多厂商 LLM 模拟 AI 搜索作答,量化"AI 答案席位" |
| **RAG 知识库** | 让生成内容锚定品牌事实,降低幻觉 |
| **平台同步** | 把优化后内容铺到高权重站点,增加被语料收录机会 |

配套能力：DAG 工作流引擎、负面监控、ROI 与成本报表、技术配置生成（robots.txt / sitemap.xml）、LLM 复用全局 Dispatcher。

---

## 与同类项目对比

> 💡 诚实对比,敢暴露劣势。**没有银弹**,按需选择。

| 维度 | **HiveMtk**（本项目） | 源雀 SCRM | MoChat 摩言 | Dify | 商业 SCRM（微伴/尘锋） |
|------|--------------------|---------|------------|------|---------------------|
| 核心定位 | 私域 AI 营销 OS | 企微 SCRM | 企微 SCRM | 通用 LLM 应用平台 | 商业 SaaS |
| 触达端 | **多端** | 1 端（企微） | 1 端（企微） | 无 | 1-3 端 |
| AI 能力 | **ReAct 智能体 + 工具集** | 简单 RAG | 无 | 可视化 Workflow | 基础客服机器人 |
| 数据部署 | **100% 私域 + 本地推理栈** | SaaS / 私有 | SaaS / 私有 | 自托管 / SaaS | SaaS |
| 开源 | ✅ AGPL-3.0 | ✅ | ✅ | ✅ | ❌ |
| 上手成本 | 5 分钟 Docker | 中等 | 中等 | 中等 | 注册即用 |
| 可定制深度 | 高（全栈开源） | 中 | 中 | 高（开发者向） | 低 |
| 适合谁 | 想**自主可控**、**多端**、**强 AI** 的团队 | 企微深度用户 | 企微深度用户 | 纯 AI 应用开发 | 无 IT 团队的中小商家 |

---

## 架构总览

### 部署架构（宿主机推理栈版）

```
   访客浏览器(公网)
       │
       │ HTTPS / WSS(经 FRP / 公网 IP / 反代)
       ▼
   ┌─────────────────────────────────────────────────────────┐
   │  客户本地(用户端)                                       │
   │                                                         │
   │   user-server (Go + Gin) :8204                          │
   │       ├── PostgreSQL user_db :8202  (pgvector 1024 维)  │
   │       ├── Redis 7           :8203  (Token / 缓存)        │
   │                                                         │
   │   宿主机推理栈(llama.cpp,非容器化)                       │
   │       ├── LLM (llama-server)    :8207  (Qwen2.5)         │
   │       ├── Embedding             :8208  (bge-m3, 1024 维)  │
   │       └── Rerank                :8209  (bge-reranker-v2-m3)│
   │                                                         │
   │   user-web (Vue 3 SPA) ─静态托管──▶ user-server         │
   │   embed-sdk (Web Widget) ─静态托管──▶ user-server       │
   └─────────────────────────────────────────────────────────┘
            │
            │ HTTPS(低频:心跳 / 商户标识校验 / 版本检查)
            ▼
       平台端(独立仓库 hivemtk-platform)
       提供版本检查、商户标识校验、官方支持服务,**不碰业务数据**
```

### 五层架构（后端代码硬约束）

后端 Go 代码严格遵循五层架构,**禁止跨层调用**:

```
Controller  →  HTTP 参数解析 / 调 service / 统一响应
    ↓
Service     →  业务编排 / 跨 repository 组合
    ↓
Repository  →  数据访问层 / GORM 操作
    ↓
Model       →  GORM 模型定义(无业务方法)
    ↓
DTO         →  传输对象(无反向引用)
```

完整规范见 [docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md),CI 检查脚本见 [scripts/check-architecture.sh](scripts/check-architecture.sh)。

### 与平台端的关系

| | 用户端（hivemtk，本仓库） | 平台端（hivemtk-platform） |
|---|---|---|
| **所有者** | 企业客户 | 平台运营方 |
| **运行位置** | 客户本地内网 | 平台云端 |
| **存储** | 全部业务数据（对话/知识库/客户） | 仅元数据（商户/版本/统计） |
| **技术栈** | Go + Vue 3 + 本地推理栈 | Go + Vue 3 + PostgreSQL |
| **通信** | → 平台端：低频 HTTPS 心跳 | → 用户端：仅元数据 + 商户标识 API |
| **License** | AGPL-3.0 | AGPL-3.0 |

**关键原则**：平台端**不接触、不存储、不访问**任何用户业务数据。

平台端仓库：[gitee.com/xhpmayun/hivemtk-platform](https://gitee.com/xhpmayun/hivemtk-platform) · [github.com/xiaofang142/hivemtk-platform](https://github.com/xiaofang142/hivemtk-platform)

---

## 功能模块

完整列表见 [docs/marketing-features/README.md](docs/marketing-features/README.md),按业务域分类:

| 业务域 | 关键能力 |
|--------|---------|
| 认证与用户管理 | JWT 鉴权、团队角色、商户初始化 |
| 多平台卡片 | 抖音/快手/小红书/闲鱼/TikTok 自动生卡 |
| 自动回复 + RAG | 通用/闲鱼/TikTok 自动回复 + 混合检索 RAG + 智能客服 |
| 邮件营销 | 列表/草稿/任务/发送/退订/SMTP/追踪 |
| 短信营销 | 渠道/签名/任务/退订 |
| 社群管理 | 企业微信/WhatsApp/Telegram/飞书/钉钉群发 + 好友管理 |
| 短链与活码 | 短链/活码/域名池 |
| 线索与客户 | 线索/客户 360/会话/标签/事件/WebSocket/OneID |
| 营销自动化 | SOP/A-B 测试/RFM 分层/流失预测/报表/看板/批量/挽回 |
| 内容创作 | AI 内容/脚本库/模板市场/素材库 |
| 系统管理 | 系统配置/可观测/升级/备份/上传/审计/追踪/SSE/LLM Provider/调优/异常检测 |
| 安全与权限 | 权限系统（角色/菜单/按钮级）/行级数据权限 |
| 第三方对接 | 集成模板/同步日志 |
| 统一消息 | 多平台消息聚合/统一收件箱/消息中心/平台账号 |
| AI 销冠核心 | 对话记忆/意图识别/SOP/LLM 路由/异议处理/销冠画像/触达 Pipeline |
| 多 AI 智能体 | 智能体管理/渠道绑定/客服挂载 |
| 数据分析 | 客户旅程/转化漏斗/智能体产能 |
| 客服 Web Widget | 嵌入式客服渠道管理 |
| GEO 智能优化 | 关键词蒸馏/内容创作/多模型验证/平台同步 |

> 平台端 10 个 `platform-*` 模块见 [hivemtk-platform/docs/platform-features/README.md](../hivemtk-platform/docs/platform-features/README.md)。

---

## 技术栈

### 后端核心

| 选型 | 介绍 | 作用 |
|------|------|------|
| **Go 1.25 + Gin** | 静态类型编译语言 + 轻量 Web 框架 | 单二进制交付、低内存占用,支撑高并发消息入站与 API 服务 |
| **五层架构** | Controller → Service → Repository → Model → DTO 硬约束 | 业务逻辑可测、可替换,AI 编码与多人协作不跑偏 |
| **JWT 鉴权** | 无状态令牌认证 | 私域部署基线下的轻量登录态管理 |
| **air 热更新** | Go 开发热重载工具 | 保存即生效,开发迭代秒级反馈 |

### 数据与缓存

| 选型 | 介绍 | 作用 |
|------|------|------|
| **PostgreSQL 15** | 开源关系数据库 | 客户/对话/SOP 全部业务数据的唯一事实源 |
| **pgvector** | PostgreSQL 向量扩展（1024 维 HNSW） | 向量与业务数据同库,语义检索无需额外向量库 |
| **Redis 7** | 内存键值存储 | Token 会话、热点缓存、分布式锁 |

### AI 推理与检索

| 选型 | 介绍 | 作用 |
|------|------|------|
| **llama.cpp + Qwen2.5** | 本地 LLM 推理运行时（GGUF Q4_K_M） | 对话生成跑在客户内网,零云端调用 |
| **TEI + bge-m3** | 文本嵌入推理服务（1024 维） | 把对话/知识库转成 pgvector 可检索的语义向量 |
| **TEI + bge-reranker-v2-m3** | 交叉编码器精排服务 | RAG 第二级精排,显著提升检索相关性 |
| **LLM Dispatcher 网关** | 多厂商统一调度层 | 一套接口接 DeepSeek/通义/GPT-4o/GLM/Kimi/本地模型 |

### 智能体框架

| 选型 | 介绍 | 作用 |
|------|------|------|
| **ReAct 循环** | 推理-行动循环智能体运行时 | 遇到预设之外的场景自主组合工具完成任务 |
| **工具装饰器链** | Permission/Retry/Timeout/RateLimit/Audit 五层装饰器 | 每次工具调用可控、可限、可查 |
| **四层记忆系统** | L1 短期/L2 长期/L3 SOP 状态/L4 业务记忆 | 智能体"记得住"上下文、偏好、进度、事实 |

### 前端与触达

| 选型 | 介绍 | 作用 |
|------|------|------|
| **Vue 3 + Vite + Element Plus + Pinia** | 组合式 API 前端栈 | B 端工作台交互体验与构建速度兼顾 |
| **embed-sdk** | 嵌入式客服 Web Widget（IIFE） | 一行脚本嵌入任意官网,客服入口即插即用 |
| **多类触达适配器** | 统一发送抽象 ReachAdapter | 新增渠道只需实现一个适配器 |

### 部署形态

| 选型 | 介绍 | 作用 |
|------|------|------|
| **Docker Compose（数据层）** | PG + Redis 容器化编排 | 数据层一键起停、迁移、备份恢复 |
| **宿主机推理栈** | llama.cpp/TEI 非容器化直跑 | 节省内存、提升推理吞吐 |
| **FRP 私域穿透** | 内网穿透隧道 | 访客从公网进、数据留在本地 |

---

## 仓库结构

```
hivemtk/                              # 用户端仓库
├── user-server/                      # Go 后端（核心业务,五层架构）
├── user-web/                         # Vue 3 前端（B 端工作台 + bridge 子模块）
├── embed-sdk/                        # 嵌入式客服 Web Widget（IIFE/ESM）
├── migrations/                       # 数据库迁移 SQL（002-018, 024-055, 幂等, 019-023 已移除）
├── scripts/
│   ├── inference-host/               # 宿主机推理栈脚本（llama.cpp + TEI）
│   ├── check-architecture.sh         # 五层架构 CI 检查
│   └── api-inventory.sh              # API 清单导出
├── docs/
│   ├── INDEX.md                      # 文档索引
│   ├── DEPLOYMENT_GUIDE.md           # 部署指南
│   ├── TROUBLESHOOTING.md            # 排查手册
│   ├── architecture/                 # 架构文档（五层架构/部署方案/ADR/FRP）
│   ├── marketing-features/           # 营销功能模块详细文档
│   ├── oneid/                        # OneID 客户标识
│   ├── bridge/                       # Bridge 桥接模块
│   ├── operations/                   # 运维文档（部署手册/初始化流程/Widget 嵌入）
│   └── standards/                    # 项目规则与编码规范
├── docker-compose.yml                # 数据层编排（PG + Redis）
├── Makefile                          # 一键安装/启动/停止/推理栈/开发热更新
├── .env-example                      # 环境变量模板
├── CONTRIBUTING.md                   # 贡献指南
├── SECURITY.md                       # 安全策略
├── DISCLAIMER.md                     # 免责声明
└── LICENSE                           # 开源协议（AGPL-3.0）
```

---

## 快速开始

### 前置要求

- Docker 24+ & Docker Compose v2
- 4 核 CPU / 8GB 内存 / 50GB 磁盘（最低,dev 档）
- 8 核 CPU / 16GB 内存 / 100GB 磁盘（推荐,prod 档含 LLM）

### 5 分钟上手（宿主机推理栈版）

```bash
# 1. 克隆仓库
git clone https://gitee.com/xhpmayun/hivemtk.git
cd hivemtk

# 2. 一键安装（生成 .env、构建前后端、下载模型、拉起数据层并启动推理栈）
#    docker-compose.yml 已随仓库提供，无需生成
make install

# 3. 编辑 .env,至少修改以下密钥
vim .env
#   POSTGRES_PASSWORD         openssl rand -hex 24
#   REDIS_PASSWORD            openssl rand -hex 24
#   JWT_SECRET                openssl rand -hex 32
#   PLATFORM_ADMIN_PASSWORD    平台代理管理员密码（与平台端 .env 保持一致）

# 4. 启动 user-server（开发态热更新）
make dev

# 5. 启动前端（另开终端）
cd user-web && npm run dev

# 6. 访问
# 用户端后台: http://localhost:8204
# 健康检查:   curl http://localhost:8204/health
```

### dev / prod 模型档切换

编辑 `scripts/inference-host/models.env` 或通过环境变量切换:

| 档位 | LLM | Embedding | Rerank | 内存需求 | 用途 |
|------|-----|-----------|--------|---------|------|
| **dev**（轻量,默认） | Qwen2.5-3B-Instruct (Q4_K_M) | bge-m3 (Q4_K_M, 1024 维) | bge-reranker-v2-m3 (Q4_K_M) | 8GB | 个人电脑/小内存部署 |
| **prod**（重量） | Qwen2.5-14B-Instruct (Q4_K_M) | bge-m3 (F16, 1024 维) | bge-reranker-v2-m3 (Q4_K_M) | 16GB+ | 生产环境 |

```bash
# 切换到 prod 档
HIVEMTK_PROFILE=prod make inference-host-models
HIVEMTK_PROFILE=prod make inference-host-up
```

> 📌 **Embedding 维度铁律**:必须保持 1024 维（与 pgvector `vector(1024)` 一致）。换其他维度需先 `ALTER TABLE`。

---

## 常用命令

```bash
# ============ 首次部署 ============
make install              # 一键安装（.env + compose + 模型 + 数据层 + 推理栈）

# ============ 数据层（Docker） ============
make db-up                # 启动 PG + Redis
make db-down              # 停止 PG + Redis
make db-ps                # 查看容器状态
make db-logs              # 查看容器日志
make db-backup            # 备份 PG
make db-restore FILE=...  # 恢复 PG

# ============ 宿主机推理栈（llama.cpp） ============
make inference-host-install       # 安装 llama.cpp（首次）
make inference-host-models        # 下载 dev 档模型
make inference-host-models-prod   # 下载 prod 档模型
make inference-host-up            # 启动 LLM + Embedding + Rerank
make inference-host-down          # 停止推理栈
make inference-host-warmup        # 预热（避免首请求慢）
make inference-host-test          # 端到端 smoke test
make inference-host-status        # 统一查看状态
make inference-host-logs          # 查看日志
make inference-host-ps            # 查看进程

# ============ 本地开发（热更新）============
make dev-install         # 一次性安装 air（已装跳过）
make dev                 # 启动 user-server 热更新（air,保存 .go/.yaml/.env 即自动重编+重启）
make dev-stop            # 停止 air
make dev-clean           # 清理 air 临时产物
make dev-help            # 热重载速查
make dev-all             # 一键全栈（数据层 + 推理栈 + air 提示）
make dev-down            # 停止全栈

# ============ 前端构建 ============
make web-build            # 构建 user-web
make sdk-build            # 构建 embed-sdk
```

---

## 文档导航

### 必读（⭐⭐⭐⭐⭐ 部署前必读）

| 文档 | 入口 |
|------|------|
| 仓库总览 | [README.md](README.md) · [README.en.md](README.en.md) |
| 文档索引 | [docs/INDEX.md](docs/INDEX.md) |
| 一键部署命令 | [Makefile](Makefile) |
| 数据层编排 | [docker-compose.yml](docker-compose.yml) |
| 环境变量模板 | [.env-example](.env-example) |
| 部署指南 | [docs/DEPLOYMENT_GUIDE.md](docs/DEPLOYMENT_GUIDE.md) |
| 排查手册 | [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) |
| 开源协议 | [LICENSE](LICENSE) · [NOTICE](NOTICE) |

### 架构规范（⭐⭐⭐⭐⭐ AI 编码必读）

| 文档 | 入口 |
|------|------|
| Go 五层架构编码规范 | [docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md) |
| 智能体工具注册表（代码即清单） | [user-server/internal/aiagent/agent/tooluse/](user-server/internal/aiagent/agent/tooluse/) |
| GEO 智能优化模块指南 | [user-server/docs/geo-module-guide.md](user-server/docs/geo-module-guide.md) |

### 部署运维

| 文档 | 入口 |
|------|------|
| 商户部署完整手册 | [docs/operations/MERCHANT_DEPLOYMENT.md](docs/operations/MERCHANT_DEPLOYMENT.md) |
| 用户端部署方案 | [docs/architecture/部署方案_用户端.md](docs/architecture/部署方案_用户端.md) |
| 宿主机推理迁移方案 | [docs/architecture/FRP私域部署指南.md](docs/architecture/FRP私域部署指南.md) |
| Chat Widget 嵌入指南 | [docs/operations/CHAT_WIDGET_EMBED.md](docs/operations/CHAT_WIDGET_EMBED.md) |
| 平台端/用户端部署分工 | [../hivemtk-platform/docs/architecture/部署方案_平台端与用户端.md](../hivemtk-platform/docs/architecture/部署方案_平台端与用户端.md) |

### 功能模块

| 文档 | 入口 |
|------|------|
| 营销功能模块总览 | [docs/marketing-features/README.md](docs/marketing-features/README.md) |
| 平台端 10 个功能模块 | [../hivemtk-platform/docs/platform-features/README.md](../hivemtk-platform/docs/platform-features/README.md) |
| 数据库迁移执行顺序 | [migrations/README.md](migrations/README.md) |

### 贡献与安全

| 文档 | 入口 |
|------|------|
| 贡献指南 | [CONTRIBUTING.md](CONTRIBUTING.md) |
| 安全策略 | [SECURITY.md](SECURITY.md) |
| 免责声明 | [DISCLAIMER.md](DISCLAIMER.md) · [DISCLAIMER.en.md](DISCLAIMER.en.md) |
| 路线图 | [.github/ROADMAP.md](.github/ROADMAP.md) |

---

## 路线图

完整路线图见 [.github/ROADMAP.md](.github/ROADMAP.md)。

### 2026 Q3
- P0/P1 全部修复项闭环（架构合规 / 安全 / 性能）
- 覆盖率门槛：user-server ≥ 50%、platform-server ≥ 40%
- 客服聊天窗口：左侧聊天 / 右侧二维码永久布局
- 坐席实时聊天看板：Vue 3 三栏 + AI/人工切换 + 拉黑 TTL
- 本地推理栈：一键部署脚本 + 5 阶段自检

### 2026 Q4
- 列表大查询改 keyset 分页 + 深度上限 1000
- WebSocket 重连 + 离线补发 + ack
- Swagger 注解补全 + 部署视频

### 不做的事
- ❌ OTA 客户端更新（违反项目硬约束）
- ❌ 计费 / 定价 / 商业化 SaaS
- ❌ 申请 / 注册 / 账号开通流程
- ❌ 修改开源协议（保持 AGPL-3.0-or-later）

---

## 常见问题 FAQ

### 这是什么项目？和 Dify / FastGPT 有什么区别？

HiveMtk 是**私域营销 AI 操作系统**，聚焦"多端社媒触达 + 销冠 SOP + CDP 客户中台 + 零出域"；Dify / FastGPT 是**通用 LLM 应用开发平台**，更偏 Workflow 编排。HiveMtk 直接面向营销获客、转化、复购业务场景，内置完整业务模块，开箱即用。

### 是真的开源吗？可以商用吗？

✅ 完全开源，AGPL-3.0 协议。可自由 fork、私有部署、二次开发、商用。**唯一限制**：修改后的版本若通过网络对外提供服务（SaaS / 云端 API），必须按 AGPL-3.0 同样开源你的修改。仅内部私有部署无需公开。

### 数据真的不出域吗？怎么保证？

✅ 100% 零出域。所有对话、知识库、向量化、检索增强全程在客户内网完成。本地推理栈（llama.cpp + TEI）跑在客户内网，FRP 私域穿透时云端不落一条对话。即使配置云端 LLM，也只传 prompt 文本，客户 PII 数据在本地脱敏后再调用。

### 支持哪些大模型？可以接 GPT-4 / DeepSeek 吗？

✅ 已接入 DeepSeek / 通义千问 / GPT-4o / 智谱 GLM / Qwen2.5（本地）。LLM 路由网关按场景动态路由：复杂异议用强模型，常规回复用轻模型。Embedding/Rerank 强制本地（bge-m3 + bge-reranker-v2-m3）。

### 多端是哪几端？海外能用吗？

抖音、快手、小红书、闲鱼、TikTok、微信/企业微信、邮件，以及 Telegram / WhatsApp / 短信，共 10+ 个社媒/沟通渠道统一接入。其中社媒五端经 **Chrome 扩展桥接**（你自己的登录态浏览器在线即可，无需无头浏览器）；Telegram / WhatsApp / 邮件为协议直连。海外版可接 TikTok + WhatsApp + Telegram + Email，适配跨境出海场景。

### 部署需要什么硬件？要多 GPU 吗？

- 最低：4 核 CPU / 8GB 内存 / 50GB 磁盘（dev 档，Qwen2.5-3B + bge-m3 Q4）
- 推荐：8 核 CPU / 16GB 内存 / 100GB 磁盘（prod 档，Qwen2.5-14B + bge-m3 F16）
- GPU 可选：NVIDIA 8GB+（dev）/ 16GB+（prod），无 GPU 也可 CPU 推理

### 不想自己部署，有 SaaS 版吗？

❌ 本项目**不提供 SaaS 版本**。坚持私有化部署、数据自主可控。如需企业级技术支持、定制集成，联系 `jideilvluoqun@gmail.com`。

### 如何贡献代码？有代码规范吗？

见 [CONTRIBUTING.md](CONTRIBUTING.md)。后端遵循五层架构（Controller→Service→Repository→Model→DTO），CI 会强制检查跨层调用。运行 `scripts/check-architecture.sh` 自检。

---

## 合规与免责

### 主动触达模块合规声明

> **请在使用本项目的「主动触达」功能前,务必仔细阅读本声明。**

HiveMtk 的**主动触达模块**（短信、邮件、微信公众号 / 企业微信、抖音 / 快手 / 小红书 / 闲鱼私信、Telegram、WhatsApp（Meta）、网页客服等向用户**主动推送消息**的能力）属于**核心敏感功能**。本项目作为开源工具,**不对使用者如何调用这些能力承担责任**。

完整合规声明见 [DISCLAIMER.md](DISCLAIMER.md)。

> 📌 **运行时强制提示**:每次主动触达发送,服务端日志都会打印一条 `[COMPLIANCE]` 合规提示,提醒操作者遵守各渠道平台规则。该提示不可关闭。

### 开源免责声明

> **HiveMtk 是一个完全开源的本地私有化客服底座工具。** 用户利用本系统本地部署任何大语言模型、构建知识库及进行对话时,**必须自行遵守所在国家、地区以及相关社交平台的法律法规**。作者不参与任何用户的实际部署与运营,亦不对用户因本地模型产生的任何言论、内容合规性及导致的任何后果承担任何法律责任。

完整免责声明见 [DISCLAIMER.md](DISCLAIMER.md)（[English](DISCLAIMER.en.md)）。

---

## 联系与社区

| 渠道 | 入口 | 说明 |
|------|------|------|
| 🐛 **Bug / Feature Request** | [Gitee Issues](https://gitee.com/xhpmayun/hivemtk/issues) / [GitHub Issues](https://github.com/xiaofang142/hivemtk/issues) | 提交问题与建议 |
| 💬 **微信交流群** | 通过 Gitee Issue / GitHub Issue / 邮箱申请加入 | 产品/技术/运营答疑，禁止广告/政治/人肉，违者秒踢 |
| 📧 **商务合作 / 技术支持** | jideilvluoqun@gmail.com | 企业级技术支持、定制集成、私有部署咨询 |
| 🔒 **安全漏洞** | jideilvluoqun@gmail.com | 私密报告，详见 [SECURITY.md](SECURITY.md) |
| 🤝 **贡献者公约** | [CONTRIBUTING.md](CONTRIBUTING.md) | 提交 PR、参与开发、行为准则 |
| 🌐 **官网联系入口** | [hivemtk-platform site-contact](https://gitee.com/xhpmayun/hivemtk-platform) | 由平台端 site-contact 模块管理 |
| 📖 **文档站** | 部署后访问 `http://<host>:8204/docs` | 本地部署自带文档站 |

> **群规**:禁止广告 / 禁止政治 / 禁止人肉,违者秒踢。

---

## 镜像仓库

| 平台 | 仓库地址 | 角色 | 同步状态 |
|------|---------|------|---------|
| Gitee | [gitee.com/xhpmayun/hivemtk](https://gitee.com/xhpmayun/hivemtk) | 主仓库（首发） | 实时同步 |
| GitHub | [github.com/xiaofang142/hivemtk](https://github.com/xiaofang142/hivemtk) | 镜像仓库 | GitHub Actions 定时同步 |
| 平台端 Gitee | [gitee.com/xhpmayun/hivemtk-platform](https://gitee.com/xhpmayun/hivemtk-platform) | 平台端主仓库 | 实时同步 |
| 平台端 GitHub | [github.com/xiaofang142/hivemtk-platform](https://github.com/xiaofang142/hivemtk-platform) | 平台端镜像 | GitHub Actions 定时同步 |

> 💡 **国内用户建议克隆 Gitee 主仓库**，下载速度更快；海外用户可用 GitHub 镜像。

---

## License

本项目采用 [GNU Affero General Public License v3.0（AGPL-3.0）](LICENSE) 发布。

**核心诉求（AGPL-3.0 第 13 条 · 远程网络交互）**：任何公司或个人只要**修改了本项目代码，并将其通过网络（SaaS / 云端 / API / 托管实例等）对外提供服务**，就必须按照 AGPL-3.0 向使用该服务的所有用户**免费提供其修改后的完整对应源代码**，且同样以 AGPL-3.0 开源。仅自己内部私有部署、不对外提供网络服务时无需公开修改；但一旦把修改后的版本放到网上为他人提供服务（即使是 SaaS 模式），强制开源即自动生效。

**合规要点**：
- ✅ 私有部署、内部使用、二次开发：完全自由，无需开源
- ✅ 修改代码但仅在公司内网使用：无需开源  
- ⚠️ 修改代码并作为 SaaS/云服务/托管 API 对外提供：必须开源修改部分
- ❌ 将 AGPL-3.0 代码包含在专有软件中分发：不合规

完整条款与免责声明见 [LICENSE](LICENSE)，版权与联系方式见 [NOTICE](NOTICE)。

如需商务合作或技术支持，欢迎通过 [Gitee Issue](https://gitee.com/xhpmayun/hivemtk/issues) / [GitHub Issue](https://github.com/xiaofang142/hivemtk/issues) 或 jideilvluoqun@gmail.com 联系。

---

## 致谢

- **FlagOpen** 团队提供的 BGE 系列 Embedding/Rerank 模型
- **Qwen** 团队提供的 Qwen2.5 指令微调模型
- **llama.cpp** / **TEI** 提供的高性能推理运行时
- 所有贡献者与早期用户

---

<div align="center">

**多端打透 · AI 真自主 · 数据封死在域内**

Made with ❤️ by HiveMtk Team

</div>
