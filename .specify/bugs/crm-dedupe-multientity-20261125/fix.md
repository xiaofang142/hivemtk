# Fix: 多实体去重 / 合并快照 / 人工撤销合并

- **Slug**: crm-dedupe-multientity-20261125
- **Date**: 2026-11-25
- **范围**: CRM-20261125-06 / 07 / 08 判据的最小可用子集

## 已实施

### CRM-06 多实体去重（`user-server/internal/service/customer_dedupe.go`）
- 新增 `DuplicateCandidate` 的 `entity_type / field / unsupported` 字段；`DedupeEntityScanOptions`、`FindEntitiesDuplicates`、`supportedDedupeEntities`。
- 新增实体→OneID 标识适配 `entityIdentityMap`（ContactInfo JSONB → phone/email/unionid/openid/external_id 等规则键），三类实体（Contact/Supplier/ServiceProvider）各有仓储读取路径。
- 候选只来自 `CustomerPermissionService.ListScoped` 授权范围内客户的实体；无登录身份拒绝扫描。
- 跨实体类型命中同强标识一律 `action=review` 且 Reason 写明业务角色冲突，禁止自动抹去 Supplier/ServiceProvider 角色；规则不支持的实体类型返回 `action=unsupported` 显式标记。
- 复用既有 OneID 规则集与 `NameSimilarityScore`，不改写客户合并规则。

### CRM-07 合并快照（同文件 + `user-server/internal/service/crm_merge_snapshot.go`）
- `Merge` 在事务前读取主/次对象、字段前后值、规则版本、操作者、并发版本、关联归属（事件/会话/DNC/许可连通组）生成 `MergeImpactSnapshot`，写入 `customer_merges.original_values`（JSONB）。
- 快照构建失败即拒绝合并；`svc.MergeCustomers` 与 `mergeRepo.Create` 保持同一 `WithTransaction`，故障注入证明任一步失败均整体回滚。
- 并发版本在事务内合并后回填（规避 GORM autoUpdateTime 把合并自身写入误判为新写入）。
- `Uncaptured` 显式列出未覆盖的表，不把部分快照冒充完整快照。
- `parseMergeImpactSnapshot` 对存量/旧格式记录返回 `Recoverable=false` + 明确原因，不伪造快照；不改 schema、不写迁移。

### CRM-08 人工撤销基础（`user-server/internal/service/crm_merge_snapshot.go`）
- `PreviewUndo` 按权限展示 07 快照影响与可恢复性；`UndoMerge` 做冲突检查（合并后新写入/链式合并/版本冲突）→ 事务内逆操作 → 写 `action=undo` 审计。
- 冲突显式拒绝并给人工处置线索；重复撤销通过 `undo_of` 审计幂等返回。
- 逆操作通过 `MergeUndoApplier` 接缝注入，不在此处直连 DB；许可事实行/连通边不删除，撤销不放宽拒收、撤销同意或营销许可。
- 历史无完整快照的记录 `UndoMerge` 直接返回「不可自动恢复」，不执行逆操作。

### 前端（`user-web/src/views/crm/Dedupe.vue`）
- 扫描表单新增实体范围多选（Contact/Supplier/ServiceProvider），列表新增「实体类型」「命中字段」列，实体候选禁用合并按钮并对 `unsupported` 给出人工复核提示。
- 不做真机点验；后端 HTTP 路由（`router/*` 禁止修改）与实体扫描 API 尚未接线，属余项。

## 文件清单
- `user-server/internal/service/customer_dedupe.go`（修改）
- `user-server/internal/service/crm_merge_snapshot.go`（新增）
- `user-server/internal/service/crm_dedupe_multientity_test.go`（新增）
- `user-server/internal/service/crm_merge_snapshot_test.go`（新增）
- `user-web/src/views/crm/Dedupe.vue`（修改，仅本页）
- `.specify/bugs/crm-dedupe-multientity-20261125/{assessment,fix,test}.md`（新增）

## 余项 / 未整项关闭
- 三项判据中「路由/Controller 接线、真实接口联调、UI 真机点验」部分因边界禁止未交付。
- CRM-06 未覆盖公司/其他实体的规则适配器、实体合并执行（仅预览与角色冲突提示）。
- CRM-07 快照未覆盖 csat_surveys/clues/script_exposure_logs/customer_channels 的逐行前后值（已在 `Uncaptured` 明示），需后续扩展后再宣称完整恢复。
- CRM-08 只交付 Service 层撤销基础；逆操作生产实现需后续授权注入，未接 HTTP 入口；链式合并自动拆分的深度逆操作未实现。
- 无 commit/push/发布，无 schema 变更、无迁移。
