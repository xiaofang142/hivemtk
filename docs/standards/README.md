# HiveMtk 标准与口径层导航（docs/standards/）

> 本页是 `hivemtk/docs/standards/` 的分层索引。这里放「约束、规范、口径、清单」类文档，
> 与 `docs/handbook/`（功能/操作手册）、`docs/architecture/`（架构与决策）分层互补。
> 基线：复核 2026-10-24。

## 文档清单

| 文档 | 说明 |
|---|---|
| [MASTER_RULES.md](MASTER_RULES.md) | 项目核心规则（工程铁律，handbook README 也引用） |
| [AI-EDITOR-CONTRACT.md](AI-EDITOR-CONTRACT.md) | **AI 编辑器契约**：所有 AI 编辑器必须逐条遵守（入口 `AGENTS.md`，最详细 `CLAUDE.md`） |
| [BACKEND-CODING-STANDARDS.md](BACKEND-CODING-STANDARDS.md) | **后端编码规范**：五层职责/命名/controller 样板/响应错误表/并发与数据访问门禁（改动落 `user-server/**` 前必读） |
| [FRONTEND-CODING-STANDARDS.md](FRONTEND-CODING-STANDARDS.md) | **前端编码规范**：管理端 Vue3+EP 与桥接 MV3 扩展双前端（api 层铁律 / eslint / 路由台账 / 隐私红线 / 发版两道门） |
| [INTERACTION-STANDARDS.md](INTERACTION-STANDARDS.md) | **交互规范**：API 路径与响应信封（含 202 Accepted 语义）/ 401·403·404 分档 / 登录端点裸调 / toast 去重 / i18n 10 语言 / 新手引导三层 |
| [TESTING-STANDARDS.md](TESTING-STANDARDS.md) | **测试规范**：Go 标准库写法与 DB 门禁判读 / vitest 配置口径与 settle 等待 / UI 规则-1 逐元素 / 扩展 fixture 必须真机实证 |
| [SPEC-KIT-WORKFLOW.md](SPEC-KIT-WORKFLOW.md) | **spec-kit 双车道执行手册**：规则 3/4 的分流表 / 命令体 / 阶段职责 / slug 解析 / 铁律全文（CLAUDE.md 只留口径与指针） |
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
