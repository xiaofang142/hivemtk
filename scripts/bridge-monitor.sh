#!/usr/bin/env bash
#
# bridge-monitor.sh — 桥接（bridge）功能健康巡检
#
# 读取两路信号，综合判断 bridge 是否正常工作：
#   1) 上报日志：最近 N 分钟窗口内的 bridge 相关 API 交互日志（ingest / outbox / ack / api_interaction / error）
#      数据源两选一：容器名（docker logs）或 BRIDGE_LOG_FILE 指向的进程日志文件
#   2) 数据库数据：message_hub（上行/下行队列）、bridge_accounts（扩展连接）、inbox_conversations（会话）
#
# 用法:
#   bash scripts/bridge-monitor.sh [窗口]       窗口默认 30m（如 1h, 15m）
#   BRIDGE_CONTAINER=mtk-user-server-dev bash scripts/bridge-monitor.sh
#   BRIDGE_LOG_FILE=/tmp/bridge-run/air.log bash scripts/bridge-monitor.sh
#
# 日志两路只走一路：容器名存在就走 docker logs，否则必须显式给 BRIDGE_LOG_FILE
# （开发态 go run / air 把进程日志写在文件里）。没有后一路时，本地跑这份巡检会在
# 日志节整段报「无日志数据可分析」，看上去像「没有错误」，其实一条都没读到。
#
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$SCRIPT_DIR/../.env"
COMPOSE_DIR="$SCRIPT_DIR/.."

# ---- 可调参数（环境变量覆盖）----
SINCE="${1:-30m}"
BRIDGE_CONTAINER="${BRIDGE_CONTAINER:-mtk-user-server}"
BRIDGE_LOG_FILE="${BRIDGE_LOG_FILE:-}"
DB_HOST="${BRIDGE_DB_HOST:-127.0.0.1}"
DB_PORT="${BRIDGE_DB_PORT:-${USER_POSTGRES_HOST_PORT:-8232}}"
DB_USER="${BRIDGE_DB_USER:-${POSTGRES_USER:-admin}}"
DB_NAME="${BRIDGE_DB_NAME:-${USER_DB_NAME:-user_db}}"

# ---- 颜色 ----
if [ -t 1 ]; then
  C_OK=$'\033[32m'; C_WARN=$'\033[33m'; C_FAIL=$'\033[31m'; C_DIM=$'\033[2m'; C_RST=$'\033[0m'
else
  C_OK=""; C_WARN=""; C_FAIL=""; C_DIM=""; C_RST=""
fi

ok()   { echo "${C_OK}[OK]${C_RST}   $*"; }
warn() { echo "${C_WARN}[WARN]${C_RST} $*"; }
fail() { echo "${C_FAIL}[FAIL]${C_RST} $*"; }
dim()  { echo "${C_DIM}      $*${C_RST}"; }

# ---- 加载 .env（获取 DB 密码等，可选）----
if [ -f "$ENV_FILE" ]; then
  set -a
  # 仅加载存在的变量，避免覆盖已显式设置的环境变量
  while IFS='=' read -r k v; do
    k="$(echo "$k" | xargs)"; v="$(echo "$v" | xargs)"
    [ -z "$k" ] && continue
    case "$k" in \#*) continue ;; esac
    [ -n "${!k:-}" ] && continue
    export "$k=$v"
  done < "$ENV_FILE"
  set +a
fi
DB_PASSWORD="${BRIDGE_DB_PASSWORD:-${POSTGRES_PASSWORD:-}}"
DB_PORT="${BRIDGE_DB_PORT:-${USER_POSTGRES_HOST_PORT:-8232}}"
DB_NAME="${BRIDGE_DB_NAME:-${USER_DB_NAME:-user_db}}"
DB_USER="${BRIDGE_DB_USER:-${POSTGRES_USER:-admin}}"

# 判词累加器：日志节与数据库节都往这里降级（OK → WARN → FAIL），【结论】段和退出码都读它。
# 必须在这里初始化：早于任何 warn/fail 分支，否则 set -u 下第一次读它就炸；
# 也不能在【结论】段再置一次 OK，那会把日志节已经降过的级悄悄抹平。
overall="OK"
downgrade_to_warn() { if [ "$overall" = "OK" ]; then overall="WARN"; fi; }
downgrade_to_fail() { overall="FAIL"; }

