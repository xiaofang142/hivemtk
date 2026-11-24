# HiveMtk 文档索引

> 双形态部署：**离线单机（私域，TENANT_MODE=off 默认）** + **SaaS 多租户（TENANT_MODE=on）**
> 三份主文档回答三个问题：**有哪些功能**（官方文档）· **如何安装运维**（运维手册）· **出问题怎么办**（排查手册）

---

## 一、快速开始（三份主文档）

| 文档 | 回答的问题 |
|------|------|
| [README.md](../README.md) | 仓库入口、目录结构、快速开始 |
| [marketing-features/README.md](marketing-features/README.md) | 官方文档：产品有哪些功能（含已知限制如实披露） |
| [DEPLOYMENT_GUIDE.md](DEPLOYMENT_GUIDE.md) | 运维手册：如何安装、日常如何运维（端口/命令以源码为准） |
| [TROUBLESHOOTING.md](TROUBLESHOOTING.md) | 排查手册：用户会遇到的问题，按现象组织，快速索引表入口 |

---

## 二、功能文档 · 操作手册（handbook）

| 文档 | 读者 |
|------|------|
| [handbook/README.md](handbook/README.md) | 五册入口索引 |
| [handbook/QUICKSTART-FOR-HUMANS.md](handbook/QUICKSTART-FOR-HUMANS.md) | 快速上手与使用手册（交接级）：五大件 · 功能架构与数据流 · 逐模块四要素详解（作用/问题/配置/使用）· 从零到第一条 AI 自动回复 · 分渠道接入 · Bridge/Widget 详解 · 角色权限 · 排障速查（人读，新接手首选） |
| [handbook/FEATURES-FOR-HUMANS.md](handbook/FEATURES-FOR-HUMANS.md) | 功能全集 · 渠道能力 · AI 引擎 · 已知限制（人读） |
| [handbook/OPERATIONS-FOR-HUMANS.md](handbook/OPERATIONS-FOR-HUMANS.md) | 部署 · 配置 · 日常运营 · 备份恢复（人读） |
| [handbook/FEATURES-FOR-AI.md](handbook/FEATURES-FOR-AI.md) | 路由表 · 模块地图 · 文件位置 · 数据结构（AI 读） |
| [handbook/OPERATIONS-FOR-AI.md](handbook/OPERATIONS-FOR-AI.md) | 分层铁律 · 命令 · 门禁 · 新增功能流程（AI 读） |
| [handbook/SAAS_PRODUCTION_CONSTRAINTS.md](handbook/SAAS_PRODUCTION_CONSTRAINTS.md) | SaaS 生产部署约束与教训（多租户编码硬约束，改相关代码前必读） |

---

## 三、部署与运维

| 文档 | 描述 |
|------|------|
| [COMPETITOR_ANALYSIS_202610.md](competitor/COMPETITOR_ANALYSIS_202610.md) | 功能模块级竞品对比论证（渠道/工具/RAG/调度/业务流程/CDP/评测 vs Chatwoot·Dify·FastGPT，2026-10 基线） |
| [COMPETITOR_FEATURE_CHECKLIST_202610.md](competitor/COMPETITOR_FEATURE_CHECKLIST_202610.md) | 逐条对照清单：竞品模块实现 ↔ 我方模块实现（A渠道~H安全 8 组 60+ 条，独有/等价/缺口三档汇总） |
| 未完成任务清单（外层独立仓 `docs/未完成任务清单.md`） | 竞品缺口任务清单已并入唯一任务跟踪入口；内层仓单独 checkout 不含该文件（2026-11-18 删 COMPETITOR_GAP_TASKLIST_202610.md，原文查 git 历史） |
| [operations/README.md](operations/README.md) | 运维层导航（部署初始化 / 运行保障 / 功能专题 / 反代模板） |
| [RELEASE_PREPARATION.md](RELEASE_PREPARATION.md) | 发布准备：用户端/平台端配置、数据初始化、安装部署逻辑核查 + 上服务器清单 |
| [operations/MERCHANT_DEPLOYMENT.md](operations/MERCHANT_DEPLOYMENT.md) | 商户部署手册 |
| [operations/MERCHANT_INITIALIZATION_FLOW.md](operations/MERCHANT_INITIALIZATION_FLOW.md) | 首次启动初始化流程（/setup 开户字段、静默合规采集、开户赠送） |
| [operations/EPAY_LIVE_CHECKLIST.md](operations/EPAY_LIVE_CHECKLIST.md) | 易支付上线前真人支付验收清单（003 · T032） |
| [operations/HA_DEPLOYMENT.md](operations/HA_DEPLOYMENT.md) | 高可用部署（单机+备份） |
| [operations/DR_RECOVERY.md](operations/DR_RECOVERY.md) | 灾难恢复方案 |
| [operations/SLA_SLO.md](operations/SLA_SLO.md) | 服务等级承诺 |
| [operations/secret_rotation.md](operations/secret_rotation.md) | 密钥轮换指南 |

