# Bug Fix: 批量导入剥离 UTF-8 BOM

- Fixed: 2026-11-21
- Assessment: ./assessment.md
- Status: applied

## Changes

`ImportFromCSV` 公共入口对首列头剥 `\ufeff` 前缀；新增 `TestBatchImportFromCSV_StripsBOM` 锁定带/不带 BOM 同结果。

## Verification

- 修复前 HTTP 双向取证：BOM 文件 success=0/failed=2（090-off-import.json）；无 BOM success=2（090-off-import-nobom.json）。
- 修复后单测：`go test ./internal/content/service -run TestBatchImportFromCSV_StripsBOM -count=1` → ok 0.852s。
- 服务重构建后 HTTP 复测带 BOM 导入 → success=2（见 090-off-import-bom-fixed.json）。
