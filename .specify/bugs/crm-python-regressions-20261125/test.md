# Bug Verification: CRM Python 回归与验收说明失真

- **Slug**: crm-python-regressions-20261125
- **Tested**: 2026-11-25
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

指定的纯 Python 回归和文档契约均通过。验证范围不包含真实 API、数据库、外部 AI 或端到端 UI；因此这不代表 tasks.md 的 T066/T070 已完成。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts.test_api_verify_full_crm` | pass | 全部 26 项通过；复现用纯 Python 客户端/SQL 替身，不发 HTTP、不连数据库。 |
| New / updated tests | 同上 | pass | 覆盖 SQL executor 双参数契约、N04、清理失败退出码、无 ID 不执行 SQL、外部 AI opt-in 和文档断言。 |
| Regression suite | 同上 | pass | `Ran 26 tests ... OK`。 |
| Lint / type-check | Python `ast.parse` 对两个 Python 文件执行 | pass | 输出 `AST parse OK: 2 files`；未运行仓库级 lint/audit。 |
| API / DB / external AI / UI integration | 未运行 | skipped | 按任务边界和安全限制；无真实服务请求或持久化数据操作。 |

## Output Excerpts

```text
Ran 26 tests in 0.032s
OK
AST parse OK: 2 files
```

## Residual Risks

- 未验证隔离 API 实例确实连接 `127.0.0.1:8217`，未验证真实数据库清理或端到端业务链路。
- T066 和 T070 明确保持 unchecked；不得将本报告视为其完成证据。

## Recommendation

关闭本次 Python 单元回归缺陷；保留真实隔离环境验收为未验证状态，只有取得 T066/T070 的实际证据后再更新任务状态。
