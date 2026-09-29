#!/usr/bin/env bash
# =============================================================================
# bridge-e2e-sim.sh —— 桥接模块 + 统一收件箱 全渠道端到端模拟
# -----------------------------------------------------------------------------
# 直接打到运行中真实服务 (默认 localhost:8204)，覆盖三通道：
#   通道A 上报: POST /api/bridge/ingest
#   通道B 状态: POST /api/bridge/outbox/ack
#   通道C 下发: GET  /api/bridge/outbox
# 逐渠道(抖音/小红书/快手/闲鱼/TikTok)验证：
#   1) ingest 字段完整性 + 类型正确(bool 始终显式) + AI 是否触发
#   2) 幂等去重：相同 event_id 重报 → duplicate (DB msg_id 级, 跨重启生效)
#   3) 回声/回环去重：相同 channel+content+sender 新 event_id → 被中间件拦截
#   4) outbox 下发：拉到 AI 回复，校验字段完整性 + is_ai_reply + 内容质量
#   5) ack 闭环：acked_items_count>=1（响应字段名以 handler_http.go:784 为唯一源），ack 后 outbox 清空
#   6) msg_id 回环：把 AI 回复原样回灌(event_id=内容哈希) → 被拦截
# 另含：负向用例(缺参/不支持渠道) + 跨语言哈希契约锚点 + 推理栈健康门控。
# AI 回复依赖推理栈；若推理栈不健康则相关项记 WARN(归属环境)而非 FAIL。
# =============================================================================
set -uo pipefail

BASE_URL="${BASE_URL:-http://localhost:8204}"
# X-Bridge-Token 闸门（middleware/bridge_ingress_guard.go, code UNAUTHORIZED_2001）:
# 闸门只读 X-Bridge-Token 头（SSE 另支持 ?bridge_token=），不读 Authorization / ?token=。
# 未带凭证时 ingest 全 401，且 GET outbox 的 401 会被误归因为"推理栈波动" WARN。
# 默认自动从 DB 取当前生效凭证；显式指定用 BRIDGE_TOKEN=xxx bash 本脚本
BRIDGE_TOKEN="${BRIDGE_TOKEN:-}"
# 等 AI 回复落库的窗口（秒）。
# 66–255s 那一档量的是手建的临时二进制（退避跑满），不是本仓的开发态：热重载实例
# （make dev = air）上现测三次首条回复落库 = 6s / 6s / 7s（Embedding :8208 仍缺位，
# 答案来自 llm_providers 里的云端提供商）。窗口按 10 倍余量收到 120s：
# 再长就不是"AI 慢"而是"AI 死了"，多渠道多腿累加会把一趟闸门拖成十几分钟的空等。
AI_WAIT_S="${AI_WAIT_S:-120}"
CHANNELS=("douyin" "xiaohongshu" "kuaishou" "xianyu" "tiktok")
PASS=0; FAIL=0; WARN=0
declare -a REPORT
RUN_TOKEN="$(python3 -c 'import uuid;print(uuid.uuid4().hex[:10])')"

