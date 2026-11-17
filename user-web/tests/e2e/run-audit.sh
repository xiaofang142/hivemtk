#!/usr/bin/env bash
# 分批运行 UI 审计，避免单次超时。用法: run-audit.sh <START> <END>
set -e
cd "$(dirname "$0")/../.."
NODE=/usr/local/n/versions/node/22.12.0/bin/node
export E2E_BASE_URL="${E2E_BASE_URL:-http://localhost:8211}"
export E2E_USER="${E2E_USER:-admin}"
export E2E_ADMIN_PASSWORD="${E2E_ADMIN_PASSWORD:?E2E_ADMIN_PASSWORD 未设置，拒绝运行}"
export E2E_PASS="${E2E_PASS:-$E2E_ADMIN_PASSWORD}"
export ROUTES_FILE="${ROUTES_FILE:-/tmp/uiwalk/routes.json}"
export REPORT_FILE="${REPORT_FILE:-/tmp/uiwalk/report.jsonl}"
export START="${1:-0}"
export END="${2:-9999}"
echo ">>> UI-AUDIT batch START=$START END=$END base=$E2E_BASE_URL"
"$NODE" node_modules/@playwright/test/cli.js test tests/e2e/ui-audit.spec.js \
  --reporter=line --workers=1 2>&1 | tail -n +1
