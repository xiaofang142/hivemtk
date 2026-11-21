# Bug Assessment: 批量导入不剥 UTF-8 BOM（Excel 中文 CSV 全批误报缺列）

- Created: 2026-11-21
- Verdict: valid
- Severity: medium（中文用户高频路径：Excel 导出 CSV 必带 BOM）

## Reproduction（8401 独立部署）

带 UTF-8 BOM 的模板形状 CSV（首列头"姓名"）`POST /api/batch/import` → 200 但 `success=0/failed=2`，逐行报「第 0 行: 缺少必要列: 姓名」；同一文件去 BOM 后 `success=2/failed=0`。证据 090-off-import.json（BOM 失败）与 090-off-import-nobom.json（成功）。

## Root Cause

`ImportFromCSV` 用 `strings.TrimSpace` 归一表头，但 U+FEFF 不属于 Unicode 空白，第一列表头解析为 "\ufeff姓名"，colMap 匹配全灭。仓库自身导出面（geo audit、citation CSV）都刻意带 BOM 供 Excel 打开——导入面却没有对称的剥离。

## Proposed Remediation

`ImportFromCSV` 公共入口 `rows[0][0] = strings.TrimPrefix(rows[0][0], "\ufeff")`——clue/user 两条导入路径同享，零依赖。

## Tests

`TestBatchImportFromCSV_StripsBOM`：同一 CSV 带/不带 BOM 均 success=2/failed=0，且不得出现「缺少必要列」。修复前用例对 BOM 版本 RED（旧码全灭）。

## Files likely to change

- user-server/internal/content/service/batch_operation.go
- user-server/internal/content/service/batch_operation_import_test.go（新增）
