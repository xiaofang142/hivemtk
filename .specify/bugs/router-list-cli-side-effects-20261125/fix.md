# Bug Fix: router-list CLI help rewrites canonical evidence

- **Slug**: router-list-cli-side-effects-20261125
- **Fixed**: 2026-11-25
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

Added argparse-driven `--out`/`-o` handling and moved ledger construction into a side-effect-free `generate_router_list()` function. `--help` now exits before any route read or write; the CLI remains the only writer and still defaults to product-root `router-list.md`.

## Changes

| File | Change | Notes |
|------|--------|-------|
| `scripts/gen_router_list.py` | modified | Added `argparse`/`Path`; threaded the selected output through header/status/guide loading; extracted pure line generation; kept the sole write in `main()` after parsing. Existing format strings and counters unchanged. |
| `scripts/test_gen_router_list.py` | added | Five unittest regressions covering import safety, help safety, default output, both custom-output aliases, and exact fixture header/row/evidence/progress invariants. |
| `.specify/bugs/router-list-cli-side-effects-20261125/assessment.md` | added | Root cause, controlled reproduction, scope, risks, and RED/GREEN evidence. |
| `.specify/bugs/router-list-cli-side-effects-20261125/fix.md` | added | This fix record. |
| `../docs/architecture/FULL_FEATURE_INVENTORY.md` | modified | Parent-required I-08 closure and §7 verification row; updated only after final verification. |

## Diff Highlights (optional)

```python
def generate_router_list(out=None):
    """Build the ledger lines without writing; return lines and row counters."""
    ...
    return lines, len(rows), done, partial, todo, bug


def main(argv=None):
    parser = argparse.ArgumentParser(...)
    parser.add_argument("--out", "-o", type=Path, default=Path(OUT), ...)
    args = parser.parse_args(argv)
    lines, n_rows, done, partial, todo, bug = generate_router_list(args.out)
    with open(args.out, "w", encoding="utf-8") as fh:
        fh.write("\n".join(lines))
```

## Tests Added or Updated

- `scripts/test_gen_router_list.py::RouterListCliTests.test_import_does_not_modify_canonical_ledger` — import leaves canonical bytes unchanged.
- `scripts/test_gen_router_list.py::RouterListCliTests.test_help_exits_zero_without_modifying_default_output` — help exits 0 and preserves a controlled default-output sentinel.
- `scripts/test_gen_router_list.py::RouterListCliTests.test_default_invocation_writes_default_output` — no-argument CLI still writes the default destination.
- `scripts/test_gen_router_list.py::RouterListCliTests.test_custom_output_aliases_write_only_requested_file` — `--out` and `-o` write only the requested path.
- `scripts/test_gen_router_list.py::RouterListCliTests.test_generated_header_rows_and_evidence_are_preserved` — nine-column rows, child-route expansion, preserved header/status/evidence, and progress counters remain intact.

## Local Verification

- RED: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v scripts/test_gen_router_list.py` → exit 1, **5 tests, 3 failures**; help did not raise `SystemExit`, and neither `--out` nor `-o` created the requested file. Default generation, fixture format/evidence, and import safety passed.
- GREEN: same regression command → exit 0, **5/5 OK**.
- Scoped Python suite: `PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s scripts -p 'test_*.py' -v` → exit 0, **10/10 OK**.
- Syntax: `python3 -m py_compile scripts/gen_router_list.py scripts/test_gen_router_list.py` and `python3 -m compileall -q scripts/gen_router_list.py scripts/test_gen_router_list.py` → exit 0, no output.
- Actual CLI: `PYTHONDONTWRITEBYTECODE=1 python3 scripts/gen_router_list.py --help` → exit 0; usage lists `--out, -o OUT`.
- Canonical safety: SHA-256 before/after remained `6e0e83a21a41268d7dd448e492678cee559fa9e0b694b2e804ca6cc4b8484cb6`.
- Output preservation: executed `HEAD` generator and fixed generator against separate temporary copies of canonical; both produced SHA-256 `b1518514d92cd04b2062fc704ba3387915f6014da276930018df8884bcaed17f` (**byte-identical**).
- Diff hygiene: `git diff --check` → exit 0, no output.

## Deviations from Assessment

- No implementation deviation. The parent session explicitly required updating the outer workspace inventory, so that documentation file is included in addition to the product-repo script/test/spec-kit files.
- Per the parent session's explicit requirement, post-fix GREEN evidence is appended to `assessment.md` after final verification; the assessment's root cause and remediation contract are unchanged.

## Follow-ups

- Canonical `router-list.md` currently has a stale progress footer (`266/8/6/0`) while its 280 data rows are `278 [x] + 2 [~]`; regeneration recalculates the footer. This predates I-08 and was deliberately not repaired here because the canonical file must remain untouched.
- No commit, push, deployment, UI test, database test, or network-dependent suite was run, per the parent session's instructions.
