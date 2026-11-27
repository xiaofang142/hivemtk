#!/usr/bin/env bash
# 显式 opt-in 的真实供应商验证；缺凭据时测试会 skip，台账必须记为 not-run，不能计为通过。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/user-server"

missing=()
for name in REAL_LLM_BASE_URL REAL_LLM_API_KEY REAL_LLM_MODEL; do
  if [[ -z "${!name:-}" ]]; then
    missing+=("$name")
  fi
done
if (( ${#missing[@]} > 0 )); then
  echo "not-run: 缺少真实供应商凭据 ${missing[*]}; 本脚本不会用 fake provider 伪造缓存命中" >&2
  exit 0
fi

echo "real-provider verification: base=$REAL_LLM_BASE_URL model=$REAL_LLM_MODEL"
go test ./internal/aiagent/llm -run '^TestRealProviderContextCaching$' -count=1 -v