# 窗口语法在入口统一判掉，两条日志源共用一条口径。
# 只收「一段数字 + 一个单位」：复合写法（1h30m）在文件那一路换算不出 ⇒ 会报 FAIL；
# 但容器那一路是把原串直接交给 `docker logs --since`，而 docker 自己认 1h30m ⇒
# 同一条命令换个日志源，"这个参数合不合法"就有两种答案，文档也没法写清。
# 所以这里先拦，判据与日志源无关。
#
# 坏参数直接退 2 而不是往下跑：rc=1 在本脚本里专指「bridge 功能异常」这条业务结论，
# 参数写错却报成 1，cron 会把一次敲错命令当成线上故障去喊人；而往下跑更糟——数据库节
# 照样出数、【结论】照样印「请立即排查」，读的人拿到的是半份报告加一条假红。
# 这类「脚本没跑起来」的独立档，与 user-server/scripts/bridge-e2e-sim.sh 用 rc=3 区分
# 「服务没起」是同一个道理。
if [[ ! "$SINCE" =~ ^[0-9]+[smhd]$ ]]; then
  echo "窗口写法「${SINCE}」不被接受：只认 <整数><s|m|h|d>（如 45s / 30m / 2h / 1d），复合写法请自己换算（1h30m ⇒ 90m）" >&2
  exit 2
fi

echo "============================================================"
if [ -n "$BRIDGE_LOG_FILE" ]; then
  LOG_DESC="文件 ${BRIDGE_LOG_FILE}"
else
  LOG_DESC="容器 ${BRIDGE_CONTAINER}"
fi
echo " Bridge 功能健康巡检  (窗口=${SINCE}, 日志源=${LOG_DESC})"
echo " 时间: $(date '+%Y-%m-%d %H:%M:%S')"
echo "============================================================"

# ---------- 1) 日志信号 ----------
# 窗口写法 → 秒数。两条日志源都先走这个换算：文件这一路拿秒数算出可比较的起点时间戳，
# 容器这一路把秒数拼成 `<秒>s` 传给 docker（docker 的 --since 不认 d，见下面那段）。
# 只收「一段数字 + 一个单位」：复合写法（1h30m）若按第一个单位解析会把 130 当成小时数，
# 于是窗口悄悄放大几十倍，读数假高，所以宁可报「换算不出」。
since_seconds() {
  local raw="$1"
  if [[ "$raw" =~ ^([0-9]+)([smhd])$ ]]; then
    case "${BASH_REMATCH[2]}" in
      s) echo "${BASH_REMATCH[1]}" ;;
      m) echo $(( ${BASH_REMATCH[1]} * 60 )) ;;
      h) echo $(( ${BASH_REMATCH[1]} * 3600 )) ;;
      d) echo $(( ${BASH_REMATCH[1]} * 86400 )) ;;
    esac
  else
    echo ""
  fi
}

# 窗口起点，格式与 slog 的 "time" 字段一致（截到秒即可按字典序比较；BSD 用 -v，GNU 用 -d）
window_cutoff() {
  local s="$1" out=""
  out="$(date -v-"${s}"S +%Y-%m-%dT%H:%M:%S 2>/dev/null)" || out=""
  if [ -z "$out" ]; then
    out="$(date -d "-${s} seconds" +%Y-%m-%dT%H:%M:%S 2>/dev/null)" || out=""
  fi
  echo "$out"
}

echo
echo "【1/2】上报日志（最近 ${SINCE}）"
LOG_LINES=""
LOG_SOURCE=""
if [ -n "$BRIDGE_LOG_FILE" ]; then
  WINDOW_SECONDS="$(since_seconds "$SINCE")"
  CUT=""
  [ -n "$WINDOW_SECONDS" ] && CUT="$(window_cutoff "$WINDOW_SECONDS")"
  if [ ! -r "$BRIDGE_LOG_FILE" ]; then
    fail "BRIDGE_LOG_FILE 指向的文件读不到：${BRIDGE_LOG_FILE}"; downgrade_to_fail
  elif [ -z "$CUT" ]; then
    # 算不出起点就整文件读：那是把几小时前的失败也算进本窗口，读数会假高，宁可不读
    fail "窗口「${SINCE}」换算不出起点时间戳（文件这一路要按日志里的 time 字段筛行；窗口请写 30m/1h/45s/2d，或确认本机 date 支持 -v/-d）"; downgrade_to_fail
  else
    LOG_SOURCE="${LOG_DESC}（起点 ${CUT}）"
    LOG_LINES="$(awk -v cut="$CUT" '
      { i = index($0, "\"time\":\"")
        if (i == 0) next
        ts = substr($0, i + 8, 19)
        if (ts >= cut) print }' "$BRIDGE_LOG_FILE")"
  fi
