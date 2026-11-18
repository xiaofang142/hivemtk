# HiveMtk 架构层导航（docs/architecture/）

> 本页是 `hivemtk/docs/architecture/` 的分层索引，补齐此前「架构文档散落、无层内导航」的缺口。
> 权威入口仍是顶层 [`docs/INDEX.md`](../INDEX.md)；本页只做层内归类，不重复造数。
> 基线：`hivemtk/` 仓库 main，复核 2026-10-24。ADR 决策记录见 [`adr/README.md`](adr/README.md)。

## 一、总览与基线

| 文档 | 说明 |
|---|---|
| [ARCHITECTURE_DIAGRAM.md](ARCHITECTURE_DIAGRAM.md) | 系统级架构图（C4 Model）· 命名铁律 E6/E7 权威源 |
| [GO_FIVE_LAYER_ARCHITECTURE.md](GO_FIVE_LAYER_ARCHITECTURE.md) | Go 五层架构编码规范（最高约束） |
| [AI_CORE_FEATURE_INVENTORY.md](AI_CORE_FEATURE_INVENTORY.md) | AI 核心链路功能点清单（F1–F15 / 短板 G1–G12） |
| [FEATURE_INVENTORY_R39.md](FEATURE_INVENTORY_R39.md) | 全功能清单与架构图（R39） |
| [GEO_FEATURE_INVENTORY.md](GEO_FEATURE_INVENTORY.md) | GEO 模块全景清单 |
| [ALL_ENDS_CAPABILITY_ANALYSIS.md](ALL_ENDS_CAPABILITY_ANALYSIS.md) | 全端点能力分析 |

## 二、深度解析

| 文档 | 说明 |
|---|---|
| [DATABASE_SCHEMA_DEEP_DIVE.md](DATABASE_SCHEMA_DEEP_DIVE.md) | 数据库 Schema 深度解析（⚠️ §3.1「单商户无租户列」已被 001 演进，勘误见 TENANT_MODE_REVIEW.md） |
| [USER_SYSTEM.md](USER_SYSTEM.md) | 用户体系规范 |
| [MENU_PERMISSION_PLAN.md](MENU_PERMISSION_PLAN.md) | 菜单与权限设计 |
| [intent_unify_plan.md](intent_unify_plan.md) | intent_records / intent_logs 整合方案 |
| [pg_enum_strategy.md](pg_enum_strategy.md) | PostgreSQL ENUM 策略 |
| [LLM_BILLING_FLOW.md](LLM_BILLING_FLOW.md) | LLM 计费流程 |
| [ORDER_WEBHOOK_SIGNATURE.md](ORDER_WEBHOOK_SIGNATURE.md) | 订单回调签名 |
| [PLATFORM_USER_COMMUNICATION.md](PLATFORM_USER_COMMUNICATION.md) | 平台用户通信 |
| [CONTENT_MODERATION_RESEARCH.md](CONTENT_MODERATION_RESEARCH.md) | 内容审核调研 |
| [MODULAR_MODULES.md](MODULAR_MODULES.md) | 模块化订阅模块 |
| [MODULE_MARKET_UI.md](MODULE_MARKET_UI.md) | 模块市场 UI |
| [PSEUDO_INCOMPLETE_INVENTORY_2026-10.md](PSEUDO_INCOMPLETE_INVENTORY_2026-10.md) | 伪不完整功能清单（2026-10） |

## 三、多租户与部署形态

| 文档 | 说明 |
|---|---|
| [SaaS_MULTI_TENANT.md](SaaS_MULTI_TENANT.md) | SaaS 多租户架构与运维（落地版，specs/001 T041） |
| [TENANT_MODE_REVIEW.md](TENANT_MODE_REVIEW.md) | 多租户文档口径与授权策略复核（009 交付） |
| [SAAS_MULTI_TENANT_EVALUATION_2026-10.md](SAAS_MULTI_TENANT_EVALUATION_2026-10.md) | 多租户改造「降侵入/降耦合」评估 |
| [SAAS_FULLCHAIN_AUDIT_2026-10-23.md](SAAS_FULLCHAIN_AUDIT_2026-10-23.md) | SaaS 全链路审计（P1/P2 清单） |
| [DEPLOYMENT_MODES_EVALUATION_2026-10.md](DEPLOYMENT_MODES_EVALUATION_2026-10.md) | 部署形态 2×2 矩阵（TENANT_MODE × PLATFORM_ENABLED） |
| [PLATFORM_MERCHANT_ISOLATION_EVALUATION_2026-10-24.md](PLATFORM_MERCHANT_ISOLATION_EVALUATION_2026-10-24.md) | 平台端商户隔离要不要做的论证 |
| [部署方案_用户端.md](部署方案_用户端.md) | 用户端部署架构 |
| [FRP私域部署指南.md](FRP私域部署指南.md) | FRP 内网穿透私域部署 |
| [HOST_INFERENCE_PLAN.md](HOST_INFERENCE_PLAN.md) | llama.cpp 宿主机推理栈 |

## 四、模块与专题

| 文档 | 说明 |
|---|---|
| [BROWSER_AUTOMATION.md](BROWSER_AUTOMATION.md) | 浏览器自动化权威合并文档（Master） |
| [BROWSER_AUTOMATION_R25_CHAIN_AUDIT.md](BROWSER_AUTOMATION_R25_CHAIN_AUDIT.md) | 浏览器自动化 R25 全链路审计 |
| [BRIDGE_TOKEN_PROTOCOL.md](BRIDGE_TOKEN_PROTOCOL.md) | Bridge 通道凭证发放与轮换协议 |
| [CHANNEL_INTEGRATION_AUDIT_2026-09.md](CHANNEL_INTEGRATION_AUDIT_2026-09.md) | 渠道集成审计（2026-09） |
| [QQ_CHANNEL_DESIGN.md](QQ_CHANNEL_DESIGN.md) | QQ 机器人渠道接入设计 |
| [R38_MODULE_INVENTORY.md](R38_MODULE_INVENTORY.md) | R38 模块清单 |
| [R38_COMPETITIVE_DECISIONS.md](R38_COMPETITIVE_DECISIONS.md) | R38 吸收/废弃决策表 |
| [R38_RED_TEAM_REVIEW.md](R38_RED_TEAM_REVIEW.md) | R38 决策表二次论证 |

## 五、ADR 决策记录

入口：[adr/README.md](adr/README.md)。现有 ADR-001、002、004~017（ADR-003 缺号不复用）；新增从 ADR-018 起。

## 相关索引

| 索引 | 内容 |
|---|---|
| [`../INDEX.md`](../INDEX.md) | 用户端子索引（快速开始 / handbook / 部署运维 / 架构 / 规格施工源） |
| [`../DEV_DOCS_INDEX.md`](../DEV_DOCS_INDEX.md) | 7 工程 × 4 份开发文档 + 硬约束矩阵 |
| [`../handbook/README.md`](../handbook/README.md) | 功能文档 / 操作手册四册入口 |
| [`../standards/README.md`](../standards/README.md) | 标准与口径文档 |
| [`../../specs/README.md`](../../specs/README.md) | spec 施工源索引 |
