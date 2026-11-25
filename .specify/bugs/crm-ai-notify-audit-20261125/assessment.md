# Assessment: CRM-13/14/15/16/17/18/26（20261125 批次）

- **Slug**: crm-ai-notify-audit-20261125
- **Created**: 2026-11-25
- **Source**: 工作区外层 `../docs/未完成任务清单.md` §3.29 各任务行原文判据（CRM-20261125-13/14/15/16/17/18/26）
- **Verdict**: valid。七项均为原文判据成立的具体代码缺口；本轮实施的是**服务层可核验证据/草稿/送达/审计/报告契约 + 限定测试**，不宣称整项关闭（原文判据中的真实渠道回执、UI、路由、真机点验仍保留开放）。
- **Severity**: high（AI 结论可追溯性、外发通知合规与业务审计）

## 逐项缺口（施工前快照，行号为 2026-11-25 工作树快照，函数名复核后施工）

### CRM-13 AI 分析使用可核验证据并校准置信度

- 判据要点：每条结论绑定可回查证据（时间/来源/引用）；无引用/伪造/越权引用不得借"全部 facts"兜底；证据不足/模型失败显式降级；置信度区分模型估计与规则分数。
- 缺口：`service/customer_ai.go` 的 `resolveEvidence(all, nil)` 对 profile/segment 回退全部 customer facts；`AIEvidence` 无时间/来源字段；`Confidence` profile/segment 固定 0.8；模型与规则分数共用一个 float，无来源标记；未接入按权限检索的会话/跟进/线索证据。
- 判定：valid。原文"空 evidence_refs 回退全部 facts、固定 0.8"与源码逐字对应。

### CRM-14 AI 建议以可确认草稿接入既有商机/跟进

- 判据要点：持久化带证据/版本的建议草稿，确认/编辑/拒绝；确认重验权限与对象版本；幂等调用既有商机或跟进接口；默认分析不改客户不外发；拒绝不写业务、重复确认不重复建单、落地失败可见且审计可追溯。
- 缺口：`CustomerAIService.Analyze` 仅返回即时建议；`AIInsightDraftService` 已有草稿/确认骨架，但没有把 CRM 画像建议作为草稿生成入口，也没有确认时权限/版本 CAS、幂等建商机/跟进与审计写失败的显式返回。
- 判定：valid。原文"未定位从该建议确认/拒绝并写入业务的闭环"成立。

### CRM-15 线索通知按事件/收件人追加送达记录

- 判据要点：按 tenant/clue/event/recipient/channel 追加业务记录并关联规则版本；路由行只存配置；无收件人/无目标/部分成功/失败状态明确；负责人 fallback、连续两条线索、多收件人、路由删除后历史回查。
- 缺口：`service/lead_notify.go:120–124` 把 `delivery_records` 数组覆盖写到路由行；无收件人直接 `return nil`；fallback 使用虚拟 routeID；无按事件/收件人的追加记录。
- 判定：valid。

### CRM-16 线索通知建立耐久幂等和失败补偿

- 判据要点：复用 outbox/任务持久化投递意图，按事件/收件人/渠道幂等；失败退避、人工重试/终止；重启、多 worker、重复事件不重复投递；回执丢失单独标记待核实；通知失败不回滚线索。
- 缺口：`service/lead_mining.go` 通知失败主要记 Warn，逐线索投递链无耐久任务/幂等键；进程内去重不构成持久保证。
- 判定：valid。

### CRM-17 外部线索通知补脱敏和收件人授权检查

- 判据要点：发送前按渠道与收件人权限生成最小摘要，优先站内需登录再鉴权的详情引用；外部目标/负责人失效拒发留原因；记录脱敏策略；手机号/邮箱/聊天敏感片段屏蔽；撤权与跨租户目标不外发。
- 缺口：`lead_notify.go:87` 直接把 Summary 拼入内容；未对 TG/飞书收件人做可见客户范围复核；脱敏函数存在于 `lead_mining_notify_external.go` 但未在 LeadNotify 出站链调用。原文明确这是静态保障缺口，不宣称已发生外泄。
- 判定：valid。

### CRM-18 CRM 业务变更审计补字段差异与失败可见性

