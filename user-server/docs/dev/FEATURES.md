# user-server 功能清单

> **规则级别**: ⭐⭐ 项目级开发文档
> **关联文档**:
> - 架构图: [./ARCHITECTURE.md](./ARCHITECTURE.md)
> - 代码开发手册: [./DEVELOPMENT.md](./DEVELOPMENT.md)
> - 代码规范: [./CONVENTIONS.md](./CONVENTIONS.md)
> - 营销功能模块索引（产品级总览）: [docs/marketing-features/README.md](../../../docs/marketing-features/README.md)
> - 工程 README: [user-server/README.md](../../README.md)

本文档按业务域分组列出 `user-server` 工程的全部功能模块，每个功能项以表格形式呈现，覆盖 **功能名称、状态、所在包、API 路由、关联文档** 五个维度。
所有功能均已在代码中实现（✅），未实现或下线的功能不在本清单内（开源版已移除 OTA / License / 版本下载 / 定价 / 注册开户）。

> ⚠️ **关于「关联文档」列**：该列给出的是计划中的逐模块文档文件名，**这些文档目前尚未编写**
> （`docs/marketing-features/` 下现仅有 `README.md` 与 `GEO_MODULE_DESIGN.md`）。
> 此前这些文件名被写成 Markdown 链接，实际全部指向不存在的文件，导致文档断链检查（lychee）长期失败。
> 现统一降级为代码文本，功能本身以代码为准；逐模块文档补齐进度见
> `bash scripts/feature-doc-coverage-report.sh`。

---

## 一、统计总览

| 业务域 | 子模块数 | 子目录前缀 |
| --- | --- | --- |
| 认证与用户管理 | 4 | `auth-*` / `user-*` / `merchant-init-*` / `websocket-*` |
| 多平台卡片 | 5 | `card-*` |
| 自动回复与 RAG | 8 | `auto-reply-*` / `rag-*` / `knowledge-*` / `script-*` |
| 邮件营销 | 7 | `email-*` |
| 短信营销 | 4 | `sms-*` |
| 社群管理 | 8 | `community-*` / `wecom-*` / `feishu-*` / `telegram-*` / `dingtalk-*` |
| 短链与活码 | 3 | `shortlink-*` / `livecode-*` / `domain-*` |
| 线索与客户 | 10 | `clue-*` / `customer-*` / `cs-*` / `oneid-*` / `tag-*` |
| 营销自动化 | 8 | `marketing-*` / `ab-*` / `rfm-*` / `churn-*` / `report-*` / `dashboard-*` / `batch-*` / `recovery-*` |
| 内容创作 | 4 | `content-*` / `template-*` / `material-*` / `file-upload-*` |
| 系统管理 | 11 | `system-*` / `obs-*` / `backup-*` / `operation-log-*` / `security-audit-*` / `trace-*` / `sse-*` / `llm-provider-*` / `tuning-*` / `anomaly-*` |
| 安全与权限 | 2 | `permission-*` / `row-level-security-*` |
| 第三方对接 | 2 | `integration-*` / `sync-*` |
| 统一消息 | 4 | `unified-message-*` / `unified-inbox-*` / `message-hub-*` / `platform-account-*` |
| **AI 销冠核心** | 7 | `dialogue-memory-*` / `intent-*` / `sop-*` / `llm-routing-*` / `objection-*` / `persona-*` / `reach-pipeline-*` |
| 多 AI 智能体 | 3 | `ai-agent-*` / `channel-agent-binding-*` / `cs-agent-mount-*` |
| 数据分析 | 3 | `customer-journey-*` / `conversion-funnel-*` / `ai-productivity-*` |
| 客服 Web Widget | 1 | `chat-channel-*` |
| **合计（用户端）** | **94** | - |

> 平台端 10 份功能文档见 `hivemtk-platform/docs/platform-features/`（独立仓库）。

---

