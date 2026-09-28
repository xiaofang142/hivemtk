# Service 层业务域结构（P0-2）

> **状态**：业务域边界已定义；物理分子目录 deferred（环境约束：git lock + 热重载）  
> **目标**：消除 service 层扁平化导致的业务域边界模糊，便于团队协作与代码定位  
> **创建时间**：2026-07-17

## 背景

`internal/service/` 目录原有 118 个非测试文件 + 89 个测试文件扁平堆放，销冠域、触达域、卡片域等
业务域物理混排，导致：
- 团队协作易产生文件命名冲突
- 包内可见性过宽，跨域私有方法可被直接调用
- 新成员难以快速定位某业务域的代码

本文档定义 8 个业务域及其文件归属，作为物理分子目录的蓝本。当环境约束解除（git lock 释放、
热重载暂停）后，按此蓝本执行物理移动 + import 重写 + 包名变更。

## 业务域划分

### 1. sales/ — AI 销冠域（22 文件）
中央调度枢纽：消息→意图→记忆→SOP→RAG→LLM→润色→审核→反馈学习完整链路。

| 文件 | 职责 |
|------|------|
| sales_engine.go | 销售引擎中央调度（9 步链路） |
| sales_engine_adapters.go | RAG/话术/客户查询适配器 |
| sales_engine_agent.go | 多智能体上下文 |
| sales_playbook.go | 销冠话术库 + 异议处理 |
| sales_action_trigger.go | 销售动作触发器 |
| sales_event_stats.go | 销售事件统计（DB 权威，H2 替代内存版 sales_dashboard） |
| sales_workbench.go | 销售工作台 |
| sales_persona_service.go | 销售人设 |
| intent_recognition.go | 意图识别（规则+LLM） |
| dialogue_memory.go | 对话记忆（短期+长期） |
| memory_system.go | 记忆系统 |
| sop_service.go | SOP 智能体 |
| sop_abtest.go | SOP A/B 测试 |
| sop_condition.go | SOP 条件评估 |
| sop_scheduler.go | SOP 调度器 |
| humanize_polisher.go | 拟人润色 |
| feedback_learner.go | 反馈学习器（AI 自我进化） |
| objection_handler_service.go | 异议处理 |
| smart_cs_orchestrator.go | 智能客服编排（人机协同） |
| followup_reminder.go | 跟进提醒 |
| repurchase_engine.go | 复购引擎 |

### 2. reach/ — 触达域（9 文件）
多渠道消息触达管道：编排→队列→发送→反馈。

| 文件 | 职责 |
|------|------|
| reach_pipeline.go | 触达管道（编排+限流+配额） |
| message.go | 消息模型 |
| message_hub_service.go | 消息中心 |
| message_queue_service.go | 消息队列 |
| webhook_service.go | Webhook 接入 |
| event_tracker.go | 事件追踪 |
| live_code.go | 活码 |
| short_link.go | 短链 |
| domain_pool.go | 域名池 |

### 3. card/ — 卡片域（10 文件）
5 平台卡片中心（抖音/快手/闲鱼/小红书/TikTok），共享 BaseController。

| 文件 | 职责 |
|------|------|
| douyin_card.go | 抖音卡片 |
| douyin_card_stats.go | 抖音卡片统计 |
| kuaishou_card.go | 快手卡片 |
| kuaishou_card_stats.go | 快手卡片统计 |
| xianyu_card.go | 闲鱼卡片 |
| xianyu_card_stats.go | 闲鱼卡片统计 |
| xiaohongshu_card.go | 小红书卡片 |
| xiaohongshu_card_stats.go | 小红书卡片统计 |
| tiktok_card.go | TikTok 卡片 |
| card_access_service.go | 卡片访问控制 |

### 4. customer/ — 客户域（13 文件）
OneID 客户体系：身份合并→360 画像→旅程→会话→流失预测。

| 文件 | 职责 |
|------|------|
| customer_360.go | 客户 360 画像 |
| customer_service.go | 客户基础服务 |
| customer_identity_service.go | OneID 身份服务 |
| customer_identity_conflict_service.go | 身份冲突解决 |
| customer_identity_normalize.go | 身份归一化 |
| customer_journey.go | 客户旅程 |
| customer_session.go | 客户会话 |
| churn_prediction.go | 流失预测 |
| rfm_calculator.go | RFM 计算 |
| session_assignment.go | 会话分配 |
| inbox_service.go | 收件箱 |
| unified_inbox.go | 统一收件箱 |
| unified_inbox_utils.go | 统一收件箱工具 |

### 5. rag/ — 知识库域（6 文件）
三级 RAG 架构：知识库管理→检索→配置。