- 判据要点：复用 OperationLog，记录真实对象/来源/可信 actor/字段前后差异；敏感值脱敏；审计写失败显式处理；创建/修改/删除/导入/导出/下载/合并/撤销/AI 确认关联业务事件；成功、拒绝、业务回滚、审计故障分别断言。
- 缺口：`service/operation_log_domain.go` 只有日志 CRUD 门面，没有统一业务审计记录器；`customer_import_export.go:275–284` 的 `writeAudit` 忽略写失败且 Detail 无结构化前后差异/版本/来源/actor；无脱敏策略。原文同时说明全局 audit 与部分导出审计已存在，不能误报"完全没有审计"。
- 判定：valid。

### CRM-26 画像报告按账号、行业和阶段受权导出

- 判据要点：复用 13 结构化画像/证据，明确报告字段、生成时间/版本与阶段口径，按授权账号/行业/阶段筛选导出可回查引用，复用 11/12 文件格式/脱敏/受限下载；无引用结论不伪装已证实。
- 缺口：`service/customer_ai.go` 只有单客户即时建议；`service/report.go` 仅 LLMUsage 聚合；无画像报告结果存储、按账号/行业/阶段筛选、报告文件格式与受限下载契约。
- 判定：valid。

## Scope / 边界

- 允许修改：`user-server/internal/service/customer_ai*.go`、`customer_notification*.go`、`lead_notify*.go`、`operation_log*.go`、`report*.go`（仅画像报告新增部分），对应新建 `_test.go`；新建本规格目录文件；必要时仅接线 `user-web/src/views/crm/` 对应页面。
- 严禁修改：`customer_permission.go`、`crm_customer.go`、`company.go`、`customer_import*.go`、`customer_dedupe*.go`、`crm_merge_snapshot.go`、`followup_record.go`、`auto_reply.go`、`lead_discovery.go`、`lead_mining.go`、`lead_react*.go`、`channel_*.go`、`outbound_*.go`、`webhook_*.go`、`router/*`、任何已存在 `.specify/bugs` 目录。
- 不改数据库 schema；不 commit/push/发布/外发/UI 真机点验。
- 架构：五层（Router→Controller→Service→Repository→Model）；Service 不碰 gorm/db、Repository 首参 ctx、新文件命名遵守禁用后缀规则、禁裸 `go func`。
- 验证：标准库 `testing`；真实 PG 用 `testutil.NewTestDB`（`POSTGRES_TEST_*` 取自 `../.env`，端口 8217）；纯逻辑用纯函数；RED 先行；限定 `go test`，`-p 1 -parallel 1`；不跑全量 service。

## 本轮交付口径与预期余项

- 只在允许文件中新增服务层纯函数/编排与内存/现有 KV 可承载的持久契约；不新增迁移/表/路由装配。
- CRM-13 授权检索依赖现有 `CustomerPermissionService.GetByIDScoped`；不重开权限服务。
- CRM-14 复用既有 `AIInsightDraftService` 草稿模型与 `FollowUpService`/商机能力，不建平行商机/提醒模型；缺路由装配时只交付服务层闭环。
- CRM-15/16 的送达"业务记录"落在新服务内的可注入仓储接口，默认实现使用现有 `system_config_kv` 租户 KV（不新增 schema），并明确多进程原子性/真实渠道回执的余项。
- CRM-17 不为本机验收向真实联系人发消息；只验证脱敏/拒发/授权纯函数与服务层决策。
- CRM-18 记录器可注入 OperationLogRepository，写失败向上返回；不改既有 Controller 调用点。
- CRM-26 画像报告复用 CRM-13 结果与 CRM-11/12 的 CSV/xlsx + 受限下载思路，服务层生成/筛选/下载校验；UI/路由余人保留。

## 已知不可在本轮关闭的原文判据（即使限定测试 GREEN）

- 真实 TG/飞书送达/回执对账、真实渠道多 worker 并发与进程重启后的 KV/任务恢复（无新 schema/消费者接线）。
- Controller/Router 的真实 HTTP 入口、Profile360.vue 的真机交互与完整 UI 呈现。
- SC-001 百条人工金标、LLM 真实调用准确率、生产环境审计/通知对账。
- 任何跨边界文件的缺陷（并行 agent 在途产物）只记录、不越界修改。
