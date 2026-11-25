# Fix: CRM-13/14/15/16/17/18/26（20261125 批次）

## 变更清单（全部落在允许边界）

### CRM-13 AI 证据与置信度
- `user-server/internal/service/customer_ai.go`：AIEvidence 增加 Source/OccurredAt/Stale；AISuggestion 增加 ConfidenceSource/RuleScore/PromptVersion/Model/DegradeReason；CustomerAIResult 增加 Model/PromptVersion/降级标记。`resolveEvidence` 取消空 refs 回退全部 facts；新增 `resolveAISuggestions`（有据结论或显式 degraded）、`aiDegradedSuggestion`、`buildCustomerAISuggestions`；`Analyze` 在调度器缺失/模型失败时返回显式降级结果而非固定 0.8 结论。
- `user-server/internal/service/customer_ai_evidence_test.go`（新建）：正反例测试（无引用/伪造/越权/失效证据降级、有据结论带来源时间、模型分数与规则分数分离）。

### CRM-14 建议草稿闭环
- `user-server/internal/service/customer_ai_draft.go`（新建）：`CRMAISuggestionDraftService` 把 CRM-13 有据结论落成带证据/版本/模型分数的草稿；`Confirm` 重验当前 scope 与对象版本、编辑留痕、幂等调用既有 FollowUpService.Schedule（不建平行提醒/商机）、审计失败显式返回；`Reject` 不写业务。
- `user-server/internal/service/customer_ai_draft_test.go`（新建）：生成/越权与版本漂移拒绝/拒绝不写业务/重复确认幂等/审计失败可见。

### CRM-15/16/17 线索通知送达/幂等/脱敏
- `user-server/internal/service/customer_notification.go`（新建）：`appendLeadNotifyDeliveryRecords`（追加、不覆盖路由历史）、`leadNotifyDeliveryStatus`（无收件人/无目标/部分/失败明确状态）、`leadNotifyIdempotencyKey` + `memoryLeadNotifyOutbox`（pending/retry/exhausted/manual retry/receipt_unknown，退避+人工重试）、`maskLeadNotifySummary`（手机/邮箱/微信片段屏蔽 + 需登录再鉴权的站内引用）、`checkLeadNotifyRecipient`（撤权/跨租户/失效负责人拒发留原因）。
- `user-server/internal/service/lead_notify_delivery_test.go`（新建）：追加历史、状态矩阵、幂等、退避与人工重试、回执丢失、重启重建、脱敏、授权负例。
- 说明：原 `lead_notify.go` 虽在允许边界，但为减少并行在途冲突未改动；上述能力以可注入服务形式交付，接入既有 NotifyOwner 链与真实渠道对账仍为余项。

### CRM-18 业务审计差异与失败可见性
- `user-server/internal/service/operation_log_business.go`（新建）：`CRMBusinessAuditService.Record` 复用 OperationLog 写真实对象/来源/actor/业务事件/字段前后差异（OldValue/NewValue），敏感字段与值形态脱敏，写失败显式返回（不再静默成功）。
- `user-server/internal/service/operation_log_business_test.go`（新建）：成功含差异/脱敏、写失败显式、拒绝也留审计、可注入脱敏策略。

### CRM-26 画像报告受权导出
- `user-server/internal/service/report.go`（仅末尾新增画像报告段）：`BuildProfileReport` 按账号/行业/阶段筛选并保留 13 证据与版本/降级状态；`ProfileReportStore` 保存筛选/权限快照，下载重验创建者/范围/跨租户/过期；`ProfileReportCSV`/`ProfileReportXLSX` 复用 11/12 文件格式与服务端脱敏；无引用结论标 degraded。
- `user-server/internal/service/report_profile_test.go`（新建）：三维度筛选、空结果、降级不伪装、受限下载四类负例、CSV/xlsx 格式与脱敏。

### 规格
- 新建 `.specify/bugs/crm-ai-notify-audit-20261125/{assessment.md,fix.md,test.md}`；未触碰任何已存在 .specify/bugs 目录。

## 未做（边界/余项）
- 未改 `customer_permission.go`/`crm_customer.go`/`company.go`/`customer_import*.go`/`followup_record.go`/`auto_reply.go`/`lead_mining*.go`/`channel_*.go`/`webhook_*.go`/`router/*`/schema；未 commit/push/发布/外发/UI 点验。
- 无 Controller/Router 接线与 Profile360.vue 前端改动：本轮交付服务层契约与限定测试，不宣称整项关闭。