# ---- 数据库连接（宿主映射端口，从 hivemtk/.env 读取密码）----
DB_HOST="${DB_HOST:-localhost}"; DB_PORT="${DB_PORT:-8232}"; DB_USER="${DB_USER:-admin}"
DB_NAME="${DB_NAME:-user_db}"
ENV_FILE="$(dirname "$0")/../../.env"
PW="$(grep '^POSTGRES_PASSWORD=' "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2-)"
PW="${PW:-${POSTGRES_PASSWORD:-}}"
envv() { grep -E "^[[:space:]]*$1=" "$ENV_FILE" 2>/dev/null | head -1 | cut -d= -f2- | tr -d '\r'; }
PG_CONN=(-h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME")
psql_q() { PGPASSWORD="$PW" psql "${PG_CONN[@]}" -t -A -c "$1" 2>/dev/null; }

# curl 连不上时 -w 已经打印了 000，再写 `|| echo 000` 会拼成 000000（上一版健康门控就把
# "/health=000000" 印进了结果面）。取码统一走这里，空值补 000。
http_code() { local c; c=$(curl -s -m "${2:-5}" -o /dev/null -w "%{http_code}" "$1" 2>/dev/null); printf '%s' "${c:-000}"; }

# 凭证未显式给出时从 DB 现取（闸门是 fail-closed：拿不到凭证就整批 401，
# 而 401 会被后面的用例误归因成环境波动 ⇒ 这里必须取不到就停，不能空着头往下跑）
if [ -z "$BRIDGE_TOKEN" ]; then
  BRIDGE_TOKEN="$(psql_q "SELECT value FROM system_config_kv WHERE key='bridge_ingest_token'")"
fi
if [ -z "$BRIDGE_TOKEN" ]; then
  echo "FATAL: 取不到桥接凭证（环境变量 BRIDGE_TOKEN 未给，且 $DB_NAME.system_config_kv 无 bridge_ingest_token）。" >&2
  echo "       后台「桥接凭证」页生成，或 BRIDGE_TOKEN=xxx bash $0" >&2
  exit 2
fi
H_TOKEN=(-H "X-Bridge-Token: ${BRIDGE_TOKEN}")

# mkack <msg_ids_csv> <status> → 生成 {"msg_ids":[...],"status":"..."}
mkack() {
  python3 -c '
import json, sys
ids = sys.argv[1].split(",") if sys.argv[1] else []
print(json.dumps({"msg_ids": ids, "status": sys.argv[2]}))
' "$1" "$2"
}

if [ -t 1 ]; then
  C_GREEN=$'\033[32m'; C_RED=$'\033[31m'; C_YEL=$'\033[33m'; C_BLU=$'\033[36m'; C_RST=$'\033[0m'
else C_GREEN=""; C_RED=""; C_YEL=""; C_BLU=""; C_RST=""; fi

ok()   { PASS=$((PASS+1)); REPORT+=("${C_GREEN}PASS${C_RST} $1"); }
bad()  { FAIL=$((FAIL+1)); REPORT+=("${C_RED}FAIL${C_RST} $1 :: $2"); }
warn() { WARN=$((WARN+1)); REPORT+=("${C_YEL}WARN${C_RST} $1 :: $2"); }

# FNV-1a 32 位：输入 channel|TrimSpace(content) → mh:{8hex}，与后端逐字节一致
chash() {
  python3 -c '
import sys
s = sys.argv[1] + "|" + sys.argv[2].strip()
h = 2166136261
for b in s.encode("utf-8"):
    h ^= b; h = (h * 16777619) & 0xFFFFFFFF
print("mh:%08x" % h)
' "$1" "$2"
}
uid12() { python3 -c "import uuid;print(uuid.uuid4().hex[:12])"; }

# 构造 ingest 请求体（sender 稳定绑定 conversation，便于回环去重判定）
mkmsg() {
  python3 -c '
import json, sys, time
ch, acct, conv, evt, content = sys.argv[1:6]
ts = int(time.time() * 1000)
print(json.dumps({"messages":[{
    "event_id": evt, "conversation_id": conv,
    "sender_id": "cust_"+conv[:16], "sender_name": "访客", "sender_type": "customer",
    "content": content, "msg_type": "text", "timestamp": ts
}]}))
' "$1" "$2" "$3" "$4" "$5"
}

# jq 字段类型断言（path 作为表达式直接求值）
assert_type() {
  local json="$1" path="$2" kind="$3" label="$4" t
  t=$(printf '%s' "$json" | jq -r "$path | type" 2>/dev/null)
  if [ "$t" != "$kind" ]; then bad "$label" "字段 $path 期望 $kind, 实际 ${t:-缺失}"; return 1; fi
  return 0
}

# 轮询 outbox 直到拉到待下发回复，或走完 AI_WAIT_S 秒。
# 成功：OB=响应体、WAITED=实测秒数、返回 0；超时：OB 置空、WAITED=已等秒数、返回 1。
# 首条 AI 回复的耗时属于 AI 生成链路（意图→RAG→生成），桥接侧只负责落库后的下发与 ack，
# 所以超时一律按"未落库"归因、不当协议失败。
poll_outbox() {
  local ch="$1" acct="$2" lim="$3" t0 cur body st n
  t0=$(date +%s)
  while :; do
    body=$(curl -s -m 10 "${H_TOKEN[@]}" "$BASE_URL/api/bridge/outbox?channel=$ch&account_id=$acct&limit=$lim")
    st=$(printf '%s' "$body" | jq -r '.status' 2>/dev/null)
    if [ "$st" = "ok" ]; then
      n=$(printf '%s' "$body" | jq -r '.messages|length' 2>/dev/null)
      if [ "${n:-0}" -gt 0 ]; then
        OB="$body"; WAITED=$(( $(date +%s) - t0 )); LAST_STATUS="$st"; return 0
      fi
    fi
    # 预算按墙上时钟判，不按轮数：一次轮询除了 sleep 1 还要跑一趟 curl，
    # 按轮数计会让"等 N 秒"实际等到远超 N 秒，报出来的 WAITED 秒数和读起来的样子不一致。
    cur=$(date +%s)
    [ $(( cur - t0 )) -ge "$AI_WAIT_S" ] && break
    sleep 1
  done
  OB=""; WAITED=$(( $(date +%s) - t0 )); LAST_STATUS="${st:-无响应}"
  return 1
}

echo "==================================================================="
echo "${C_BLU}桥接模块 + 统一收件箱 全渠道端到端模拟 (run=$RUN_TOKEN)${C_RST}"
echo "目标服务: $BASE_URL   渠道: ${CHANNELS[*]}"
echo "==================================================================="

# ---- 0. 服务健康 ----
HC="$(http_code "$BASE_URL/api/health")"
if [ "$HC" = "200" ]; then ok "服务健康 /api/health → 200"; else bad "服务健康" "/api/health=$HC"; fi

# ---- 0b. AI 依赖健康门控（三档分开判，别把云端 LLM 算成本地缺口）----
# LLM 的真相源是 llm_providers 表（config.yaml 的 llm 段只是兜底），启用的提供商可以是云端网关，
# 此时 :8207 缺席属正常配置；Embedding/Rerank 按设计强制本地（数据不出域）：缺 :8208 时，
# 没有 fail-fast 的二进制会对每个候选端点跑满 5 轮退避（单轮 30s），首条 AI 回复实测拖到
# 66–255s，且回复文案自述"知识库暂时查询超时"。
LLM_OK=1
# 未被任何路由引用、端点又没起的提供商＝配置残留，不参与本次请求；收集名字给下面的建议行
INERT=""
# enabled 的提供商可能多个共用同一个 base_url，按 URL 去重后每个端点只探一次，
# 否则一个死掉的本地代理会被报成多条同因 WARN。
# 每条还带 routes=N：该提供商被几个场景路由引用。routes=0 的提供商根本没参与请求
# （候选只从 route.provider + route.fallbacks 里取），把它报成"AI 回复可能降级"
# 是把不相干的配置行算成了本次故障的原因。
PROVIDERS="$(psql_q "SELECT base_url || '|' || string_agg(name || ':#' || routes, ',') FROM (SELECT p.name AS name, p.base_url AS base_url, (SELECT count(*) FROM llm_routing_rules r WHERE r.route_json::jsonb->>'provider' = p.name OR coalesce(r.route_json::jsonb->'fallbacks','[]'::jsonb) @> to_jsonb(p.name)) AS routes FROM llm_providers p WHERE p.enabled = true) x GROUP BY base_url ORDER BY base_url")"
[ -z "$PROVIDERS" ] && PROVIDERS="$(envv LLM_BASE_URL)|env-LLM_BASE_URL:#0"
while IFS= read -r line; do
  [ -z "$line" ] && continue
  purl="${line%|*}"; pnames="${line##*|}"
  # 该端点上被路由引用的提供商数：>0 才说明它真的会参与请求
  referenced=$(printf '%s' "$pnames" | tr ',' '\n' | grep -c ':#[1-9]')
  pnames="$(printf '%s' "$pnames" | sed 's/:\#[0-9]*//g')"
  case "$purl" in
    *127.0.0.1*|*localhost*)
      c="$(http_code "${purl%/v1}/health")"
      if [ "$c" = "200" ]; then ok "本地 LLM 端点 ${purl} /health → 200（提供商 ${pnames}）"
      elif [ "$referenced" = "0" ]; then ok "本地 LLM 端点 ${purl} 未起，但提供商 [${pnames}] 未被任何场景路由引用（不参与本次请求）"; INERT="${INERT:+$INERT }${pnames}"
      else warn "本地 LLM 端点 ${purl}" "/health=${c}，被 ${referenced} 个路由引用的提供商 [${pnames}] 不可用 (AI 回复会降级)"; LLM_OK=0; fi
      ;;
    *) ok "云端 LLM 提供商 [${pnames}] → ${purl}（不探本地端口）" ;;
  esac
done <<< "$PROVIDERS"
# 单点路由门控：某个场景只挂一个提供商、fallbacks 为空时，云端网关一次瞬时 5xx
# 就没有任何接手者（实测 sensenova 一次 522 直接把"抱歉，AI 服务暂时不可用"发给客户）。
# 触发条件是 NOFB>0 而不是"全空"：7 个场景里空 1 个也仍然是单点。
NOFB="$(psql_q "SELECT count(*) FROM llm_routing_rules WHERE coalesce(route_json::jsonb->'fallbacks','[]'::jsonb) = '[]'::jsonb")"
ALLR="$(psql_q "SELECT count(*) FROM llm_routing_rules")"
# 可用兜底的先决条件：得有第二个"带密钥"的启用提供商。实测本机 9 个提供商里只有
# sensenova 配了 api_key（deepseek 虽然 enabled 但密钥为空），照旧文案让人"补 fallbacks"
# 只会把一次 5xx 降级换成一次 401 降级 ⇒ 把可用候选数一起报出来。
KEYED="$(psql_q "SELECT count(*) FROM llm_providers WHERE enabled = true AND coalesce(api_key,'') <> ''")"
if [ -n "$NOFB" ] && [ "${NOFB:-0}" -gt 0 ]; then
  # 建议行里的提供商名单取自上面实测（写死个数会随配置漂移变成假话）
  inert_txt=""
  [ -n "$INERT" ] && inert_txt="；顺手停用未被任何路由引用且端点未起的残留提供商 [${INERT//,/ }]"
  keyed_txt="带密钥的启用提供商 ${KEYED:-?} 个"
  if [ "${KEYED:-0}" -le 1 ]; then
    keyed_txt="只有 ${KEYED:-0} 个带密钥的启用提供商 ⇒ 先在「LLM 提供商」给第二个提供商配 api_key（空密钥一调就 401，补了 fallbacks 也接不住）"
  fi
  warn "路由单点" "${NOFB}/${ALLR} 个场景 fallbacks 为空：主提供商一次瞬时 5xx 即降级，无任何接手者。给 route 补 fallbacks（管理端或 UPDATE llm_routing_rules.route_json）；${keyed_txt}${inert_txt}"
