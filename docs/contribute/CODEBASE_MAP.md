# HiveMtk 代码目录清单(2026-09-04,基于 master 86575d6)

> 本仓库为 **HiveMtk 用户端**(商户私有化部署部分)。平台端(`hivemtk-platform`)是独立仓库,不在本仓库内。

## 一、顶层结构

```
hivemtk/
├── user-server/          # Go 后端(Gin+GORM+PostgreSQL+Redis+pgvector)——核心,88MB,11.2万行
├── user-web/             # Vue 3 前端(Vite+Element Plus+Pinia),8.5万行 JS/Vue
├── embed-sdk/            # 嵌入式客服聊天浮窗 SDK(原生 JS+Vite)
├── migrations/           # SQL 迁移脚本(50 个)+ init-user-db.sql(建库/建扩展)
├── scripts/              # 运维/CI/推理栈脚本(check-architecture.sh、inference-host/ 等)
├── tests/                # 回归测试脚本(asset_market / permission API)
├── docs/                 # 部署指南、端口注册表、架构文档、排障手册
├── deploy/               # 部署物料
├── docker-compose.yml    # 仅数据层:PG15+pgvector(8202) + Redis(8203)
├── Makefile              # 全部构建/启动入口(make install / dev / test-go / lint)
├── .env-example          # 环境变量样例(端口/密钥/模型)
├── CONTRIBUTING.md       # 贡献指南
├── GIT_RULES.md          # Git 分支与 commit 规范(唯一权威)
├── CLAUDE.md             # AI 编码铁律(五层架构/API 规范)
├── CLA.md / LICENSE      # 贡献者协议 / AGPL-3.0
└── hivemtk/scripts/      # 子目录仅含脚本(非重复代码)
```

## 二、user-server(Go 后端)

### 入口 `cmd/`
| 目录 | 用途 |
|------|------|
| `cmd/api/` | **主服务**(8204 端口,main.go 装配 DI+cron+迁移) |
| `cmd/bridge-mock/` | 渠道桥接模拟器(本地调试) |
| `cmd/embedding-server/` | 独立 embedding 服务 |
| `cmd/geo-run/`、`cmd/eval-trace/`、`cmd/perf/`、`cmd/routeinspect/` | GEO/评测/压测/路由巡检工具 |
| `cmd/importfaq/`、`cmd/syncscripts/` | FAQ 导入、脚本同步工具 |

### 内部五层架构 `internal/`(共 38 个包)
```
Router → Controller(Handler) → Service → Repository → Model
CI 通过 scripts/check-architecture.sh 强制检查,禁止跨层调用
```

| 层 | 目录 | 规模(非测试文件) |
|----|------|------|
| Router | `router/` | 27 个路由文件(按域拆分:auth/business/system/admin/geo/embed_static...) |
| Controller | `controller/` | 210 个文件 |
| Service | `service/` | 289 个文件(501 个含测试) |
| Repository | `repository/` | 201 个文件 |
| Model | `model/` | 174 个文件 |

### service 层主要业务域(60+,列举代表性)
- **渠道触达**:bridge(桥接入站)、chat_channel、wecom/dingtalk/feishu/telegram/whatsapp/email/sms 各渠道
- **AI 核心**:ai_agent(ReAct 智能体)、ai_debounce、intent_recognition、humanize(拟人化)、confidence
- **客户运营**:customer_360、customer_journey、user_segment(RFM)、clue(线索)、churn_score(流失)
- **营销资产**:card(抖音/快手/小红书/闲鱼卡片)、short_link、script_template、asset_bundle/asset_market
- **知识/RAG**:knowledge、faq、rag、geo(GEO 优化)
- **SOP 自动化**:sop_agent、sop_template、workflow_orchestrator、ab_experiment
- **平台协作**:platform(心跳/授权)、asset_market(资产市场)、alert_*(告警)

