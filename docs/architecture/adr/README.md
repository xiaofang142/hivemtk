# HiveMtk 架构决策记录索引（ADR Index）

> **位置**：`hivemtk/docs/architecture/adr/`  
> **状态**：2026-08-16 整理
> **总 ADR 数**：14 份

---

## 📋 ADR 清单

| 编号 | 标题 | 状态 | 决策日期 | 影响范围 |
|------|------|------|----------|----------|
| [ADR-001](ADR-001-five-layer-architecture.md) | 五层架构（Controller→Service→Repository→Model→DTO） | ✅ Accepted | 2026-Q1 | 后端全局 |
| [ADR-002](ADR-002-agpl-license.md) | AGPL-3.0 许可证 | ✅ Accepted | 2026-Q1 | 法律 / 开源 |
| [ADR-004](ADR-004-cors-strict-whitelist.md) | CORS 严格白名单 | ✅ Accepted | 2026-Q2 | 网关 |
| [ADR-005](ADR-005-database-design.md) | 数据库设计 | ✅ Merged | 2026-Q3 | 后端全局 |
| [ADR-006](ADR-006-llm-selection.md) | LLM 选型与多模型路由 | ✅ Merged | 2026-Q3 | AI 销冠 |
| [ADR-007](ADR-007-rag-retrieval.md) | RAG 检索增强生成架构 | ✅ Merged | 2026-Q3 | AI 销冠 |
| [ADR-008](ADR-008-reach-rate-limit.md) | 触达限流策略 | ✅ Merged | 2026-Q3 | 触达 |
| [ADR-009](ADR-009-error-handling.md) | 错误处理规范 | ✅ Merged | 2026-Q3 | 后端全局 |
| [ADR-010](ADR-010-log-masking.md) | 日志与敏感信息脱敏 | ✅ Merged | 2026-Q3 | 后端全局 |
| [ADR-011](ADR-011-chat-widget-embed.md) | 嵌入式客服 Widget 架构 | ✅ Accepted | 2026-Q2 | 前端 / 集成 |
| [ADR-012](ADR-012-config-package-relocation.md) | config 包迁移 | ✅ Accepted | 2026-Q3 | 后端架构 |
| [ADR-013](ADR-013-module-rename.md) | 模块重命名（market → hivemtk） | ✅ Accepted | 2026-Q3 | 全局 |
| [ADR-014](ADR-014-knowledge-group-isolation.md) | 知识库隔离架构 | ⚠️ Simplified | 2026-Q3 | AI 销冠 |
| [ADR-015](ADR-015-empty-package-disposition.md) | 空壳包处置策略 | ✅ Accepted | 2026-Q3 | 仓库清理 |

**统计**：
- Accepted：8 份
- Merged：5 份
- Simplified：1 份

---

## 🏷️ 编号策略

### 已使用编号

| 段 | 用途 |
|----|------|
| 001-099 | 全局架构（核心决策）|
| 100-199 | 安全 / 鉴权相关 |
| 200-299 | AI 销冠 / RAG / Agent |
| 300-399 | 触达 / 桥接 / 渠道 |
| 400-499 | 数据 / 缓存 |
| 500-599 | 前端 / 用户端 |
| 600-699 | 部署 / 运维 / CI |
| 700-799 | 法律 / 合规 |
| 800-899 | 流程 / 治理 |
| 900-999 | Reserved |

### 缺号说明

| 编号 | 状态 | 原因 |
|------|------|------|
| ADR-003 | ❌ 已删除 | 原为 WebSocket 方案决策；该方案已弃用（现行为 HTTP 长轮询 + SSE），故编号作废且**不复用**，以保持历史引用的稳定性 |

> 现有编号：001、002、004~015 共 **14 份**（ADR-003 缺号）。
> 新增 ADR 请从 **ADR-016** 起顺延，不要回填 ADR-003。

### 已合并说明

| 编号 | 主题 | 合并目标 | 状态 |
|------|------|----------|------|
| [ADR-005](ADR-005-database-design.md) | 数据库设计 | `../../migrations/` SQL 文件 | ✅ Merged |
| [ADR-006](ADR-006-llm-selection.md) | LLM 选型 | `../../operations/AI_AGENT_PERF_API.md` | ✅ Merged |
| [ADR-007](ADR-007-rag-retrieval.md) | RAG 检索 | `../../operations/AI_AGENT_PERF_API.md` | ✅ Merged |
| [ADR-008](ADR-008-reach-rate-limit.md) | 触达限流 | `../../operations/SLA_SLO.md` | ✅ Merged |
| [ADR-009](ADR-009-error-handling.md) | 错误处理 | `../../standards/MASTER_RULES.md` | ✅ Merged |
| [ADR-010](ADR-010-log-masking.md) | 日志脱敏 | `../../standards/MASTER_RULES.md` | ✅ Merged |

---

## 🔗 相关文档

- [部署指南](../../operations/) — 部署与运维
- [服务等级承诺](../../operations/SLA_SLO.md) — SLA
- [项目规则](../../standards/MASTER_RULES.md) — 编码规范

---

## 📝 修订历史

| 版本 | 日期 | 修订人 | 内容 |
|------|------|--------|------|
| v1.0 | 2026-08-16 | audit-agent | 初版 |
| v1.1 | 2026-08-16 | audit-agent | 删除 ADR-003（已弃用 WebSocket）、简化 ADR-014、修正引用 |
| v1.2 | 2026-09-15 | audit-agent | 修复 ADR-001 死链（`ADR-001-layered-architecture.md` → `ADR-001-five-layer-architecture.md`）；将 ADR-003 缺号说明从修订历史提升为独立小节，避免新 ADR 回填该编号 |