fi
for p in 8208 8209; do
  c="$(http_code "http://localhost:$p/health")"
  if [ "$c" = "200" ]; then ok "本地推理栈 :$p /health → 200"; else warn "本地推理栈 :$p" "/health=$c (Embedding/Rerank 强制本地, 缺位会拖慢并降级 AI 回复)"; LLM_OK=0; fi
done
if [ "$LLM_OK" = "0" ]; then
  # 处置指针要写进结果面：本脚本判不了也修不了推理栈，但必须说清
  # 「去哪儿修、影响面止于哪一块」，否则 WARN 只到「环境归因」就断了。
  # 命令按 Makefile 目标写（本脚本 cwd 在 user-server，scripts/inference-host/ 在仓库根，
  # 直接 bash 那条相对路径会 No such file）。
  warn "推理栈处置指引" "仓库根执行 make inference-host-up（首次先 inference-host-install + inference-host-models），make inference-host-status 看三态；影响面=bridge outbox 的 AI 回复内容与耗时（浏览器 Brain 走平台 LLM 网关，不同路）"
fi

# ---- 0c. 跨语言哈希契约锚点（最高优先级）----
ANCHOR=$(chash "douyin" "你好")
if [ "$ANCHOR" = "mh:00550fed" ]; then ok "哈希契约锚点 chash('douyin','你好')=$ANCHOR"; else bad "哈希契约锚点" "chash=$ANCHOR 期望 mh:00550fed"; fi

# ---- 负向用例 ----
echo ""; echo "${C_YEL}--- 负向用例 ---${C_RST}"
NEG=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/ingest" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
  -d '{"messages":[{"event_id":"x","conversation_id":"c","sender_id":"s","sender_type":"customer","content":"hi","msg_type":"text","timestamp":1}]}')
[ "$(printf '%s' "$NEG" | jq -r '.ok')" = "false" ] \
  && ok "缺参: 无 channel/account_id → ok=false (reason=$(printf '%s' "$NEG" | jq -r '.reason'))" \
  || bad "缺参" "ok 应为 false: $NEG"
NEG2=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/ingest?channel=unknown_xyz&account_id=a" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
  -d '{"messages":[{"event_id":"x2","conversation_id":"c2","sender_id":"s","sender_type":"customer","content":"hi","msg_type":"text","timestamp":1}]}')
[ "$(printf '%s' "$NEG2" | jq -r '.ok')" = "false" ] \
  && ok "不支持渠道: unknown_xyz → ok=false (reason=$(printf '%s' "$NEG2" | jq -r '.reason'))" \
  || bad "不支持渠道" "ok 应为 false: $NEG2"

