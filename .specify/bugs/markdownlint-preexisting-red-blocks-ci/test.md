# Bug Verification: markdownlint 存量红清零（CI 口径 0 红复跑）

- **Slug**: markdownlint-preexisting-red-blocks-ci
- **Tested**: 2026-11-11
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

CI 同口径命令（仓库根 `npx -y markdownlint-cli2`，读 `.markdownlint-cli2.jsonc` + `.markdownlint.json`）修复前实跑 5 红/5 文件（assessment 登记的 9 处已漂移为这 5 处新红），本轮修复后复跑 `Summary: 0 issues in 0 files`（rc=0）；反向验证（临时造缺尾换行文件）证明门真会判红；`bug-verify.sh --lint-md` 档在干净克隆（HEAD+点名路径）里全绿 rc=0。assessment 的 9 处登记未由本轮修复（已被历史提交清零），如实记为漂移。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| CI 口径确认 | 读 `.github/workflows/markdownlint.yml` + `.markdownlint-cli2.jsonc` + `.markdownlint.json` + `scripts/bug-verify.sh:353` | pass | CI action globs `**/*.md`；本地同口径 = 仓库根 `npx -y markdownlint-cli2`（Finding 行实测逐条对上） |
| 修复前复跑（实跑红清单） | `npx -y markdownlint-cli2` | fail（5 红，预期内） | 413 files，`Summary: 5 issues in 5 files`，rc=1；逐条见 Output Excerpts |
| 修复后复跑 | `npx -y markdownlint-cli2` | pass | 413 files，`Summary: 0 issues in 0 files`，**rc=0** |
| 反向验证（门灵敏度） | 临时造 `_md047_probe_repro.md`（缺尾换行）→ lint → 删除 | pass | 带探针时报 `1 issue`（MD047）rc=1；删除后复跑 rc=0，探针已删，树内无残留 |
| CI 作业本地复现（--lint-md 档） | `bash scripts/bug-verify.sh --slug markdownlint-preexisting-red-blocks-ci --skip-audit --lint-md --allow-dirty router-list.md docs/research/CHATGPT-ON-CS-VS-HIVEMTK-AUDIT-2026-11.md specs/013-template-skill-market/delivery.md specs/013-template-skill-market/tasks.md .specify/bugs/web-token-expired-no-redirect/assessment.md .specify/bugs/markdownlint-preexisting-red-blocks-ci/fix.md .specify/bugs/markdownlint-preexisting-red-blocks-ci/test.md` | pass | 干净 `--shared` 克隆（HEAD 54ba724c + 点名 7 路径），`go build` / `go vet` / `markdownlint-cli2 (--lint-md)` 三档全绿，退出 rc=0；克隆状态仅含点名 7 文件；MEASURED_COMMIT=54ba724c8471af5b61619639c8afa4919554802f，取证 `/tmp/bug-verify/markdownlint-preexisting-red-blocks-ci-20261110-204013` |
| 链接门（CLAUDE.md 3.5 文档档） | `python3 scripts/check-md-links-offline.py .` | pass | 0 断链 |
| CI 线上作业本体 | GitHub Actions 实跑 | not-run | 本环境无法触发线上 workflow；以同工具同配置同 globs 的本地命令 + --lint-md 档（克隆树）作为最强本地等效，见 Residual Risks |

## Output Excerpts

修复前（实跑红清单，逐条 文件:规则:行）：

```text
markdownlint-cli2 v0.24.0 (markdownlint v0.42.0)
Finding: **/*.md !**/node_modules/** !**/dist/** !**/build/** !**/pg_data/** !**/uploads/** !**/logs/** !**/vendor/** !**/.git/** !**/tmp/** !**/backups/** !models/**
Linting: 413 files
Summary: 5 issues in 5 files
.specify/bugs/web-token-expired-no-redirect/assessment.md:49:1 error MD018/no-missing-space-atx No space after hash on atx style heading [Context: "#/dashboard（数据全空、只有过期 toast）。登..."]
docs/research/CHATGPT-ON-CS-VS-HIVEMTK-AUDIT-2026-11.md:81:4 error MD004/ul-style Unordered list style [Expected: dash; Actual: plus]
router-list.md:325:62 error MD047/single-trailing-newline Files should end with a single newline character
specs/013-template-skill-market/delivery.md:34:3 error MD004/ul-style Unordered list style [Expected: dash; Actual: plus]
specs/013-template-skill-market/tasks.md:46:3 error MD004/ul-style Unordered list style [Expected: dash; Actual: plus]
```

修复后（0 红证明，输出尾部原样）：

```text
markdownlint-cli2 v0.24.0 (markdownlint v0.42.0)
Finding: **/*.md !**/node_modules/** !**/dist/** !**/build/** !**/pg_data/** !**/uploads/** !**/logs/** !**/vendor/** !**/.git/** !**/tmp/** !**/backups/** !models/**
Linting: 413 files
Summary: 0 issues in 0 files
RC=0
```

反向验证（探针在树时）：

```text
Linting: 415 files
Summary: 1 issue in 1 file
_md047_probe_repro.md:3:9 error MD047/single-trailing-newline Files should end with a single newline character
RC_WITH_VIOLATION=1
```

--lint-md 档（干净克隆里，14-markdownlint.log 尾部原样；415 files = 413 库内 + 2 份本轮新报告被 --allow-dirty 复制进克隆）：

```text
markdownlint-cli2 v0.24.0 (markdownlint v0.42.0)
Finding: **/*.md !**/node_modules/** !**/dist/** !**/build/** !**/pg_data/** !**/uploads/** !**/logs/** !**/vendor/** !**/.git/** !**/tmp/** !**/backups/** !models/**
Linting: 415 files
Summary: 0 issues in 0 files
```

bug-verify 结论段原样：

```text
 MEASURED_COMMIT 54ba724c8471af5b61619639c8afa4919554802f
 EVIDENCE        /tmp/bug-verify/markdownlint-preexisting-red-blocks-ci-20261110-204013
✅ 全绿（前提齐、干净树判定通过）
BUGVERIFY_RC=0
```

## Residual Risks

- **CI 线上作业未实跑**：本环境无 GitHub runner。等效依据：本地命令与 action 用同一工具（markdownlint-cli2）、同一规则文件、同一 globs；`--lint-md` 档更是在干净克隆树里复跑的同一命令。残余差异仅在 action 固定的工具版本（本地 npx 拉到 v0.24.0 / markdownlint v0.42.0，action v24 锁定其内嵌版本）——若未来版本规则行为变化需另行复核。
- **登记 9 处与实跑 5 处的漂移**已由历史提交（`38f9fce2` 等）消化；`.specify/bugs/_md047_probe.md` 正文仍写「故意缺尾换行」但文件已合规（文案过时，非 lint 红），留作文档治理小项。
- `bug-verify.sh` 判定树 = HEAD + `--allow-dirty` 点名路径；推送由主智能体统一收口后，CI 将对纯 HEAD（含本轮全部改动）复判，口径只会更严不会更松。

## Recommendation

Close the bug — 同口径 lint 0 红、反向验证通过、--lint-md 档干净克隆全绿；assessment 的 9 处登记以「漂移 + 已由前序提交修复」如实结案。
