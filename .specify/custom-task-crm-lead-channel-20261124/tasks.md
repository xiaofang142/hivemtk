# Tasks: 跨境电商 CRM、线索审计、智能体剥离与渠道账号统一配置

**Input**: Design documents from `.specify/custom-task-crm-lead-channel-20261124/`

**Prerequisites**: plan.md（required）、spec.md（required）、research.md、data-model.md、contracts/

**Organization**: 按 user story 分组；每个 story 可独立实现与验证。US4 地基部分前置到 Phase 2（见 plan.md「实施排期与依赖倒置」），文档顺序仍为一→二→三→四。

## Format: `[ID] [P?] [Story] Description`
- **[P]**: 不同文件、无依赖，可并行
- **[Story]**: US1=一(CRM) / US2=二(线索) / US3=三(智能体剥离) / US4=四(渠道地基与配置)

---

## Phase 1: Setup（共享基础设施）

- [ ] T001 创建本 feature 目录与五份设计文档（spec/plan/research/data-model/contracts/quickstart），路径 `.specify/custom-task-crm-lead-channel-20261124/`
- [ ] T002 确认内层仓分支与工作区干净：`git -C hivemtk status --short`、`git remote -v`（双远端 gitee-upstream/upstream，不写死 origin）
- [ ] T003 [P] 梳理现有渠道账号表与识别逻辑清单，输出到 `.specify/custom-task-crm-lead-channel-20261124/research.md` 附录（bridge/handler_http.go、channelgw、channelbot、webhook.go:451 ResolveTenantByChannelAccount）
- [ ] T004 [P] 确认迁移编号从仓库**顶层** `migrations/` 续编：当前最大 087，088~095 空闲可用；若新迁移需 Go 注册，同步在 `user-server/internal/migration/migrations/` 增加注册文件（参照 `v3_79_0_tenant_leading_index_migration.go`）

---

## Phase 2: Foundational（阻塞全部 user story，US4 地基提前落地）⚠️

**Purpose**: 渠道账号主数据、唯一识别码、统一封装方法、三路消息合并骨架——US2/US3 的前置。

- [ ] T005 新增渠道/账号/识别码 Model：`user-server/internal/model/channel.go`、`channel_account.go`、`channel_identify_code.go`、`channel_agent_binding.go`（字段见 data-model.md）
- [ ] T006 [P] 新增迁移 `migrations/088_channels.sql`（channels 表，code 唯一约束）
- [ ] T007 [P] 新增迁移 `migrations/089_channel_accounts.sql`（account 含 auto_reply_agent_id/lead_mining_agent_id/owner_user_ids/data_scope/identify_code）
- [ ] T008 [P] 新增迁移 `migrations/090_channel_identify_codes.sql`（code 唯一、可重置、version）
- [ ] T009 实现 Repository：`user-server/internal/repository/channel.go`、`channel_account.go`、`channel_identify_code.go`（CRUD + ListOptions；裸 domain.go 命名，禁 `_repository.go` 冗余后缀）
- [ ] T010 实现统一渠道账号列表/下拉 Service：`user-server/internal/service/channel_account.go` 的 `ListOptions()`（FR-029，所有内部选择统一调用）
- [ ] T011 实现 `ChannelResolver`（按识别码解析租户/账号 + 统一自他识别，单聊/群聊分支）：`user-server/internal/service/channel_resolver.go`（FR-028/030）
- [ ] T012 实现 Controller：`user-server/internal/controller/channel.go`、`channel_account.go`、`channel_identify_code.go`（裸 domain.go 命名，禁 `_controller.go` 冗余后缀）
- [ ] T013 Router 映射（仅映射，无内联闭包）：`user-server/internal/router/router.go` 追加 channels/channel-accounts/identify-codes 路由
- [ ] T014 迁移每租户一行的 `lead_mining_configs`（主键 = tenant_id，非全局 id=1）为三粒度规则（global/channel/account），存量为每租户生成一条 global 规则（迁移 `migrations/091_lead_rules_migrate.sql`），不丢存量配置；新增配置项须同步更新 model 字段与 087 之后迁移
- [ ] T015 三路消息入口归一化为 `InboundMessage` 收件箱 + 适配器骨架：改造 `user-web/bridge/src/channels/` 适配器、`user-server/internal/service/webhook.go`（ResolveTenantByChannelAccount 改走 ChannelResolver），`user-server/internal/service/channelgw.go`
- [ ] T016 识别码三步原子切换第一步——双写识别码与 bartoken：`user-server/internal/service/channel_identify.go`
- [ ] T017 原 RBAC 不改；新增数据授权表与 Model：`migrations/092_channel_account_authorizations.sql`、`user-server/internal/model/channel_account_authorization.go`（FR-016/027）

