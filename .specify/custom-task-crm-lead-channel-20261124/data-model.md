# Data Model: 四大部分定制任务

**Date**: 2026-11-24 | 依据 spec.md 的 Key Entities 与 FR 约束

> 字段约束在 tasks.md 的任务描述中逐条引用，实施时不得自行放宽。

## 渠道域（第四部分地基）

### channels（渠道，预设数据表驱动）
- `id`, `name`, `code`（唯一）, `type`（单聊/群聊/客服/...）, `enabled`, `remark`, `created_at`, `updated_at`
- 约束：`code` 唯一；新增渠道为一次性配置，全系统读取本表。

### channel_accounts（渠道账号，单渠道多账号）
- `id`, `channel_id`(FK), `name`, `remark`, `self_identify_name`, `type`
- `auto_reply_agent_id`（nullable）, `lead_mining_agent_id`（nullable）
- `owner_user_ids`（多通知人/负责人）, `data_scope`（可见账号/行业范围 JSON）
- `identify_code`（关联 channel_identify_codes）, `status`, `created_at`, `updated_at`
- 约束：渠道账号列表/下拉统一走 `ChannelAccountService.ListOptions`（FR-029）；自他识别走 `ChannelResolver`（FR-030）。

### channel_identify_codes（唯一识别码，替代 bartoken）
- `id`, `channel_id`(FK, 唯一), `code`（唯一、可重置）, `version`, `created_at`, `reset_at`
- 约束：一次性替换 bartoken；三条链路均用 `code` 解析租户/账号（FR-028）。

### channel_agent_bindings（改造）
- 移除“智能体管理中绑定渠道账号”的逻辑；绑定关系收口到 `channel_accounts` 的 `auto_reply_agent_id`/`lead_mining_agent_id`。

## CRM 域（第一部分）

### customers（扩展）
- 现有字段 + `source_channel_account_id`, `source_type`, `industry`, `owner_user_id`, `merged_into_id`
- 关系：1:1 OneID；1:N Contact/Company 关联；N:M Tag；1:N FollowUp；1:N Clue

### one_ids / customer_merges（身份归一）
- `one_id`, `customer_id`, `merge_log`（来源、操作人、时间、原始值）——去重合并可追溯（FR-003/005）

### companies（扩展既有：迁移 053 + 079，已有 model/company.go + service/company.go）
- 现有字段 + `tenant_id`, `owner_user_id`, `source`, `source_ref`（迁移 079 已加）；扩展行业/区域检索字段
- 约束：不重复新建公司域，仅在既有应用层上扩展（勘察结论）

### contacts / suppliers / service_providers（新建）
- `id`, `name`, `customer_id`(FK), `channel_account_id`(FK), `contact_info`, `role`, `remark`

### customer_tags / follow_up_records / operation_logs
- 标签与跟进记录挂客户；`operation_logs` 记录新增/改/删/合并/导入导出（FR-005）

## 线索域（第二部分）

### lead_mining_configs（去单例化）
- 由租户单例扩展为支持三粒度：`scope`（global/channel/account）+ `scope_id`；保留旧单例数据迁移为 global 规则。

### lead_mining_rules（新）
- `id`, `scope`, `scope_id`, `keywords`, `semantic_keywords`, `system_description`, `enabled`

### lead_mining_hits（三路并行证据）
- `id`, `judgement_id`, `type`（keyword/semantic/llm）, `payload`, `evidence_message_ids`, `created_at`

### lead_mining_judgements（二次 LLM 结论）
- `id`, `message_id`, `channel_account_id`, `is_clue`, `clue_type`, `role`（demand/supply/resource/broker/unknown）, `summary`, `context_message_ids`, `created_at`

### lead_mining_notify_routes（通知路由）
- `id`, `channel_account_id`, `notify_user_ids`, `notify_types`（站内/TG/飞书）, `delivery_records`

### conversation_contexts（共享对话历史）
- `id`, `channel_account_id`, `external_conversation_id`, `member_roles`, `messages`（多轮+历史）, `updated_at`
- 供两智能体交叉引用（FR-020/021）

### clues（扩展）
- 现有字段 + `customer_id`, `user_info`, `source_hit_ids`, `role`, `archived_at`

## 权限域（第十六 FR，原 RBAC 不改）

### channel_account_authorizations（新，不触碰原 RBAC 表）
- `user_id`, `channel_account_id`, `visible_industry_scope`, `role`
- 所有客户/消息列表接口强制按本表 + 账号范围过滤（FR-002/010/016）

## 状态转换

- 线索：`检测中 → 已判定(is_clue) → 已归档 → 已通知`；LLM 超时转 `降级判定` 并标记证据不完整。
- 识别码：`生效 → 双写 → 切换读 → 下版本移除 bartoken`。
- 客户合并：`独立 → 合并中 → 已合并(merged_into_id)`；支持拆回并留审计。