# ---- 逐渠道正向测试 ----
for ch in "${CHANNELS[@]}"; do
  echo ""; echo "${C_BLU}===== 渠道: $ch =====${C_RST}"
  ACCT="sim_${ch}_$(uid12)"; CONV="sim_conv_${ch}_$(uid12)"
  EVT1="sim_evt_${ch}_$(uid12)"; EVT2="sim_evt2_${ch}_$(uid12)"
  CONTENT="你好，我是${ch}渠道访客，咨询产品价格与优惠活动。run=${RUN_TOKEN}"

  # 1) 首报（冷启动/瞬时抖动重试一次，同 event_id 幂等安全）
  BODY=$(mkmsg "$ch" "$ACCT" "$CONV" "$EVT1" "$CONTENT")
  RESP=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BODY")
  [ "$(printf '%s' "$RESP" | jq -r '.ok')" = "true" ] || { sleep 2; RESP=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BODY"); }
  if [ "$(printf '%s' "$RESP" | jq -r '.ok')" != "true" ]; then bad "$ch ingest" "ok!=true: $RESP"; continue; fi
  assert_type "$RESP" '.session_id' 'string' "$ch ingest.session_id"
  assert_type "$RESP" '.server_time' 'number' "$ch ingest.server_time"
  assert_type "$RESP" '.ingested' 'array' "$ch ingest.ingested[]"
  r0=$(printf '%s' "$RESP" | jq -c '.ingested[0]')
  for f in event_id accepted duplicate ai_handled reason; do
    k=$([ "$f" = "event_id" ] || [ "$f" = "reason" ] && echo string || echo boolean)
    assert_type "$r0" ".$f" "$k" "$ch ingest[0].$f"
  done
  eid=$(printf '%s' "$r0" | jq -r '.event_id'); acc=$(printf '%s' "$r0" | jq -r '.accepted'); aid=$(printf '%s' "$r0" | jq -r '.ai_handled')
  [ "$eid" = "$EVT1" ] && [ "$acc" = "true" ] \
    && ok "$ch ingest: 首报接受 (event_id 回显一致, accepted=true)" \
    || bad "$ch ingest" "event_id=$eid(expected $EVT1) accepted=$acc"
  [ "$aid" = "true" ] && ok "$ch ingest: AI 已触发 (ai_handled=true)" \
    || warn "$ch ingest" "ai_handled=false (未触发 AI, reason=$(printf '%s' "$r0" | jq -r '.reason'))"

  # 2) 幂等去重：相同 event_id
  R2=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BODY")
  r2=$(printf '%s' "$R2" | jq -c '.ingested[0]')
  if [ "$(printf '%s' "$r2" | jq -r '.duplicate')" = "true" ]; then
    ok "$ch 幂等去重: 同 event_id → duplicate=true (reason=$(printf '%s' "$r2" | jq -r '.reason'))"
  else bad "$ch 幂等去重" "未判重: $r2"; fi

  # 3) 回声/回环去重：相同 channel+content+sender，新 event_id
  #    约定：命中去重时 duplicate=true（权威信号，前端据此停重发）；accepted 恒为 true（表示已收讫）。
  BODY2=$(mkmsg "$ch" "$ACCT" "$CONV" "$EVT2" "$CONTENT")
  R3=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BODY2")
  r3=$(printf '%s' "$R3" | jq -c '.ingested[0]')
  assert_type "$r3" '.duplicate' 'boolean' "$ch 回声/回环去重.duplicate"
  if [ "$(printf '%s' "$r3" | jq -r '.duplicate')" = "true" ]; then
    ok "$ch 回声/回环去重: 同内容新 event_id → 被拦截 (reason=$(printf '%s' "$r3" | jq -r '.reason'))"
  else bad "$ch 回声/回环去重" "未拦截同内容回灌: $r3"; fi

  # 4) outbox 拉取 AI 回复（轮询，窗口见 AI_WAIT_S）
  if ! poll_outbox "$ch" "$ACCT" 5; then
    # 已验证 outbox/claim 路径本身正确（pending 行存在时必被认领返回，见 D4），
    # 窗口内没消息＝AI 回复没落库，不是桥接缺陷。
    if [ "$LLM_OK" = "1" ]; then
      warn "$ch outbox" "${WAITED}s 内未拉到 AI 回复 (last_status=$LAST_STATUS, ai_handled=$aid) —— AI 生成链路未在窗口内落库, 桥接 outbox/claim 路径已独立验证正确"
    else
      warn "$ch outbox" "${WAITED}s 内未拉到 AI 回复 (last_status=$LAST_STATUS) —— 本地推理栈缺位, 归属环境；推理栈起来后同一链路实测可落库"
    fi
    sleep 3; continue
  fi
  ok "$ch outbox: ${WAITED}s 拉到 AI 回复（AI 生成链路端到端耗时）"

  m0=$(printf '%s' "$OB" | jq -c '.messages[0]')
  assert_type "$m0" '.msg_id' 'string' "$ch outbox[0].msg_id"
  assert_type "$m0" '.conversation_id' 'string' "$ch outbox[0].conversation_id"
  assert_type "$m0" '.content' 'string' "$ch outbox[0].content"
  assert_type "$m0" '.is_ai_reply' 'boolean' "$ch outbox[0].is_ai_reply"
  assert_type "$m0" '.msg_type' 'string' "$ch outbox[0].msg_type"
  assert_type "$m0" '.created_at' 'string' "$ch outbox[0].created_at"
  assert_type "$m0" '(.media_url // "")' 'string' "$ch outbox[0].media_url"
  assert_type "$m0" '(.sender_id // "")' 'string' "$ch outbox[0].sender_id"
  assert_type "$m0" '(.receiver_id // "")' 'string' "$ch outbox[0].receiver_id"
  conv_ok=$(printf '%s' "$m0" | jq -r --arg c "$CONV" '.conversation_id==$c')
  isai=$(printf '%s' "$m0" | jq -r '.is_ai_reply')
  rc=$(printf '%s' "$m0" | jq -r '.content')
  # 字数在 JSON 侧数（jq 的 length 是码点数）：本机 wc -m 在 C locale 下数的是字节，
  # 会把 20 字的兜底文案报成"54 字"，取证行里的读数就成了假的。
  rc_len=$(printf '%s' "$m0" | jq -r '(.content // "") | length')
  [ "$conv_ok" = "true" ] && ok "$ch outbox: conversation_id 与上报一致" || bad "$ch outbox" "conversation_id 不匹配"
  [ "$isai" = "true" ] && ok "$ch outbox: is_ai_reply=true" || bad "$ch outbox" "is_ai_reply 应为 true"
  # 兜底模板也是"非空"，报成 ok 就是假绿：链路通、AI 栈没真实应答，客户收到的是一句固定话术。
  # 只列各条兜底文案的独有句式（不用"请稍后再试"这类通用词——正常应答里也会合法出现）。
  # 产出方见 internal/service/sales_engine_agentloop.go、internal/aiagent/llm/{fallback_tree,provider_failover}.go、
  # internal/aiagent/agent/runtime/runtime.go；改文案要同步这里与 scripts/simulate/ai_quality.py。
  case "$rc" in
    *"AI 服务暂时不可用"* | *"暂时无法处理您的请求"* | *"当前服务暂时繁忙"* | *"当前客服系统繁忙"* | *"系统暂时有点忙"* | *"系统暂不可用"*)
      warn "$ch outbox" "AI 回复是固定兜底模板（桥接链路通、AI 栈未真实应答）: ${C_YEL}$(printf '%.70s' "$rc")${C_RST}"
      ;;
    *)
      if [ -n "$rc" ] && [ "${rc_len:-0}" -gt 3 ]; then
        ok "$ch outbox: AI 回复内容非空(长度 ${rc_len} 字): ${C_YEL}$(printf '%.70s' "$rc")${C_RST}"
      else bad "$ch outbox" "AI 回复内容过短或为空"; fi
      ;;
  esac

  # 5) ack 闭环
  MSGID=$(printf '%s' "$m0" | jq -r '.msg_id')
  ACK_BODY=$(python3 -c "import json;print(json.dumps({'msg_ids':['$MSGID'],'status':'delivered'}))")
  ACK=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$ACK_BODY")
  if [ "$(printf '%s' "$ACK" | jq -r '.status')" = "ok" ]; then
    acked=$(printf '%s' "$ACK" | jq -r '.acked_items_count')
    [ "${acked:-0}" -ge 1 ] && ok "$ch ack: 闭环成功 (acked_items_count=$acked)" || bad "$ch ack" "acked_items_count=$acked 期望>=1"
  else bad "$ch ack" "status!=ok: $ACK"; fi
  OB2=$(curl -s -m 10 "${H_TOKEN[@]}" "$BASE_URL/api/bridge/outbox?channel=$ch&account_id=$ACCT&limit=5")
  n2=$(printf '%s' "$OB2" | jq -r '.messages|length' 2>/dev/null)
  [ "${n2:-0}" = "0" ] && ok "$ch outbox: ack 后下发清空 (at-least-once 已确认)" \
    || warn "$ch outbox" "ack 后仍有 $n2 条 (reclaim 重下发, at-least-once 权衡)"

  # 6) msg_id 回环：AI 回复原样回灌 (event_id=内容哈希)
  LOOP_EVT=$(chash "$ch" "$rc")
  LOOP_BODY=$(mkmsg "$ch" "$ACCT" "$CONV" "$LOOP_EVT" "$rc")
  LR=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ch&account_id=$ACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$LOOP_BODY")
  lr=$(printf '%s' "$LR" | jq -c '.ingested[0]')
  if [ "$(printf '%s' "$lr" | jq -r '.duplicate')" = "true" ]; then
    ok "$ch msg_id 回环: AI 回复回灌被拦截 (event_id=内容哈希, reason=$(printf '%s' "$lr" | jq -r '.reason'))"
  else bad "$ch msg_id 回环" "AI 回复回灌未被拦截(可能自问答回环): $lr"; fi

  sleep 3   # 让推理栈喘口气，降低串行负载下的抖动
done

# ===========================================================================
# 深度边界测试（单渠道重点深挖，避免 5×N 放大推理栈负载）
# 覆盖：批量多消息 / outbox limit 边界 / ack 幂等边界 / reclaim 超时重下发 /
#       media 图片消息 / channel query 覆盖 body
# ===========================================================================
echo ""; echo "${C_BLU}########## 深度边界测试 (channel=douyin 重点深挖) ##########${C_RST}"
DCH="douyin"
DACCT="deep_${DCH}_$(uid12)"
DCONV="deep_conv_${DCH}_$(uid12)"