**Checkpoint**: 渠道地基就绪——US1/US2/US3 可基于稳定的 ChannelAccount 与 ChannelResolver 开始。

---

## Phase 3: User Story 1 — 跨境电商客户统一主数据与跨账号检索（Priority: P1）

**Goal**: 统一客户主数据 + 权限隔离 + 去重合并在 + 360/交叉检索 + AI 证据化 + 线索负责人推送。

**Independent Test**: 构造两账号重复客户与两员工，验证隔离/去重/360/交叉检索/AI 证据/归档推送（见 spec US1）。

### Implementation for User Story 1

- [ ] T018 [P] [US1] 扩展 Customer Model：来源字段已有（customer.go:66-67 + 迁移 080）须复用不重复建模，仅补行业/负责人/合并字段：`user-server/internal/model/customer.go`（见 research.md R7-5）
- [ ] T019 [P] [US1] 新建 Company/Contact/Supplier/ServiceProvider Model：`user-server/internal/model/company.go`、`contact.go`、`supplier.go`、`service_provider.go`（补齐应用层，迁移 053 已有 companies 表）
- [ ] T020 [P] [US1] 新建 OneID/合并/标签/跟进/操作日志 Model：`user-server/internal/model/one_id.go`、`customer_merge.go`、`customer_tag.go`、`follow_up_record.go`、`operation_log.go`
- [ ] T021 [US1] 新增 CRM 迁移 `migrations/093_crm_entities.sql`：仅补 contact/supplier/service_provider 新表与 companies 扩展字段（053+079 已有应用层，不重建）；来源字段已由 080/customer.go/clue.go/companies 建模，不重复（R7-4/5）
- [ ] T022 [US1] 实现去重合并 Service（手机号/邮箱/外部 ID/名称相似度 + OneID + 合并审计）：`user-server/internal/service/customer_dedupe_service.go`（FR-003/005）
- [ ] T023 [US1] 实现数据权限过滤（账号/员工/行业/负责人 + 授权表）：`user-server/internal/service/customer_permission_service.go`（FR-002）
- [ ] T024 [US1] 实现导入导出 Service（按权限过滤字段与范围 + 审计）：`user-server/internal/service/customer_import_export_service.go`（FR-004/005）
- [ ] T025 [US1] 实现 360 视图与跨账号行业交叉检索 Service：`user-server/internal/service/customer_360_service.go`（FR-006）
- [ ] T026 [US1] 实现 AI 画像/分层/商机/跟进建议 Service（每条建议附 evidence）：`user-server/internal/service/customer_ai_service.go`（FR-007）
- [ ] T027 [US1] 实现 Repository：`user-server/internal/repository/customer_repository.go`、`company_repository.go`、`contact_repository.go`（及 supplier/service_provider）
- [ ] T028 [US1] 实现 Controller：`user-server/internal/controller/crm_controller.go`（CRUD/去重/合并/导入导出/360/AI/审计）
- [ ] T029 [US1] Router 追加 `/api/manage/crm/*` 映射：`user-server/internal/router/router.go`
- [ ] T030 [US1] 前端 CRM 主数据页面（列表/去重/合并/360/标签/跟进）：`user-web/src/views/crm/`（逐元素 ego-browser 验证，规则 -1）
- [ ] T031 [US1] 线索归档后关联客户并推送负责人：`user-server/internal/service/clue_archive_service.go` 与 US2 归档 Service 对接（FR-008）