elif command -v docker >/dev/null 2>&1; then
  # 窗口必须先换算成秒再交给 docker：`docker logs --since` 用 Go 的 ParseDuration，实测只认
  # s/m/h，**不认 d**（`--since 1d` 退 1、报 invalid value for "since"）。原样把用户写法传下去，
  # 同一个 1d 在文件那一路能读、在容器这一路读不到，而且它的报错还会被下面"容器没运行"
  # 那格的文案盖成无关原因。换成 <秒>s 之后两条日志源的窗口口径才是同一个。
  WINDOW_SECONDS="$(since_seconds "$SINCE")"   # 入口闸保证写法必是 <整数><s|m|h|d>
  LOG_RAW="$(docker logs "$BRIDGE_CONTAINER" --since "${WINDOW_SECONDS}s" 2>&1)"
  DOCKER_RC=$?
  if [ "$DOCKER_RC" -ne 0 ]; then
    # docker 自己的第一行必须打出来：只报"容器未运行或名称不符"会把人引去猜容器名，
    # 而真因可能是守护进程没起、权限、或窗口语法 docker 不认。
    warn "读不到容器 ${BRIDGE_CONTAINER} 的日志（docker 退 ${DOCKER_RC}）：$(printf '%s\n' "$LOG_RAW" | head -1)｜容器名由 BRIDGE_CONTAINER 指定；开发态 go run/air 没有容器，请改给 BRIDGE_LOG_FILE"; downgrade_to_warn
    LOG_LINES=""
    LOG_SOURCE=""
  elif [ -z "$LOG_RAW" ]; then
    # 读通了、只是这个窗口内一条都没有：这和"数据源读不到"是两回事，
    # 留 LOG_SOURCE 有值，让下面「日志源可读但窗口内零行」出那条准确判词。
    LOG_LINES=""
    LOG_SOURCE="${LOG_DESC}（--since ${WINDOW_SECONDS}s）"
  else
    LOG_LINES="$LOG_RAW"
    LOG_SOURCE="${LOG_DESC}（--since ${WINDOW_SECONDS}s）"
  fi
else
  warn "既没给 BRIDGE_LOG_FILE，也没检测到 docker ⇒ 日志这一路没有数据源"; downgrade_to_warn
fi

