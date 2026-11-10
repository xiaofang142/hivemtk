# Bug Fix: markdownlint 存量红清零（CI Markdown Lint 作业与 --lint-md 档转绿）

- **Slug**: markdownlint-preexisting-red-blocks-ci
- **Fixed**: 2026-11-11
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

动手前实跑 CI 同口径命令（仓库根 `npx -y markdownlint-cli2`，自动读 `.markdownlint-cli2.jsonc` 的 globs/ignores 与 `.markdownlint.json` 规则集）复核：**assessment 登记的 9 处红已全部被历史提交清掉**（漂移，见 Deviations），当前真实红清单为 **5 处 / 5 文件**（413 个 md 参与 lint）。本轮按实跑结果逐条修复 5 处，全部为内容格式问题，**未收敛任何规则、未改任何代码文件**；复跑同口径 lint `Summary: 0 issues`（rc=0）。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `router-list.md:325` | modified | MD047：末尾补 1 个 `\n`（纯字节，正文零变化，163260→163261 字节） |
| `docs/research/CHATGPT-ON-CS-VS-HIVEMTK-AUDIT-2026-11.md:81` | modified | MD004：行首 ASCII `+`（被解析为 plus 风格嵌套列表）改全角 `＋`，保留「加上」连接词语义；循本仓先例（`docs/handbook/SAAS_PRODUCTION_CONSTRAINTS.md:134` 同一字节级修法 `＋` = U+FF0B） |
| `specs/013-template-skill-market/delivery.md:34` | modified | MD004：blockquote 内行首 `+` 改全角 `＋`，同上（该行是「建表登记」清单的连接词，非列表） |
| `specs/013-template-skill-market/tasks.md:46` | modified | MD004：T022 条目续行行首 `+` 改全角 `＋`，同上（行中 `+` 不触发 MD004，保持不动，与同文件 T020/T021 等行内 `+` 风格一致） |
| `.specify/bugs/web-token-expired-no-redirect/assessment.md:49` | modified | MD018：行首 `#/dashboard`（hash 路由片段，非标题）转义为 `\#`，渲染输出与作者原意完全一致（零语义漂移）；不用全角 `＃` 是因 URL 片段需保持可复制 |

未改动：`.markdownlint.json`、`.markdownlint-cli2.jsonc`、`.github/workflows/markdownlint.yml`、任何 Go/JS 代码文件。

## Diff Highlights

```diff
--- a/docs/research/CHATGPT-ON-CS-VS-HIVEMTK-AUDIT-2026-11.md
-   + 每轮全量 action——重发/漏发靠 `NO_REPLY` 与人工，无 at-least-once 保障。
+   ＋ 每轮全量 action——重发/漏发靠 `NO_REPLY` 与人工，无 at-least-once 保障。

--- a/.specify/bugs/web-token-expired-no-redirect/assessment.md
-#/dashboard（数据全空、只有过期 toast）。登录表单永远不出现。
+\#/dashboard（数据全空、只有过期 toast）。登录表单永远不出现。

--- a/router-list.md（末字节）
-- 备注登记：…仅第 9 列变化）。
-（EOF，缺换行）
+- 备注登记：…仅第 9 列变化）。
```

## Tests Added or Updated

- 无新增自动化用例（纯 md 格式修复，回归手段 = 重跑同口径 lint 门，见下）。

## Local Verification

- 修复前：`npx -y markdownlint-cli2`（v0.24.0 / markdownlint v0.42.0，Linting 413 files）→ `Summary: 5 issues in 5 files`，rc=1（逐条清单见 test.md）。
- 修复后：`npx -y markdownlint-cli2` → `Summary: 0 issues in 0 files`，**rc=0**。
- 反向验证（证明门真会咬）：仓库根临时造缺尾换行文件 → lint 报 1 × MD047（rc=1）→ 删除 → 复跑 rc=0。
- `scripts/bug-verify.sh --slug markdownlint-preexisting-red-blocks-ci --skip-audit --lint-md --allow-dirty <7 个本轮文件>`：干净克隆（HEAD+点名路径）里 build/vet/lint-md 全绿，rc=0（日志与 MEASURED_COMMIT 见 test.md）。
- `python3 scripts/check-md-links-offline.py .`：0 断链（新写报告无外链）。

## Deviations from Assessment

1. **红清单漂移（重大）**：assessment 登记的 9 处（8 × MD047 + 1 × MD004@SAAS）在 HEAD=54ba724c 已全部不存在——历史提交（含 `38f9fce2` fix(pricing+lint)）已把 8 个 MD047 文件补尾换行、SAAS 的 `+` 改全角 `＋`、`_md047_probe.md` 补尾换行（探针文件仍在库、正文旧文案已过时但不再违规）。assessment 的 Preferred 1–3 实际已由前序任务落地。本轮以**实跑为准**修新漂移出的 5 处，符合任务纪律「红多少修多少」。
2. **MD004 修法取全角 `＋` 而非 assessment 提议的顿号 `、`**：仓内已落地先例（SAAS_PRODUCTION_CONSTRAINTS.md:134）用的是 `＋`（U+FF0B），且 `＋` 完整保留「加上」连接语义、`、` 只表并列；按先例收敛，语义漂移更小。
3. **新增了 assessment 未列的文件**（`router-list.md`、`specs/013/*` 两份、`web-token-expired-no-redirect/assessment.md`、research 审计文）：因为实跑红清单已漂移，属于 assessment「Files likely to change」清单过期，非根因误判；修法与 assessment 的成因分析（连接词 `+` / 缺尾换行 / 行首 `#` 内容）完全同型。

## Follow-ups

- `.specify/bugs/_md047_probe.md` 正文仍写「故意缺尾换行」，但该文件已被 38f9fce2 补尾换行、不再违规（文案与事实不符）。本轮不改语义，留作后续文档治理小项：要么改写成 assessment 建议的「MD047 反向验证配方」，要么明确其历史样本身份。
- CI action `DavidAnson/markdownlint-cli2-action@v24` 与本地 `npx -y markdownlint-cli2` 的工具版本可能不同步（本地现为 v0.24.0），规则集行为随版本升级可能漂移；推送后以 CI 实际作业为准。