## 二、认证与用户管理域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 登录认证与 JWT 鉴权 | ✅ | `controller/auth.go` · `service/auth.go` · `service/mfa.go` · `service/login_risk.go` | `POST /api/auth/login` / `POST /api/auth/refresh` / `GET /api/auth/current-user` / `POST /api/auth/change-password` | [auth-login-jwt.md`auth-login-jwt.md` |
| 用户管理 CRUD | ✅ | `controller/user.go` · `service/user.go` · `service/system_user.go` | `GET/POST/PUT/DELETE /api/system/users` | [user-management.md`user-management.md` |
| 商户初始化向导 | ✅ | `controller/auth.go` · `service/install.go` | `GET /api/public/init` / `POST /api/system/init-admin` | [merchant-initialization.md`merchant-initialization.md` |
| WebSocket 实时通信 | ✅ | `websocket/hub.go` · `websocket/handler.go` · `websocket/visitor_handler.go` · `websocket/seq.go` · `websocket/ack_tracker.go` · `websocket/notify.go` | `GET /api/ws/agent` / `GET /api/ws/visitor` | [websocket-realtime.md`websocket-realtime.md` |

---

## 三、多平台卡片域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 抖音卡片生成 | ✅ | `controller/douyin_card.go` · `service/douyin_card.go` | `POST /api/cards/douyin` / `GET /api/cards/douyin/:id` | [card-douyin.md`card-douyin.md` |
| 快手卡片生成 | ✅ | `controller/kuaishou_card.go` · `service/kuaishou_card.go` | `POST /api/cards/kuaishou` / `GET /api/cards/kuaishou/:id` | [card-kuaishou.md`card-kuaishou.md` |
| 小红书卡片生成 | ✅ | `controller/xiaohongshu_card.go` · `service/xiaohongshu_card.go` | `POST /api/cards/xiaohongshu` | [card-xiaohongshu.md`card-xiaohongshu.md` |
| 闲鱼卡片生成 | ✅ | `controller/xianyu_card.go` · `service/xianyu_card.go` | `POST /api/cards/xianyu` | [card-xianyu.md`card-xianyu.md` |
| TikTok 卡片生成 | ✅ | `controller/tiktok_card.go` · `service/tiktok_card.go` | `POST /api/cards/tiktok` | [card-tiktok.md`card-tiktok.md` |

---

## 四、自动回复与 RAG 域

> 📌 **RAG 三文档边界关系**：`rag-knowledge-base`(配置) → `knowledge-management`(内容入库) → `agent-rag-qa`(应用调用)。

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 通用自动回复（chromedp） | ✅ | `service/auto_reply.go` · `aiagent/agent/browser/` | `POST /api/auto-reply/execute` | [auto-reply-universal.md`auto-reply-universal.md` |
| 闲鱼自动回复 | ✅ | `service/auto_reply.go`（闲鱼 adapter） | `POST /api/auto-reply/xianyu` | [auto-reply-xianyu.md`auto-reply-xianyu.md` |
| TikTok 自动回复 | ✅ | `service/auto_reply.go`（TikTok adapter） | `POST /api/auto-reply/tiktok` | [auto-reply-tiktok.md`auto-reply-tiktok.md` |
| RAG 知识库配置 | ✅ | `service/rag_health.go` · `aiagent/rag/` · `aiagent/embedding/` · `aiagent/vector/` | `GET/POST /api/rag/config` | [rag-knowledge-base.md`rag-knowledge-base.md` |
| RAG 产品配置 | ✅ | `service/rag_health.go` | `POST /api/rag/products` | [rag-product-config.md`rag-product-config.md` |
| 知识库文档管理 | ✅ | `aiagent/knowledge/controller/` · `aiagent/knowledge/service/` · `aiagent/knowledge/repository/` | `POST /api/knowledge/documents` / `GET /api/knowledge/search` | [knowledge-management.md`knowledge-management.md` |
| RAG 智能客服 | ✅ | `service/sales_engine.go`（RAG 检索调用） | `POST /api/chat/public/sessions/:session_id/messages` | [agent-rag-qa.md`agent-rag-qa.md` |
| 话术库 | ✅ | `controller/sop.go` · `service/sop_loader.go` | `GET/POST /api/script-library` | [script-library.md`script-library.md` |

---

## 五、邮件营销域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 邮件列表与收件人 | ✅ | `email/service/emaillist.go` · `dto/emaillist.go` | `GET/POST /api/email/lists` | [email-list-management.md`email-list-management.md` |
| SMTP 配置管理 | ✅ | `email/service/emailsmtp.go` · `dto/emailsmtp.go` | `GET/POST /api/email/smtp` | [email-smtp-config.md`email-smtp-config.md` |
| 邮件草稿 | ✅ | `email/service/emaildraft.go` · `dto/emaildraft.go` | `GET/POST/PUT /api/email/drafts` | [email-draft-management.md`email-draft-management.md` |
| 邮件任务 | ✅ | `email/service/emailjobs.go` · `dto/emailjobs.go` | `GET/POST /api/email/jobs` | [email-jobs-management.md`email-jobs-management.md` |
| 邮件发送执行 | ✅ | `email/service/emailsend.go` · `dto/emailsend.go` | `POST /api/email/send` | [email-send-execution.md`email-send-execution.md` |
| 邮件追踪（打开/点击像素 + Webhook） | ✅ | `email/service/`（tracking 模块） | `GET /api/email/track/open/:id` · `GET /api/email/track/click/:id` | [email-tracking.md`email-tracking.md` |
| 退订管理 | ✅ | `email/service/`（unsubscribe 模块） | `POST /api/email/unsubscribe` | [email-unsubscribe.md`email-unsubscribe.md` |

---

## 六、短信营销域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 短信配置（阿里云/腾讯云/华为云） | ✅ | `controller/sms.go` · `service/sms.go` | `GET/POST /api/sms/config` | [sms-config.md`sms-config.md` |
| 短信列表与发送 | ✅ | `controller/sms.go` · `service/sms.go` · `dto/sms.go` | `GET/POST /api/sms/list` | [sms-list-management.md`sms-list-management.md` |
| 短信草稿 | ✅ | `controller/sms.go` · `service/sms.go` | `GET/POST /api/sms/drafts` | [sms-draft-management.md`sms-draft-management.md` |
| 短信任务调度 | ✅ | `controller/sms.go` · `service/sms.go` · `service/sms_tracking.go` | `POST /api/sms/jobs` | [sms-jobs-management.md`sms-jobs-management.md` |

---

## 七、社群管理域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| WhatsApp 营销 | ✅ | `service/whatsapp.go` · `controller/whatsapp.go` | `POST /api/whatsapp/send` | [community-whatsapp.md`community-whatsapp.md` |
| Telegram AI 销售自动化 | ✅ | `channelbot/telegram/` | `POST /api/telegram/automation` | [agent-telegram-automation.md`agent-telegram-automation.md` |
| Telegram 账号管理 | ✅ | `controller/telegram.go` · `service/telegram.go` | `GET/POST /api/telegram/accounts` | [telegram-account.md`telegram-account.md` |
| 企业微信 | ✅ | `controller/wecom.go` · `service/wecom.go` · `dto/community.go` | `GET/POST /api/wecom/*` | [community-wecom.md`community-wecom.md` |
| 企微账号管理（含健康度） | ✅ | `controller/wecom.go` · `service/wecom.go` · `repository/wecom.go` | `GET/POST /api/wecom/accounts` | [wecom-account.md`wecom-account.md` |
| 飞书账号管理 | ✅ | `controller/feishu.go` · `service/feishu.go` · `repository/feishu.go` | `GET/POST /api/feishu/accounts` | [feishu-account.md`feishu-account.md` |
| 钉钉应用账号管理 | ✅ | `controller/dingtalk.go` · `service/dingtalk.go` | `GET/POST /api/dingtalk/apps` | [dingtalk-app-account.md`dingtalk-app-account.md` |
| 通用社群管理 | ✅ | `controller/community.go` · `service/community.go` | `GET/POST /api/community` | [community-management.md`community-management.md` |

---

## 八、短链与活码域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 短链管理（含统计） | ✅ | `controller/short_link.go` · `service/short_link.go` · `dto/shortlink.go` | `POST /api/short-link/create` / `GET /api/short-link/list` | [shortlink-management.md`shortlink-management.md` |
| 活码管理 | ✅ | `controller/live_code.go` · `service/live_code.go` · `dto/livecode.go` | `GET/POST /api/live-code/*` | [livecode-management.md`livecode-management.md` |
| 域名池管理 | ✅ | `controller/domain_pool.go` · `service/domain_pool.go` · `dto/domain_pool.go` | `GET/POST /api/domain-pool` | [domain-pool.md`domain-pool.md` |

---

## 九、线索与客户管理域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 线索管理 | ✅ | `controller/clue.go` · `service/clue.go` · `service/clue_score.go` · `dto/clue.go` · `dto/clue_score.go` | `GET /api/clue/list` / `POST /api/clue/score` | [clue-management.md`clue-management.md` |
| 客户 360 视图 | ✅ | `controller/customer.go` · `service/customer.go` | `GET /api/customer/360/:id` | [customer-360.md`customer-360.md` |
| 客户事件追踪 CDP | ✅ | `service/customer.go`（事件追踪） | `POST /api/customer/events` | [cdp-event-tracking.md`cdp-event-tracking.md` |
| OneID 身份统一 | ✅ | `identity/normalize.go` | `POST /api/oneid/merge` | [oneid.md`oneid.md` |
| 标签分层 | ✅ | `service/segment.go` · `model/customer_tag.go` | `GET/POST /api/customer-tags` | [tag-segmentation.md`tag-segmentation.md` |
| 客服会话 | ✅ | `controller/customer_session.go` · `service/customer_session.go` | `GET/POST /api/customer-sessions` | [cs-session.md`cs-session.md` |
| 客服代理 | ✅ | `controller/customer_service_agent.go` · `service/customer_service_agent.go` | `GET/POST /api/cs-agents` | [cs-agent.md`cs-agent.md` |
| 快捷回复 | ✅ | `controller/quick_reply.go` · `service/quick_reply.go` | `GET/POST /api/quick-reply` | [cs-quick-reply.md`cs-quick-reply.md` |
| 会话标签 | ✅ | `controller/session_tag.go` · `service/session_tag.go` | `GET/POST /api/session-tags` | [cs-session-tag.md`cs-session-tag.md` |
| AI 建议 | ✅ | `service/sales_engine.go`（AI 建议调用） | `POST /api/cs/ai-suggest` | [cs-ai-suggest.md`cs-ai-suggest.md` |

---

## 十、营销自动化域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 营销流程编排 | ✅ | `service/sop.go` · `service/sop_loader.go` · `service/sop_abtest.go` | `GET/POST /api/marketing-flow` | [marketing-flow.md`marketing-flow.md` |
| A/B 测试 | ✅ | `service/sop_abtest.go` | `GET/POST /api/ab-test` | [ab-test.md`ab-test.md` |
| 用户分层 RFM | ✅ | `service/segment.go` · `model/customer_rfm.go` · `model/rfm_rule.go` · `dto/customer_rfm.go` | `GET/POST /api/rfm/segment` | [rfm-segment.md`rfm-segment.md` |
| 流失预警 | ✅ | `service/customer.go`（流失预测） | `GET /api/churn/prediction` | [churn-prediction.md`churn-prediction.md` |
| 流失挽回队列 | ✅ | `controller/recovery_queue.go` · `service/recovery_queue.go` · `dto/recovery_queue.go` | `GET/POST /api/recovery-queue` | [recovery-queue.md`recovery-queue.md` |
| 自定义报表 | ✅ | `controller/custom_report.go` · `service/custom_report.go` | `GET/POST /api/custom-reports` | [custom-report.md`custom-report.md` |
| 数据大屏 | ✅ | `controller/dashboard.go` · `service/sse_hub.go` | `GET /api/dashboard/sse` | [dashboard.md`dashboard.md` |
| 批量操作 | ✅ | `controller/batch.go` · `service/batch.go` | `POST /api/batch/operation` | [batch-operation.md`batch-operation.md` |

---

## 十一、内容创作域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| AI 内容创作 | ✅ | `content/controller/` · `content/service/` | `POST /api/content/ai-create` | [ai-content.md`ai-content.md` |
| 模板市场 | ✅ | `content/controller/` · `content/service/` | `GET /api/template-market` | [template-market.md`template-market.md` |
| 素材管理 | ✅ | `content/controller/` · `content/service/` | `GET/POST /api/material` | [material-management.md`material-management.md` |
| 文件上传 | ✅ | `controller/upload.go` · `service/upload.go` | `POST /api/upload` | [file-upload.md`file-upload.md` |

---

## 十二、系统管理域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 系统配置 | ✅ | `controller/system_config.go` · `service/system_config.go` | `GET/POST /api/system/config` | [system-config.md`system-config.md` |
| 系统运维 | ✅ | `ops/controller/` · `ops/service/` · `ops/repository/` | `GET /api/ops/*` | [system-ops.md`system-ops.md` |
| OBS 对象存储配置 | ✅ | `controller/obs_config.go` · `service/obs_config.go` · `dto/obs_config.go` | `GET/POST /api/obs/config` | [obs-config.md`obs-config.md` |
| 备份恢复 | ✅ | `controller/backup.go` · `service/backup.go` · `repository/backup.go` · `model/backup.go` | `GET/POST /api/backup/*` | [backup-recovery.md`backup-recovery.md` |
| 操作日志（Event Bus 订阅） | ✅ | `event/subscribers.go`（OperationLogSubscriber） | `GET /api/operation-logs` | [operation-log.md`operation-log.md` |
| 安全审计 | ✅ | `middleware/audit.go`（通过 `auditLogChan` 异步落库到 `operation_logs` 表，**不经过 Event Bus**） | `GET /api/audit-logs` | [security-audit.md`security-audit.md` |
| 全链路追踪驾驶舱 | ✅ | `controller/trace.go` · `aiagent/llm/trace_bus.go` | `GET /api/trace/dashboard` | [trace-dashboard.md`trace-dashboard.md` |
| SSE 实时驾驶舱 | ✅ | `service/sse_hub.go` · `controller/sse.go` | `GET /api/dashboard/sse` | [sse-dashboard.md`sse-dashboard.md` |
| LLM Provider 降级管理 | ✅ | `aiagent/llm/failover.go` · `controller/llm_provider.go` | `GET/POST /api/llm-providers/*` | [llm-provider.md`llm-provider.md` |
| 置信度/拟人度/反馈学习面板 | ✅ | `controller/tuning.go` · `service/tuning.go` · `service/confidence/` · `service/humanize/` · `service/feedback_loop/` | `GET/POST /api/tuning/*` | [tuning-panel.md`tuning-panel.md` |
| 异常登录检测 | ✅ | `service/login_risk.go` · `middleware/brute_force.go` | `GET /api/anomaly/login` | [anomaly-login-detector.md`anomaly-login-detector.md` |

---

## 十三、安全与权限域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 权限系统（角色/菜单/按钮级） | ✅ | `controller/role.go` · `service/role.go` · `service/permission.go` · `middleware/permission.go` | `GET/POST /api/system/roles` / `GET/POST /api/system/permissions/*` | [permission-system.md`permission-system.md` |
| 行级数据权限（data_scope 中间件） | ✅ | `middleware/data_scope.go` | （中间件自动注入） | [row-level-security.md`row-level-security.md` |

---

## 十四、第三方对接域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 集成账号管理 | ✅ | `controller/integration.go` · `service/integration.go` · `model/integration.go` | `GET/POST /api/integration/accounts` | [integration-account.md`integration-account.md` |
| 同步日志 | ✅ | `controller/sync_log.go` · `service/sync_log.go` | `GET /api/sync-logs` | [sync-log.md`sync-log.md` |

---

## 十五、统一消息域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 统一消息 | ✅ | `service/message.go` · `model/message.go` · `repository/message.go` | `GET/POST /api/messages` | [unified-message.md`unified-message.md` |
| 统一收件箱 | ✅ | `controller/inbox.go` · `service/inbox.go` | `GET /api/inbox` | [unified-inbox.md`unified-inbox.md` |
| 消息中心（7 大渠道接入总览） | ✅ | `service/message_hub.go` | `GET /api/message-hub` | [message-hub.md`message-hub.md` |
| 平台账号管理 | ✅ | `controller/account.go` · `service/account.go` · `repository/account.go` · `model/account.go` | `GET/POST /api/platform-accounts` | [platform-account.md`platform-account.md` |

---

## 十六、AI 销冠核心域

> 📌 **AI 销冠引擎核心模块**：围绕 `service.SalesEngine.Handle()` 主流程（感知 → 决策 → 行动 → 记忆）构建。
> 详见 [ARCHITECTURE.md §3.1](./ARCHITECTURE.md) AI Agent 子系统。

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 对话记忆中心（短期/长期/RAG） | ✅ | `service/memory.go` · `model/memory.go` · `dto/memory.go` | `GET/POST /api/memory/*` | [dialogue-memory.md`dialogue-memory.md` |
| 意图识别中心（12 意图分类） | ✅ | `controller/intent.go` · `service/intent.go` · `model/intent_log.go` · `dto/intent.go` | `POST /api/intent/recognize` | [intent-recognition.md`intent-recognition.md` |
| SOP 智能体（DAG 流转） | ✅ | `controller/sop.go` · `service/sop.go` · `service/sop_loader.go` · `model/sop_executor.go` | `GET/POST /api/sop/*` | [sop-agent.md`sop-agent.md` |
| LLM 多模型路由（6 厂商/8 场景） | ✅ | `aiagent/llm/dispatcher.go` · `aiagent/llm/failover.go` · `aiagent/llm/trace_bus.go` | （内部调用，由 SalesEngine 触发） | [llm-routing.md`llm-routing.md` |
| 异议处理 | ✅ | `service/objection_handler.go`（SalesEngine 子流程） | （内部调用） | [objection-handler.md`objection-handler.md` |
| 销冠画像独立 UI | ✅ | `service/persona.go` · `repository/persona.go` | `GET/POST /api/persona` | [sales-persona.md`sales-persona.md` |
| 触达 Pipeline 框架（9 步执行） | ✅ | `controller/reach_pipeline.go` · `service/reach_pipeline.go` · `service/reach_send_pipeline.go` · `repository/reach_pipeline.go` | `GET/POST /api/reach-pipeline/*` | [reach-pipeline.md`reach-pipeline.md` |

---

## 十七、多 AI 智能体域

> 📌 **多 AI 智能体架构**：一个商户可配置多个独立智能体（销售型/客服型/混合型），每个智能体可绑定不同 LLM、SOP、知识库。

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 多 AI 智能体管理（CRUD/测试/上下文加载） | ✅ | `controller/ai_agent.go` · `service/ai_agent.go` · `repository/ai_agent.go` · `model/ai_agent.go` | `GET/POST/PUT/DELETE /api/ai-agents` / `POST /api/ai-agents/:id/test` / `GET /api/ai-agents/:id/context` | [ai-agent.md`ai-agent.md` |
| 渠道账号绑定智能体 | ✅ | `controller/channel_agent_binding.go` · `service/channel_agent_binding.go` | `GET/POST /api/channel-agent-bindings` | [channel-agent-binding.md`channel-agent-binding.md` |
| 客服座席挂载智能体 | ✅ | `controller/customer_service_agent.go` · `service/customer_service_agent.go` | `GET/POST /api/cs-agent-mount` | [cs-agent-mount.md`cs-agent-mount.md` |

---

## 十八、数据分析域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 客户旅程大屏（9 阶段监控） | ✅ | `controller/customer_journey.go` · `service/customer_journey.go` | `GET /api/customer-journey` | [customer-journey.md`customer-journey.md` |
| 转化漏斗 | ✅ | `controller/conversion_funnel.go` · `service/conversion_funnel.go` | `GET /api/conversion-funnel` | [conversion-funnel.md`conversion-funnel.md` |
| 智能体产能 | ✅ | `controller/ai_productivity.go` · `service/ai_productivity.go` | `GET /api/ai-productivity` | [ai-productivity.md`ai-productivity.md` |

---

## 十九、客服 Web Widget 域

| 功能名称 | 状态 | 所在包 | API 路由 | 关联文档（计划文件名，尚未编写） |
| --- | --- | --- | --- | --- |
| 客服 Web Widget 渠道管理 | ✅ | `controller/chat_channel.go` · `service/chat_channel.go` · `model/chat_channel.go` | `GET/POST /api/chat-channels` / `POST /api/chat/public/sessions/:session_id/messages` | [chat-channel.md`chat-channel.md` |

---

## 二十、AI Agent 工具注册表

`internal/aiagent/agent/tooluse/` 下注册 41 个原子工具，供 Agent Loop (ReAct) 调用。详见 [../../docs/architecture/agent-tools-inventory.md](../AGENT_TOOLS.md)。

| 工具类别 | 数量 | 工具列表（节选） |
| --- | --- | --- |
| 触达工具（reach） | 20 | 短信发送 / 邮件发送 / 企微消息 / WhatsApp 发送 / Telegram 发送 / 短链生成 / 活码生成 / 卡片生成 / ... |
| 项目管理（pm） | 3 | 项目查询 / 项目创建 / 项目更新 |
| 客户工具（customer） | 8 | 客户查询 / 客户创建 / 客户标签 / 客户分层 / 客户 360 / OneID 合并 / 客户旅程 / RFM 评分 |
| 知识库（knowledge） | 4 | 知识库检索 / 文档导入 / 文档分段 / 向量化 |
| 业务工具（business） | 6 | 订单查询 / 售后处理 / 投诉处理 / 异议处理 / SOP 触发 / 通知发送 |

工具注册入口: `router.go` 的 `registerAllAgentTools(db)`，由 `initGlobalToolExecutor()` 装配全局 `ToolExecutor`（含限流 / 重试 / 审计 / 计费装饰器链）。

---

## 二十一、平台对接能力

平台端降级为可选本地组件：`PLATFORM_ENABLED` 未开启时不加载平台配置、不注册商户、不心跳、不拉市场，本地功能与资产运行不受影响。OTA / 版本下载 / 定价 / 注册开户一律不在本版。

| 能力 | 状态 | 所在包 | 说明 |
| --- | --- | --- | --- |
| 心跳上报（3 分钟间隔 + 9 分钟容错） | 仅 `PLATFORM_ENABLED=true` | `platform/heartbeat_sender.go` · `middleware/install_status.go` | 采集设备指纹 / 主机信息 / 运行指标，IP 由平台侧采集；关态不启动 |
| 安装信息回传 | 仅 `PLATFORM_ENABLED=true` | `platform/sync.go` · `platform/client.go` | 由 `main.go` 的开关分支内的 `platform.InitSync()` 装配；install.lock 持久化，install_id 上报 |
| 资产市场拉取 | 仅 `PLATFORM_ENABLED=true` | `platform/asset_market_client.go` · `platform/asset_market_adapter.go` | 拉取平台端上架的资产，落本地 `local_asset` 表并同步版本日志；关态由 `disabledClient{}` 顶替（读面恒空集合/空对象 + nil error，写面返回 `ErrPlatformNotConfigured`），HTTP 侧写操作由 `rejectPlatformDisabled` 挡为 403 |
| 平台配置加载 | 仅 `PLATFORM_ENABLED=true` | `platformconfig.LoadPlatform("config/platform.yaml")` | api_url / secret / admin；关态早退并把 `PlatformCfg` 置 nil |
| 平台端地址解析 | 仅 `PLATFORM_ENABLED=true` | `config/platform.go: PlatformURL()` | 关态恒返回空串；开启态取 `PlatformCfg.APIURL` → `PLATFORM_API_HOST` env → `PLATFORM_API_URL` env → 空串。**刻意没有任何默认域名**，空串即"没有平台可连"，下游按 `PlatformCfg == nil` 快速失败 |

---

## 二十二、Webhook 入站能力

路由全部注册在 `internal/controller/webhook.go: RegisterRoutes`（`/api/webhook` 组），入站处理在
`internal/service/webhook.go: WebhookService.Receive` 这条漏斗上。**本表 2026-09-20 按代码逐项核实重写**：
旧版把 `service/kuaishou.go`、`service/xiaohongshu.go`、`service/xianyu.go`、`service/tiktok.go`、
`service/douyin.go` 写成 ✅，这五个文件从未存在（`internal/service/` 下无同名文件），
且路由形态也不是 `/api/webhook/<渠道>/:id` 而是通用通配 `/:channel/:account_id`。

| 渠道 | 入站路由 | 实际处理位置 | 状态 |
| --- | --- | --- | --- |
| 企业微信 | `POST /api/webhook/wecom/:account_id` · `GET /api/webhook/wecom/:account_id`（URL 验证） | `service/webhook_channel_wecom.go`（`dispatchWeCom`），加解密在 `service/wecom.go` | ✅ |
| 飞书 | `POST /api/webhook/feishu/:account_id` · `GET /api/webhook/feishu/:account_id` | `service/webhook_channel_feishu.go` | ✅ |
| 钉钉 | `POST /api/webhook/dingtalk/:account_id` · `GET /api/webhook/dingtalk/:account_id` | **专用路由**，走 `service/dingtalk_app.go`（不进 `Receive` 漏斗）；出站客户端在 `service/dingtalk.go` | ✅ |
| 微信公众号 | `POST /api/webhook/wechat/:account_id` · `GET /api/webhook/wechat/:account_id` | `controller/wechat.go`（`router/platform_routes.go: setupWechatWebhookRoutes` 注册），自行验签后直接进 Ingress | ✅ |
| WhatsApp Cloud | `POST /api/webhook/whatsapp/:account_id` · `GET /api/webhook/whatsapp/:account_id`（Meta 挑战） | `service/webhook_channel_whatsapp.go`，出站客户端在 `service/whatsapp.go` | ✅ |
| Telegram | `POST /api/webhook/telegram/:account_id` | `service/webhook_channel_telegram.go` + `channelbot/telegram/` | ✅ |
| QQ 官方机器人 | `POST /api/webhook/qq/:account_id` | `service/webhook_channel_qq.go` + `channelbot/qq/` | ✅ |
| 抖音 | `POST /api/webhook/douyin/:account_id` | `service/webhook_channel_douyin.go`；入站验签为**本地策略**（官方 dop 被签串未取到原文） | ✅ 代码在位，无凭据未真机验证 |
| TikTok | `POST /api/webhook/tiktok/:account_id` | 与抖音共用解析、按渠道分平台落库；验签按官方 `TikTok-Signature: t=,s=` | ✅ 代码在位，无凭据未真机验证 |
| 快手 / 小红书 / 闲鱼 / custom | 通配路由**存在但固定 400** | `Receive` 的 `webhookInboundCapable` 能力闸：这三家+custom 无入站适配器，拒收并在响应里指路，绝不"收下回 200"（审计 D-03） | ✘ 无渠道 API，真实链路是 `POST /api/bridge/ingest` 浏览器桥 |
| 邮件追踪 | `GET /api/email/track/open/:id` · `GET /api/email/track/click/:id` | `internal/email/service/` | ✅ |

`GET /api/webhook/stats` 与 `GET /api/webhook/health` 是运维端点，不属于渠道入站。

---

## 二十三、健康检查与监控

| 端点 | 用途 | 状态 | 所在包 |
| --- | --- | --- | --- |
| `GET /health` | 全量健康检查（PG / Redis / LLM / Embedding / Rerank） | ✅ | `router/health.go` |
| `GET /healthz` | 存活探针（K8s liveness） | ✅ | `router/health.go` |
| `GET /readyz` | 就绪探针（K8s readiness） | ✅ | `router/health.go` |

> ℹ️ **Swagger 当前未注册**：`router.Setup()` 未挂载任何 `gin-swagger` 路由，故无 `/swagger/*` 端点。如需开启，请在 `router.go` 中自行添加 `gin-swagger` 中间件并生成 swagger doc。

> 私域部署: 无 `/metrics` 外部监控端点。关键指标 (LLM 调用 / RAG 检索 / Fallback 触发) 落库 `layer_decision_logs` / `audit_logs`，通过 SQL + `scripts/post_deploy_check.sh` 巡检。

---

## 二十四、开源版已下线功能

以下功能已从开源版移除，不在本清单内：

- ❌ OTA 自动更新
- ❌ 商户授权 / 授权码校验（本版无授权概念，无授权也可构建与运行资产）
- ❌ 版本下载
- ❌ 定价方案
- ❌ 注册开户
- ❌ 多租户隔离（已改为单租户私域部署，`merchant_id` 字段可空）

---

## 二十五、相关文档导航

| 主题 | 文档路径 |
| --- | --- |
| 架构图（模块 / 时序 / 子系统） | [./ARCHITECTURE.md](./ARCHITECTURE.md) |
| 代码开发手册（环境 / 启动 / 调试 / 部署） | [./DEVELOPMENT.md](./DEVELOPMENT.md) |
| 代码规范（分层约束 / 命名 / 错误处理 / 日志） | [./CONVENTIONS.md](./CONVENTIONS.md) |
| 营销功能模块索引（94+ 子模块详细文档） | [../../docs/marketing-features/README.md](../../../docs/marketing-features/README.md) |
| AI Agent 41 工具注册表 | [../../docs/architecture/agent-tools-inventory.md](../AGENT_TOOLS.md) |
| 系统级 C4 / Container / Deployment | [../../docs/architecture/ARCHITECTURE_DIAGRAM.md](../../../docs/architecture/ARCHITECTURE_DIAGRAM.md) |
| 用户/角色/授权三模块 | [../../docs/architecture/USER_SYSTEM.md](../../../docs/architecture/USER_SYSTEM.md) |
| 菜单与权限设计 | [../../docs/architecture/MENU_PERMISSION_PLAN.md](../../../docs/architecture/MENU_PERMISSION_PLAN.md) |
| 平台端 10 个功能模块（独立仓库） | `hivemtk-platform/docs/platform-features/` |
| 工程级 README | [../README.md](../../README.md) |

---

最近更新日期: 2026-07-26
