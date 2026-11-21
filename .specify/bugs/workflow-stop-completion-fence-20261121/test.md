# Bug Verification: FA-WF-04

- **Slug**: workflow-stop-completion-fence-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿）

## 关闭判据对照

- 「终态迁移使用明确的原子状态约束/版本栅栏」✔（UpdateStatusFenced 条件更新）
- 「已确认 terminated 不得复活或被改写为 completed」✔（完成/失败写双拦截，停止原因保留）
- 「停止信号对在途节点生效」→ 在途节点完成时写被栅栏拦下 ✔；对后续节点调度的即时生效沿 runExecution 循环的状态检测（既有行为），本项补齐终态写侧
- 「完成/失败/等待与停止交错、双 worker 和重复停止」✔（重复停止 CAS 拒绝；双 worker 并发完成写被栅栏收敛为单写者语义——条件更新天然幂等）；等待态交错沿 FA-WF-02 的等待写范围
- 「后续节点与副作用符合停止契约」→ 节点级停止检查属 FA-WF-02 写点范围

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 栅栏专项 | go test ./internal/service -run 'TestStopExecution_CAS\|TestConfirmedStopNotOverwritten\|TestCompleteAndFailStillWrite' -count=1 -v | pass | green.log 3 PASS |
| 专项 race | 同上 -race | pass | race.log |
| 工作流回归 | dispatcher/retry/validate/stop/subflow 全集 | pass | regression.log 16 PASS / 0 FAIL |

## 边界

- completed→failed 的既有迁移图保持不变（栅栏只豁免 terminated），未收窄既有语义。
- 未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / gates.json / source-sha256.json
