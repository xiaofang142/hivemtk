#!/usr/bin/env bash
# FA-RELEASE-01 合成日志测试：验证 gate_verdict 分类逻辑，不执行真实 go/ssh/发布。
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${SCRIPT_DIR}/test_gate.sh"

TMPD="$(mktemp -d "${TMPDIR:-/tmp}/gate-test.XXXXXX")"
trap 'rm -rf "${TMPD}"' EXIT

pass=0 fail=0
check() {
  local name="$1" expected="$2" log="$3" rc="$4"
  local got
  got="$(gate_verdict "${log}" "${rc}")"
  if [[ "${got}" == "${expected}" ]]; then
    echo "PASS ${name} → ${got}"
    pass=$((pass+1))
  else
    echo "FAIL ${name} → got=${got} want=${expected}"
    fail=$((fail+1))
  fi
}

# 1. 纯业务失败 → BLOCK
cat > "${TMPD}/biz.log" <<'EOF'
=== RUN   TestFoo
--- FAIL: TestFoo (0.00s)
    foo_test.go:12: want 1 got 2
FAIL
FAIL	server/internal/x	0.01s
EOF
check "pure_business_failure" BLOCK "${TMPD}/biz.log" 1

# 2. 纯 DB 门禁失败 → UNVERIFIED
cat > "${TMPD}/db.log" <<'EOF'
=== RUN   TestRepo
--- FAIL: TestRepo (0.00s)
    repo_test.go:5: test password is missing; set POSTGRES_TEST_PASSWORD
FAIL
FAIL	server/internal/repo	0.01s
EOF
check "pure_db_gate" UNVERIFIED "${TMPD}/db.log" 1

# 3. 混合：DB 门禁 + 业务失败 → BLOCK
cat > "${TMPD}/mixed.log" <<'EOF'
=== RUN   TestRepo
--- FAIL: TestRepo (0.00s)
    repo_test.go:5: test password is missing; set POSTGRES_TEST_PASSWORD
=== RUN   TestFoo
--- FAIL: TestFoo (0.00s)
    foo_test.go:12: want 1 got 2
FAIL
EOF
check "mixed_failure" BLOCK "${TMPD}/mixed.log" 1

# 4. 编译失败（无 --- FAIL 行，rc!=0）→ BLOCK
cat > "${TMPD}/build.log" <<'EOF'
# server/internal/x
internal/x/a.go:10:2: undefined: someSymbol
FAIL	server/internal/x [build failed]
EOF
check "build_failure" BLOCK "${TMPD}/build.log" 1

# 5. 进程异常/panic（rc!=0，无 DB 门禁）→ BLOCK
cat > "${TMPD}/panic.log" <<'EOF'
panic: runtime error: invalid memory address
EOF
check "panic_failure" BLOCK "${TMPD}/panic.log" 2

# 6. 零用例/空跑（rc=0，无执行证据）→ BLOCK
printf -- '?   \tserver/internal/x\t[no test files]\n' > "${TMPD}/empty.log"
check "zero_cases" BLOCK "${TMPD}/empty.log" 0

# 7. 全绿（rc=0，有 ok 行）→ GREEN
cat > "${TMPD}/green.log" <<'EOF'
=== RUN   TestFoo
--- PASS: TestFoo (0.00s)
PASS
ok  	server/internal/x	0.01s
EOF
check "all_green" GREEN "${TMPD}/green.log" 0

echo "--- result: pass=${pass} fail=${fail}"
[[ "${fail}" -eq 0 ]]