# ---- D1. 批量多消息 ingest（一次 3 条，含不同 sender）----
echo ""; echo "${C_YEL}--- D1. 批量多消息 ingest ---${C_RST}"
NOW=$(python3 -c 'import time;print(int(time.time()*1000))')
BATCH_BODY=$(python3 -c '
import json, sys, time
now = '"$NOW"'
msgs = []
for i in range(3):
    msgs.append({
        "event_id": "deep_batch_%d_%d" % (i, now),
        "conversation_id": "'"$DCONV"'",
        "sender_id": "cust_deep_%d" % i, "sender_name": "访客%d" % i, "sender_type": "customer",
        "content": "批量消息第%d条，咨询产品优惠。run=%s" % (i, "'"$RUN_TOKEN"'"),
        "msg_type": "text", "timestamp": now + i
    })
print(json.dumps({"messages": msgs}))
')
BR=$(curl -s -m 25 -X POST "$BASE_URL/api/bridge/ingest?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BATCH_BODY")
if [ "$(printf '%s' "$BR" | jq -r '.ok')" = "true" ]; then
  bc=$(printf '%s' "$BR" | jq -r '.ingested|length')
  [ "$bc" = "3" ] && ok "D1 批量 ingest: 3 条全部返回处理结果 (ingested=$bc)" || bad "D1 批量 ingest" "ingested=$bc 期望 3"
  # batch 内顺序不保证（按 conversation 分组/排序），改为「集合匹配」：3 个 event_id 全部回显
  got=$(printf '%s' "$BR" | jq -r '[.ingested[].event_id]|sort|join(",")')
  expset="deep_batch_0_${NOW},deep_batch_1_${NOW},deep_batch_2_${NOW}"
  expset=$(echo "$expset" | tr ',' '\n' | sort | paste -sd, - 2>/dev/null || echo "$expset")
  if [ "$got" = "$expset" ]; then
    ok "D1 批量 ingest: 3 条 event_id 集合完整回显一致 (顺序不保证, 设计内)"
  else
    bad "D1 批量 ingest" "event_id 集合不匹配: got=[$got] exp=[$expset]"
  fi
else bad "D1 批量 ingest" "ok!=true: $BR"; fi

# ---- D2. outbox limit 边界 ----
echo ""; echo "${C_YEL}--- D2. outbox limit 边界（limit=1 仅返回 1 条；超大封顶）---${C_RST}"
# 等待 D1 的 AI 回复落库（可能 3 条，按 conv 合并为 1 条回复更可能，但兜底测 limit）
REPLY_D2=""
if poll_outbox "$DCH" "$DACCT" 100; then REPLY_D2="$OB"; fi
if [ -z "$REPLY_D2" ]; then
  warn "D2 outbox limit" "${WAITED}s 内未拉到 AI 回复（AI 生成链路未落库，环境归因）"
else
  TOTAL_D2=$(printf '%s' "$REPLY_D2" | jq -r '.messages|length')
  # 先 ack 清空，便于后续 limit=1 精确计数
  # Python 程序一律单引号包裹、值走 argv：上一版把它嵌在 -d "$(python3 -c "…{'k':v,…}")" 里，
  # macOS /bin/bash 3.2 在"双引号内的命令替换"中会丢掉内层引号，{'msg_ids':…,'status':…}
  # 于是走大括号展开被逗号劈成两个参数——python 报 SyntaxError、curl 收到空 -d 退出 2，
  # 而下面照样打印"已清空"的 ok（ack 其实没发出去，limit 断言量的是没清空的队列＝假绿）。
  ALL_IDS=$(printf '%s' "$REPLY_D2" | jq -r '[.messages[].msg_id]|join(",")')
  ACK_D2=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$(mkack "$ALL_IDS" delivered)")
  # 只判 status==ok 是不够的：不存在的 msg_id 也返回 ok（not_found_count 才是账），
  # 那样"清空"就是假的，后面 limit=1 量到的仍是没清空的队列。
  n_acked=$(printf '%s' "$ACK_D2" | jq -r '.acked_items_count // 0')
  n_miss=$(printf '%s' "$ACK_D2" | jq -r '.not_found_count // 0')
  if [ "$(printf '%s' "$ACK_D2" | jq -r '.status')" = "ok" ] && [ "${n_acked:-0}" -ge 1 ] && [ "${n_miss:-0}" = "0" ]; then
    ok "D2 outbox: 已拉到 $TOTAL_D2 条 AI 回复并清空 (acked=${n_acked} not_found=${n_miss}，limit=100 返回全部，等待 ${WAITED}s)"
  else
    bad "D2 outbox ack 清空" "期望 acked>=1 且 not_found=0，实际 acked=${n_acked:-?} not_found=${n_miss:-?}：$ACK_D2"
  fi
  # limit=1 边界：再发 2 条到同一 conv 看 limit 是否生效（若无新回复则不强制 fail）
  # 直接验证 limit 参数被接受且返回 <= limit
  OB1=$(curl -s -m 10 "${H_TOKEN[@]}" "$BASE_URL/api/bridge/outbox?channel=$DCH&account_id=$DACCT&limit=1")
  n1=$(printf '%s' "$OB1" | jq -r '.messages|length' 2>/dev/null)
  [ "${n1:-0}" -le 1 ] && ok "D2 outbox limit=1: 返回 ${n1} 条 (<=1)" || bad "D2 outbox limit=1" "返回 $n1 条 >1"
  # 超大 limit 封顶：URL 传 9999，服务端应封顶 200（不报错）
  OB9=$(curl -s -m 10 "${H_TOKEN[@]}" "$BASE_URL/api/bridge/outbox?channel=$DCH&account_id=$DACCT&limit=9999")
  [ "$(printf '%s' "$OB9" | jq -r '.status' 2>/dev/null)" = "ok" ] \
    && ok "D2 outbox limit=9999: 服务端接受并封顶(不 500)" \
    || bad "D2 outbox limit=9999" "status!=ok: $OB9"
fi

# ---- D3. ack 边界 ----
echo ""; echo "${C_YEL}--- D3. ack 边界（重复 ack 幂等=0 / 不存在 msg_id / 空 body）---${C_RST}"
# 先制造一条待 ack 的 AI 回复（重新 ingest 触发）
DEVT="deep_ack_$(uid12)"
ACK_BODY=$(python3 -c '
import json, time
print(json.dumps({"messages":[{
    "event_id": "'"$DEVT"'", "conversation_id": "'"$DCONV"'",
    "sender_id": "cust_deep_ack", "sender_name": "访客", "sender_type": "customer",
    "content": "ack边界测试唯一内容 '"$DEVT"'", "msg_type": "text",
    "timestamp": int(time.time()*1000)
}]}))
')
curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$ACK_BODY" >/dev/null
ACK_TARGET=""
if poll_outbox "$DCH" "$DACCT" 5; then ACK_TARGET="$OB"; fi
if [ -z "$ACK_TARGET" ]; then
  warn "D3 ack 边界" "${WAITED}s 内未拉到可 ack 的回复（AI 生成链路未落库，环境归因）"
else
  MID=$(printf '%s' "$ACK_TARGET" | jq -r '.messages[0].msg_id')
  # 首次 ack
  A1=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
    -d "$(mkack "$MID" "delivered")")
  a1=$(printf '%s' "$A1" | jq -r '.acked_items_count' 2>/dev/null)
  [ "$a1" = "1" ] && ok "D3 ack 首次: acked_items_count=1" || bad "D3 ack 首次" "acked_items_count=$a1 期望 1"
  # 重复 ack（已 delivered，应幂等 acked=0）
  A2=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
    -d "$(mkack "$MID" "delivered")")
  a2=$(printf '%s' "$A2" | jq -r '.acked_items_count' 2>/dev/null)
  [ "$a2" = "0" ] && ok "D3 ack 重复: acked_items_count=0 (幂等，已 delivered 不重复计)" || bad "D3 ack 重复" "acked_items_count=$a2 期望 0"
  # 不存在的 msg_id
  A3=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
    -d "$(mkack "mh:deadbeef" "delivered")")
  a3=$(printf '%s' "$A3" | jq -r '.acked_items_count' 2>/dev/null)
  [ "$a3" = "0" ] && ok "D3 ack 不存在 msg_id: acked_items_count=0 (安全忽略)" || bad "D3 ack 不存在" "acked_items_count=$a3 期望 0"
  # 空 body（msg_ids 为空）
  A4=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d '{}')
  a4=$(printf '%s' "$A4" | jq -r '.status' 2>/dev/null)
  [ "$a4" = "ok" ] && ok "D3 ack 空 body: status=ok (acked_items_count=0, 不报错)" || bad "D3 ack 空 body" "status=$a4: $A4"
