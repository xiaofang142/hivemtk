#!/usr/bin/env bash
# smoke-laya.sh —— Laya 决策端点冒烟测试（对齐 smoke-test.sh）
set -uo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=_common.sh
source "$SCRIPT_DIR/_common.sh"
: "${LAYA_PORT:=8210}"

STRICT=0
for arg in "$@"; do case "$arg" in --strict) STRICT=1 ;; esac; done

pass=0; fail=0
print_inference_host_banner
echo "[smoke-laya] 目标：http://127.0.0.1:${LAYA_PORT}"
echo

echo "=== [0/2] 健康检查 ==="
code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://127.0.0.1:${LAYA_PORT}/health" || echo "000")
if [ "$code" = "200" ]; then echo "  ✅ /health 200"; pass=$((pass+1)); else echo "  ❌ /health $code"; fail=$((fail+1)); fi
echo

echo "=== [1/2] POST /v1/decide ==="
code=$(curl -s -o /tmp/laya.json -w "%{http_code}" -X POST "http://127.0.0.1:${LAYA_PORT}/v1/decide" \
  -H 'Content-Type: application/json' \
  -d '{"state":"客户说要退款","questions":{"intent":{"type":"choice","instructions":"识别意图","criteria":{"refund":"退款","greet":"打招呼"}}}}' \
  --max-time 120 || echo "000")
echo "  HTTP $code"
if [ "$code" = "200" ] && grep -q '"result"' /tmp/laya.json 2>/dev/null; then
  echo "  ✅ decide 返回 result"; pass=$((pass+1))
  head -c 400 /tmp/laya.json; echo
else
  echo "  ❌ decide 未返回预期内容（见 /tmp/laya.json）"; fail=$((fail+1))
  head -c 400 /tmp/laya.json 2>/dev/null; echo
fi
echo

echo "=== [2/2] POST /v1/decide/batch ==="
code=$(curl -s -o /tmp/laya_batch.json -w "%{http_code}" -X POST "http://127.0.0.1:${LAYA_PORT}/v1/decide/batch" \
  -H 'Content-Type: application/json' \
  -d '{"states":["hi","我要退款"],"questions":{"intent":{"type":"choice","instructions":"识别意图","criteria":{"refund":"退款","greet":"打招呼"}}}}' \
  --max-time 120 || echo "000")
echo "  HTTP $code"
if [ "$code" = "200" ] && grep -q '"results"' /tmp/laya_batch.json 2>/dev/null; then
  echo "  ✅ batch 返回 results"; pass=$((pass+1))
else
  echo "  ❌ batch 未返回预期内容"; fail=$((fail+1))
  head -c 400 /tmp/laya_batch.json 2>/dev/null; echo
fi
echo

echo "============================================================"
echo "[smoke-laya] 结果：通过 $pass / 失败 $fail"
if [ "$fail" = "0" ]; then echo "✅ 全部通过"; exit 0; else echo "❌ 存在失败（日志：$HIVEMTK_RUNTIME_DIR/laya.log）"; exit 1; fi
