#!/usr/bin/env bash
# 测试 license-compliance-scan.sh 的核心判定
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SCAN_SCRIPT="$SCRIPT_DIR/license-compliance-scan.sh"

if [[ ! -x "$SCAN_SCRIPT" ]]; then
    echo "FAIL: scan script not found or not executable: $SCAN_SCRIPT"; exit 1
fi

# 准备测试 fixture
TMPDIR=$(mktemp -d)
trap "rm -rf $TMPDIR" EXIT

# Case 1: 纯本地 .git 仓库（无 remote）→ 应 PASS（remote 空仅记 WARN 级提示，官方 remote 缺失判定为 WARN）
mkdir -p "$TMPDIR/local"
cd "$TMPDIR/local"
git init -q
git -C "$TMPDIR/local" commit -q --allow-empty -m "init" 2>/dev/null || true
mkdir -p "$TMPDIR/local/scripts"
cp "$SCAN_SCRIPT" "$TMPDIR/local/scripts/license-compliance-scan.sh"
result=$("$TMPDIR/local/scripts/license-compliance-scan.sh" --dry-run --no-color 2>&1)
if ! echo "$result" | grep -qE "VERDICT: (PASS|WARN)"; then
    echo "FAIL: local-only should PASS or WARN"; echo "$result"; exit 1
fi
cd - > /dev/null

# Case 2: 显式 --public-url 私网 → PASS（维度2 私网 IP ✓）
result2=$(PUBLIC_BASE_URL= bash "$SCAN_SCRIPT" --dry-run --no-color --public-url http://192.168.1.10:8204 2>&1)
if ! echo "$result2" | grep -q "私网 IP"; then
    echo "FAIL: private url should be detected as 私网"; echo "$result2"; exit 1
fi

# Case 3: 退出码——--dry-run 非 FAIL 场景必须为 0
if ! bash "$SCAN_SCRIPT" --dry-run --no-color > /dev/null 2>&1; then
    echo "FAIL: dry-run exit code should be 0 (PASS/WARN)"; exit 1
fi

echo "test passed"