---

## 四、架构

| 文档 | 描述 |
|------|------|
| [architecture/README.md](architecture/README.md) | 架构层导航（总览 / 深度解析 / 多租户与部署形态 / 模块专题 / ADR） |
| [integration/PILOT-CONTRACT.md](integration/PILOT-CONTRACT.md) | EC-00 试点启动基线、身份映射、PilotContract v1、00-A/B/C 模拟门与验证边界 |
| [architecture/TENANT_MODE_REVIEW.md](architecture/TENANT_MODE_REVIEW.md) | 多租户文档口径与授权策略复核（009 · C4） |
| [architecture/部署方案_用户端.md](architecture/部署方案_用户端.md) | 用户端部署架构 |
| [architecture/HOST_INFERENCE_PLAN.md](architecture/HOST_INFERENCE_PLAN.md) | llama.cpp 推理方案 |
| [architecture/adr/](architecture/adr/) | 架构决策记录 |
| [channel-onboarding.md](channel-onboarding.md) | 渠道接入手册（六触点清单 + curl 冒烟；specs/024 T1203，配套机器门 `scripts/check_im_touchpoints.py`） |

---

## 五、核心功能

| 文档 | 描述 |
|------|------|
| [marketing-features/README.md](marketing-features/README.md) | 核心功能总览（同快速开始） |
| [operations/AI_AGENT_PERF_DEPLOY.md](operations/AI_AGENT_PERF_DEPLOY.md) | AI 销冠部署 |
| [operations/AI_AGENT_PERF_API.md](operations/AI_AGENT_PERF_API.md) | AI 销冠 API |
| [operations/KNOWLEDGE_GROUP_DEPLOY.md](operations/KNOWLEDGE_GROUP_DEPLOY.md) | 知识库部署 |
| [operations/PERFORMANCE_BENCHMARK.md](operations/PERFORMANCE_BENCHMARK.md) | 性能基准测试 |

---

## 六、项目规则

| 文档 | 描述 |
|------|------|
| [standards/README.md](standards/README.md) | 标准与口径层导航（规则 / 模板 / 后端 i18n 口径） |
| [standards/MASTER_RULES.md](standards/MASTER_RULES.md) | 项目核心规则与编码规范 |
| [standards/BACKEND_MESSAGE_I18N_POLICY.md](standards/BACKEND_MESSAGE_I18N_POLICY.md) | 后端文案国际化口径与清单（F3） |

---

## 七、嵌入 SDK 与反向代理

| 文档 | 描述 |
|------|------|
| [operations/CHAT_WIDGET_EMBED.md](operations/CHAT_WIDGET_EMBED.md) | Chat Widget 嵌入 SDK 接入指南（`embed-sdk`，ADR-011） |
| [architecture/adr/ADR-011-chat-widget-embed.md](architecture/adr/ADR-011-chat-widget-embed.md) | ADR-011：Chat Widget 嵌入方案决策记录 |
| [architecture/FRP私域部署指南.md](architecture/FRP私域部署指南.md) | FRP 内网穿透私域部署指南 |
| [operations/reverse-proxy/frpc.toml.template](operations/reverse-proxy/frpc.toml.template) | frpc 客户端配置模板 |

