# Bug Verification: FA-31 GEO 决策链上游契约

- **Slug**: geo-step-results-contract-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿；真实 Run 闭环成立）

## 关闭判据对照

- 冻结带步骤 ID/类型/结果的传递契约 ✔（`map[string]*dto.StepResult`，浅拷贝隔离键集）
- 跑实际 WorkflowService.Run 的探测→归因→补缺→线索捕获 ✔（`TestWorkflowRun_DecisionChainUpstreamContractClosedLoop`：真实测试库 + fakeProbe + 计数 LeadPort，补位任务落库含 rival-source.example.com，线索恰好一次）
- 上游失败/未知结果按约定处理 ✔（缺上游显式报错；旧 `map[string]string` 形状不得通过上游校验——防契约回退守卫）
- 禁止仅直调单个 executor 验收 ✔（闭环走 svc.Run 全路径；直调用例仅作既有回归保留）

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 闭环+守卫专项 | go test ./internal/geo/service -run 'TestWorkflowRun_DecisionChain.*' -count=1 -v | pass | green.log 2 PASS |
| 专项 race | 同上 -race | pass | race.log |
| geo 全回归 | go test ./internal/geo/... -count=1 | pass | regression.log：controller 7.0s / repository 2.8s / service 34.2s 全 ok |

## 边界

- 探针为本地 fakeProbe、无网络/真实搜索；未连生产库。
- 「重复类型、多分支」语义由既有 decision_executors_test 覆盖保留；未运行真实页面。
- 未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / source-sha256.json