**Checkpoint**: US1 独立可用；越权用例 100% 通过（SC-002），去重抽样 ≥95%（SC-001）。

---

## Phase 4: User Story 2 — 线索模块全链路审计与多规则并行检测（Priority: P2）

**Goal**: 渠道→账号→通知人配置、三粒度规则、三路并行检测、二次 LLM 总结、多轮/历史上下文、防抖、归档通知。

**Independent Test**: 不同账号不同规则，注入三类命中与指代对话，验证三路证据/二次结论/上下文引用/防抖/归档通知（spec US2）。

### Implementation for User Story 2

- [ ] T032 [P] [US2] 新建 LeadMiningRule/Hit/Judgement/NotifyRoute Model：`user-server/internal/model/lead_mining_rule.go`、`lead_mining_hit.go`、`lead_mining_judgement.go`、`lead_mining_notify_route.go`
- [ ] T033 [P] [US2] 新建 ConversationContext/Clue 扩展 Model：`user-server/internal/model/conversation_context.go`、`clue.go`（扩展 user_info/source_hit_ids/role）
- [ ] T034 [US2] 新增线索域迁移（rules/hits/judgements 扩展/notify_routes/conversation_contexts/clues 扩展）于 `migrations/094_lead_mining.sql`
- [ ] T035 [US2] 实现三粒度规则解析（global/channel/account，按 scope 优先级匹配）：`user-server/internal/service/lead_rule_service.go`（FR-011）
- [ ] T036 [US2] 实现关键词 + 语义关键词并行检测：`user-server/internal/service/lead_detector_keyword.go`、`lead_detector_semantic.go`（FR-012）
- [ ] T037 [US2] 实现 LLM 检测（异步、超时降级、记证据不完整）：`user-server/internal/service/lead_detector_llm.go`（FR-012）
- [ ] T038 [US2] 三路结果落库为独立 hits：`user-server/internal/repository/lead_mining_hit_repository.go`
- [ ] T039 [US2] 二次 LLM 汇总判断（纳入多轮 + 历史消息 + 角色分类）：改造 `user-server/internal/service/lead_mining.go` 的 `dispatcherJudge`（FR-013）
- [ ] T040 [US2] 防抖去重（复用现有 debounce）：`user-server/internal/service/lead_debounce_service.go`（FR-014）
- [ ] T041 [US2] 归档 Service（用户信息 + 线索信息 + 关联客户）：`user-server/internal/service/lead_clue_archive_service.go`（FR-015，与 T031 对接）
- [ ] T042 [US2] 通知路由（账号维护人→渠道维护人→租户默认，站内/TG/飞书，业务码判成败 + 送达记录）：`user-server/internal/service/lead_notify_service.go`（FR-015/宪法 IV）
- [ ] T043 [US2] 配置人员权限内消息/客户列表：`user-server/internal/service/lead_scoped_list_service.go`（FR-010）
- [ ] T044 [US2] Repository + Controller + Router：`user-server/internal/repository/lead_mining_rule_repository.go`、`user-server/internal/controller/lead_controller.go`、router 追加 `/api/manage/leads/*`（FR-011/015）
- [ ] T045 [US2] 前端线索规则/通知路由/判定证据/归档页面：`user-web/src/views/leads/`（逐元素 ego-browser 验证）

**Checkpoint**: US2 独立可用；三路留证率 100%（SC-003/004），归档通知到达率 100%（SC-008）。

---

## Phase 5: User Story 3 — 自动回复智能体与线索发现智能体解耦（Priority: P3）

**Goal**: 两模块独立启停；自动回复负责意图/NLP/情感/拟人工/转人工；线索发现只识别归档；共享对话历史与用户数据、交叉引用。

**Independent Test**: 分别启停两模块验证互不影响；一方写入备注可被另一方引用（spec US3）。

### Implementation for User Story 3