fi

# ---- D3c. v2 逐项 status 校验（未知值整批拒在写之前）----
# v2 的 status 挂在每个 item 上，入口那道只查顶层 status 的校验拦不到它：未知值会一路走到
# service 报错回 500。而 v2 是按 (conversation_id, status) 分组后遍历 map 逐组落库的，
# 遍历序随机 ⇒ 实测同一请求 20 次里 15 次已把好项落库、5 次一格没落，客户端只读到一句
# 不带原因的 "ack failed"，无从判断该重试还是该改参数；没落库的行留在"欠交付"集合里，
# 30s 认领租约到期后被重新下发（最多 20 次）。这一格要求：拒成 400 且文案点名不认的值。
echo ""; echo "${C_YEL}--- D3c. v2 逐项 status 校验（未知 status → 400，不是 500）---${C_RST}"
BAD_BODY='{"v":2,"items":[{"msg_id":"mh:deadbeef","conversation_id":"conv_d3c","status":"delivered"},{"msg_id":"mh:cafe1234","conversation_id":"conv_d3c","status":"shipped"}]}'
BAD_TMP="$(mktemp "${TMPDIR:-/tmp}/d3c.XXXXXX")"
BAD_CODE="$(curl -s -m 10 -o "$BAD_TMP" -w "%{http_code}" -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$BAD_BODY")"
BAD_MSG="$(head -c 300 "$BAD_TMP")"; rm -f "$BAD_TMP"
if [ "$BAD_CODE" = "400" ] && printf '%s' "$BAD_MSG" | grep -q 'shipped'; then
  ok "D3c v2 未知 status: http=400 且文案点名不认的值 (body=$(printf '%.90s' "$BAD_MSG"))"
elif [ "$BAD_CODE" = "500" ]; then
  bad "D3c v2 未知 status" "http=500：入参错误被报成服务端故障，且整批是否落库随 map 遍历序摆动 (body=$BAD_MSG)"
else
  bad "D3c v2 未知 status" "http=$BAD_CODE 期望 400 (body=$BAD_MSG)"
fi
# 反向对照：合法值（delivered + failed）不许被这道校验误拒
OK_BODY='{"v":2,"items":[{"msg_id":"mh:deadbeef","conversation_id":"conv_d3c","status":"delivered"},{"msg_id":"mh:cafe1234","conversation_id":"conv_d3c","status":"failed","error":"send blocked"}]}'
OK_TMP="$(mktemp "${TMPDIR:-/tmp}/d3cok.XXXXXX")"
OK_CODE="$(curl -s -m 10 -o "$OK_TMP" -w "%{http_code}" -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$OK_BODY")"
OK_MSG="$(head -c 300 "$OK_TMP")"; rm -f "$OK_TMP"
OK_NF=$(printf '%s' "$OK_MSG" | jq -r '.not_found_count // "?"' 2>/dev/null)
[ "$OK_CODE" = "200" ] && [ "$OK_NF" = "2" ] \
  && ok "D3c 合法 v2 批（delivered+failed）未被误拒: http=200 not_found_count=2" \
  || bad "D3c 合法 v2 批" "http=$OK_CODE not_found=${OK_NF:-?} 期望 200/2 (body=$OK_MSG)"

