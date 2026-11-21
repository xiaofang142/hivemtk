# Bug Verification: FA-WF-05

- **Slug**: workflow-subflow-depth-json-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿）

## 关闭判据对照

- 「覆盖数据库往返、JSON 数值/非法值」✔（JSON 往返 float64=5 被拒；负值/非数值夹 0）
- 「恰到上限、嵌套及循环引用，以有限夹具断言到界后零新增子执行」→ 到界拒绝零子执行 ✔（orchestrator 缺席即证深度守卫先于创建）；嵌套链与循环引用的端到端夹具待 FA-WF-03/06 的恢复协议夹具一并建，不在本项重复计
- 「深度元数据用稳定类型并校验范围/来源」→ 稳定类型 ✔；「每次创建从权威父执行原子派生，不能由调用方降低」为派生协议改造，登记为余项（payload 来源防篡改属 FA-WF-03/06 协议范围）

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 深度往返专项 | go test ./internal/service -run 'TestSubflowDepth' -count=1 -v | pass | green.log 2 PASS |
| 工作流回归 | dispatcher/retry/validate/stop/subflow 全集 | pass | regression.log 16 PASS / 0 FAIL |

## Evidence Files

fix.md / green.log / regression.log / gates.json / source-sha256.json
