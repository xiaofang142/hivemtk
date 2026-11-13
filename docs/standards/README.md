# HiveMtk 标准与口径层导航（docs/standards/）

> 本页是 `hivemtk/docs/standards/` 的分层索引。这里放「约束、规范、口径、清单」类文档，
> 与 `docs/handbook/`（功能/操作手册）、`docs/architecture/`（架构与决策）分层互补。
> 基线：复核 2026-10-24。

## 文档清单

| 文档 | 说明 |
|---|---|
| [MASTER_RULES.md](MASTER_RULES.md) | 项目核心规则（工程铁律，handbook README 也引用） |
| [AI-EDITOR-CONTRACT.md](AI-EDITOR-CONTRACT.md) | **AI 编辑器契约**：所有 AI 编辑器必须逐条遵守（入口 `AGENTS.md`，最详细 `CLAUDE.md`） |
| [FEATURE_DOCUMENTATION_TEMPLATE.md](FEATURE_DOCUMENTATION_TEMPLATE.md) | 功能文档 8 节模板（人读版功能文档的写作规范） |
| [BACKEND_MESSAGE_I18N_POLICY.md](BACKEND_MESSAGE_I18N_POLICY.md) | 后端文案国际化口径与清单（F3 交付：必须 i18n / 不 i18n / 改造顺序 + 未覆盖清单） |

## 术语与命名

- 命名规范统一见 [`user-server/docs/dev/CONVENTIONS.md`](../../user-server/docs/dev/CONVENTIONS.md) §2（文件命名 / 标题规范）。
- 官网命名统一规范见 [`hivemtk/website/TERMINOLOGY.md`](../../website/TERMINOLOGY.md)。
- 智能体命名铁律（禁「机器人/助手」）见 [`architecture/ARCHITECTURE_DIAGRAM.md`](../architecture/ARCHITECTURE_DIAGRAM.md) §九 与 [`DEV_DOCS_INDEX.md`](../DEV_DOCS_INDEX.md) E6/E7。

## 相关索引

| 索引 | 内容 |
|---|---|
| [`../INDEX.md`](../INDEX.md) | 用户端子索引 |
| [`../DEV_DOCS_INDEX.md`](../DEV_DOCS_INDEX.md) | 工程开发文档索引 + 硬约束矩阵 |
| [`../architecture/README.md`](../architecture/README.md) | 架构层导航 |
| [`../handbook/README.md`](../handbook/README.md) | 功能文档 / 操作手册四册入口 |
| [`../../user-web/src/i18n/TRANSLATOR_GUIDE.md`](../../user-web/src/i18n/TRANSLATOR_GUIDE.md) | 前端 i18n 翻译协作指南 |
