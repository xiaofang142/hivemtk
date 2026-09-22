#!/usr/bin/env bash
# deep_lib.sh - 深度 API 测试公共库：真实调用 + 数据库结果校验
# 用法: source deep_lib.sh 后调用 api / dbq / assert_* 等函数
set +u
export PATH="/opt/homebrew/bin:/opt/homebrew/sbin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

# ---- 连接配置 ----
BASE="http://127.0.0.1:8204"
PLAT_BASE="http://127.0.0.1:8205"
PGHOST=127.0.0.1
PGPORT=8232
PGUSER=admin
# 口令仅从环境注入（本文件不落任何明文字面量）：
#   user 库    ← hivemtk/.env 的 POSTGRES_PASSWORD
#   platform 库 ← hivemtk-platform/.env 的 POSTGRES_PASSWORD，导出为 PLATFORM_POSTGRES_PASSWORD
# 本机跑法： set -a && . ../.env && set +a && export PLATFORM_POSTGRES_PASSWORD=<平台库口令>
: "${POSTGRES_PASSWORD:?缺少 POSTGRES_PASSWORD（user 库口令，见 hivemtk/.env）}"
: "${PLATFORM_POSTGRES_PASSWORD:?缺少 PLATFORM_POSTGRES_PASSWORD（platform 库口令，见 hivemtk-platform/.env）}"
PGPASSWORD="$POSTGRES_PASSWORD"
PGDB=user_db
PLAT_PGPASSWORD="$PLATFORM_POSTGRES_PASSWORD"
PLAT_PGDB=platform_db

# ---- token 管理 ----
TOKEN=""
PLAT_TOKEN=""

mtk_login() {
  # 口令来源与 bootstrap 同一条链：显式入参 > SEED_PASSWORD > 仓库公开的演示默认值
  local pw="${1:-${SEED_PASSWORD:-Seed@123456}}"
  local resp
  resp=$(curl -s --max-time 15 -X POST "$BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"$pw\"}")
  TOKEN=$(printf '%s' "$resp" | jq -r '.data.token // empty')
  if [ -z "$TOKEN" ]; then echo "LOGIN_FAIL: $resp" >&2; return 1; fi
  return 0
}

mtk_plat_login() {
  local pw
  pw=$(grep '^PLATFORM_ADMIN_PASSWORD=' /Users/xiaofang/Documents/www/go/hivemtk/hivemtk-platform/.env | sed 's/^PLATFORM_ADMIN_PASSWORD=//')
  local resp
  resp=$(curl -s --max-time 15 -X POST "$PLAT_BASE/api/auth/login" \
    -H 'Content-Type: application/json' \
    -d "{\"username\":\"admin\",\"password\":\"$pw\"}")
  PLAT_TOKEN=$(printf '%s' "$resp" | jq -r '.data.token // empty')
  if [ -z "$PLAT_TOKEN" ]; then echo "PLAT_LOGIN_FAIL: $resp" >&2; return 1; fi
  return 0
}

# ---- HTTP 调用（带 429 退避重试） ----
# 用法: api METHOD PATH [BODY_JSON]
# 输出到全局: API_CODE, API_BODY, API_HTTP
api() {
  local method="$1"; local path="$2"; local body="${3:-}"
  local url="$BASE$path"
  local attempt=0 max=6 delay=1
  API_HTTP=""; API_BODY=""; API_CODE=""
  while [ $attempt -lt $max ]; do
    attempt=$((attempt+1))
    local hdr=(-H "Authorization: Bearer $TOKEN")
    local curl_args=(-s --max-time 20 -w '\n__HTTP__%{http_code}')
    curl_args+=(-X "$method")
    if [ -n "$body" ]; then
      curl_args+=(-H 'Content-Type: application/json' -d "$body")
    fi
    local out
    if [ -n "$TOKEN" ]; then
      out=$(curl "${curl_args[@]}" -H "Authorization: Bearer $TOKEN" "$url" 2>/dev/null)
    else
      out=$(curl "${curl_args[@]}" "$url" 2>/dev/null)
    fi
    local hcode="${out##*__HTTP__}"
    local b="${out%__HTTP__*}"
    API_HTTP="$hcode"; API_BODY="$b"
    if [ "$hcode" = "429" ]; then
      sleep "$delay"; delay=$((delay*2)); continue
    fi
    API_CODE=$(printf '%s' "$b" | jq -r '.code // empty' 2>/dev/null)
    return 0
  done
  return 0
}

# 平台端调用
api_plat() {
  local method="$1"; local path="$2"; local body="${3:-}"
  local url="$PLAT_BASE$path"
  local attempt=0 max=6 delay=1
  API_HTTP=""; API_BODY=""; API_CODE=""
  while [ $attempt -lt $max ]; do
    attempt=$((attempt+1))
    local out
    if [ -n "$body" ]; then
      out=$(curl -s --max-time 20 -w '\n__HTTP__%{http_code}' -X "$method" \
        -H "Authorization: Bearer $PLAT_TOKEN" -H 'Content-Type: application/json' \
        -d "$body" "$url" 2>/dev/null)
    else
      out=$(curl -s --max-time 20 -w '\n__HTTP__%{http_code}' -X "$method" \
        -H "Authorization: Bearer $PLAT_TOKEN" "$url" 2>/dev/null)
    fi
    local hcode="${out##*__HTTP__}"; local b="${out%__HTTP__*}"
    API_HTTP="$hcode"; API_BODY="$b"
    if [ "$hcode" = "429" ]; then sleep "$delay"; delay=$((delay*2)); continue; fi
    API_CODE=$(printf '%s' "$b" | jq -r '.code // empty' 2>/dev/null)
    return 0
  done
  return 0
}

# ---- 数据库查询 ----
# dbq "SQL" -> 原始结果；dbqv "SQL" -> 单行单值
dbq() {
  PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" -tAc "$1" 2>&1
}
dbqv() {
  PGPASSWORD="$PGPASSWORD" psql -h "$PGHOST" -p "$PGPORT" -U "$PGUSER" -d "$PGDB" -tAc "$1" 2>/dev/null | head -1 | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}
dbq_plat() {
  PGPASSWORD="$PLAT_PGPASSWORD" psql -h 127.0.0.1 -p 8201 -U admin -d "$PLAT_PGDB" -tAc "$1" 2>&1
}
dbqv_plat() {
  PGPASSWORD="$PLAT_PGPASSWORD" psql -h 127.0.0.1 -p 8201 -U admin -d "$PLAT_PGDB" -tAc "$1" 2>/dev/null | head -1 | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'
}

# ---- 断言工具 ----
PASS=0; FAIL=0; CASES=()
pass() { PASS=$((PASS+1)); echo "  [PASS] $1"; }
fail() { FAIL=$((FAIL+1)); echo "  [FAIL] $1"; CASES+=("FAIL:$1"); }
info() { echo "  [INFO] $1"; }

# 提取 data 字段某路径的值
jdata() { printf '%s' "$API_BODY" | jq -r --arg p "$1" '.data | (if type=="object" then (getpath($p|split("."))) else . end) // empty' 2>/dev/null; }
# 提取 data.list 中第 n 个元素的字段
jlist() { printf '%s' "$API_BODY" | jq -r --arg n "$1" --arg f "$2" '.data.list['"$n"'] | .[$f] // empty' 2>/dev/null; }
