# Bug Assessment: 证据有效性与候选后业务副作用统一准入

- **Slug**: lead-side-effect-gate-20261125
- **Task**: LM-20261125-03
- **Verdict**: valid

## Report

`process` 在三路/ReAct 判定后仅按 `jd.IsLead && score>=threshold` 就调用 `persistLead`，后者打标签、写线索、通知、自动转商机；`jd.Degraded`、ReactOutcome 降级虽然写入判定记录，但不影响副作用准入。模型高分且带降级短语仍可触发通知与自动商机。

## Scope

新增统一有效性闸：输入判定 + 证据完整度 + 降级状态 + 配置阈值，输出是否允许业务副作用；候选、负例、降级、证据不全分别有确定状态。闸在 `persistLead` 之前统一执行，标签/线索/通知/商机共用同一判定。
