#!/usr/bin/env bash
# FA-RELEASE-01: 发布门测试结果的结构化分类（可独立测试，不执行真实发布）。
# gate_verdict <logfile> <test_rc> → 输出 GREEN | UNVERIFIED | BLOCK
#   GREEN      : 测试真跑且全绿（rc=0，无构建/进程异常，有测试执行证据）
#   UNVERIFIED : 全部失败均为 DB 环境门禁（缺 POSTGRES_TEST_PASSWORD），标未验、不阻断
#   BLOCK      : 任一业务失败 / 混合故障 / 编译或进程异常 / 零用例 —— 必须阻断发布
# 判定只依赖日志内容与退出码，不调用 go/ssh，便于用合成日志夹具验证。

gate_verdict() {
  local log_file="${1:-}" test_rc="${2:-0}"
  if [[ -z "${log_file}" || ! -f "${log_file}" ]]; then
    echo "BLOCK"
    return
  fi

  # 逐用例分类：一条 --- FAIL 块内若含 "test password is missing" 视为 DB 门禁，否则业务失败。
  local counts
  counts="$(awk '
    /^--- FAIL:/ {
      if (intest && !gated) business++
      intest=1; gated=0; next
    }
    intest && /test password is missing/ { gated=1 }
    intest && /^(--- |=== RUN|PASS$|FAIL$|^ok[[:space:]]|^\?[[:space:]])/ {
      if (!gated) business++
      intest=0
    }
    END {
      if (intest && !gated) business++
      print business+0, gated+0
    }
  ' "${log_file}")"
  local business_fails db_gated
  business_fails="$(echo "${counts}" | awk '{print $1}')"
  db_gated="$(echo "${counts}" | awk '{print $2}')"

  # 构建失败 / 初始化失败 / 进程异常：这些错误通常没有 --- FAIL 行，不能误报全绿。
  local build_fail
  build_fail="$(grep -cE '\[build failed\]|\[setup failed\]|^panic:|undefined:|cannot use |not enough arguments|SIGSEGV|signal: killed' "${log_file}" || true)"

  # 测试执行证据：至少出现一条包级 ok / 用例 PASS / --- PASS / 具体包结果。
  local executed
  executed="$(grep -cE '^ok[[:space:]]|^--- PASS:|^PASS$|^FAIL[[:space:]]' "${log_file}" || true)"

  if [[ "${business_fails}" -gt 0 || "${build_fail}" -gt 0 ]]; then
    echo "BLOCK"
  elif [[ "${test_rc}" != "0" && "${db_gated}" -eq 0 ]]; then
    # 非零退出码但既无业务 FAIL 也无 DB 门禁 → 编译失败/零用例/进程异常，不能通过。
    echo "BLOCK"
  elif [[ "${test_rc}" != "0" && "${db_gated}" -gt 0 ]]; then
    echo "UNVERIFIED"
  elif [[ "${executed}" -eq 0 ]]; then
    # rc=0 却没有任何测试执行证据 → 零用例/空跑，不能当全绿。
    echo "BLOCK"
  else
    echo "GREEN"
  fi
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  gate_verdict "${1:-}" "${2:-0}"
fi
