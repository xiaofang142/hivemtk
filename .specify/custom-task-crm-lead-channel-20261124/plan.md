# Implementation Plan: 跨境电商 CRM、线索审计、智能体剥离与渠道账号统一配置

**Branch**: `custom-task-crm-lead-channel-20261124` | **Date**: 2026-11-24 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `.specify/custom-task-crm-lead-channel-20261124/spec.md`

## Summary

在现有 hivemtk 五层架构内融合四大部分能力，不另起系统：

1. 扩展 Customer/OneID/Clue 主数据，补齐 Company/Contact/Supplier/ServiceProvider 应用层，叠加账号/员工/行业/责任人数据权限与审计，接 AI/RAG 出带证据画像。
2. 线索模块升级为「渠道→账号→通知人」配置 + 三粒度规则 + 三路并行检测 + 二次 LLM 总结（含多轮/历史上下文）+ 防抖 + 归档通知。
3. 自动回复与线索发现拆为两个独立模块，共享对话历史与用户数据、可交叉引用。
4. 渠道/渠道账号统一数据表驱动配置，唯一识别码一次性替换 bartoken，封装统一渠道账号列表与自他识别方法，三条消息路径合并治理。

## Technical Context

**Language/Version**: Go（后端，现有版本见 `user-server/go.mod`）；Vue3 + Element Plus + Pinia（user-web 管理端）；TypeScript（bridge MV3 扩展）

**Primary Dependencies**: Gin + GORM + PostgreSQL + pgvector；AI/LLM 调用现有封装；RAG 复用现有向量检索能力

**Storage**: PostgreSQL；渠道账号/线索/CRM 主数据均落表；向量存 pgvector

**Testing**: `go test ./...`（注意 `POSTGRES_TEST_PASSWORD` 外导，否则 `t.Skipf` 假绿）；`scripts/api_verify_full.py` 三端 API 验证

**Target Platform**: Linux 服务端 + Chrome MV3 扩展 + Web 管理端

**Project Type**: web-service + web-frontend + browser-extension

**Performance Goals**: 线索判定在单条消息到达后分钟级内完成（异步）；客户检索交互级响应

**Constraints**: 五层架构 / 主智能体编排子智能体（v1.0.5，按用户「一次一个任务」串行推进） / 平台业务码判成败 / 原 RBAC 不改 / 整体完成才提交 / UI 测试逐元素显式操作

**Scale/Scope**: 四大部分、跨 user-server 与 user-web 与 bridge；多租户；多渠道账号

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 合规判断 | 说明 |
|------|----------|------|
| I 五层架构 | ✅ | 所有新增 Go 代码按 Router→Controller→Service→Repository→Model 四层齐全，无跨层调用、无内联闭包；DI 在 main.go |
| II 主智能体编排子智能体 | ✅ | 宪法 v1.0.5 已去掉并行禁令；实施阶段由主智能体调度/监督/纠正，按用户「一次一个任务」串行推进 |
| III 验证纪律 | ✅ | 文档明确测试需真实跑并贴输出；跳过测试须记 `skipped`/原因，不编造 |
| IV 业务码判成败 | ✅ | 通知/平台调用沿用仓内业务码口径（飞书 code、企微 errcode 等），新增调用先 grep 对齐 |
| V API 契约 | ✅ | 新端点遵循 `/api/{domain}/{resource}`（用户端）、`/api/manage/{resource}`（管理端）与统一响应格式 |
| 提交纪律 | ✅ | 本轮不提交；实施整体完成并验证全绿后一次性 commit+push 双远端 |
| 测试陷阱 | ✅ | 已记录 `POSTGRES_TEST_PASSWORD` 与 `t.Skipf` 假绿约束 |
| graphify 停用 | ✅ | 未作为规则；架构以代码与 docs 为准 |

**Re-check after Phase 1 design**: 设计不引入跨层调用、不改 RBAC、不硬编码密钥、不引入并行 Agent，全部保持合规。

## 实施排期与依赖倒置（重要）

用户要求文档顺序为 **一 → 二 → 三 → 四**，spec.md 已严格遵循。但在**实施依赖**上，第四部分的「渠道账号主数据 + 唯一识别码 + 统一封装方法」是第二、三部分的前置地基：

- 线索规则按账号粒度、通知人按账号配置、智能体按账号绑定，都依赖 `ChannelAccount` 与统一识别码。
- 三路消息路径合并与自他识别，是线索判定与自动回复读取对话历史的前提。

因此 **tasks.md 的 Phase 2（Foundational）将提前落地 US4 的地基任务**（渠道表/渠道账号表/识别码/统一封装/三路合并骨架），随后 Phase 3–6 依次为 US1（一）→ US2（二）→ US3（三）→ US4 剩余（四）。这不改变用户要求的文档顺序，只是保证依赖可行。

## Project Structure

### Documentation (this feature)

```text
.specify/custom-task-crm-lead-channel-20261124/
├── spec.md              # 用户规格（本轮产出）
├── plan.md              # 本文件
├── research.md          # Phase 0 关键决策
├── data-model.md        # Phase 1 数据模型
├── quickstart.md        # Phase 1 验证指南
├── contracts/           # Phase 1 API 契约
│   ├── crm.md
│   ├── leads.md
│   └── channels.md
├── checklists/
│   └── requirements.md  # 规格质量检查单
└── tasks.md             # Phase 2 任务清单（/speckit-tasks 产出）
```

### Source Code (repository root = 内层仓 hivemtk/)

