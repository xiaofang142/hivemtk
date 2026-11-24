# Bug Assessment: 功能文档工具仍按旧嵌套仓路径定根

- **Slug**: feature-doc-product-root-20261125
- **Created**: 2026-11-25T02:13:27+08:00
- **Source**: pasted text — 主智能体确认的新发现 I-17
- **Verdict**: valid
- **Severity**: medium

## Report (verbatim or summarized)

> 功能文档工具仍假设旧嵌套仓路径：`scripts/check-feature-doc.sh` 与
> `scripts/feature-doc-coverage-report.sh` 用 `SCRIPT_DIR/../..` 作为 `PROJECT_ROOT`，
> 再拼 `$PROJECT_ROOT/hivemtk/docs/marketing-features`。当前产品根是
> `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user`，两个脚本都在文档存在时失败。
> 另外，`docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md` §四仍写
> Router → Handler → Service → Repository → Model，与权威 Controller 口径不一致。

## Symptom

从当前产品根运行结构校验或覆盖率报告，两者均在扫描文档前以“目录不存在”退出；
预期应定位当前产品仓的 `docs/marketing-features` 并完成 normal/strict 校验与 table/JSON 报告。
文档模板还会引导作者采用已废弃的 `Handler` 层名。

## Reproduction

1. 按 `AGENTS.md` §0 向上查找 `deploy/deploy-api.sh`，确认应用根为
   `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user`。
2. 在该根确认 `docs/marketing-features` 存在（当前有 `README.md` 与
   `GEO_MODULE_DESIGN.md`，非跳过文档 1 篇），而旧推导路径
   `/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/docs/marketing-features` 不存在。
3. 在产品根执行 `bash scripts/check-feature-doc.sh`：退出码 1，输出
   `目录不存在: /Users/xiaofang/Documents/www/go/hivemtk/hivemtk/docs/marketing-features`。
4. 在产品根执行 `bash scripts/feature-doc-coverage-report.sh --json`：退出码 1，输出同一旧路径错误。
5. 检查 `docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md:104`，仍为
   `Router → Handler → Service → Repository → Model`。

## Suspected Code Paths

- `scripts/check-feature-doc.sh:19-20,39-43` — 将产品根的父目录当作工作区根，并硬编码旧子仓名 `hivemtk`。
- `scripts/feature-doc-coverage-report.sh:17-20,49-52` — 同样的根推导与旧子仓路径。
- `docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md:102-105` — §四沿用旧层名 Handler。
- `docs/marketing-features/` — 当前产品根内实际存在且应被两个工具扫描的文档目录。

## Root Cause Hypothesis

高置信度：两个工具为旧“工作区根/内层 hivemtk”布局编写，仓名改为 `hivemtk-user`
后没有同步脚本定根逻辑。`SCRIPT_DIR/../..` 现在得到外层普通目录，再拼 `hivemtk`
必然指向不存在的旧目录；模板的 Handler 文案则是同一轮架构命名迁移未覆盖到的文档漂移。

## Proposed Remediation

**Preferred**：

1. 两个脚本的 `PROJECT_ROOT` 改为产品根 `SCRIPT_DIR/..`。
2. 两个脚本的 `DOCS_DIR` 改为 `$PROJECT_ROOT/docs/marketing-features`。
3. 新增 `scripts/check-feature-doc.test.sh`，从实际产品根运行 checker normal/strict 与
   coverage table/JSON，断言输出路径为当前 `docs/marketing-features`，并校验 JSON 可解析且
   `docs_dir` 精确一致。先运行取得 RED，再改生产脚本取得 GREEN。
4. 模板 §四的 `Handler` 改为 `Controller`。
5. 按用户授权，仅更新外层审计清单
   `../docs/architecture/FULL_FEATURE_INVENTORY.md` 的 I-17 与验证记录；不新建任务/进度/台账文档。

**Alternatives**：

- 保留旧布局并增加 `hivemtk-user` 兼容分支：拒绝，当前产品根已明确，兼容分支会继续掩盖路径漂移。
- 用 `git rev-parse` 动态找根：拒绝，脚本按自身位置定根更简单，也不依赖调用者是否处于 Git 上下文。

**Files likely to change**:

- `scripts/check-feature-doc.sh`
- `scripts/feature-doc-coverage-report.sh`
- `scripts/check-feature-doc.test.sh`（新增）
- `docs/standards/FEATURE_DOCUMENTATION_TEMPLATE.md`
- `../docs/architecture/FULL_FEATURE_INVENTORY.md`（用户明确允许的外层文件）
- `.specify/bugs/feature-doc-product-root-20261125/fix.md`（Bug 车道产物）
- `.specify/bugs/feature-doc-product-root-20261125/test.md`（Bug 车道产物）

**Tests to add or update**:

- checker normal 从产品根定位正确目录并成功完成。
- checker `--strict` 从产品根定位正确目录并成功完成；若存量 WARN 导致非零，如实报告，不削弱判据。
- coverage table 从产品根定位正确目录并成功完成。
- coverage `--json` 从产品根定位正确目录、退出码为 0、JSON 可解析且 `docs_dir` 精确一致。

## Risks & Considerations

- 仅修改脚本定根，不改变节判定、警告/错误语义或报告统计逻辑。
- 旧嵌套布局将不再受支持；这是当前仓库结构的预期行为。
- 模板只改层名，不扩散修改其他功能文档中的历史表述。
- 工作树有大量并行会话改动；不得 stage、reset、stash 或覆盖无关文件，且按用户要求不 commit/push/deploy。

## Open Questions

- 无。故障已在当前产品根稳定复现，修复范围由主智能体明确限定。