### 其他关键包
| 包 | 用途 |
|----|------|
| `config/` | 配置加载,`ports.go` 是端口唯一代码源(8204/8202/8203/8207-8209) |
| `pkg/db/` | GORM 连接 + AutoMigrate(自动建 pgvector 扩展,白名单校验) |
| `aiagent/` | 智能体运行时、LLM dispatcher(含 failover/trace/cache) |
| `migration/` | 启动期迁移框架(注册制,`migrations/` 下 30+ 个迁移器) |
| `secrets/` | MASTER_KEY AEAD 字段加密(release 模式缺失则拒绝启动) |
| `security/` | NetworkExposureGuard 私网护栏 |
| `websocket/`、`event/`、`cron/` | WS hub、事件总线、定时任务 |

## 三、user-web(Vue 3 前端)

```
user-web/src/
├── api/            # API 出口(统一封装)
├── views/          # 80 个视图目录,180 个 .vue(与后端业务域一一对应)
│   ├── 渠道:chat/ bridge/ email/ sms/ telegram/ wecomAccount/ dingtalkApp/
│   │        feishu/ whatsapp/ whatsappCloud/ whatsappBot/ xiaohongshuCard/
│   │        douyinCard/ kuaishouCard/ tiktokCard/ xianyuCard/
│   ├── AI:aiAgent/ intentRecognition/ humanize/ confidence/ sopAgent/
│   ├── 客户:customer360/ customerJourney/ userSegment/ tagSegmentation/
│   ├── 营销:reachPipeline/ marketingFlow/ abExperiment/ leadMining/
│   ├── 系统:system/ setup/ llmRouting/ backup/ securityAudit/
│   └── 其他:knowledge/ knowledgeBase/ geo/ assetMarket/ livecode/ ...
├── components/     # 24 个公共组件(PageHeader/PageState/QuickReplyPanel...)
├── stores/         # Pinia 状态
├── router/、layout/、i18n/、styles/、utils/、composables/、stories/(Storybook)
└── 约束:纯 JavaScript,禁止 TypeScript
```

## 四、embed-sdk(嵌入式 SDK)

原生 JavaScript + Vite,构建为 IIFE 产物 `marketing-chat-widget.iife.js`,由 user-server 托管在 `/embed/*`,第三方网站通过 `<script>` 引入即得客服聊天浮窗。

## 五、数据层与推理栈

| 组件 | 端口 | 说明 |
|------|------|------|
| PostgreSQL 15 + pgvector | 8202(Docker)/ 8232(原生 dev) | 1024 维 HNSW 向量索引,knowledge_embeddings 等 300+ 表(AutoMigrate 自动建) |
| Redis 7 | 8203 | 幂等守卫/缓存/限流 |
| llama.cpp LLM | 8207 | Qwen2.5-3B(Q4_K_M,2GB),支持 ReAct function-call |
| Embedding | 8208 | bge-m3(1024 维) |
| Rerank | 8209 | bge-reranker-v2-m3 |

`scripts/inference-host/` 提供 install/download/start/warmup/smoke-test 全套脚本;LLM 提供商配置运行时存 `llm_providers` 表(后台「LLM 路由」页管理),不落配置文件。

## 六、启动顺序(官方)

```
cp .env-example .env(改密钥)
→ make install(前端构建+模型下载+数据层+推理栈)
→ make dev(air 热更新启动 user-server)
→ cd user-web && npm run dev(前端 5173)
→ 健康检查 curl http://localhost:8204/health
→ 首次访问调 POST /api/system/init-admin 创建超管
```

## 七、质量护栏

| 工具 | 命令 | 作用 |
|------|------|------|
| check-architecture.sh | CI | 五层架构静态检查(禁止跨层调用) |
| golangci-lint + depguard | `make lint` | 分层依赖方向护栏(`user-server/.golangci.yml`) |
| go vet / go test | `make vet` / `make test-go` | 静态检查+单测 |
| Playwright | `tests/ui/user/` | 前端 UI 测试 |
| smoke-test.sh | `make inference-host-test` | 推理栈端到端 |
| commit-msg 钩子 | `git config core.hooksPath .githooks` | Conventional Commits 校验 |