---

## 八、相关资源

- [ADR 决策记录](architecture/adr/) — 架构决策历史
- [Bridge 运行手册](operations/Bridge_Runbook.md) — Bridge 桥接模块（内部设计稿 docs/bridge/ 按 .gitignore 只留本地，不入库）
- [AI 功能清单基线](architecture/AI_CORE_FEATURE_INVENTORY.md) — AI 核心链路功能点 F1-F15 / 短板 G1-G12 事实来源（源码实测）

---

## 九、规格与施工文档（specify / specs，唯一施工源）

| 文档 | 说明 |
|------|------|
| [../specs/README.md](../specs/README.md) | spec 施工源索引（001/002/005/006/009/014/015/016） |
| [../specs/001-saas-multi-tenant/](../specs/001-saas-multi-tenant/) | 001 多租户底座（TENANT_MODE 双形态、tenancy、契约） |
| [../specs/005-reach-compliance-policy/](../specs/005-reach-compliance-policy/) | 005 主动触达合规策略（E1，已交付） |
| [../specs/006-wave2-gates-and-selector-health/](../specs/006-wave2-gates-and-selector-health/) | 006 Wave2 门禁与选择器健康（lane 在途） |
| [../specs/009-docs-and-i18n-closeout/](../specs/009-docs-and-i18n-closeout/) | 009 F1 文档分层 + F3 国际化收口 + C4 多租户口径（本 lane） |
| [../specs/014-egolite-blocking-confirm-gate/](../specs/014-egolite-blocking-confirm-gate/) | 014 ego-lite 腿收口：事前阻塞写确认闸（decision 文件握手）+ runner 接线杂项（已交付，2026-10-27） |
| [../specs/015-egolite-runtime-supervision/](../specs/015-egolite-runtime-supervision/) | 015 ego-lite 运行时监督：域白名单三层（静扫/守卫/事件审计）+ 官方升级指令 + macOS 安装器（已交付，2026-10-28） |
| [../specs/016-egolite-scheduling-and-selfupdate/](../specs/016-egolite-scheduling-and-selfupdate/) | 016 ego-lite 调度闭环：流程 cron 触发 + egolite_run AI 工具 + runner 自更新（已交付，2026-10-28） |
| [../specs/024-im-seam-alignment/](../specs/024-im-seam-alignment/) | 024 IM 接入接缝对齐：出站注册表 + 事件类型 + 路由 + 渲染 + 身份双层键 + 生命周期读侧 + 门禁收口（B1–B6 已交付，2026-11-18） |
| [../specs/025-im-platforms/](../specs/025-im-platforms/) | 025 新平台接入：wecomcs / OneBot / Slack / wecombot / custom（已交付，2026-11-18；真实账号项 T707/T806/T904 用模拟信号验收，见 delivery.md） |
| 历史路径 `../.specify/002-modular-subscription/`（当前工作区不包含） | 002 模块订阅一期（模块闸、订单状态机、MarkPaid 唯一入账口、LLM 双轨计费） |
| 历史路径 `../.specify/003-saas-billing-v2/`（当前工作区不包含） | 003 SaaS 计费二期：订阅 + 易支付 + 初始化赠送 + 官网云端入口（原 `plan.md` 含 ADR-M8/M9 与「审查修订」） |

> `.specify/*` 归档说明（2026-11-18）：`002-modular-subscription`、`003-saas-billing-v2`、
> `004-inapp-notification`、`005-geo-site-audit-v2`、`006-geo-autopilot`、`integrations/`、
> `workflows/` 与旧 bug 目录已整体移出当前工作区（内容保留在 git 历史；施工源以 `specs/` 为准）。
> 代码注释与文档中仍可见 `.specify/...` 路径，一律是**历史出处标注**，不代表现存文件，勿据此查找。