- [ ] T046 [P] [US3] 梳理 ingest 后交织链路并定义模块边界：`user-server/internal/service/aiagent/`、`intent_recognition/`、`humanize/`（输出边界说明到 research.md 附录）
- [ ] T047 [US3] 将“线索发现智能体”改名并独立注册（列表/新增/改/删）：`user-server/internal/service/lead_mining_agent_service.go`、`user-server/internal/controller/lead_mining_agent_controller.go`（FR-026）
- [ ] T048 [US3] 自动回复模块独立启停开关（按 channel_account 的 auto_reply_agent_id）：`user-server/internal/service/auto_reply_service.go`（FR-017/018）
- [ ] T049 [US3] 线索发现模块独立启停开关（按 channel_account 的 lead_mining_agent_id，不触发自动回复）：`user-server/internal/service/lead_discovery_service.go`（FR-017/019）
- [ ] T050 [US3] 共享对话历史与用户数据读写：`user-server/internal/service/conversation_context_service.go`（基于 T033 的 ConversationContext，FR-020）
- [ ] T051 [US3] 交叉引用机制（标签/备注/摘要写入与读取）：改造 `user-server/internal/service/aiagent/` 与 `lead_discovery_service.go`（FR-020）
- [ ] T052 [US3] 群聊多成员上下文聚合：`user-server/internal/service/conversation_context_service.go`（FR-021）
- [ ] T053 [US3] 前端智能体管理去掉渠道账号绑定逻辑，改为渠道账号管理内配置：`user-web/src/views/agents/`、`user-web/src/views/channels/account-manage.vue`（FR-025）
- [ ] T054 [US3] 前端两类智能体独立启停与交叉引用展示：`user-web/src/views/channels/account-manage.vue`

**Checkpoint**: US3 独立可用；独立启停互不影响、交叉引用可用率 100%（SC-007）。

---

## Phase 6: User Story 4 — 渠道配置统一与识别码替换收尾（Priority: P4）

**Goal**: 完成第四部分剩余配置能力（预设渠道、渠道账号 CRUD、识别码替换第二步与 bartoken 移除、三路合并收尾、自他识别全覆盖）。

**Independent Test**: 新增渠道账号、重置识别码、三条链路均用新码、单聊群聊自他识别正确（spec US4）。

### Implementation for User Story 4

- [ ] T055 [US4] 预设渠道表驱动与开启开关（新增渠道一次配置全系统生效）：`user-server/internal/service/channel_service.go`（FR-022/023）
- [ ] T056 [US4] 渠道账号 CRUD 完整化（名称/备注/自他识别名称/类型/两类智能体/负责人/数据授权）：`user-server/internal/service/channel_account.go`（FR-024/027）
- [ ] T057 [US4] 散落账号配置收敛到渠道账号管理（清理旧配置入口）：`user-server/internal/service/channel_account.go` + grep 旧散落配置并移除（FR-027）
- [ ] T058 [US4] 识别码切换第二步——读路径改走识别码，旧 Bridge token 不再被接受。注意全仓无 `bartoken` 字面量，实为 `X-Bridge-Token`/`bridge_token`，须按 research.md R7-7 六点同步（guard 候选源、service KV key、扩展端 5+ 发送点、CORS 允许头、脱敏清单、协议文档与测试），否则浏览器端静默 401（FR-028，SC-005）
- [ ] T059 [US4] TG hook / 轮询 / SageX 改用识别码：`user-server/internal/service/webhook.go`、`user-web/bridge/src/channels/`、SageX 接入点（调研定位后修改，FR-028）
- [ ] T060 [US4] 三条消息路径合并收尾，统一落 InboundMessage 收件箱：`user-server/internal/service/channelgw.go`、`user-web/bridge/src/channels/`（FR-031）
- [ ] T061 [US4] 识别码重置功能（version+1、在途消息窗口处理）：`user-server/internal/service/channel_identify.go`
- [ ] T062 [US4] 前端渠道管理/渠道账号管理/识别码管理页面：`user-web/src/views/channels/`（逐元素 ego-browser 验证，规则 -1）
- [ ] T063 [US4] 移除旧 Bridge token 写与字段（三步切换第三步）：迁移 `migrations/095_drop_bridge_token.sql` + 按 R7-7 更新 `docs/architecture/BRIDGE_TOKEN_PROTOCOL.md` 及测试（`mcp_guard_test.go`、`cors_bridge_preflight_test.go`、`audit_redact_test.go`）

