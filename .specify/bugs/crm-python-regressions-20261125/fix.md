# Bug Fix: CRM Python 回归与验收说明失真

- **Slug**: crm-python-regressions-20261125
- **Fixed**: 2026-11-25
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

修正 SC-001 负样本夹具、清理汇总顺序和外部 AI 默认调用保护；修复验证替身的真实双参数契约，并将 Quickstart 和 tasks 中未核实的环境/完成声明改正。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `scripts/api_verify_full.py` | modified | N04 左右号码保留共享前缀但最后一位区分 side；SC-001 先清理再统一汇总/返回，确保清理失败纳入退出码；CRM 画像请求仅在 `HIVEMTK_ALLOW_EXTERNAL_AI=1` 时发出；环境指引改为当前仓 `.env`。 |
| `scripts/test_api_verify_full_crm.py` | modified | 测试替身实现 `execute(sql, params)`；增加默认 AI 不调用、显式开启才调用，以及零/部分/全部创建下清理与退出码回归。未弱化现有断言。 |
| `.specify/custom-task-crm-lead-channel-20261124/quickstart.md` | modified | 更正产品根、8217 测试库前提、真实路由和显式子命令；说明清理检查、AI/外发安全与证据目录。 |
| `.specify/custom-task-crm-lead-channel-20261124/tasks.md` | modified | T066/T070 恢复未勾选，写明实跑前提与本轮未执行的端到端验证。 |

## Tests Added or Updated

- `scripts/test_api_verify_full_crm.py::CRMSafetyTests.test_operation_log_cleanup_is_scoped_to_captured_ids` — 使用与 `qexec(sql, params)` 一致的执行器形状，继续断言 SQL 只限定捕获的审计/客户 ID。
- `scripts/test_api_verify_full_crm.py::CRMAICallTests` — 默认无请求；只有显式环境变量时才调用 API client。
- `scripts/test_api_verify_full_crm.py::SC001CleanupTests` — 验证全失败无 ID 不执行 SQL、部分创建的删除异常进入汇总、正常校验后清理异常让退出码为 1。
- 既有 SC-001 标签、唯一性、CLI、Quickstart 路由与 task 状态断言保持启用。

## Local Verification

- Commands run: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts.test_api_verify_full_crm` → 26 tests, OK。
- Commands run: Python `ast.parse` 检查 `scripts/api_verify_full.py`、`scripts/test_api_verify_full_crm.py` → `AST parse OK: 2 files`。
- No HTTP, database, external AI, Go suite, UI suite, or repository-wide lint/audit was run. T066/T070 remain unchecked.

## Deviations from Assessment

- 代码核对发现 `external_ai_calls_allowed()` 原先仅定义和单测，CRM 深测的画像请求却无条件执行。为遵守“外部 AI 默认禁止”的既有接口契约及本轮边界，将实际调用接入 opt-in，并加入纯 Python 回归。
- SC-001 创建全部失败时客户 ID 列表为空，按实现不应调用删除 SQL；故拆成“零 ID 不发 SQL”和“部分/全部创建后删除异常”用例，覆盖真实条件而不制造无效清理失败。

## Follow-ups

- 在获授权且服务/DB 均指向隔离环境后，主智能体可按 Quickstart 执行真实 T066/T070；本报告不把单元测试等同于端到端验收。
