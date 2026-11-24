#!/usr/bin/env bash
# =============================================================================
# check-feature-doc.test.sh —— 功能文档工具的仓根定位回归
#
# 用法：bash scripts/check-feature-doc.test.sh
#   rc=0：checker normal/strict 与 coverage table/JSON 均从当前产品根定位成功
#   rc=1：任一工具定位错目录、内容校验失败或 JSON 输出不合法
#
# 本测试只使用真实产品根与真实文档，不造临时仓、不改生产文档；strict 的存量
# WARN 仍按工具原语义判红，不在此处放宽。
# =============================================================================

set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DOCS_DIR="$PROJECT_ROOT/docs/marketing-features"
WORK=$(mktemp -d "${TMPDIR:-/tmp}/check-feature-doc.test.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

FAILURES=0

show_output() {
  sed -n '1,8p' "$1" | sed 's/^/       | /'
}

run_checker() {
  local mode="$1"
  shift
  local out="$WORK/checker-$mode.out"
  local rc

  (cd "$PROJECT_ROOT" && bash scripts/check-feature-doc.sh "$@") >"$out" 2>&1
  rc=$?

  if [ "$rc" -eq 0 ] \
    && grep -qF -- "目录: $DOCS_DIR" "$out" \
    && ! grep -qF -- "目录不存在" "$out"; then
    printf 'PASS checker %-6s rc=0 docs_dir=%s\n' "$mode" "$DOCS_DIR"
  else
    printf 'FAIL checker %-6s rc=%s，未从产品根完成校验\n' "$mode" "$rc"
    show_output "$out"
    FAILURES=$((FAILURES + 1))
  fi
}

run_coverage_table() {
  local out="$WORK/coverage-table.out"
  local rc

  (cd "$PROJECT_ROOT" && bash scripts/feature-doc-coverage-report.sh) >"$out" 2>&1
  rc=$?

  if [ "$rc" -eq 0 ] \
    && grep -qF -- "> **目录**: \`$DOCS_DIR\`" "$out" \
    && ! grep -qF -- "目录不存在" "$out"; then
    printf 'PASS coverage table rc=0 docs_dir=%s\n' "$DOCS_DIR"
  else
    printf 'FAIL coverage table rc=%s，未从产品根完成报告\n' "$rc"
    show_output "$out"
    FAILURES=$((FAILURES + 1))
  fi
}

run_coverage_json() {
  local out="$WORK/coverage-json.out"
  local parse_out="$WORK/coverage-json-parse.out"
  local rc

  (cd "$PROJECT_ROOT" && bash scripts/feature-doc-coverage-report.sh --json) >"$out" 2>&1
  rc=$?

  if [ "$rc" -eq 0 ] \
    && python3 - "$out" "$DOCS_DIR" >"$parse_out" 2>&1 <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    report = json.load(handle)

if report.get("docs_dir") != sys.argv[2]:
    raise SystemExit(
        f"docs_dir mismatch: expected {sys.argv[2]!r}, got {report.get('docs_dir')!r}"
    )
if not isinstance(report.get("total_docs"), int) or report["total_docs"] < 1:
    raise SystemExit("total_docs 必须为非零整数")
PY
  then
    printf 'PASS coverage json  rc=0 docs_dir=%s\n' "$DOCS_DIR"
  else
    printf 'FAIL coverage json  rc=%s，未从产品根输出合法 JSON\n' "$rc"
    show_output "$out"
    if [ -s "$parse_out" ]; then
      sed -n '1,4p' "$parse_out" | sed 's/^/       | /'
    fi
    FAILURES=$((FAILURES + 1))
  fi
}

if [ ! -f "$PROJECT_ROOT/deploy/deploy-api.sh" ] || [ ! -d "$DOCS_DIR" ]; then
  printf 'FAIL 产品根或功能文档目录不存在: root=%s docs=%s\n' "$PROJECT_ROOT" "$DOCS_DIR"
  exit 1
fi

run_checker normal
run_checker strict --strict
run_coverage_table
run_coverage_json

if [ "$FAILURES" -ne 0 ]; then
  printf '失败 %s 格\n' "$FAILURES"
  exit 1
fi

printf '全部通过（4/4）\n'
exit 0
