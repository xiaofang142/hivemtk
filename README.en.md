<div align="center">

# 🐝 HiveMtk · Open-Source Self-Hosted AI Marketing System

**Turn your best salesperson's playbook into every rep's daily workflow.**

One workspace for every social channel. One AI agent for every customer conversation. Zero data leaving your perimeter.

[Open-Source SCRM](#how-it-compares) · [ReAct Autonomous Agent](#-react-autonomous-agents-not-dead-workflows) · [10+ Channels](#-all-channel-reach-one-workspace) · [Zero Data Egress](#-data-security-100-on-prem-zero-egress) · [5-Minute Self-Hosting](#-up-and-running-in-5-minutes)

</div>

<div align="center">

[![Go 1.25](https://img.shields.io/badge/Go-1.25-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![Vue 3](https://img.shields.io/badge/Vue-3-42b883?logo=vue.js&logoColor=white)](https://vuejs.org)
[![Docker](https://img.shields.io/badge/Docker-24+-2496ED?logo=docker&logoColor=white)](https://www.docker.com)
[![PostgreSQL 15+](https://img.shields.io/badge/PostgreSQL-15+-336791?logo=postgresql&logoColor=white)](https://www.postgresql.org)
[![License AGPL-3.0](https://img.shields.io/badge/license-AGPL--3.0-blue)](LICENSE)
[![Gitee](https://img.shields.io/badge/Gitee-xhpmayun%2Fhivemtk-C71D23?logo=gitee)](https://gitee.com/xhpmayun/hivemtk)
[![GitHub](https://img.shields.io/badge/GitHub-xiaofang142%2Fhivemtk-181717?logo=github)](https://github.com/xiaofang142/hivemtk)

[📖 中文文档](README.md) · [🚀 Live Demo](#-live-demo) · [📦 Feature Modules](docs/marketing-features/README.md)

</div>

---

## At a Glance

| | | | |
|---|---|---|---|
| **94** business modules<br>SCRM + AI + CDP + automation | **10+** reach channels<br>Douyin / Xiaohongshu / TikTok / WhatsApp… | **41** agent tools<br>ReAct autonomous orchestration | **0** bytes of data egress<br>Local inference, fully offline-capable |

---

## What Is This

**HiveMtk** ("Hive" + "Marketing Toolkit") is an **open-source AI marketing system for private-domain operations**, nailing four things in a single repo:

1. **All-channel social reach** — Douyin / Kuaishou / Xiaohongshu / Xianyu / TikTok via a Chrome-extension bridge; WeCom / Telegram / WhatsApp / Email / SMS via protocol-direct; one unified inbox
2. **ReAct autonomous agents** — perceive → plan → tool-call → reflect; 41 atomic tools composed on the fly, not hardcoded if-else workflows
3. **Local knowledge base RAG** — pgvector 1024-dim hybrid retrieval + bge-m3 + bge-reranker-v2-m3 fine ranking
4. **Zero data egress** — llama.cpp (Qwen2.5) + TEI local inference stack; conversations, knowledge base, and embeddings never leave your network

Covers the full **acquisition → outreach → conversion → repurchase** funnel. Not an LLM wrapper — a complete system that runs real marketing operations, plus a GEO module closing the AI-search acquisition loop. The full feature list (and known limitations) is honestly disclosed in [docs/marketing-features/README.md](docs/marketing-features/README.md).

**Who it's for**: growing teams of 5–50 · compliance-sensitive industries (finance / healthcare / government) · operators done with SaaS vendor lock-in.

---

## How It Compares

> Honest comparison, weaknesses included. **No silver bullets** — pick what fits.

| Dimension | **HiveMtk** (this repo) | Dify / FastGPT | Commercial SCRM | SourceQue / MoChat |
|-----------|------------------------|----------------|-----------------|--------------------|
| Core positioning | AI marketing system | General LLM app platform | Commercial SaaS | WeCom SCRM |
| Reach channels | **Multi-channel (10+)** | None built-in | 1-3 | 1 (WeCom) |
| AI capability | **ReAct agents + 41 tools** | Visual Workflow | Basic CS bot | Simple RAG / none |
| Data deployment | **100% on-prem + local inference** | Self-host / SaaS | Cloud | SaaS / private |
| License | AGPL-3.0 | Apache-2.0 | Proprietary | Partially open |
| Best for | Teams wanting **self-control, multi-channel, strong AI** | Pure AI app dev | SMBs without IT | WeCom power users |

---

## Core Capabilities

### 🌐 All-Channel Reach: One Workspace

| Channel | Outreach | Smart Cards | Auto-Reply | RAG CS | Notes |
|---------|----------|-------------|------------|--------|-------|
| Douyin / Kuaishou / Xiaohongshu / Xianyu / TikTok | ✅ | ✅ | ✅ | ✅ | Chrome-extension bridge over your own logged-in browser — no headless browser |
| WeChat / WeCom | ✅ | — | ✅ | ✅ | Groups + Moments |
| Telegram | ✅ | — | ✅ | ✅ | Bot protocol direct: group-lead mining + proactive DM + gate control |
| WhatsApp | ✅ | — | ✅ | ✅ | Cloud API + template messages |
| Email / SMS | ✅ | — | ✅ | — | SMTP; Aliyun / Tencent / Huawei |

Unified CDP customer profiles, one profile reaching everywhere; unified inbox — every conversation, ticket, and DM in one place. Per-channel details and Telegram's five acquisition capabilities: [docs/marketing-features/README.md](docs/marketing-features/README.md).

### 🤖 ReAct Autonomous Agents: Not Dead Workflows

- **ReAct loop**: perceive → plan → tool-call → reflect; unseen scenarios get handled by composing tools on the fly
- **41 atomic tools** across five domains (reach / projects / customers / knowledge base / business), each wrapped by a five-layer decorator chain: Permission → Retry → Timeout → RateLimit → Audit
- **Hybrid RAG**: pgvector HNSW + BM25 with RRF fusion, bge-reranker fine ranking, optional HyDE / MultiQuery rewrite
- **Multi-agent collaboration**: reactive answering agent + proactive outreach agent; four-layer memory (short-term / long-term / SOP state / business facts)
- **Visual workflow builder**: zero-code SOP editor for marketing automation

> Tool registration logic, per source: [user-server/internal/aiagent/agent/tooluse/](user-server/internal/aiagent/agent/tooluse/) (code as inventory).

### 🔒 Data Security: 100% On-Prem, Zero Egress

- **Local inference stack**: llama.cpp (Qwen2.5) + TEI (bge-m3 + bge-reranker-v2-m3) — three OpenAI-compatible services (LLM / Embedding / Rerank) inside your network
- **Fully offline-capable**: conversations, knowledge base, embeddings, and RAG all complete within your perimeter
- **FRP private tunneling**: visitors come in from the public internet, data flows back through the tunnel — the cloud never stores a single message
- **Compliance-friendly**: meets data-residency and private-deployment baselines
- **Optional cloud LLM**: point `LLM_BASE_URL` at DeepSeek/OpenAI for stronger models; Embedding/Rerank stay strictly local

### 🎯 GEO Optimization: AI-Search Acquisition Loop

> SEO fights for search ranking; GEO (Generative Engine Optimization) fights for a **seat in the AI answer** — getting your brand cited when ChatGPT Search, Perplexity, or Google SGE answers.

```
Brand config → Keyword distillation → Content creation → Multi-model validation → Platform syndication
                    ↑                                                  ↓
              Data feedback ← Historical validation data ←──────────────┘
```

Keyword distillation · E-E-A-T + Schema injection · multi-vendor LLM simulation quantifying your "AI answer seat" · RAG grounding to brand facts · high-authority platform syndication. Full guide: [user-server/docs/geo-module-guide.md](user-server/docs/geo-module-guide.md).

---

## Typical Use Cases

| Scenario | Key Capabilities | Industries |
|----------|-----------------|------------|
| Private-domain acquisition → AI auto-selling | Multi-channel lead intake + ReAct agent handles inbound + objection handling + closing | Franchise / Medical aesthetics / EdTech / B2B |
| WeCom SCRM multi-account aggregation | Unified inbox + customer asset sink + departure inheritance | Chain stores / Brands |
| AI customer service / RAG knowledge base | Hybrid retrieval + 7×24 auto-reply | E-commerce / SaaS / After-sales |
| Marketing automation SOP | Visual SOP editor + RFM segmentation + churn prediction + reactivation | Membership retail / Beauty |
| Cross-border multi-channel outreach | TikTok + WhatsApp + Email + Telegram unified | Cross-border e-commerce |
| Compliant on-premise deployment | Local inference + zero egress + row-level security + audit archive | Finance / Healthcare / Government |

---

## 🚀 Up and Running in 5 Minutes

> Requirements: Docker 24+ with Docker Compose v2. Minimum hardware 4 cores / 8GB RAM / 50GB disk (dev tier); recommended 8 cores / 16GB (prod tier with 14B model). GPU optional — CPU-only inference works.

```bash
# 1. Clone (Gitee primary for China; GitHub mirror elsewhere)
git clone https://gitee.com/xhpmayun/hivemtk.git && cd hivemtk

# 2. One-click install: .env template + build frontends + download models + start data layer & inference stack
make install

# 3. Set 4 secrets
vim .env   # POSTGRES_PASSWORD / REDIS_PASSWORD / JWT_SECRET / PLATFORM_ADMIN_PASSWORD

# 4. Start
make dev                    # user-server with hot reload → http://localhost:8204
cd user-web && npm run dev  # Admin workspace → http://localhost:8211 (another terminal)
```

**Live Demo**: https://hiveuser.xapptool.cn/ (login `admin` / `Seed@123456`; demo data is public — do not upload real business data; subject to [Releases](https://github.com/xiaofang142/hivemtk/releases) announcements)

For dev/prod model tiers, common commands, and backup/restore operations, see [docs/DEPLOYMENT_GUIDE.md](docs/DEPLOYMENT_GUIDE.md) and [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md).

---

## FAQ

**How is it different from Dify / FastGPT?**
HiveMtk is a marketing system focused on "all-channel reach + sales-copilot SOP + CDP + zero egress", shipping 94 business modules out of the box. Dify / FastGPT are general LLM application platforms oriented around Workflow orchestration, without built-in marketing business.

**Is it really open-source? Can I use it commercially?**
✅ Fully open-source under AGPL-3.0. Fork, self-host, extend, and use commercially — free. The only constraint: if you offer a modified version as a network service (SaaS / cloud API), you must release your modifications under AGPL-3.0 as well. Internal-only self-hosting never triggers this.

**Does data really stay on-premise?**
✅ All conversations, knowledge base, embeddings, and RAG stay inside your network. The local inference stack runs on-prem; with FRP tunneling the cloud never stores a message. Even with a cloud LLM, only prompt text is sent, and customer PII is desensitized locally first.

**Which LLMs are supported?**
✅ DeepSeek / Qwen / GPT-4o / GLM / local Qwen2.5. The LLM routing gateway dispatches by scenario: strong models for complex objections, light models for routine replies. Embedding/Rerank are strictly local.

**Can it be used overseas?**
✅ TikTok + WhatsApp + Telegram + Email cover cross-border scenarios.

**Is there a SaaS version?**
❌ No. We insist on private deployment and data sovereignty. For enterprise support or custom integration: jideilvluoqun@gmail.com.

---

## Architecture at a Glance

```
   Visitor Browser (Public Internet)
       │ HTTPS / WSS (via FRP / public IP / reverse proxy)
       ▼
   ┌─────────────────────────────────────────────────┐
   │  Customer On-Prem Network                       │
   │                                                 │
   │  user-server (Go + Gin) :8204                   │
   │    ├── PostgreSQL + pgvector :8202 (1024-dim)   │
   │    └── Redis 7 :8203                            │
   │                                                 │
   │  Host Inference Stack (llama.cpp, no containers)│
   │    ├── LLM :8207 (Qwen2.5)                      │
   │    ├── Embedding :8208 (bge-m3)                 │
   │    └── Rerank :8209 (bge-reranker-v2-m3)        │
   │                                                 │
   │  user-web (Vue 3) · embed-sdk (CS Web Widget)   │
   └─────────────────────────────────────────────────┘
            │ HTTPS (low-frequency: heartbeat / merchant-key check)
            ▼
   Platform (separate repo: hivemtk-platform): metadata only.
   Never touches, stores, or accesses your business data.
```

The backend strictly follows a five-layer architecture (Controller → Service → Repository → Model → DTO, no cross-layer calls, CI-enforced). Full spec: [docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md](docs/architecture/GO_FIVE_LAYER_ARCHITECTURE.md); deployment split with the platform: [docs/operations/MERCHANT_DEPLOYMENT.md](docs/operations/MERCHANT_DEPLOYMENT.md).

**Tech stack**: Go 1.25 + Gin + GORM · Vue 3 + Vite + Element Plus + Pinia · PostgreSQL 15 + pgvector · Redis 7 · llama.cpp + Qwen2.5 · TEI + bge-m3 / bge-reranker-v2-m3 · Docker Compose.

---

## Repository Layout

```
hivemtk/                        # User-side repo
├── user-server/                # Go backend (core business, 5-layer arch) → user-server/README.md
├── user-web/                   # Vue 3 frontend (B-side workspace) → user-web/README.md
├── embed-sdk/                  # Embeddable CS Web Widget (IIFE/ESM)
├── migrations/                 # DB migration SQL (idempotent)
├── scripts/inference-host/     # Host inference stack scripts (llama.cpp + TEI)
├── docs/                       # Docs: INDEX.md / DEPLOYMENT_GUIDE.md / marketing-features/ …
├── docker-compose.yml          # Data layer orchestration (PG + Redis)
├── Makefile                    # install / up / down / inference / dev
└── .env-example                # Env template
```

---

## Documentation

| Doc | Entry |
|-----|-------|
| 📖 Chinese readme | [README.md](README.md) |
| 📚 Doc index | [docs/INDEX.md](docs/INDEX.md) |
| 🚀 Deployment guide / Troubleshooting | [docs/DEPLOYMENT_GUIDE.md](docs/DEPLOYMENT_GUIDE.md) · [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) |
| 📦 94 feature modules in detail | [docs/marketing-features/README.md](docs/marketing-features/README.md) |
| 🧭 Roadmap | [.github/ROADMAP.md](.github/ROADMAP.md) |
| 🤝 Contributing / Security | [CONTRIBUTING.md](CONTRIBUTING.md) · [SECURITY.md](SECURITY.md) |

---

## Compliance & Disclaimer

Proactive outreach (SMS / email / social DM / Telegram / WhatsApp) is a core sensitive capability; as an open-source tool, this project takes no responsibility for how users invoke it. Every proactive send prints an unclosable `[COMPLIANCE]` notice in server logs. Full statement: [DISCLAIMER.md](DISCLAIMER.md) ([English](DISCLAIMER.en.md)).

**License**: released under [AGPL-3.0](LICENSE). Private deployment, internal use, and modification are fully free with no obligation to open-source; offering a modified version as a SaaS / cloud service / managed API triggers the copyleft obligation for your modifications. See [NOTICE](NOTICE) for copyright and contact.

---

## Contact & Community

| Channel | Entry | Notes |
|---------|-------|-------|
| 🐛 Bug / Feature Request | [GitHub Issues](https://github.com/xiaofang142/hivemtk/issues) / [Gitee Issues](https://gitee.com/xhpmayun/hivemtk/issues) | Report issues & suggestions |
| 💬 WeChat group | Apply via Issue / email | Product / tech / ops Q&A; no ads, no politics |
| 📧 Business / Support | jideilvluoqun@gmail.com | Enterprise support, custom integration, deployment consulting |
| 🔒 Security reports | jideilvluoqun@gmail.com | Private disclosure, see [SECURITY.md](SECURITY.md) |

**Mirrors**: Gitee primary [gitee.com/xhpmayun/hivemtk](https://gitee.com/xhpmayun/hivemtk) (faster in China) · GitHub mirror [github.com/xiaofang142/hivemtk](https://github.com/xiaofang142/hivemtk) (Actions-synced)

---

<div align="center">

⭐ **Star / Watch to follow along**

**All Channels · True AI Autonomy · Zero Data Egress**

Made with ❤️ by HiveMtk Team

</div>
