# Feature Specification: 线索发掘模块缺口收口（统计维度 / 群聊上下文 / 评分视图）

**Feature Branch**: `lead-mining-gap-close-20261124`

**Created**: 2026-11-24

**Status**: Draft

**Input**: 用户「完整线索发掘模块」六组需求；基线方案 `外层 docs/线索发掘模块-完整方案.md`；
已落地规格 `.specify/custom-task-crm-lead-channel-20261124/`（Phase 2–7 已完成并推送双远端）。

## 背景与范围

前一轮定制任务已把「个微/QQ 上报 → ReAct 线索判定（单聊双向/群聊多成员）→ 归档 → 通知 →
客户360/OneID → 渠道账号配置 → 统一收件箱」全链路落地（证据：`git log` 提交 `74d69c48f` /
`6ba2ac382`，迁移 088–096，`user-web/src/views/leads/`）。本轮**不重复实现已落地能力**，
只收口经代码核验仍存在的三处真实缺口：

1. 线索统计接口 `/api/clues/statistics` 只按 `type` 聚合、无时间窗与角色/渠道/单群聊维度，
   前端 `views/clue/Statistics.vue` 靠拉 10000 行自行聚合，且会静默截断。
2. 群聊上下文聚合函数 `AggregateGroupContext` 只在 `lead_discovery.go` 被 best-effort 调用，
   二次 LLM 判定提示（`lead_judge_context.go`）只读 `MemberRoles` 原始映射，未消费聚合后的
   `[角色:发送者]` 富化消息与角色计数，群聊「谁在求购/谁在供货」证据表达弱。
3. 线索评分后端 API（`/clue/score*`）齐全，前端无评分看板页——**本轮不做**（不属于用户六组需求，
   记为已知非阻塞缺口，后续单独立项）。本轮只把统计页接到新的区间统计接口。

本轮同时把前一轮 `tasks.md` 的 T001–T071 全部标记为已完成（代码与提交证据已坐实），
消除规格台账与代码现实的偏差；不修改前一轮 spec/plan 的实质内容。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 线索统计按时间窗与多维聚合（Priority: P1）

作为线索负责人，我需要在线索统计页按时间区间查看「需求方/供货方/资源方/中介」分布、
渠道/账号对比、单聊/群聊占比，且统计结果不被 10000 行上限静默截断。

**Why this priority**: 用户第 4 组需求「线索列表、线索统计如何展示需求方/供货方/上游下游」；
当前实现用前端假聚合 + 静默截断，读数不可信。

**Independent Test**: 构造跨时间窗的线索夹具（不同 lead_role / source_channel /
conversation_type），调用扩展后的统计接口，验证时间窗过滤生效、各维度计数与夹具一致、
无截断提示。

**Acceptance Scenarios**:

1. **Given** 库中存在今天与 30 天前的线索，**When** 传入 `start`/`end` 查询统计，**Then**
   只返回时间窗内的线索聚合，返回结构含 `by_lead_role` / `by_status` / `by_channel` /
   `by_conversation` / `by_type` / `total` / `truncated=false`。
2. **Given** 同一线索既是需求方又属某渠道的群聊，**When** 查看统计，**Then** 角色、渠道、
   单群聊三个维度各自计数正确，互不覆盖。
3. **Given** 时间窗参数非法（start > end 或非整数），**When** 调用接口，**Then** 返回 400
   参数错误，不返回误导性空统计。

### User Story 2 - 群聊判定使用聚合后的成员角色上下文（Priority: P2）

作为线索发掘运营，我需要二次 LLM 判定时看到的群聊消息带 `[角色:发送者]` 前缀与角色计数，
从而正确区分「群内求购的人」与「群内供货的人」。

**Why this priority**: 用户第 2 组需求「群聊也需要结合群聊内容分析大家的发言」；
现有提示只回显 `MemberRoles` 原始映射，缺少逐条消息的说话人标识。

**Independent Test**: 构造一个含 3 名成员角色标记的群聊上下文，调用判定提示构造函数，
验证提示中包含每个 `[role:sender]` 富化消息与角色计数行；未标记角色的消息明确标 `unknown`。

**Acceptance Scenarios**:

1. **Given** 群聊上下文有 member_roles={u1:demand,u2:supply}，**When** 构造判定提示，
   **Then** 提示中包含 `[demand:u1]` / `[supply:u2]` 前缀的消息和「群成员发言统计」行。
2. **Given** 单聊上下文，**When** 构造判定提示，**Then** 不出现群聊角色统计行，保持既有行为。
3. **Given** `AggregateGroupContext` 读取失败，**When** 构造判定提示，**Then** 降级回原始
   `MemberRoles` 映射与原始消息，并在提示中标注证据不完整（fail-visible）。

### User Story 3 - 统计页使用服务端区间聚合（Priority: P3）

作为线索运营，我在线索统计页选日期区间时，读数应来自服务端区间统计接口，不再靠拉
10000 行前端自行聚合（会静默截断、且无法覆盖角色/渠道/单群聊维度）。

**Why this priority**: 用户第 4 组需求要求统计展示需求方/供货方/上游下游；后端新接口已就绪，
前端必须接上才对用户可见。

**Independent Test**: 在统计页选择日期区间并刷新，断言请求打到 `/clues/statistics/range`，
返回的六维数据被渲染，且不再调用分页列表拉取全量行。

**Acceptance Scenarios**:

1. **Given** 用户选了时间区间，**When** 刷新统计，**Then** 前端请求区间统计接口并渲染六维读数。
2. **Given** 用户未选时间区间，**When** 刷新统计，**Then** 行为与现状一致（不传 start/end）。
3. **Given** 接口返回错误，**When** 页面渲染，**Then** 显示明确错误，不展示误导性零值。

### Edge Cases

- 时间窗跨越业务时区自然日：统计按 create_time unix 秒过滤，前端传入按本地自然日换算的边界。
- 无数据时各维度返回空数组、`total=0`，不返回 null。
- 群聊上下文 `channel_account_id=0`（未绑定账号主数据）时跳过聚合，不报错。
- 评分 API 返回 404（未评分）时看板显示「未评分」而非空白。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 扩展 `GET /api/clues/statistics`，支持 `start`/`end` unix 秒查询参数，非法参数返 400。
- **FR-002**: 统计接口 MUST 返回 `by_type`、`by_lead_role`、`by_status`、`by_channel`、
  `by_conversation`、`total` 六个维度，且 `truncated=false`（服务端全量聚合，无行数上限）。
- **FR-003**: `buildLeadJudgeSystemPrompt` MUST 在群聊场景消费 `AggregateGroupContext` 的富化消息
  与角色计数；读取失败 MUST 降级并标注证据不完整。
- **FR-004**: 线索统计页 MUST 在时间区间刷新时调用 `/api/clues/statistics/range`，渲染六维聚合；
  不选区间时保持既有行为。
- **FR-005**: 新增或修改的 API 必须走五层架构（Repository 聚合 / Service 校验 / Controller 响应），
  错误走 `response.Error`，不把 `err.Error()` 透传前端。
- **FR-006**: 前一轮 `.specify/custom-task-crm-lead-channel-20261124/tasks.md` 的 T001–T071 MUST
  按代码现实标记为已完成，不改其 spec/plan 实质内容。

### Key Entities

- `Clue`：已存在，复用 lead_role / status / source_channel / is_group / create_time 字段。
- `ConversationContext`：已存在，复用 member_roles / messages。
- `ClueScore`：已存在，复用 grade / 五维分数。

## Success Criteria *(mandatory)*

- **SC-001**: 统计接口对同一批夹具的聚合结果与直接 SQL COUNT 完全一致（测试断言）。
- **SC-002**: 群聊判定提示含 `[role:sender]` 富化消息与角色计数（测试断言）。
- **SC-003**: 统计页区间刷新请求打到区间统计接口、六维数据渲染，有前端单测覆盖。
- **SC-004**: `go build ./...`、`go vet ./...`、`npm run test`、`npm run lint:check`、
  `check-architecture.sh` 全绿；新增违规 0。

## Assumptions

- 不引入新表/迁移（所有字段已存在）。
- 不改变已有 behaviors（单聊判定、通知路由、渠道账号等），仅补缺口。
- 前端路由新增需同步 `router-list.md` 与 i18n 双语、新手引导（FRONTEND-CODING-STANDARDS §1.6）。
- 新增评分页作为线索模块子页，不新增顶级菜单，因此只需页面级引导（4 步）。