| 文件 | 职责 |
|------|------|
| knowledge_base.go | 知识库基础 |
| knowledge_merchant.go | 知识库业务适配 |
| knowledge_service.go | 知识库服务 |
| knowledge_statistics_service.go | 知识库统计 |
| rag_config_service.go | RAG 配置 |
| rag_factory.go | RAG 工厂 |
| rag_searcher.go | RAG 召回器 |

### 6. marketing/ — 营销域（6 文件）
营销自动化：流程编排→A/B 实验→报表→转化漏斗。

| 文件 | 职责 |
|------|------|
| marketing_flow.go | 营销流程编排 |
| ab_experiment.go | A/B 实验 |
| custom_report.go | 自定义报表 |
| conversion_funnel_service.go | 转化漏斗 |
| dashboard_screen.go | 看板屏幕 |
| performance_test_service.go | 性能测试 |

### 7. channel/ — 渠道域（22 文件）
9 渠道适配器：企微/WhatsApp/SMS/邮件/飞书/抖音/闲鱼/小红书/TikTok 自动回复。

| 文件 | 职责 |
|------|------|
| wecom.go | 企业微信 |
| wecom_account_health.go | 企微账号健康度 |
| wecom_integration.go | 企微集成 |
| Whatsapp.go | WhatsApp |
| whatsapp_template_service.go | WhatsApp 模板 |
| Sms.go | 短信 |
| sms_helpers.go | 短信工具 |
| email_draft.go | 邮件草稿 |
| email_jobs.go | 邮件任务 |
| email_list.go | 邮件列表 |
| email_send.go | 邮件发送 |
| email_smtp.go | 邮件 SMTP |
| feishu_service.go | 飞书 |
| AutoReply.go | 自动回复（通用） |
| tiktok_auto_reply.go | TikTok 自动回复 |
| xianyu_auto_reply.go | 闲鱼自动回复 |
| xiaohongshu_auto_reply.go | 小红书自动回复 |
| template_market.go | 模板市场 |
| script_template.go | 话术模板 |
| platform_account.go | 平台账号 |
| integration.go | 渠道集成 |
| openapi_service.go | OpenAPI 服务 |

### 8. system/ — 系统域（30 文件）
系统基础设施：认证/授权/用户/团队/备份/监控/配置/AI 能力。

| 文件 | 职责 |
|------|------|
| auth.go | 认证 |
| account.go | 账户 |
| user.go | 用户 |
| system_user.go | 系统用户 |
| team_user.go | 团队用户 |
| permission_check.go | 权限校验 |
| system_config.go | 系统配置 |
| system_init.go | 系统初始化 |
| system_monitor.go | 系统监控 |
| backup.go | 备份 |
| obs_config.go | OBS 配置 |
| llm_routing_service.go | LLM 路由 |
| ai_agent_service.go | AI 智能体 |
| ai_content.go | AI 内容 |
| ai_productivity_service.go | AI 生产力 |
| ai_tagger.go | AI 标签 |
| auto_tagger.go | 自动标签 |
| batch_operation.go | 批量操作 |
| clue.go | 线索 |
| community.go | 社群 |
| material.go | 素材 |
| order.go | 订单 |
| order_draft.go | 订单草稿 |
| order_draft_helpers.go | 订单草稿工具 |
| sm_list.go | 短信列表 |
| unified_message.go | 统一消息 |
| knowledge_merchant.go | 知识库业务适配（跨 rag/system） |

## 物理移动 Deferred 计划

当前环境约束（Trae IDE git 扩展持续持有 index.lock + air 热重载监控）使物理移动 207 文件风险不可控
（无法 commit 回滚 + 热重载风暴）。待环境稳定后执行：

1. 创建 8 个子目录（sales/ reach/ card/ customer/ rag/ marketing/ channel/ system/）
2. `git mv` 移动文件至对应子目录
3. 修改每个文件的 `package service` → `package <domain>`
4. 全局重写 import 路径：`hivemtk-user/internal/service` → `hivemtk-user/internal/service/<domain>`
5. 处理跨域共享类型（如 `SalesRequest` 被多处引用，需决定归属或提取到 `shared/`）
6. 运行 `goimports -w` 自动整理 import
7. `go build ./... && go test ./...` 验证
8. 单次 commit

## 跨域共享类型（需特殊处理）

以下类型被多个业务域引用，物理移动时需决定归属或提取到 `shared/` 子包：

- `SalesRequest` / `SalesResponse`（sales 定义，channel 调用）
- `Message`（reach 定义，sales/channel 调用）
- `Customer` model（customer 定义，sales/reach 调用）
- `ScriptTemplate`（sales 定义，channel 调用）