if [ -n "$LOG_LINES" ]; then
  dim "日志来源: ${LOG_SOURCE}"
  dim "窗口内日志行数:                   $(printf '%s\n' "$LOG_LINES" | grep -c '' || true)"
  cnt_ingest=$(printf '%s\n' "$LOG_LINES" | grep -c 'http_ingest_request' || true)
  cnt_ingest_ok=$(printf '%s\n' "$LOG_LINES" | grep -c 'http_ingest_response' || true)
  cnt_ingest_err=$(printf '%s\n' "$LOG_LINES" | grep -c 'http_ingest_failed' || true)
  cnt_api=$(printf '%s\n' "$LOG_LINES" | grep -c '"event":"api_interaction"' || true)
  # 仅统计 bridge 相关错误（避免平台端/触达工具等无关噪声）。4xx 与 5xx 必须分开看：
  # 4xx 是调用方没带对凭证/参数（开发机上大量是仿真脚本的负向用例），5xx 才是服务端真把
  # 请求处理挂了。混成一个数，读的人要么跟着 119 条 4xx 紧张，要么把 2 条 500 当噪声跳过。
  bridge_err_lines=$(printf '%s\n' "$LOG_LINES" | grep -E '"event":"api_interaction"' | grep -E '/bridge' | grep -E '"status":[45][0-9][0-9]' || true)
  cnt_err_4xx=0
  cnt_err_5xx=0
  if [ -n "$bridge_err_lines" ]; then
    cnt_err_4xx=$(printf '%s\n' "$bridge_err_lines" | grep -cE '"status":4' || true)
    cnt_err_5xx=$(printf '%s\n' "$bridge_err_lines" | grep -cE '"status":5' || true)
  fi
  # ingest 失败是写侧没落库，归到服务端故障一侧
  cnt_err_5xx=$(( cnt_err_5xx + $(printf '%s\n' "$LOG_LINES" | grep -c 'http_ingest_failed' | tr -d ' ') ))
  cnt_err=$(( cnt_err_4xx + cnt_err_5xx ))

  dim "桥接上报(http_ingest_request): ${cnt_ingest} 次"
  dim "桥接响应(http_ingest_response): ${cnt_ingest_ok} 次"
  dim "桥接失败(http_ingest_failed):   ${cnt_ingest_err} 次"
  dim "API 交互日志(api_interaction):  ${cnt_api} 条"
  dim "桥接相关错误(4xx/5xx):          ${cnt_err} 条（调用方 4xx ${cnt_err_4xx} / 服务端 5xx ${cnt_err_5xx}）"

  if [ "$cnt_ingest" -gt 0 ] && [ "$cnt_ingest_err" -eq 0 ]; then
    ok "上报日志正常：桥接扩展在持续上行消息"
  elif [ "$cnt_ingest" -gt 0 ] && [ "$cnt_ingest_err" -gt 0 ]; then
    warn "上报日志存在失败：${cnt_ingest_err} 次 ingest 失败"; downgrade_to_warn
  elif [ "$cnt_ingest" -eq 0 ]; then
    warn "近 ${SINCE} 无桥接上报日志（可能扩展离线 / 渠道无流量 / 日志源里根本没有 ingest 记录）"; downgrade_to_warn
  fi
  if [ "$cnt_err_5xx" -gt 0 ]; then
    fail "近 ${SINCE} 有 ${cnt_err_5xx} 条服务端故障级桥接请求（5xx 或 ingest 未落库），这是要立刻查的那类"
    downgrade_to_fail
  elif [ "$cnt_err_4xx" -gt 0 ]; then
    warn "近 ${SINCE} 有 ${cnt_err_4xx} 条 4xx：调用方凭证/参数不对（扩展没带 X-Bridge-Token、渠道名写错等），服务端本身没坏"
    downgrade_to_warn
  fi
  if [ "$cnt_err" -gt 0 ]; then
    printf '%s\n' "$LOG_LINES" | grep -E '"event":"api_interaction".*/bridge' | grep -iE '"status":[45]' | tail -n 5 | while read -r l; do dim "$l"; done
  fi
else
  if [ -n "$LOG_SOURCE" ]; then
    # 数据源是好的、窗口里确实一行都没有：这和「读不到源」是两回事，得单列出来，
    # 否则进程静默（或 time 字段格式/时区对不上筛选）会被读成「日志无异常」。
    warn "日志源可读（${LOG_SOURCE}）但窗口 ${SINCE} 内零行 ⇒ 进程这段时间没打日志，或日志时间格式与筛选口径不符；这一节不等于「无异常」"; downgrade_to_warn
  else
    warn "无日志数据可分析（数据源没落地，日志节的结论缺失，不等于「无异常」）"; downgrade_to_warn
  fi
fi

# ---------- 2) 数据库信号 ----------
echo
echo "【2/2】数据库数据"