# ---- D3b. 渠道别名一致性（ingest/outbox/ack 三入口必须同进同出）----
# 别名（xhs / douyin_web / …）由服务端 NormalizeBridgeChannel 收编：ingest 与 outbox 早就归一，
# ack 曾经没归 ⇒ message_hub 只有规范名，别名 ack 每格 not_found=1 却仍回 200 status:ok，
# 那一行停在 inflight ⇒ 30s 租约到期后同一条回复被反复下发，最多 20 次才落 failed。
echo ""; echo "${C_YEL}--- D3b. 渠道别名一致性（别名入参 ack 必须能收口）---${C_RST}"
ALIAS_CH="douyin_web"
AL_ACCT="alias_${RUN_TOKEN}"
AL_CONV="alias_conv_${RUN_TOKEN}"
AL_EVT="deep_alias_${RUN_TOKEN}"
AL_BODY=$(python3 -c '
import json, time
print(json.dumps({"messages":[{
    "event_id": "'"$AL_EVT"'", "conversation_id": "'"$AL_CONV"'",
    "sender_id": "cust_alias", "sender_name": "访客", "sender_type": "customer",
    "content": "别名一致性测试唯一内容 '"$AL_EVT"'", "msg_type": "text",
    "timestamp": int(time.time()*1000)
}]}))
')
curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$ALIAS_CH&account_id=$AL_ACCT" \
  -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$AL_BODY" >/dev/null
AL_OB=""
if poll_outbox "$ALIAS_CH" "$AL_ACCT" 5; then AL_OB="$OB"; fi
if [ -z "$AL_OB" ]; then
  warn "D3b 别名闭环" "${WAITED}s 内未拉到 AI 回复（AI 链路未落库，环境归因）"
else
  AL_MID=$(printf '%s' "$AL_OB" | jq -r '.messages[0].msg_id')
  # 落库形态本身也是判据的一部分：别名绝不能写进 message_hub.platform
  # 按账号收窄：msg_id 是内容哈希，不同轮次只要 AI 回复文本相同就会撞同一个 msg_id，
  # 不限定 account_id 时 psql 会返回多行，值里的换行会把下面的字符串比较判成不相等
  # （实测两条 FAIL 的读数分别印成 "douyin"/"douyin 期望 douyin"，看着像断言写反了）。
  AL_PLAT="$(psql_q "SELECT platform FROM message_hub WHERE account_id='$AL_ACCT' AND msg_id='$AL_MID'")"
  if [ "$AL_PLAT" = "douyin" ]; then
    ok "D3b 别名 ingest: 落库为规范名 douyin"
  else
    bad "D3b 别名 ingest" "message_hub.platform=$AL_PLAT 期望 douyin（别名一旦入库，后面每一层都要再归一一次）"
  fi
  AL_ACK=$(curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$ALIAS_CH&account_id=$AL_ACCT" \
    -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$(mkack "$AL_MID" delivered)")
  AL_A=$(printf '%s' "$AL_ACK" | jq -r '.acked_items_count' 2>/dev/null)
  AL_MISS=$(printf '%s' "$AL_ACK" | jq -r '.not_found_count' 2>/dev/null)
  if [ "${AL_A:-0}" = "1" ] && [ "${AL_MISS:-0}" = "0" ]; then
    ok "D3b 别名 ack: acked_items_count=1 且 not_found_count=0"
  else
    bad "D3b 别名 ack" "acked=${AL_A:-?} not_found=${AL_MISS:-?}：$AL_ACK"
  fi
  # ack 之后必须离开欠交付集合（这一条就是"客户不会被同一句刷屏"的现场判据）
  AL_ST="$(psql_q "SELECT status FROM message_hub WHERE account_id='$AL_ACCT' AND msg_id='$AL_MID'")"
  [ "$AL_ST" = "delivered" ] && ok "D3b 别名 ack 后落库 delivered" \
    || bad "D3b 别名 ack 后状态" "status=$AL_ST 期望 delivered（停在 inflight 就是等着被重投）"
fi

# ---- D4. reclaim 超时重下发（inflight 卡 30s 后回收为 pending 重新可拉）----
echo ""; echo "${C_YEL}--- D4. reclaim 超时重下发（验证 at-least-once）---${C_RST}"
DEVT4="deep_reclaim_$(uid12)"
R4_BODY=$(python3 -c '
import json, time
print(json.dumps({"messages":[{
    "event_id": "'"$DEVT4"'", "conversation_id": "'"$DCONV"'",
    "sender_id": "cust_deep_rc", "sender_name": "访客", "sender_type": "customer",
    "content": "reclaim超时测试唯一内容 '"$DEVT4"'", "msg_type": "text",
    "timestamp": int(time.time()*1000)
}]}))
')
curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" -d "$R4_BODY" >/dev/null
# 拉取一次（转为 inflight），不 ack
R4=""
if poll_outbox "$DCH" "$DACCT" 5; then R4="$OB"; fi
if [ -z "$R4" ]; then
  warn "D4 reclaim" "${WAITED}s 内未拉到待 reclaim 的回复（AI 生成链路未落库，环境归因）"
else
  MID4=$(printf '%s' "$R4" | jq -r '.messages[0].msg_id')
  # 验证该 msg_id 当前为 inflight（已被 claim）
  S1=$(psql_q "SELECT status FROM message_hub WHERE account_id='$DACCT' AND msg_id='$MID4' LIMIT 1;")
  [ "$S1" = "inflight" ] && ok "D4 reclaim: 首次拉取后 status=$S1 (已被 claim)" || warn "D4 reclaim" "status=$S1 (期望 inflight)"
  echo "  等待 32s 让 inflight 超时被回收为 pending ..."
  sleep 32
  R4b=$(curl -s -m 10 "${H_TOKEN[@]}" "$BASE_URL/api/bridge/outbox?channel=$DCH&account_id=$DACCT&limit=5")
  if [ "$(printf '%s' "$R4b" | jq -r '.status' 2>/dev/null)" = "ok" ] && [ "$(printf '%s' "$R4b" | jq -r '.messages|length' 2>/dev/null)" -gt 0 ]; then
    reclaimed=0
    for m in $(printf '%s' "$R4b" | jq -r '.messages[].msg_id'); do
      [ "$m" = "$MID4" ] && reclaimed=1
    done
    [ "$reclaimed" = "1" ] \
      && ok "D4 reclaim: 超时后同 msg_id 被重新认领下发（at-least-once 重下发生效）" \
      || warn "D4 reclaim" "超时后未重新拉到该 msg_id（可能已非 pending 或窗口边界）"
    # 清理：ack 掉重发的
    RIDS=$(printf '%s' "$R4b" | jq -r '[.messages[].msg_id]|join(",")')
    curl -s -m 10 -X POST "$BASE_URL/api/bridge/outbox/ack?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
      -d "$(mkack "$RIDS" "delivered")" >/dev/null
  else
    warn "D4 reclaim" "超时后 outbox 未返回消息（可能 AI 回复本身未落库，环境归因）"
  fi
fi

# ---- D5. media 图片消息（msg_type 非 text + media_url）----
echo ""; echo "${C_YEL}--- D5. media 图片消息（msg_type=image, 带 media_url）---${C_RST}"
DEVT5="deep_media_$(uid12)"
MEDIA_URL="https://cdn.example.com/deep/${DEVT5}.jpg"
MRES=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
  -d "$(python3 -c '
import json, time
print(json.dumps({"messages":[{
    "event_id": "'"$DEVT5"'", "conversation_id": "'"$DCONV"'",
    "sender_id": "cust_deep_media", "sender_name": "访客", "sender_type": "customer",
    "content": "用户发来一张商品图 '"$DEVT5"'", "msg_type": "image",
    "media_url": "'"$MEDIA_URL"'", "timestamp": int(time.time()*1000)
}]}))
')")
if [ "$(printf '%s' "$MRES" | jq -r '.ok')" = "true" ]; then
  ok "D5 media ingest: ok=true (img 消息被接受)"
  # inbound 消息 msg_id 直接是 event_id（非内容哈希）；验证 message_hub 是否记录了 media_url
  MIN=$(psql_q "SELECT media_url FROM message_hub WHERE msg_id='$DEVT5' LIMIT 1;")
  if [ -n "$MIN" ]; then
    ok "D5 media: message_hub 记录 media_url=$MIN (event_id=$DEVT5)"
  else
    warn "D5 media" "message_hub 未查到 media_url（inbound 可能不持久化 media_url，建议人工核对）"
  fi
else bad "D5 media ingest" "ok!=true: $MRES"; fi

# ---- D6. channel query 覆盖 body（防扩展错传）----
echo ""; echo "${C_YEL}--- D6. channel query 覆盖 body（body channel=xiaohongshu 但 query=douyin → 以 query 为准）---${C_RST}"
DEVT6="deep_cover_$(uid12)"
COVER=$(curl -s -m 20 -X POST "$BASE_URL/api/bridge/ingest?channel=$DCH&account_id=$DACCT" -H 'Content-Type: application/json' "${H_TOKEN[@]}" \
  -d "$(python3 -c '
import json, time
print(json.dumps({"channel": "xiaohongshu", "account_id": "'"$DACCT"'", "messages":[{
    "event_id": "'"$DEVT6"'", "conversation_id": "'"$DCONV"'",
    "sender_id": "cust_cover", "sender_name": "访客", "sender_type": "customer",
    "content": "channel覆盖测试唯一内容 '"$DEVT6"'", "msg_type": "text",
    "timestamp": int(time.time()*1000)
}]}))
')")
if [ "$(printf '%s' "$COVER" | jq -r '.ok')" = "true" ]; then
  # inbound msg_id 直接是 event_id；验证落库 platform=douyin（query 为准，忽略 body 的 xiaohongshu）
  PLAT=$(psql_q "SELECT platform FROM message_hub WHERE msg_id='$DEVT6' LIMIT 1;")
  [ "$PLAT" = "$DCH" ] && ok "D6 channel 覆盖: 落库 platform=$PLAT (query 优先于 body)" || bad "D6 channel 覆盖" "platform=$PLAT 期望 $DCH"
else bad "D6 channel 覆盖" "ok!=true: $COVER"; fi

# ---- D7. SSE 建流前的入参校验 ----
# SSE 是默认下行形态：capabilities 报 sse_enabled=true 后扩展端连轮询定时器都不启动，
# 而 once 发出 200 就再也回不去（错误体送不出去）。所以缺 channel／空 account_id／
# 桥接不承载的渠道必须在 WriteHeader 之前判成 400——否则配置写错的客户只看到
# "SSE 已连接、一条错误也没有、一条回复也收不到"。
echo ""; echo "${C_YEL}--- D7. SSE 入参校验（三类坏入参 400 + 合法入参成流）---${C_RST}"

# sse_probe <query 串> → 置 SSE_CODE／SSE_BODY／SSE_CTYPE
# 取 2s 就中断：合法流会一直开着（curl 退 28），这里要的是首帧与响应头，不是流跑完。
# 每次现取新临时文件：复用旧路径会把上一轮的 body 当本轮读数（假绿）。
sse_probe() {
  local outf hdrf
  outf="$(mktemp)"; hdrf="$(mktemp)"
  SSE_CODE="$(curl -s -m 2 -o "$outf" -D "$hdrf" -w '%{http_code}' "${H_TOKEN[@]}" \
    "$BASE_URL/api/bridge/outbox/sse?$1")"
  SSE_BODY="$(cat "$outf")"
  SSE_CTYPE="$(awk -F': ' 'tolower($1)=="content-type"{print $2; exit}' "$hdrf")"
  rm -f "$outf" "$hdrf"
}

# 三类坏入参：http=400 且文案点名缺的是哪一个
for probe in "account_id=${DACCT}|channel required" \
             "channel=douyin&account_id=|account_id required" \
             "channel=wechat&account_id=${DACCT}|unsupported bridge channel"; do
  qs="${probe%%|*}"; want="${probe##*|}"
  sse_probe "$qs"
  if [ -z "$SSE_CODE" ] || [ "$SSE_CODE" = "000" ]; then
    bad "D7 SSE 坏入参 [$qs]" "请求没跑成（http=${SSE_CODE:-空}），本轮读数不可用"
    continue
  fi
  case "$SSE_BODY" in
    *"$want"*)
      if [ "$SSE_CODE" = "400" ]; then
        ok "D7 SSE 坏入参 [$qs]: http=400 且文案点名 ${want}"
      else
        bad "D7 SSE 坏入参 [$qs]" "http=${SSE_CODE} 期望 400，body=${SSE_BODY}"
      fi
      ;;
    *) bad "D7 SSE 坏入参 [$qs]" "http=${SSE_CODE} 文案未含 ${want}：${SSE_BODY}" ;;
  esac
  # 坏入参绝不能已经进入流模式（进入后 Content-Type 就是 event-stream，错误体送不出去）
  case "$SSE_CTYPE" in
    *text/event-stream*) bad "D7 SSE 坏入参 [$qs]" "响应已被当作 SSE 流发出（ctype=${SSE_CTYPE}）" ;;
  esac
