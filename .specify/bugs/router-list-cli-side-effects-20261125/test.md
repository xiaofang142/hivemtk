# Bug Verification: router-list CLI help rewrites canonical evidence

- **Slug**: router-list-cli-side-effects-20261125
- **Tested**: 2026-11-25
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

The original symptom no longer reproduces: real `--help` exits 0 and leaves canonical `router-list.md` byte-for-byte unchanged. Default generation and both custom-output aliases work, the generated format/evidence behavior is unchanged, and all scoped checks pass. No unrelated regression was found in the exercised Python paths.

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (post-fix) | `PYTHONDONTWRITEBYTECODE=1 python3 scripts/gen_router_list.py --help` | pass | Exit 0; usage lists `--out, -o OUT`; canonical SHA-256 unchanged. |
| New / updated tests | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts/test_gen_router_list.py` | pass | **5/5 OK** in 0.080s. |
| Regression suite | `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_*.py' -v` | pass | **10/10 OK** in 0.077s; includes existing config-default tests. |
| Syntax / compile | `python3 -m py_compile scripts/gen_router_list.py scripts/test_gen_router_list.py`；`python3 -m compileall -q scripts/gen_router_list.py scripts/test_gen_router_list.py` | pass | Both exit 0 with no output. |
| Output preservation | Run `HEAD` generator and fixed generator against separate temporary copies of canonical | pass | Both SHA-256 `b1518514d92cd04b2062fc704ba3387915f6014da276930018df8884bcaed17f`; byte-identical. |
| Canonical safety | `sha256sum router-list.md` before/after all checks | pass | `6e0e83a21a41268d7dd448e492678cee559fa9e0b694b2e804ca6cc4b8484cb6` unchanged. |
| Diff hygiene | `git diff --check` | pass | Exit 0, no output. |
| Inventory closure | Update root `docs/architecture/FULL_FEATURE_INVENTORY.md` I-08 and §7 | pass | Marked 已修复 with RED/GREEN, hash, byte-preservation, and gate evidence; no new ledger. |
| Concurrent-file guard | Scoped `git status --short` | pass | `router-list.md` remains pre-existing `M`; `user-server/internal/router/router.go` remains pre-existing `MM`; neither changed by this task. |
| UI / external / deployment | — | skipped | Not relevant to this CLI bug and prohibited by parent instructions; no commit/push/deploy. |

## Output Excerpts

```text
test_custom_output_aliases_write_only_requested_file ... ok
test_default_invocation_writes_default_output ... ok
test_generated_header_rows_and_evidence_are_preserved ... ok
test_help_exits_zero_without_modifying_default_output ... ok
test_import_does_not_modify_canonical_ledger ... ok
Ran 5 tests in 0.080s
OK
```

```text
Ran 10 tests in 0.077s
OK
```

```text
usage: gen_router_list.py [-h] [--out OUT]
options:
  -h, --help     show this help message and exit
  --out, -o OUT  输出文件路径（默认：产品根目录 router-list.md）
```

```text
before_fix_sha256=b1518514d92cd04b2062fc704ba3387915f6014da276930018df8884bcaed17f
after_fix_sha256=b1518514d92cd04b2062fc704ba3387915f6014da276930018df8884bcaed17f
generated_output_byte_identical=True
canonical_sha256_unchanged=True
```

## Residual Risks

- Canonical `router-list.md` has a pre-existing stale progress footer (`266/8/6/0`) while its 280 data rows are `278 [x] + 2 [~]`. Regeneration recalculates it; this task intentionally did not rewrite canonical.
- The route tree and broader worktree are concurrently changing. Exact-output preservation was proven against the current canonical snapshot and `HEAD` generator, not against future route changes.
- Only the scoped Python unittest discovery pattern was run; no Go, frontend, database, UI, network-dependent, or deployment suite was run.
- No commit, push, staging, reset, stash, or deployment was performed.

## Recommendation

Close I-08 as verified. Keep the canonical ledger untouched; any future regeneration should be an explicit, separately reviewed action.