```text
migrations/                             # 仓库顶层：新增渠道/线索/CRM 迁移（088~095）
user-server/
├── internal/migration/migrations/      # 需 Go 注册的迁移同步在此新增注册文件
├── internal/
│   ├── model/                          # 新增/扩展：Channel, ChannelAccount, ChannelIdentifyCode,
│   │                                   #   Company, Contact, Supplier, ServiceProvider,
│   │                                   #   LeadMiningRule, LeadMiningHit, NotifyRoute, ConversationContext
│   ├── repository/                     # 对应五层数据访问
│   ├── service/                        # 业务逻辑：CRM 主数据/去重/权限、渠道统一封装、线索三路检测、
│   │                                   #   二次 LLM 汇总、通知路由、智能体解耦
│   ├── controller/                     # 对应 REST 控制器
│   ├── router/                         # URL→Controller 映射（仅映射）
│   └── ...                             # aiagent / intent_recognition / humanize / webhook / channelgw（改造）
scripts/
└── api_verify_full.py                  # API 三端验证
user-web/
├── src/                                # 管理端：渠道管理/渠道账号管理/线索规则/智能体管理/CRM 主数据页面
└── bridge/
    └── src/channels/                   # MV3 扩展渠道适配器（识别码替换、三条路径合并）
```

**Structure Decision**: 沿用现有单仓多模块结构（user-server 后端 + user-web 前端 + bridge 扩展）；新增实体按五层落在 user-server/internal 对应目录；迁移放 migrations；前端与扩展在 user-web。无新增顶层项目。

## Phase 0 — Research 决策摘要（详见 research.md）

1. **识别码替换策略**：一次性硬切换。新增 `channel_identify_code`（每渠道唯一、可重置），所有链路改用识别码；旧 bartoken 在某次发布中同步下线。为处理在途消息窗口，采用“先双写识别码与 bartoken → 发布切换读路径 → 下一版本移除 bartoken 写”的三步原子切换（非长期兼容期）。
2. **三条消息路径合并**：统一抽象为 `InboundMessage` 收件箱模型 + `ChannelResolver`（按识别码解析租户/账号）；原 bridge/API/hook 各自入口保留为“适配器”，仅负责把消息归一化后投递到统一收件箱，不再各自实现账号识别与线索触发。
3. **CRM 范围**：扩展现有 Customer/OneID/Clue，补 Company/Contact/Supplier/ServiceProvider 应用层；来源字段统一建模；不新建独立 CRM 系统。
4. **三路检测 + 二次 LLM**：关键词与语义检测同步执行，LLM 检测异步；三者结果落 `lead_mining_hits`，由 `dispatcherJudge` 二次汇总，上下文取多轮消息 + 历史消息（ConversationContext）。

## Phase 1 — Design

### Data Model（详见 data-model.md）

- 渠道域：`channels`、`channel_accounts`、`channel_identify_codes`、`channel_agent_bindings`（改造，移除智能体内绑定逻辑）
- CRM 域：扩展 `customers`/`oneid`/`clues`；补 `companies`、`contacts`、`suppliers`、`service_providers`、`customer_tags`、`follow_up_*`、`operation_logs`
- 线索域：`lead_mining_configs`（去单例化，支持三粒度）、`lead_mining_rules`、`lead_mining_hits`、`lead_mining_judgements`、`lead_mining_notify_routes`、`conversation_contexts`
- 权限域：新增数据授权表（账号负责人、可见行业/账号范围），不改原 RBAC 表

### API Contracts（详见 contracts/）

- CRM：`/api/manage/crm/customers`、contacts/companies/suppliers/service-providers、dedupe、import/export、360、ai/profile、audit
- 线索：`/api/manage/leads/rules`、`/api/manage/leads/judgements`、`/api/manage/leads/clues`、`/api/manage/leads/notify-routes`
- 渠道：`/api/manage/channels`、`/api/manage/channel-accounts`、`/api/manage/channel-identify-codes`、`/api/manage/agents/lead-mining`

所有端点遵循宪法 V 的路径与响应格式；平台通知调用遵循宪法 IV 业务码判成败。

### Quickstart（详见 quickstart.md）

以“新增渠道账号 → 配置识别码 → 配置三类智能体绑定与规则 → 注入多轮对话 → 验证三路检测与二次 LLM → 验证归档通知 → 验证 CRM 360 与隔离”为主线，给出可运行的验证步骤与预期结果，不写实现代码。

## Risks & Mitigations

| 风险 | 影响 | 缓解 |
|------|------|------|
| 一次性替换 bartoken 导致旧客户端断连 | 高 | 发布前同步升级 bridge/TG hook/三方 API 调用方；按 research.md 三步原子切换；发布清单列明升级顺序 |
| 三路 LLM 调用成本/超时 | 中 | 关键词/语义先判，LLM 异步；超时降级并标记证据不完整；设置调用预算 |
| 数据权限与原 RBAC 叠加产生越权 | 高 | 权限前置，所有列表接口强制数据范围过滤；越权用例 100% 覆盖；SC-002 验收 |
| 重复实现渠道账号列表/自他识别 | 中 | 强制统一方法，SC-006 验收；代码评审检查无散落实现 |
| 测试假绿 | 中 | 显式导出 `POSTGRES_TEST_PASSWORD`；检查 `t.Skipf` 输出，不把跳过当通过 |
| 前端页面范围大 | 中 | 按 US 增量交付，每页逐元素 ego-browser 显式验证（规则 -1） |

## Complexity Tracking

> 无宪法违规，无需 complexity tracking。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| （无） | | |