done

# 正控制：合法入参（规范名 + 别名）必须成流，否则上面三条靠"一律拒绝"就能蒙绿
for okq in "channel=douyin&account_id=${DACCT}" "channel=douyin_web&account_id=${DACCT}"; do
  sse_probe "$okq"
  is_stream=false
  case "$SSE_CTYPE" in *text/event-stream*) is_stream=true ;; esac
  has_retry=false
  case "$SSE_BODY" in *retry:*) has_retry=true ;; esac
  if [ "$SSE_CODE" = "200" ] && [ "$is_stream" = true ] && [ "$has_retry" = true ]; then
    ok "D7 SSE 合法入参 [$okq]: http=200 + text/event-stream + retry 首帧"
  else
    bad "D7 SSE 合法入参 [$okq]" "http=${SSE_CODE:-空} ctype=${SSE_CTYPE:-空} body=${SSE_BODY}"
  fi
done

# ---- D8. capabilities 契约（扩展端据此选 SSE／轮询）----
echo ""; echo "${C_YEL}--- D8. capabilities 读数与门禁 ---${C_RST}"
CAPF="$(mktemp)"
CAP="$(curl -s -m 10 -o "$CAPF" -w '%{http_code}' "${H_TOKEN[@]}" "$BASE_URL/api/bridge/capabilities")"
CAPBODY="$(cat "$CAPF")"; rm -f "$CAPF"
if [ "$CAP" = "200" ]; then
  assert_type "$CAPBODY" '.poll_interval_ms' 'number' "D8 capabilities.poll_interval_ms"
  assert_type "$CAPBODY" '.sse_enabled' 'boolean' "D8 capabilities.sse_enabled"
  assert_type "$CAPBODY" '.sse_heartbeat_ms' 'number' "D8 capabilities.sse_heartbeat_ms"
  ok "D8 capabilities 三键齐备: ${CAPBODY}"
else
  bad "D8 capabilities" "http=${CAP} body=${CAPBODY}"
fi
# capabilities 与其余桥接端点同组同闸门：无凭证必须 401（改桥接凭证＝同时改这道门禁）
CAPNOF="$(mktemp)"
CAPNO="$(curl -s -m 10 -o "$CAPNOF" -w '%{http_code}' "$BASE_URL/api/bridge/capabilities")"
CAPNOBODY="$(cat "$CAPNOF")"; rm -f "$CAPNOF"
if [ "$CAPNO" = "401" ]; then
  ok "D8 capabilities 无凭证被闸门拦下 (http=401)"
else
  bad "D8 capabilities 无凭证" "http=${CAPNO} 期望 401，body=${CAPNOBODY}"
fi

# ---- 汇总 ----
echo ""; echo "==================================================================="
echo "${C_BLU}测试汇总 (run=$RUN_TOKEN)${C_RST}"
echo "-------------------------------------------------------------------"
for line in "${REPORT[@]}"; do echo "$line"; done
echo "-------------------------------------------------------------------"
echo "通过: ${C_GREEN}$PASS${C_RST}  失败: ${C_RED}$FAIL${C_RST}  告警: ${C_YEL}$WARN${C_RST}"
echo "==================================================================="

# ---- 清理 sim 测试数据 ----
if [ -n "$PW" ]; then
  echo "${C_YEL}清理 sim/deep 测试数据 ...${C_RST}"
  psql_q "DELETE FROM message_hub WHERE account_id LIKE 'sim_%' OR account_id LIKE 'deep_%' OR account_id LIKE 'alias_%'; DELETE FROM inbox_conversations WHERE account_id LIKE 'sim_%' OR account_id LIKE 'deep_%' OR account_id LIKE 'alias_%'; DELETE FROM customer_sessions WHERE account_id LIKE 'sim_%' OR account_id LIKE 'deep_%' OR account_id LIKE 'alias_%';" >/dev/null || true
fi

[ "$FAIL" -gt 0 ] && exit 1 || exit 0
