# Bug Assessment: FA-15 同意查询故障拒发

- **Slug**: consent-query-fail-closed（自动生成，目录此前不存在）
- **Created**: 2026-11-20
- **Source**: 外层 `docs/未完成任务清单.md` FA-15
- **Verdict**: valid
- **Severity**: high

## Report

同意事实查询失败被当作 missing，偏好查询失败被忽略；默认 revocation_only 可能在无法确认撤回或暂停时继续发送。故障必须独立于正常 missing/granted，保留拒发原因，恢复后按真实事实判定。

## Symptom

`LatestEvent` 报错时 `CheckSendConsent` 返回 missing；有 grant 而 `GetPreference` 报错时返回 granted。前者被默认策略放行，后者两种策略均放行。

## Reproduction

1. 在专用 Postgres 测试库创建同意事件与偏好表，记录真实 grant/revoke/暂停事实。
2. 通过该独立测试连接的 GORM 查询 callback，分别仅对事件表或偏好表返回 `context.DeadlineExceeded`，执行真实 service 和发送管线。
3. 断言故障状态、原因码、permission step 和 audit；主渠道与回退适配器调用次数必须为零。关闭注入后重发，按 grant/revoke/paused 和无历史记录策略判定。

评估阶段仅以源码坐实根因，尚未执行动态复现；fix 阶段先新增回归用例并记录红灯输出。

## Suspected Code Paths

- `user-server/internal/service/contact_consent.go:160-192`：事件错误回 missing、偏好错误漏处理。
- `user-server/internal/repository/contact_consent_repo.go:37-40,91-94`：未初始化 DB 同样返回 nil/nil，伪装正常无记录。
- `user-server/internal/service/reach_send_pipeline_steps.go:137-153,223-278`：permission checker 未装配时提前返回；consent switch 默认分支将未知状态当作 granted。
- `user-server/internal/app/reach_tool_wiring.go:44-48`：生产配置使用 revocation_only，说明正常 missing 放行是需保留的存量策略。

## Root Cause Hypothesis

置信度 high。业务状态与基础设施故障混在同一状态；管线又将非已知拒绝状态全部当作允许。底层未初始化也返回正常空结果。仅改为 enforce 会误伤正常存量且无法解决偏好错误，伪造 revoked 则会污染授权语义。

## Proposed Remediation

**Preferred**：增加 `unavailable` 状态及事件/偏好/仓储不可用原因码，Trusted=false；仓储读操作未初始化须返回错误。事件/偏好查询失败均返回 unavailable。管线仅显式 granted 可成功、missing 保持既有两种模式口径，unavailable/未知状态一律终止并留故障原因。permission checker 未装配不能跳过已装配的 consent。沿用既有 SendResponse 和 audit 结构，无新增配置或依赖，不自动改写授权事实。

**Alternatives**：统一收紧为 enforce 会改变存量策略；把故障视为 revoked 会谎报客户决策，均不采用。

**Files likely to change**:

- `user-server/internal/service/contact_consent.go`
- `user-server/internal/service/reach_send_pipeline.go`
- `user-server/internal/service/reach_send_pipeline_steps.go`
- `user-server/internal/repository/contact_consent_repo.go`
- `user-server/internal/service/contact_consent_failure_test.go`（新增）
- `user-server/internal/repository/contact_consent_failure_test.go`（新增）

**Tests to add or update**:

- 事件查询错误、偏好查询错误、空仓储和未初始化 DB 均不是正常 missing/granted。
- revocation_only/enforce × 事件/偏好故障 × 渠道分支：所有主/回退适配器零调用，permission/audit 保留来源原因。
- 故障恢复后 grant 正常发送，revoke/暂停继续阻断；无历史存量仍按原模式放行或拒发。
- checker 的未知/空状态和 nil permission checker 不能使故障绕过闸门。

## Risks & Considerations

- DB 故障将导致暂时拒发，这是预期行为；故障恢复后可重新提交并重新读取事实，不在渠道重试环节绕过校验。
- 不向对外回执或日志输出原始数据库错误、SQL 或凭据，仅保留稳定的查询来源原因码。
- 保留其他会话的 workflow/client_device 改动；不改共享配置、数据库结构或业务事实。
- 本轮采用受控查询故障和适配器模拟，不能据此宣称第三方真实送达。

## Open Questions

无；用户已授权逐项研发、测试和交付，外部依赖通过模拟信号验收。
