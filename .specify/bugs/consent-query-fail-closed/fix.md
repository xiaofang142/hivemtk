# Bug Fix: FA-15 同意查询故障拒发

- **Slug**: consent-query-fail-closed
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md

## Changes

按评估列举的六个文件修复和补充回归，没有修改生产同意模式、配置或客户事实。

- 同意事件查询失败返回 `unavailable / consent_event_unavailable`；偏好查询失败返回 `unavailable / consent_preference_unavailable`，均不可信。
- 未初始化仓储或数据库返回故障，不再伪装为正常无记录。
- 发送管线只允许显式 granted；正常 missing 仍按 revocation_only/enforce 处理。unavailable 与未知判定在 permission 处拒发，主渠道及回退均不执行。
- permission checker 为空时仍执行已装配的同意检查。回执保留稳定来源原因码和 retryable 标记，发送审计错误也保留该来源，不含底层数据库错误。

## Reproduction Before Fix

在 `127.0.0.1:8217` 专用测试库执行新回归，命令退出 1，确认是业务断言失败而非连接或编译失败：事件查询超时被判为 missing，偏好查询超时仍判为 granted，未知判定触发发送适配器。未初始化数据库返回 nil/nil。

命令：`go test -count=1 -v ./internal/service ./internal/repository -run '(ConsentQueryFailureAndRecovery|UninitializedFailsClosed|ConsentUnknownDecisionFailsClosed|ContactConsentRepository_UninitializedReadFails)'`。

## Regression Tests

`contact_consent_failure_test.go` 在独立 GORM 连接注入事件/偏好查询超时，覆盖两种同意模式、28 个渠道名称（含未知名称），逐次断言主/备适配器零外发及 permission/audit 来源原因；撤销故障后同一管线的 grant 恢复发送。另覆盖 revoke/paused/missing 的恢复判定、空依赖及未知状态。

修复后执行 `go test -count=1 -v ./internal/service ./internal/repository -run Consent`，退出 0，119 条含子用例的 PASS、0 FAIL。完整验证结果另记 test.md。

## Deviations

源码改动遵循 assessment 范围。共享工作区在本会话验证期间被另一会话提交：assessment 与测试已进入 `5e6a37734`，四个生产文件修复已进入 `370e51414`；保留这些提交，不重写历史，不将其他会话的改动归为本次交付。

## Limits

故障来自受控查询超时注入，发送适配器为可计数模拟；此证据证明管线故障阻断与恢复，不代表第三方平台真实送达。未新增自动故障重试，调用方恢复后重新提交时再次检查真实授权。
