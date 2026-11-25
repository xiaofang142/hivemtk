# Bug Assessment: 多实体去重 / 合并快照 / 人工撤销合并

- **Slug**: crm-dedupe-multientity-20261125
- **Created**: 2026-11-25
- **Source**: 外层 `docs/未完成任务清单.md` §3.29 `CRM-20261125-06 / 07 / 08`
- **Verdict**: valid（三项均为真实代码缺口；本批只交付判据可成立的最小子集，不关闭任务行）
- **Severity**: high（07/08 为成功合并后数据可恢复性缺口）

## Report

### CRM-06 多实体去重
- `service/customer_dedupe.go:FindDuplicates` 只遍历 `model.Customer`（`customers.List` / scoped `ListScoped`），`DuplicateCandidate` 只有 `LeftID/RightID` 客户字段；`service/crm_customer.go` 虽有 Contact/Supplier/ServiceProvider CRUD，但没有实体→标识字段的适配，也没有实体候选输出。
- 三类实体把身份字段放在 `ContactInfo`（JSONB），客户侧规则键是 phone/email/unionid 等；实体侧需从 `ContactInfo` 适配出同类键，并保留客户范围（`ChannelAccountID` / 所属 `CustomerID` 的可见性）与规则不支持实体的显式标记。
- 多人共用手机号且分别承担客户/供应商/服务商角色时，按单一强标识去重会错误抹去业务角色；候选必须标注 `role_conflict`（或类似）并以 review 动作呈现，不自动合并。

### CRM-07 合并快照
- `Merge` 仅在审计 `OriginalValues` 里保存次客户基础字段；`service/customer.go:MergeCustomers` 实际会移动 events/session/DNC/channels/csat/clues/exposure 并连通许可身份，然后软删次客户。现有快照不足以恢复。
- `Merge` 把 `svc.MergeCustomers` 和 `mergeRepo.Create` 包在同一个 `customers.WithTransaction` 里，故可在此事务内写完整快照；快照字段（主次对象、关联归属、字段前后值、规则版本、操作者、并发版本）可复用 `CustomerMerge.OriginalValues`（JSONB）与 `RuleName/OperatorName/OperatorID`，无需新增列、不改 schema。
- 存量记录 `OriginalValues` 不含 `snapshot_version/relations` 等结构，应显式标 `recoverable=false, reason=历史快照不完整`，不能伪造。

### CRM-08 人工撤销合并
- 路由仅 `dedupe/scan / merge / merge-audit`，未定位 unmerge 入口；07 快照是撤销前提。
- 撤销必须校验：合并后是否有新写入/链式合并/版本冲突（通过快照里的 `source_one_id/target_one_id` 当前客户状态与合并记录顺序判断），冲突时显式拒绝并给人工线索；成功后按快照事务逆操作；重复撤销幂等；拒收/撤销同意/营销许可不得因拆回放宽（只恢复原归属，不删除/改写事实行，不删除同意连通边，避免把原身份的 revoke 放宽）。

## Symptom / Reproduction

- CRM-06：构造两个 Supplier（相同手机号于 ContactInfo，不同客户）+ 同名 Supplier，调用 `FindDuplicates` 期望返回实体候选（含 `entity_type`、命中字段/规则、越权候选排除、角色并存不自动去重、不支持实体显式标记）；当前 API 无此字段/行为。
- CRM-07：真实 PG 合并两个客户后读 `CustomerMerge.OriginalValues`，当前缺少可恢复结构；注入合并事务故障应回滚审计记录（现状已同事务，但快照不存在）。
- CRM-08：合并成功后调用撤销（当前无入口）应恢复次客户、字段/关联归属与许可不扩大；有新写入/链式合并/版本不匹配时拒绝并给线索；重复撤销幂等。

## Scope

- 允许修改：`user-server/internal/service/customer_dedupe.go`、`user-server/internal/service/crm_dedupe*.go` / merge service、`user-server/internal/model/customer_merge*.go`、`user-web/src/views/crm/Dedupe.vue`（仅 UI 接线）、新建对应 `_test.go`、本规格目录三份 md。
- 禁止修改：`customer_permission.go`、`crm_customer.go`、`company.go`、`customer_import_job.go`、`followup_record.go`、`auto_reply.go`、`lead_*.go`、`channel_*.go`、`outbound_*.go`、`webhook_*.go`、`router/*`、任何既有 `.specify/bugs` 目录。
- 不改 schema：快照全部落在 `customer_merges.original_values`（JSONB）及既有列 `rule_name/operator_name/operator_id`；撤销记录复用 `customer_merges` 的 `action=undo`。不写迁移。
- 路由侧限制：用户明确禁止改 `router/*`，因此 CRM-08 的 Controller/HTTP 入口不在本批实现；Service 层提供 `UndoMerge/PreviewUndo` 供后续授权接线，UI 只做「展示快照影响 + 标记需要人工撤销入口」的最小接线，不伪造已连通接口。
- 依赖：01–03 的数据权限语义（复用 `CustomerPermissionService.CanAccess/ListScoped`），不改权限实现。

## Verification Plan

RED（新测试对当前实现失败）→ 最小实现 → GREEN。真实 PG 用 `testutil.NewTestDB` + 显式模型列表；限定 `go test -p 1 -parallel 1`，POSTGRES_TEST_HOST/PORT/PASSWORD 由环境注入。跑 gofmt + go build ./... + go vet ./... + 限定测试；不跑全量 service 回归（并行会话 Bridge/Webhook 基线失败干扰），不做 UI 真机点验、不 commit/push/发布。

## Valid / Invalid 判定

三项判据均可由代码缺口支撑为 valid；但受「不改 router/*、不改 schema、不改其他 agent 在途文件」边界限制，本批实现范围有限，判据中需要路由入口/真实接口联调/UI 点验的部分不宣称完成，任务行保留开放。
