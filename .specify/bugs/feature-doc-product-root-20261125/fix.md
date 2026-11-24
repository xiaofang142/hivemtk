# Bug Fix: 功能文档工具按当前产品根定位

- **Slug**: feature-doc-product-root-20261125
- **Fixed**: 2026-11-25T02:14:45+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

将功能文档校验与覆盖率报告的产品根从旧工作区布局 `SCRIPT_DIR/../..` 修正为
`SCRIPT_DIR/..`，文档目录同步改为 `$PROJECT_ROOT/docs/marketing-features`；新增真实产品根
回归测试，并把文档模板 §四的层名从 `Handler` 对齐为 `Controller`。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `scripts/check-feature-doc.sh` | modified | `PROJECT_ROOT=SCRIPT_DIR/..`；`DOCS_DIR=$PROJECT_ROOT/docs/marketing-features` |
| `scripts/feature-doc-coverage-report.sh` | modified | 同上，仅修正定根与文档路径 |
| `scripts/check-feature-doc.test.sh` | added | 从真实产品根运行 checker normal/strict、coverage table/JSON；校验路径、退出码与 JSON |
| `docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md` | modified | §五层架构文案 `Handler` → `Controller` |
| `../docs/architecture/FULL_FEATURE_INVENTORY.md` | modified | 按用户授权记录 I-17 已修复与门禁证据；不新建任务/进度/台账文档 |

## Diff Highlights

```bash
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCS_DIR="$PROJECT_ROOT/docs/marketing-features"
```

```text
Router → Controller → Service → Repository → Model
```

## Tests Added or Updated

- `scripts/check-feature-doc.test.sh` — 四格真实工具回归：
  - checker normal：rc=0 且输出当前产品根的 `docs/marketing-features`。
  - checker `--strict`：rc=0 且输出当前产品根的 `docs/marketing-features`。
  - coverage table：rc=0 且表头目录为当前产品根的 `docs/marketing-features`。
  - coverage `--json`：rc=0、JSON 可解析、`docs_dir` 精确一致且 `total_docs` 非零。

## Local Verification

### RED（修复生产脚本前）

- `bash scripts/check-feature-doc.test.sh` → rc=1，`失败 4 格`。
- 四格均输出：
  `目录不存在: /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/docs/marketing-features`。
- 直接复现：
  - `bash scripts/check-feature-doc.sh` → rc=1。
  - `bash scripts/feature-doc-coverage-report.sh --json` → rc=1。

### GREEN（最小修复后）

- `bash scripts/check-feature-doc.test.sh` → rc=0，`全部通过（4/4）`。
- `bash scripts/check-feature-doc.sh` → rc=0，通过 1、失败 0、跳过 1。
- `bash scripts/check-feature-doc.sh --strict` → rc=0，通过 1、失败 0、跳过 1，**无 WARN**。
- `bash scripts/feature-doc-coverage-report.sh` → rc=0，1 篇文档、全节覆盖率 **100.0%**。
- `bash scripts/feature-doc-coverage-report.sh --json` → rc=0，`total_docs=1`、
  `total_filled=8`、`coverage_pct=100.0`。
- `bash -n scripts/check-feature-doc.sh scripts/feature-doc-coverage-report.sh scripts/check-feature-doc.test.sh`
  → rc=0。
- `shellcheck -S error scripts/check-feature-doc.sh scripts/feature-doc-coverage-report.sh scripts/check-feature-doc.test.sh`
  → rc=0。
- `git diff --check` → rc=0。

### ShellCheck 存量警告（不扩大修复面）

- `shellcheck -S warning` 在 `scripts/feature-doc-coverage-report.sh` 报既有
  `SC2034`（未使用的 `CYAN`）与 `SC2188`（裸重定向）。
- 对 `git show HEAD:scripts/feature-doc-coverage-report.sh` 的未修改版本执行同一命令，
  两条警告与行号完全一致，确认不是本次引入；本次不放宽检查，也不做无关重构。

## Deviations from Assessment

无。生产改动严格限定为两个脚本的定根/路径与模板层名；新增测试覆盖评估中列出的四种调用。
外层清单更新是主智能体明确授权的报告动作。

## Follow-ups

- 无本缺陷遗留。按主智能体要求不 commit、不 push、不部署。
- ShellCheck 两条存量 warning 保留原状，未在本问题中扩散修复。
