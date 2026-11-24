# Bug Verification: 功能文档工具按当前产品根定位

- **Slug**: feature-doc-product-root-20261125
- **Tested**: 2026-11-25T02:15:19+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

原始“目录不存在”故障已在修复前稳定复现；修复后，checker normal/strict 与 coverage
table/JSON 均从当前产品根定位 `hivemtk-user/docs/marketing-features` 并成功完成。
新增回归 4/4 通过，strict 无 WARN，覆盖率 100%，语法、ShellCheck error 级与 diff 空白检查通过。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (pre-fix) | `bash scripts/check-feature-doc.test.sh` | fail（预期 RED） | rc=1，4/4 格均指向旧 `.../hivemtk/docs/marketing-features` |
| New regression | `bash scripts/check-feature-doc.test.sh` | pass | rc=0，`全部通过（4/4）` |
| Checker normal | `bash scripts/check-feature-doc.sh` | pass | rc=0；通过 1、失败 0、跳过 1 |
| Checker strict | `bash scripts/check-feature-doc.sh --strict` | pass | rc=0；通过 1、失败 0、跳过 1；无 WARN |
| Coverage table | `bash scripts/feature-doc-coverage-report.sh` | pass | rc=0；1 篇文档，全节覆盖率 100.0% |
| Coverage JSON | `bash scripts/feature-doc-coverage-report.sh --json` | pass | rc=0；合法 JSON，`docs_dir` 为当前产品根，`total_docs=1`、`total_filled=8` |
| Shell syntax | `bash -n scripts/check-feature-doc.sh scripts/feature-doc-coverage-report.sh scripts/check-feature-doc.test.sh` | pass | rc=0 |
| ShellCheck error gate | `shellcheck -S error scripts/check-feature-doc.sh scripts/feature-doc-coverage-report.sh scripts/check-feature-doc.test.sh` | pass | rc=0 |
| Whitespace gate | `git diff --check` | pass | rc=0 |
| Stale text check | grep 两个脚本旧路径表达式与模板旧 Handler 链路 | pass | 零残留 |

## Output Excerpts

RED：

```text
FAIL checker normal rc=1，未从产品根完成校验
       | ❌ 目录不存在: /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/docs/marketing-features
FAIL checker strict rc=1，未从产品根完成校验
FAIL coverage table rc=1，未从产品根完成报告
FAIL coverage json  rc=1，未从产品根输出合法 JSON
失败 4 格
regression_red_exit=1
```

GREEN：

```text
PASS checker normal rc=0 docs_dir=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/docs/marketing-features
PASS checker strict rc=0 docs_dir=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/docs/marketing-features
PASS coverage table rc=0 docs_dir=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/docs/marketing-features
PASS coverage json  rc=0 docs_dir=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/docs/marketing-features
全部通过（4/4）
```

```json
{
  "docs_dir": "/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/docs/marketing-features",
  "total_docs": 1,
  "total_slots": 8,
  "total_filled": 8,
  "coverage_pct": 100.0
}
```

## Residual Risks

- `shellcheck -S warning` 在覆盖率脚本仍有两条存量 warning（`SC2034`、`SC2188`）；
  已对 `git show HEAD` 版本复跑确认同警告、同行号，不是本次引入。`-S error` 门禁通过。
- 未修改 `GEO_MODULE_DESIGN.md` 等业务文档中的历史 Handler 文案；本问题只按授权修正模板。
- 未做 UI、外部渠道、数据库或生产部署验证；按主智能体要求未 commit/push/deploy。

## Recommendation

关闭 I-17：原始路径故障已复现并验证修复，四种调用与静态门均有新鲜通过证据。
两条 ShellCheck 存量 warning 单独保留，不冒充全仓 ShellCheck warning 级全绿。