> 覆盖关系：003 **显式覆盖** 002 的两处口径——「本阶段不接任何真实支付通道」→ 接入易支付；「收费模块按年订阅、全量线年度合计 999 元/年」→「23 模块 0 元 + 浏览器自动化 ¥99 买断 + 软件订阅 149 元/月」。002 的架构资产全部沿用。

---

## 十、UI 验证与回归（2026-11-25 A 轮补充）

| 文档 | 用途 |
|---|---|
| [verification/ui-audit-playbook.md](verification/ui-audit-playbook.md) | 通用逐页点验流程（环境/判定/常见坑/go test 快测库） |
| [verification/ui-audit-round-a-20261125.md](verification/ui-audit-round-a-20261125.md) | 280 条全量逐页重测 + 22 条新增页重点二次回归实战补充：测试账号、ego-browser 两阶段法脚本模板、台账脚本、版本过滤回归要点、go test 4 个失败根因与修复、CSV 导出缺陷修复、测试数据副作用与清理结果 |

---

*最后更新: 2026-10-24 · 009 收口：修正头句为双形态、挂入 architecture/operations/standards/specs 四层导航、补 SAAS_PRODUCTION_CONSTRAINTS 与 TENANT_MODE_REVIEW、补 005/006/009 规格施工源；三份主文档与 AI 功能清单基线口径不变*
*2026-11-25 补充：第十节挂入 UI 全量逐页点验 playbook 与 A 轮实战补充*

---

## 四、按主题索引（2026-10-26 目录收敛后补登）

### 当前待办与验证台账

| 文档 | 用途 |
|---|---|
| docs/OPEN_ITEMS.md、docs/G0-*.md、docs/verification/ 轮报 ×7、operations/WhatsApp-库替换方案 | **已删**（2026-10-29 历史清扫，CLAUDE.md 规则 0c）：任务只跟踪工作区根 `docs/未完成清单.md`，交付记录只进 commit message；原文查 git 历史 |

### 竞品与调研

| 文档 | 用途 |
|---|---|
| [competitor/COMPETITOR_ANALYSIS_202610.md](competitor/COMPETITOR_ANALYSIS_202610.md) | 模块级竞品对比 |
| [competitor/COMPETITOR_FEATURE_CHECKLIST_202610.md](competitor/COMPETITOR_FEATURE_CHECKLIST_202610.md) | 逐条能力对照清单 |
| [competitor/COMPETITOR_DEEP_DIVE_20261024.md](competitor/COMPETITOR_DEEP_DIVE_20261024.md) | 实现原理级对照 |
| [research/OURSTACK_IMPL_PRINCIPLES_20261024.md](research/OURSTACK_IMPL_PRINCIPLES_20261024.md) | 我方实现原则（原理层） |

### 部署 / 贡献 / 设计

| 文档 | 用途 |
|---|---|
| [SECRETS_DEPLOYMENT.md](SECRETS_DEPLOYMENT.md) | 密钥与凭证的部署注入口径 |
| [CONFIG_INVENTORY_20261020.md](CONFIG_INVENTORY_20261020.md) | 配置项全量盘点（参数中心/环境变量） |
| [contribute/CODEBASE_MAP.md](contribute/CODEBASE_MAP.md) | 代码地图（按层定位） |
| [contribute/CODE_COLLABORATION.md](contribute/CODE_COLLABORATION.md) | 协作与改动边界 |
| [marketing-features/GEO_MODULE_DESIGN.md](marketing-features/GEO_MODULE_DESIGN.md) | GEO 模块设计说明 |
| `bridge/BRIDGE-ARCHITECTURE-REVIEW.md` | 浏览器桥接架构复核结论（**本地文档**：`docs/bridge/` 已被 `.gitignore` 排除、不入库，故此处不给链接，避免 CI 断链门红） |
| [architecture/adr/ADR-016-telegram-group-gate-policy.md](architecture/adr/ADR-016-telegram-group-gate-policy.md) | ADR-016 Telegram 社群门控三项口径 |
