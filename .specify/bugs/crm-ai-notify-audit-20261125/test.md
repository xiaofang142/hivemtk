# Test: CRM-13/14/15/16/17/18/26（20261125 批次）

## 环境与口径
- 工作目录：`hivemtk-user/user-server`；命令均限定 service 包模式 `-run <前缀> -count=1 -p 1 -parallel 1`，未跑全量 service（并行 agent 全量回归基线大且慢）。
- 测试以标准库 testing + 纯函数/替身为主；按协调指令本批未实际连 PG（`POSTGRES_TEST_*` 从 `../.env` 取、端口 8217 的 DB 用例未运行）。
- 并行 agent 在途多次导致非本边界编译阻断（如 `channel_master_delete_test.go` 的 `GetByValidCode`/`RotateCAS` 接口漂移、`lead_mining_config_snapshot_test.go` 的 `NewLeadMiningConfigResolverWithDeps` 未定义、`channel.go`/`lead_rule.go` 临时未用 import 等）；均等待重试，未越界修改。

## 真实命令与结果

### GREEN
1. CRM-13（先 RED：undefined `resolveAISuggestions`/`Stale` 等符号，确认失败后实现）
   - `cd user-server && go test ./internal/service/ -run '^TestResolveAISuggestionsEvidence_' -count=1 -p 1 -parallel 1`
   - 结果：`ok hivemtk-user/internal/service 1.080s`（5/5 子用例：无引用降级、有据带来源时间、伪造/越权降级、规则分数分离、失效证据拒绝）。
2. CRM-18（先 RED：undefined `NewCRMBusinessAuditServiceWithRepo`/`CRMBusinessAuditInput` 后实现）
   - `go test ./internal/service/ -run '^TestCRMAudit' -count=1 -p 1 -parallel 1`
   - 结果：`ok hivemtk-user/internal/service 1.036s`（字段差异/脱敏、写失败显式、拒绝留审计）。
3. CRM-26（先 RED：undefined `BuildProfileReport`/`ProfileReportStore` 等后实现；期间修正 1 条断言：降级行仍计入报告但标 degraded）
   - `go test ./internal/service/ -run '^TestProfileReport|^TestCRMAudit|^TestResolveAISuggestionsEvidence_' -count=1 -p 1 -parallel 1`
   - 结果：`ok hivemtk-user/internal/service 0.889s`（三维度筛选与降级、受限下载四类负例、CSV/xlsx 格式与脱敏）。

### 阻断/未 GREEN（如实记录，不虚报）
4. CRM-14/15/16/17 组合命令
   - `go test ./internal/service/ -run '^TestLeadNotify|^TestCRM14_' -count=1 -p 1 -parallel 1`
   - 最后一次结果：`test_rc=1`；`--- FAIL: TestCRM14_ConfirmIdempotentAndAuditVisible ... err=草稿版本已变化，请重新读取后确认`。这是测试替身时间戳口径与确认时 CAS 校验不一致（mock 仓储写入时 UpdatedAt 未随每次 UpdateReview 推进），非业务代码写错；按硬性收口指令停止修复。其余 `TestLeadNotify*` 与 CRM-14 其他用例此前受并行 agent 编译错误阻断，未取得全绿读数。
5. gofmt/build
   - `gofmt -l <本批 10 文件>` 输出为空（gofmt_rc=0）。
   - `go build ./... > /tmp/crm_final_build.log 2>&1` 最后一次 `build_rc=0`（并行 agent 中间态曾短暂失败，等待后恢复）。

## 未覆盖判据（每项的原文余项）
- CRM-13：真实会话/跟进/线索/RAG 授权检索未接入（只覆盖客户主档证据）；真实 LLM 调用与置信度校准未验。
- CRM-14：机会转换幂等未走 OpportunityConvertService（默认写既有 FollowUpService.Schedule）；Controller/Router 与 Profile360.vue 确认/编辑/拒绝交互未接线；真实审计落库未连 PG。
- CRM-15：追加记录目前是纯函数 + 路由行历史 JSON，未接入真实 `NotifyOwner` 全链路；路由删除后历史回查依赖原路由行仍在，未做跨表持久载体。
- CRM-16：outbox 为共享 map 参考实现，未持久到现有 KV/任务表，未做真机重启/多 worker/真实渠道回执对账；"回执丢失"只标记待核实，不承诺 exactly-once。
- CRM-17：脱敏/授权是出站前决策函数，未接入真实 TG/飞书发送；未向真实联系人发消息，未做真实渠道矩阵。
- CRM-18：仅新增通用记录器，未改 `crm_customer.go`/`customer_import_export.go` 的既有调用点；同步导出/后台下载/合并/撤销等具体操作的审计接线未做。
- CRM-26：报告为服务层生成 + 内存快照，未接 Controller/Router/UI 下载；复用 11/12 文件格式但未复用其 Controller 文件访问入口。
- 所有项：未做 UI 真机点验、未提交/推送/发布/外发；并行 agent 产物造成的中间态编译失败未越界修复。
