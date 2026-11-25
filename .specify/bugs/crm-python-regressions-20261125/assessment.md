# Bug Assessment: CRM Python 回归与验收说明失真

- **Slug**: crm-python-regressions-20261125（按本轮症状自动生成，目录此前不存在）
- **Created**: 2026-11-25
- **Source**: 主智能体任务与 `.planning/2026-11-25-workspace-commit/user-go-python.log`
- **Verdict**: valid
- **Severity**: medium；影响合成数据标签、清理失败的退出码和验收证据可信度。

## Report

限定修复 `scripts/api_verify_full.py`、`scripts/test_api_verify_full_crm.py`、feature 的 `quickstart.md` / `tasks.md`；仅纯 Python 单测。现有改动保留，不暂存、不提交、不推送，不调用真实 HTTP、DB 或外部 AI。

## Symptom / Reproduction

在 `hivemtk-user/` 执行：

```text
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts/test_api_verify_full_crm.py
Ran 22 tests in 0.084s
FAILED (failures=4, errors=1)
```

1. 审计清理测试传 `list.append`，收到双参数时抛 `TypeError`。
2. Quickstart 使用旧仓路径、旧路由和缺子命令的 API 命令。
3. T066/T070 在尚未实跑 API / 端到端时标成完成。
4. 清理测试未得到预期的失败项。
5. N04 同前缀负例的两个完整号码相等，例如 `19262200091`。

## Suspected Code Paths / Root Cause Hypothesis

置信度 high，已通过代码与上述单测核实：

- `qexec(sql, params=())` 将双参数传给 psycopg2 cursor；`cleanup_crm_operation_logs(..., execute)` 同样使用此契约。`list.append` 只接一个对象，错误在测试替身，不应改真实 API。
- `build_sc001_sample` 的 N04 手机号只含 run 标签与 pair 序号，没有 side 标识，因此同一对完全相等。
- `run_sc001` 在 `try` 中每条正常/提前退出路径均先求值 `finish(keep)`，再执行 `finally: cleanup()`。清理失败会追加到 REPORT，却无法改变已经算出的退出码，也不在最终汇总中。
- 现有清理测试另有前提错误：所有 create 都失败时 `customer_ids` 为空，清理不应执行任何 DELETE；仅把 `qexec` 设成抛异常不能制造真正清理异常。需保留失败断言并用至少一个实际返回的合成 ID 触发清理；另测零 ID 无 SQL 以及全流程成功后清理失败导致非零退出码。
- `quickstart.md` 与真实路由不一致：auth 前缀为 `/api`（`router.go`），渠道路由位于 `channel_routes.go`；CRM 位于 `crm_routes.go`；判定明细/归档列表见 `content_routes.go`，hits 见 `lead_routes.go`。`AGENTS.md` 指定独立产品仓 `hivemtk-user/`；`CLAUDE.md` 指定专用测试库端口 8217。
- `tasks.md` T066/T070 的完成标记没有本轮真实运行证据。按派发任务恢复未勾选，明确运行前提。

## Proposed Remediation

- 保持 SQL/参数的作用域断言，改用显式双参数记录替身。
- N04 仅在号码后四位编码 pair 与 side，保留相同前七位和不同完整号码的语义。
- 把 SC-001 验证步骤放入局部函数，提前退出只结束验证；外层先在 finally 清理，再统一汇总并返回退出码。异常仍传播，不用 finally 的 return 吞异常。
- 补齐真实清理路径的纯 Python 回归；网络和 DB 边界全部替身，不写证据文件到外部位置。
- 按路由实现更新 Quickstart，显式隔离 DB/API、子命令与 AI/外发前提；T066/T070 未实跑保持未勾选。

**Files likely to change**:

- `scripts/api_verify_full.py`
- `scripts/test_api_verify_full_crm.py`
- `.specify/custom-task-crm-lead-channel-20261124/quickstart.md`
- `.specify/custom-task-crm-lead-channel-20261124/tasks.md`

**Tests to add or update**:

- 清理执行器接收 `(sql, params)`，原有清理作用域断言全部保留。
- 零 create 成功时不做 SQL，且清理结果在报告汇总前产生。
- 部分 create 成功、清理异常时汇总包含失败。
- 正常验证完成但清理异常时退出码必须为 1。
- 保留全部原有号码、标签、文档与 CLI 断言。

## Risks & Considerations

- 不引入真实连接，不修改 SQL 删除范围，不覆盖其他会话文件/报告。
- 本轮只验证 Python 回归与文档契约，不宣称 T066/T070 或 SC-001 真实准确率通过。
- 图谱、Go/lint/audit 门禁和最终提交由主智能体处理；依任务边界不执行。
- `.specify/extensions.yml` 为 `hooks: {}`，assess/fix/test 均无扩展钩子。

## Open Questions

无；所需执行器契约与路由均已从代码核实。