**Checkpoint**: US4 独立可用；旧 bartoken 识别成功率 0%（SC-005），重复实现数 0（SC-006）。

---

## Phase 7: Polish & Cross-Cutting Concerns

- [ ] T064 [P] 全量编译与静态检查：`go build ./...`、`go vet ./...`、`gofmt -l`（宪法 Quality Gates）
- [ ] T065 受影响包测试真实运行（显式 `export POSTGRES_TEST_PASSWORD=...`），贴输出；检查 `t.Skipf` 不把跳过当通过（宪法 III / 测试陷阱）
- [ ] T066 API 三端验证：`python3 scripts/api_verify_full.py`（入参/返回/预期/DB），全过（SC-009）
- [ ] T067 更新架构文档：`docs/architecture/BRIDGE_TOKEN_PROTOCOL.md` 改为识别码协议；外层 `定制任务清单.md`、`线索发掘模块-完整方案.md` 标记已落地
- [ ] T068 UI 全页面逐元素 ego-browser 显式点验并留证据（快照/截图/接口码/toast，规则 -1）
- [ ] T069 权限与安全复查：所有新增列表接口强制数据范围过滤、平台调用业务码判成败、无硬编码密钥
- [ ] T070 按 quickstart.md 跑通端到端主线并贴证据
- [ ] T071 整体完成且验证全绿后一次性 `git add` + `commit` + `push` 双远端（gitee-upstream/upstream，不写死 origin；宪法提交纪律）

---

## Dependencies & Execution Order

- **Phase 1 Setup** → 无依赖。
- **Phase 2 Foundational（US4 地基）** → 依赖 Setup；**阻塞 US1/US2/US3**。
- **Phase 3 US1（一）** → 依赖 Phase 2；与 US2/US3 独立（T031 与 T041 有接口对接）。
- **Phase 4 US2（二）** → 依赖 Phase 2 + T031/CRM 客户模型（T018-T021）。
- **Phase 5 US3（三）** → 依赖 Phase 2 + US2 的归档（T041）与 ConversationContext（T033）。
- **Phase 6 US4 收尾** → 依赖 Phase 2；配置能力可与 US1-US3 并行，bartoken 移除（T063）须最后。
- **Phase 7 Polish** → 依赖全部 US 完成。

### Parallel Opportunities
- T003/T004/T006/T007/T008 可并行（Setup/Foundational 不同文件）。
- T018-T021 模型迁移可并行；T032-T034 线索模型迁移可并行。
- 各 US 的 Controller/Router/前端在对应 Model/Service 完成后可并行。

## Implementation Strategy（MVP First）

1. 完成 Phase 1 + Phase 2（地基 CRITICAL）。
2. 先交付 US1 MVP：统一客户主数据 + 权限隔离 + 去重 + 360（独立验证）。
3. 增量加 US2：三粒度规则 + 三路检测 + 归档通知。
4. 增量加 US3：智能体剥离 + 共享上下文。
5. 增量加 US4 收尾：识别码硬切换 + 三路合并。
6. 每步独立验证，最后一次性提交。

## Notes
- [P] = 不同文件、无依赖；[Story] 映射到 spec.md 的 US1-US4。
- 数据模型字段约束见 data-model.md；任务描述中引用的约束（唯一/可重置/多通知人/业务码判成败等）不得在实作时自行放宽。
- UI 测试必须逐元素 ego-browser 显式操作（全局规则 -1），禁止批量点击脚本。
- 子智能体由主智能体编排（宪法 v1.0.5）；按用户指令「一次一个任务」串行推进，主智能体负责调度/监督/纠正。
