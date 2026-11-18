# HiveMtk 功能文档 / 操作手册 · 入口

五份文档，两类读者，覆盖全部已实现功能。基线：`hivemtk/` 仓库 main，复核 2026-10-13。

| 读者 | 文档 | 内容 |
|------|------|------|
| 👤 **人** | [快速上手与使用手册（人读版）](./QUICKSTART-FOR-HUMANS.md) | **新接手从这里开始（交接级）**：五大件 · 功能架构与数据流 · **逐模块四要素详解（作用/解决什么问题/如何配置/如何使用）**· 从零到第一条 AI 自动回复 · 分渠道接入步骤 · Bridge/Widget 详细指南 · 角色权限 · 排障速查 · 避坑 15 条 |
| 👤 **人** | [功能文档（人读版）](./FEATURES-FOR-HUMANS.md) | 定位边界 · 部署拓扑图 · 21 渠道矩阵 · 消息入站数据流图 · 19 大业务域 · AI 引擎 F1–F15 · LTC 流程图 · 已知限制 |
| 👤 **人** | [操作手册（人读版）](./OPERATIONS-FOR-HUMANS.md) | 部署流程图 · 密钥配置 · 推理栈 · 前端菜单地图 · 七类日常运营路径 · 备份恢复 · 排障表 |
| 🤖 **AI** | [功能文档（AI 读版）](./FEATURES-FOR-AI.md) | 42 个 internal 包图 · 分层时序图 · API 契约 · 373 条路由索引 · 45 工具 / 15 装饰器 · 中间件 30 项 · 环境变量陷阱 |
| 🤖 **AI** | [操作手册（AI 读版）](./OPERATIONS-FOR-AI.md) | 依赖方向图 · 命令速查 · 四种新增功能流程图 · 验证证据链 · audit 失败项规避 · 坑位表 |
| ⚠️ **必读** | [SaaS 生产部署约束与教训](./SAAS_PRODUCTION_CONSTRAINTS.md) | 003 生产联调沉淀：多租户编码硬约束（WithContext/禁单行列名/幂等判 nil）· SaaS 首访路由契约 · 平台集成装配链 · 生产播种 · mock 全绿≠生产能过的方法论 · 修复提交索引 · 上线检查清单 |

## 怎么选

| 你的问题 | 看哪份 |
|---|---|
| 我刚接手，从哪开始？怎么用起来？ | [快速上手指南（人读版）](./QUICKSTART-FOR-HUMANS.md) |
| 这个系统能干什么？边界在哪？ | 人读版功能文档 |
| 怎么把它跑起来？怎么教运营用？ | 人读版操作手册 |
| 这个接口在哪？改哪里会影响谁？ | AI 读版功能文档 |
| 我要改代码 / 加功能，怎么不返工？ | AI 读版操作手册 |
| 我要改多租户/计费/平台对接相关代码？ | [SaaS 生产部署约束与教训](./SAAS_PRODUCTION_CONSTRAINTS.md)（每条约束都对应一次真实翻车） |
| 出问题了怎么办？ | [`../TROUBLESHOOTING.md`](../TROUBLESHOOTING.md) + 人读版操作手册第 9 节 |

## 阅读约定

1. **只写已实现并在代码中可验证的能力**。未完成、半成品、已知缺陷集中在《功能文档（人读版）》第 11 节，编号 G4/G7/G9/G10/G11/G12，逐条说明影响。
2. **挂靠不重写**。部署细节指向 `../DEPLOYMENT_GUIDE.md`；子模块明细指向 `../../user-server/docs/dev/FEATURES.md`（94 个业务子模块）；引擎能力指向 `../marketing-features/README.md`（F1–F15）。
3. **计数口径**。文中所有计数（179 controller / 359 service / 373 路由 / 45 工具 / 15 装饰器 / 7 顶级菜单 / 9 语言等）均为 2026-10-13 对 main 分支的源码实测，与 README 或旧文档口径不一致时**以本文为准**，差异处已显式订正说明。
4. **图均为 Mermaid**，在 GitHub / VS Code / Typora 可直接渲染。

## 相关索引

| 索引 | 内容 |
|------|------|
| [`../INDEX.md`](../INDEX.md) | 全仓文档索引 |
| [`../DEV_DOCS_INDEX.md`](../DEV_DOCS_INDEX.md) | 各工程开发文档索引（7 工程 × 4 份） |
| [`../capabilities.md`](../capabilities.md) | 渠道能力矩阵（自动生成，**勿手改**） |
| [`../PORT_REGISTRY.md`](../PORT_REGISTRY.md) | 端口注册表 |
| [`../HARDCODE_INVENTORY.md`](../HARDCODE_INVENTORY.md) | 可调参数硬编码清单 |
| [`../standards/MASTER_RULES.md`](../standards/MASTER_RULES.md) | 工程铁律 |
