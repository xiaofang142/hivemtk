# Bug Assessment: router-list CLI help rewrites canonical evidence

- **Slug**: router-list-cli-side-effects-20261125
- **Created**: 2026-11-25
- **Source**: pasted text（父会话指派：修复 `docs/architecture/FULL_FEATURE_INVENTORY.md` I-08）
- **Verdict**: valid
- **Severity**: medium
- **Canonical baseline SHA-256**: `6e0e83a21a41268d7dd448e492678cee559fa9e0b694b2e804ca6cc4b8484cb6`

## Report (verbatim or summarized)

`scripts/gen_router_list.py` has no argparse; `main()` unconditionally writes canonical `router-list.md`, so even `python3 scripts/gen_router_list.py --help` rewrites it. The canonical file contains important per-page evidence and must not be touched by help/import. Add side-effect-free `--help` and `--out`/`-o`; keep the default product-root `router-list.md` and preserve the exact current output format and route/evidence content.

## Symptom

Invoking the generator with `--help` generates and overwrites the default route ledger instead of printing help and exiting. Expected behavior is an exit-0 help response with no filesystem mutation; imports and argument parsing must also remain side-effect-free.

## Reproduction

The real canonical file was not passed to the defective command. A controlled reproduction imported the module, redirected its `OUT` to a temporary sentinel file, set `sys.argv = ["gen_router_list.py", "--help"]`, and called `main()`:

```text
canonical_import_unchanged=True
help_SystemExit=None
help_main_return=None
help_output_unchanged=False
help_output_first_line='# 前端页面路由清单（任务四逐页点验 + 任务六新手引导文案）'
```

The same run printed `写入 <temporary>/router-list.md：280 条页面路由 ...`, proving that argument handling never occurs and the write is unconditional.

## Suspected Code Paths

- `scripts/gen_router_list.py:13-20` — imports only `os`/`re`; no argument parser exists. `OUT` is the canonical product-root path.
- `scripts/gen_router_list.py:209-260` — status and guide loaders read the global `OUT`, coupling generation to one mutable destination.
- `scripts/gen_router_list.py:263-374` — `main()` ignores argv, builds the whole ledger, and unconditionally opens `OUT` for writing.
- `scripts/gen_router_list.py:377-378` — guarded `__main__` block calls `main()` directly, but the write is not isolated behind argument parsing.
- `router-list.md:1-71,354-360` — canonical header, nine-column row format, per-page evidence cells, and progress section that must remain byte-for-byte unchanged by this fix.
- `scripts/test_config_param_defaults.py:1-10` — existing repository pattern for Python script regression tests (`importlib.util` + `unittest`).

## Root Cause Hypothesis

**Confirmed, high confidence:** CLI parsing and filesystem mutation are absent/coupled. `main()` neither accepts argv nor creates an `argparse.ArgumentParser`; therefore `--help` is just an ignored token and execution falls through to the unconditional `open(OUT, "w")`. Import itself is already side-effect-free because writes occur only inside `main()`. The loaders' reliance on global `OUT` also means a custom destination needs the path threaded through generation rather than assigned globally.

## Proposed Remediation

**Preferred**: add `argparse` at the start of `main(argv=None)` with `--out`/`-o` defaulting to the current product-root `router-list.md`. Extract the existing line-building body into a side-effect-free generator that accepts the destination path, reads existing header/status/guide from that same destination, and returns the exact same lines/counts. Keep the only write in the CLI path after successful parsing/generation. Preserve every existing format string, row field, header template, status lookup, and progress calculation.

**Alternatives**:

- Parse arguments in `main()` and temporarily mutate global `OUT`: smaller diff, but retains hidden global state and makes import/test isolation fragile.
- Add only an early `if "--help" in sys.argv` check: rejects normal argparse behavior and does not provide `--out`.

**Files likely to change**:

- `scripts/gen_router_list.py`
- `scripts/test_gen_router_list.py`（新增）
- `.specify/bugs/router-list-cli-side-effects-20261125/{assessment,fix,test}.md`
- `../docs/architecture/FULL_FEATURE_INVENTORY.md`（父会话明确要求更新 I-08 与 §7）

**Tests to add or update**:

- Importing the module leaves the canonical ledger unchanged.
- `main(["--help"])` exits 0 and leaves a controlled default-output sentinel unchanged.
- `main([])` still writes the controlled default output.
- `main(["--out", path])` and `main(["-o", path])` write only the requested destination.
- Generated header/table/progress structure is unchanged, and an existing row's status/evidence cell survives regeneration.

## Risks & Considerations

- The canonical ledger has concurrent uncommitted changes; it must not be regenerated or restored. Verify its baseline hash before and after all checks.
- Custom output paths should use their own existing header/evidence, not silently consume the canonical ledger.
- The current route tree is concurrently changing; regression tests should use a small fixture for exact structural assertions and avoid hard-coding today's 280-route total.
- No dependencies, network access, database access, UI interaction, or deployment are needed.

## RED/GREEN Evidence

- **RED（修复前，已实跑）**：controlled `OUT` reproduction above showed `help_SystemExit=None` and `help_output_unchanged=False`; the sentinel was replaced by a 280-row generated ledger. Canonical import hash remained unchanged.
- **GREEN（修复后，已实跑）**：`python3 -m unittest -v scripts/test_gen_router_list.py` exit 0、**5/5 OK**；scoped Python discover exit 0、**10/10 OK**；`py_compile`、`compileall`、`git diff --check` 均 exit 0。真实 `python3 scripts/gen_router_list.py --help` exit 0 且 canonical SHA-256 保持 `6e0e83a21a41268d7dd448e492678cee559fa9e0b694b2e804ca6cc4b8484cb6`。`HEAD` 与修复后生成器在 canonical 的两份临时副本上产物 SHA-256 均为 `b1518514d92cd04b2062fc704ba3387915f6014da276930018df8884bcaed17f`（**逐字节一致**）。
- **残留观察（非本轮修复）**：canonical 数据行为 `278 [x] + 2 [~]`，但既有进度页脚仍写 `266/8/6/0`；重新生成会按既有口径重算页脚。本轮遵守“不触碰 canonical”要求，未改写该文件。

## Open Questions

- None.
