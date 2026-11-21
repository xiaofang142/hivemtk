# Bug Assessment: FA-21 触达预检零副作用

- **Slug**: proactive-reach-dry-run-20261121
- **Created**: 2026-11-21
- **Source**: 外层 docs/未完成任务清单.md 的 FA-21；用户要求逐步推进清单
- **Verdict**: valid
- **Severity**: high

## Report

手机号、邮箱和客户自动选路的 dry-run 必须零外发、零发送额度消耗，不记录发送成功；正式发送继续经过现有退订、审批、合规及适用的冷却门。

## Symptom

ValidateReach 强制 DryRun=true，但 ReachByCustomer 的 Phone/Email 分支在唯一 DryRun 判断之前直接发送，并通过 defer 累计成功发送。自动选路的 dry-run 另用虚构 dry_run_account，不代表正式选路结果。

## Reproduction

源码确认：controller/proactive_reach.go 的 ValidateReach → service/proactive_reach.go:417–446（显式发送）→ 483（仅客户分支 dry-run）。将发送 registry 注入本机 spy 并以 DryRun=true 请求即可确定性验证，不调用外部服务；将在 fix 阶段先跑新增回归记录 RED。评估阶段未实际执行，不能记为已复现。

## Suspected Code Paths

- user-server/internal/service/proactive_reach.go:392–553：三个发送分支与成功计数 defer。
- user-server/internal/service/proactive_reach.go:638–676：预检与正式选路算法分叉。
- user-server/internal/controller/proactive_reach.go:188–200：验证入口强制 dry-run。
- user-server/internal/service/proactive_reach_gate_test.go:279–308：既有契约明确 dry-run 不调用发送审批钩子。

## Root Cause Hypothesis

高置信：dry-run 的短路仅放在客户分支内；显式联系方式完全绕过。另预览为了避免发送误将账号读取也替换为虚构结果。应复用只读选路，并在各分支外发门、冷却占位和成功计数前返回预览。

## Proposed Remediation

显式分支在现有退订检查后、发送审批/合规和发送前返回统一 dry_run 响应；客户分支复用正式 pickChannel，去除虚构账号选路，选路后在副作用之前返回。保留既有预览不执行发送审批契约；不将预览解释成已获外发授权。不改真实发送门、租户机制、API形状或生产配置。

**Files likely to change**:
- user-server/internal/service/proactive_reach.go
- user-server/internal/service/proactive_reach_dry_run_test.go（新增）
- user-server/internal/controller/proactive_reach_dry_run_test.go（新增）
- .specify/bugs/proactive-reach-dry-run-20261121/ 下阶段报告与验证证据
- 外层 docs/未完成任务清单.md（验证后针对 FA-21 更新）

**Tests to add or update**:
- 手机、邮箱（含同时指定时手机优先）、客户选路：dry-run 不调用任何 spy、不计发送额度、不占冷却；相同请求转正式发送正常。
- 预检与正式选路的 channel/recipient/account 一致；缺真实账号返回 ErrNoActiveAccount。
- 正式发送保留退订、审批、合规拒绝与成功计数。
- ValidateReach 强制 dry-run，即使请求 dry_run=false 也零外发；正常 send 的 dry_run=true 同样零外发。

## Risks & Considerations

预检真实读取账号会将以前虚构成功的无账号请求改为明确失败，这是与正式选路一致的修复。专用 PostgreSQL 实例仅 127.0.0.1:8217；发送器全部本地替身，不真实外发。主检出有大量其他会话在途改动（包括 FA-17），本轮不覆盖、不纳入提交或发布。

## Open Questions

无阻塞决策。真实发布另须隔离归属明确的交付范围；不得将混合工作树整体部署。