PSQL=()
if command -v psql >/dev/null 2>&1 && [ -n "$DB_PASSWORD" ]; then
  PSQL=(psql -X -tA -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME")
  export PGPASSWORD="$DB_PASSWORD"
else
  if ! command -v psql >/dev/null 2>&1; then
    warn "未检测到 psql 客户端，跳过数据库分析"
  else
    warn "未在 .env 找到 POSTGRES_PASSWORD，跳过数据库分析"
  fi
fi

psql_val() {
  if [ ${#PSQL[@]} -eq 0 ]; then echo "N/A"; return; fi
  local out
  out="$("${PSQL[@]}" -c "$1" 2>/dev/null)"
  [ -z "$out" ] && out="0"
  echo "$out"
}

if [ ${#PSQL[@]} -gt 0 ]; then
  # 在线判定必须与服务端同一条口径（internal/bridge/account_repo.go 的 isOnlineByLastSync）：
  # status<>'offline' 且 last_sync_at 非空且距今 < bridge.online_grace_window（默认 30 秒）。
  # 只数 status='online' 数的是粘住的列：扩展断开后没人把它改回去，本机实测 152 行标 online、
  # 服务端却判 0 个在线（最近一次同步是 20 天前），于是这条巡检常年报
  # 「桥接账号在线：153/155 [OK]」——运维据此以为链路健康，实际补投门早已按离线走。
  grace_raw="$(psql_val "SELECT param_value FROM config_params WHERE param_group='bridge' AND key='online_grace_window';")"
  grace_num="${grace_raw%%[!0-9]*}"
  grace_unit="${grace_raw#"$grace_num"}"
  grace_unparsed=false
  case "$grace_unit" in
    "" | s)  online_grace_secs="${grace_num:-30}" ;;
    m)       online_grace_secs=$(( ${grace_num:-30} * 60 )) ;;
    h)       online_grace_secs=$(( ${grace_num:-30} * 3600 )) ;;
    ms)      online_grace_secs=$(( ${grace_num:-30000} / 1000 )) ;;
    *)       online_grace_secs=30; grace_unparsed=true ;;
  esac
  case "$online_grace_secs" in '' | *[!0-9]*) online_grace_secs=30; grace_unparsed=true ;; esac
  [ "$online_grace_secs" -le 0 ] && online_grace_secs=30
  # 后端 GetDuration 吃 Go duration 全文（"1m30s" 也认），这里只认单一数值＋单位；
  # 读不懂就退回 30s 并显式报警——退回的方向是"更少地判在线"，不会造出假绿。
  if [ "$grace_unparsed" = true ]; then
    warn "bridge.online_grace_window 读数 [${grace_raw}] 不是「单一数值＋ms/s/m/h」形状，在线判定退回默认 30s（可能比后端更严）"
  fi
  # ONLINE_PRED 与后端 isOnlineByLastSync 逐条等价；改后端口径必须同步改这里。
  # @A@ 是关系限定符占位：直查 bridge_accounts 用表名，嵌进子查询时用别名——
  # message_hub 自己也有 status 列，不限定会静默比到外层那张表上。
  ONLINE_PRED_TPL="@A@.status <> 'offline' AND @A@.last_sync_at IS NOT NULL
                   AND now() - @A@.last_sync_at < interval '${online_grace_secs} seconds'"
  ONLINE_PRED="${ONLINE_PRED_TPL//@A@/bridge_accounts}"

  # 桥接渠道白名单：本脚本统计的「下行队列」只可能是这些渠道。权威在 channelgw 注册表
  # （internal/bridge/channel.go 的 init 把 gw.Default.Names() 注入 service），
  # SQL 里只能手抄一份，所以配两条漂移守卫：白名单落后于 bridge_accounts 实际渠道 ⇒ WARN，
  # 非桥接渠道的出站 pending 单独打印（不静默丢弃），否则新增渠道会从报表里消失。
  BRIDGE_PLATFORMS_SQL="'douyin','xiaohongshu','kuaishou','xianyu','tiktok'"
  inbound_1h=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='inbound' AND created_at > now() - interval '1 hour';")
  inbound_24h=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='inbound' AND created_at > now() - interval '24 hours';")
  pending_total=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL});")
  # 注意：EXTRACT(EPOCH ...) 返回「秒」，须 /60 换算为分钟（曾误当分钟导致阈值失真）
  pending_oldest_min=$(psql_val "SELECT COALESCE((EXTRACT(EPOCH FROM (now() - min(COALESCE(sent_at, created_at))))/60)::int, 0) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL});")
  # 不可达/待观察目标：
  #  - 占位账号(<channel>-unknown)：真正不可达，后端已标 failed（不会进 here，但若存量则计入）。
  #  - 昵称派生会话(conv:<名>)：前端现已尝试按列表项 name 匹配投递，可尽力送达；
  #    打不开的会留 pending，下一轮 downlink 仍可重试，归为「待观察」而非「孤儿」。
  pending_placeholder=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL}) AND account_id LIKE '%-unknown';")
  pending_convname=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL}) AND conversation_id LIKE 'conv:%';")
  # 不可达/待观察并集（UNION，非求和）：占位账号与 conv: 名有大量重叠，求和会虚高导致「可达目标」变负。
  pending_undeliverable=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL}) AND (account_id LIKE '%-unknown' OR conversation_id LIKE 'conv:%');")
  pending_deliverable=$(( pending_total - pending_undeliverable ))
  pending_oldest_deliverable_min=$(psql_val "SELECT COALESCE((EXTRACT(EPOCH FROM (now() - min(COALESCE(sent_at, created_at))))/60)::int, 0) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform IN (${BRIDGE_PLATFORMS_SQL}) AND account_id NOT LIKE '%-unknown' AND conversation_id NOT LIKE 'conv:%';")
  failed_total=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='failed' AND platform IN (${BRIDGE_PLATFORMS_SQL});")
  # 两种自动判弃必须分开数：同一列 push_error 写的是不同根因，混在一起就等于
  # 「重推在失败」和「账号再也不回来」看起来是同一件事，而排查方向完全相反。
  failed_exhausted=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='failed' AND push_error='outbound_push_exhausted' AND platform IN (${BRIDGE_PLATFORMS_SQL});")
  failed_orphan=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='failed' AND push_error='outbound_orphan_expired' AND platform IN (${BRIDGE_PLATFORMS_SQL});")
  # 非桥接渠道的出站 pending：直投渠道里 telegram/feishu/qq/email 已在发送成功时把行结算成
  # delivered，剩下的 pending 就是真没发出去的（例如企微「先落库后发送」在缺 CorpID 时整块跳过、
  # 以及本批未覆盖的其它 producer）。它不是桥接投递故障，绝不能混进下面的积压归因；
  # 若把它算成故障，运维会去查扩展队列，而真实问题是写入侧没结算状态。
  pending_nonbridge=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND platform NOT IN (${BRIDGE_PLATFORMS_SQL});")
  pending_nonbridge_ai=$(psql_val "SELECT count(*) FROM message_hub WHERE direction='outbound' AND status='pending' AND is_ai_reply=true AND platform NOT IN (${BRIDGE_PLATFORMS_SQL});")
  # 卡住的 AI 回复：仅统计「桥接渠道 + 可达目标」且 >10min 未送达；不可达目标本就投不出，不计入 FAIL。
  STUCK_BASE="m.direction='outbound' AND m.status='pending' AND m.is_ai_reply=true
              AND m.platform IN (${BRIDGE_PLATFORMS_SQL})
              AND m.account_id NOT LIKE '%-unknown' AND m.conversation_id NOT LIKE 'conv:%'
              AND COALESCE(m.sent_at, m.created_at) < now() - interval '10 minutes'"
  stuck_ai_deliverable=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE};")
  # 区分「会话在系统存在(用户未在界面打开)」vs「会话在系统无记录(可能已删/屏蔽)」：
  # 前者属小红书无法主动打开屏外会话的正常待观察(WARN)；后者才是真实投递故障(FAIL)。
  stuck_exists=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE}
    AND EXISTS (SELECT 1 FROM inbox_conversations i WHERE i.conversation_id = m.conversation_id);")
  stuck_missing=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE}
    AND NOT EXISTS (SELECT 1 FROM inbox_conversations i WHERE i.conversation_id = m.conversation_id);")
  # 再叠一层「账号到底在不在线」：outbox 只服务正在拉取的扩展，账号按服务端口径离线时
  # 这条回复本就该躺在待办里等重连补投，把它报成「投递故障」会把运维引向错误的排查方向。
  ONLINE_SUB="${ONLINE_PRED_TPL//@A@/b}"
  stuck_online=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE}
    AND EXISTS (SELECT 1 FROM bridge_accounts b WHERE b.channel = m.platform AND b.account_id = m.account_id AND ${ONLINE_SUB});")
  stuck_online_missing=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE}
    AND NOT EXISTS (SELECT 1 FROM inbox_conversations i WHERE i.conversation_id = m.conversation_id)
    AND EXISTS (SELECT 1 FROM bridge_accounts b WHERE b.channel = m.platform AND b.account_id = m.account_id AND ${ONLINE_SUB});")
  # 无注册行的那批既不是"在线"也不是"断开过"：bridge_accounts 里根本没有这个账号
  # （实测本机 68 条积压全属此类，账号名形如 acct-e2e-*），任何扩展都不会来拉它们，
  # 报成"扩展断开了"会把运维引向重启扩展这种无效动作。
  stuck_unregistered=$(psql_val "SELECT count(*) FROM message_hub m WHERE ${STUCK_BASE}
    AND NOT EXISTS (SELECT 1 FROM bridge_accounts b WHERE b.channel = m.platform AND b.account_id = m.account_id);")
  stuck_offline=$(( stuck_ai_deliverable - stuck_online - stuck_unregistered ))
  acct_total=$(psql_val "SELECT count(*) FROM bridge_accounts;")
  acct_online=$(psql_val "SELECT count(*) FROM bridge_accounts WHERE ${ONLINE_PRED};")

  dim "上行消息(inbound) 近1h/近24h: ${inbound_1h} / ${inbound_24h}"
  dim "下行队列(outbound) 待发送/失败: ${pending_total} / ${failed_total}"
  dim "  失败按根因: 重推 20 次到界 ${failed_exhausted} / 账号久不回来(孤儿结算) ${failed_orphan}"
  dim "  其中 可达目标 / 占位账号(-unknown) / 昵称会话(conv:名): ${pending_deliverable} / ${pending_placeholder} / ${pending_convname}"
  dim "下行最旧待发送(全部/可达): ${pending_oldest_min} / ${pending_oldest_deliverable_min} 分钟"
  dim "卡住的 AI 回复(可达目标,>10min): ${stuck_ai_deliverable}（会话存在待观察:${stuck_exists} / 会话无记录故障:${stuck_missing}）"
  dim "  其中按服务端口径账号仍在线:${stuck_online}（在线且会话无记录=${stuck_online_missing} 才是真故障）/ 账号已离线:${stuck_offline}（等扩展重连补投）/ bridge_accounts 无注册行:${stuck_unregistered}"
  dim "非桥接渠道出站 pending(不计入上面归因): ${pending_nonbridge}（其中 AI 回复 ${pending_nonbridge_ai}）"
  dim "桥接账号 总数/在线:            ${acct_total} / ${acct_online}  (在线=last_sync_at 在 ${online_grace_secs}s 内，与服务端同口径)"

  # 白名单漂移守卫在这段只取数，判词留到下面的【结论】段统一下（那里是全部降级语句的落点，
  # 读数与判词成对出现，改口径时不会只动一半）。overall 现在在脚本开头初始化，
  # 提前调用 downgrade_* 也安全。
  acct_offlist=$(psql_val "SELECT count(*) FROM bridge_accounts WHERE channel NOT IN (${BRIDGE_PLATFORMS_SQL});")
  offlist_names=""
  if [ "$acct_offlist" -gt 0 ]; then
    offlist_names=$(psql_val "SELECT string_agg(DISTINCT channel, ',') FROM bridge_accounts WHERE channel NOT IN (${BRIDGE_PLATFORMS_SQL});")
  fi

  # 分渠道账号
  echo
  dim "桥接账号按渠道 (channel | 总数 | 在线${online_grace_secs}s):"
  if [ -n "$DB_PASSWORD" ]; then
    PGPASSWORD="$DB_PASSWORD" "${PSQL[@]}" -c "SELECT channel, count(*), count(*) FILTER (WHERE ${ONLINE_PRED}) FROM bridge_accounts GROUP BY channel ORDER BY channel;" 2>/dev/null | while read -r line; do dim "  $line"; done
  fi

  # 会话按平台
  echo
  dim "收件箱会话按平台 (platform | 会话数):"
  PGPASSWORD="$DB_PASSWORD" "${PSQL[@]}" -c "SELECT platform, count(*) FROM inbox_conversations GROUP BY platform ORDER BY platform;" 2>/dev/null | while read -r line; do dim "  $line"; done

  # ---- 健康判定 ----
  echo
  echo "【结论】"
  if [ "$acct_total" -eq 0 ]; then
    warn "无桥接账号：扩展尚未连接注册（bridge_accounts 为空）"; downgrade_to_warn
  elif [ "$acct_online" -eq 0 ]; then
    warn "所有桥接账号均离线：扩展可能已全部断开"; downgrade_to_warn
  else
    ok "桥接账号在线：${acct_online}/${acct_total}"
  fi

  if [ "$acct_offlist" -gt 0 ]; then
    warn "bridge_accounts 里有 ${acct_offlist} 个账号属白名单外渠道 [${offlist_names}]：本脚本的桥接渠道白名单落后于服务端渠道注册表（权威见 internal/bridge/channel.go 的 gw.Default.Names()），这些渠道的积压不会进上面的 FAIL 判定"; downgrade_to_warn
  fi

  if [ $(( stuck_online + stuck_offline + stuck_unregistered )) -ne "$stuck_ai_deliverable" ]; then
    fail "积压归因不自洽：在线 ${stuck_online} + 离线 ${stuck_offline} + 无注册 ${stuck_unregistered} ≠ 可达积压 ${stuck_ai_deliverable}（口径被改动，或 bridge_accounts.channel 与 message_hub.platform 不同名）"; downgrade_to_fail
  elif [ "$stuck_online_missing" -gt 0 ]; then
    fail "有 ${stuck_online_missing} 条 AI 回复：账号按服务端口径仍在线（${online_grace_secs}s 内同步过）、会话在系统却无记录（可能已删/屏蔽），超过 10 分钟未送达——真实投递故障"; downgrade_to_fail
  elif [ "$stuck_online" -gt 0 ]; then
    warn "有 ${stuck_online} 条 AI 回复：账号在线却未送达（会话存在:${stuck_exists} 多为小红书等无法主动打开屏外会话，需用户在网页端打开该会话；会话无记录:${stuck_missing} 请核对是否已删)"; downgrade_to_warn
  elif [ "$stuck_unregistered" -gt 0 ] || [ "$stuck_offline" -gt 0 ]; then
    warn "有 $(( stuck_unregistered + stuck_offline )) 条 AI 回复积压超 10 分钟且当前不可能被投递：bridge_accounts 无注册行 ${stuck_unregistered} 条（任何扩展都不会来拉这批，要清队列须先人工确认归属）、账号已离线 ${stuck_offline} 条（重连后按 ${online_grace_secs}s 窗口补投）——先确认扩展是否还在运行，而不是查投递链路"; downgrade_to_warn
  elif [ "$pending_deliverable" -gt 0 ] && [ "$pending_oldest_deliverable_min" -gt 15 ]; then
    warn "下行队列有 ${pending_deliverable} 条可达目标待发送，最旧已 ${pending_oldest_deliverable_min} 分钟（xiaohongshu 等无法主动打开会话，需用户在网页端打开该会话才下发）"; downgrade_to_warn
  elif [ "$pending_deliverable" -gt 0 ]; then
    ok "下行队列有 ${pending_deliverable} 条可达目标待发送（最旧 ${pending_oldest_deliverable_min} 分钟，正常）"
  else
    ok "下行队列无可达目标积压"
  fi

  if [ "$pending_placeholder" -gt 0 ]; then
    warn "下行队列有 ${pending_placeholder} 条 pending 属占位账号(<channel>-unknown)，真正不可达（后端已对新增标 failed）；存量建议归档"; downgrade_to_warn
  fi
  if [ "$pending_convname" -gt 0 ]; then
    warn "下行队列有 ${pending_convname} 条 pending 属昵称派生会话(conv:<名>)：前端现会按列表项 name 尝试投递，打不开则留 pending 下一轮重试（待观察，非必失败）"; downgrade_to_warn
  fi

  if [ "$failed_total" -gt 0 ]; then
    warn "下行队列有 ${failed_total} 条 failed 状态消息（含不可达目标标记，需排查投递失败原因）"; downgrade_to_warn
  fi

  if [ "$inbound_1h" -eq 0 ] && [ "$inbound_24h" -gt 0 ]; then
    warn "近 1 小时无客户上行消息（可能渠道静默 / 扩展离线）"; downgrade_to_warn
  fi

  if [ "$overall" = "OK" ]; then
    echo
    ok "Bridge 功能整体正常 ✅"
  elif [ "$overall" = "WARN" ]; then
    echo
    warn "Bridge 功能基本可用，但存在需关注项 ⚠️"
  else
    echo
    fail "Bridge 功能异常，请立即排查 ❌"
  fi
else
  warn "无数据库数据可分析"; downgrade_to_warn
fi

echo "============================================================"

# 退出码跟着判词走：判词只印在终端上，cron/CI 读的是 rc。FAIL 必须让 rc 非 0，
# 否则「Bridge 功能异常，请立即排查」这条红字配上 rc=0，等于把巡检本身变成假绿。
# WARN 仍退 0：安静但健康的部署天天会出 WARN，把它做成非 0 只会让人关掉这个巡检。
case "${overall}" in
  FAIL) exit 1 ;;
  *)    exit 0 ;;
esac
