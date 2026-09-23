#!/usr/bin/env bash
# =============================================================
# check-admin-default-credential.sh —— 「仓库公开的口令还能不能登进这套实例」探针
#
# 为什么要有这个脚本（不是又一份文档）：本仓的超管 seed 口令是**随开源仓库公开的字节**
# （user-server/cmd/seed/seed_users.go 的 seedPasswordDefault），照 README 走完 bootstrap
# 又没换过口令的部署，admin 的密码就是这四个字节能猜中的值。docs/DEPLOYMENT_GUIDE.md §6.2
# 的加固路线②早就把判据写成"拿仓库公开的默认值试登录，必须从 200 变 401"，但那条判据此前
# 只存在于文档里、靠人记着手工 curl —— 脚本化的轮换器见同目录 rotate-admin-password.sh，
# 而"换没换成"这条读数此前没有可复跑的入口。本脚本补的就是这条读数。
#
# 与防爆破中间件的关系（这决定了默认只发一次请求）：/api/auth/login 挂着
# middleware.BruteForceGuard("auth.login")，口径 Window 15m / MaxFailures 5 /
# LockDuration 30m，计数键 ClientIP|endpoint，进程内计数 ⇒ **探针自己就是爆破载荷**。
# 在共享开发实例上扫满 5 次会把 127.0.0.1 的登录锁住 30 分钟，连带别的泳道的 e2e 一起红，
# 而那红看起来像"别人的用例坏了"。所以：
#   - 默认只发 1 次尝试（seed 公开值，最可能命中的那个），命中即停、不再攒锁；
#   - --full-ladder 才扫满全部公开字面量（最多 4 次），且要先接受下面打印的锁风险；
#   - 连通性预检发的是 '{}'（请求体绑定校验在进 service 之前就 400 返回），
#     它不记失败计数，因此不占上面那 1/4 次额度。
#
# 公开字面量不在本脚本里另抄一份：seed 值从 seed_users.go 抽，其余从
# user-web/tests/auth.setup.spec.js 的 CANDIDATES 数组抽 —— 这两处本身就是"本仓公开了
# 哪些口令"的事实源，抄进脚本就成了第三个会过期的副本（改了源码忘了改脚本 ⇒ 探针恒绿）。
#
# 用法：
#   bash scripts/check-admin-default-credential.sh                 # 单次探针（推荐）
#   PROBE_BASE_URL=http://127.0.0.1:8204 bash scripts/check-admin-default-credential.sh
#   bash scripts/check-admin-default-credential.sh --full-ladder   # 扫满公开字面量
#   bash scripts/check-admin-default-credential.sh --list          # 只印抽到了哪些来源，不发请求
#
# 退出码：0 公开口令不认（绿）/ 1 公开口令仍能登录（红，附改法）/ 2 前置不满足（没得出结论）。
#
# 安全口径：全程只印形状（用户名、口令长度、来源 file:line、HTTP 码），不回显口令值；
# 口令从 stdin 进 curl（不进 argv，避开 ps 可见面）。
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

SEED_SRC="${SEED_SRC:-$PROJECT_DIR/user-server/cmd/seed/seed_users.go}"
WEB_SETUP_SRC="${WEB_SETUP_SRC:-$PROJECT_DIR/user-web/tests/auth.setup.spec.js}"

FULL_LADDER=0
LIST_ONLY=0
for arg in "$@"; do
  case "$arg" in
    --full-ladder) FULL_LADDER=1 ;;
    --list)        LIST_ONLY=1 ;;
    -h|--help) awk 'NR==1 {next} /^#/ {print; next} {exit}' "$0"; exit 0 ;;
    *) echo "未知参数：${arg}（可用 --full-ladder / --list）" >&2; exit 2 ;;
  esac
done

BASE_URL="${PROBE_BASE_URL:-http://127.0.0.1:${USER_SERVER_PORT:-8204}}"
LOGIN_PATH="${PROBE_LOGIN_PATH:-/api/auth/login}"
USER="${PROBE_USERNAME:-admin}"

die2() { echo "PRECHECK-FAILED: $*" >&2; exit 2; }

# ---- 抽公开字面量（顺序即尝试顺序：seed 默认值最可能命中，放第一个）----
[[ -f "$SEED_SRC" ]] || die2 "缺 seed 源 ${SEED_SRC}（公开口令的事实源，抽不到就没有判据）"
SEED_PW="$(sed -n 's/^const seedPasswordDefault = "\([^"]*\)"$/\1/p' "$SEED_SRC" | head -1 | tr -d '\r')"
[[ -n "$SEED_PW" ]] || die2 "从 ${SEED_SRC} 没抽出 seedPasswordDefault —— 常量改名/改形状会让本探针恒绿，先修探针"
if printf '%s' "$SEED_PW" | grep -q '[[:space:]]'; then
  die2 "抽出的 seedPasswordDefault 含空白字符（长度 ${#SEED_PW}）—— 抽取锚点串到了别的行"
fi
echo "seed: 公开默认口令 长度=${#SEED_PW} 来源=${SEED_SRC#"$PROJECT_DIR"/}"

LADDER=("$SEED_PW")
if [[ -f "$WEB_SETUP_SRC" ]]; then
  while IFS= read -r line; do
    [[ -n "$line" ]] || continue
    seen=0
    for have in "${LADDER[@]}"; do
      [[ "$have" == "$line" ]] && seen=1 && break
    done
    [[ "$seen" -eq 1 ]] || LADDER+=("$line")
  # 只抽数组**项**：先把收尾行的 `].filter(…)` 尾巴截掉（那行带 `'string'` 与 `''` 两对引号，
  # 上一版照抽不误 ⇒ 候选从 4 项涨成 5 项，多出来的 `string` 会真发一次失败登录（`''` 那条被下面的
  # `-n` 判空挡掉），而防爆破口径就是 5 次/15m —— 一次余量都不该白烧），再剔掉 `//` 注释行。
  done < <(sed -n '/const CANDIDATES = \[/,/^\]/p' "$WEB_SETUP_SRC" \
            | sed -e '/^\]/ s/\].*//' \
            | grep -v '^[[:space:]]*//' \
            | grep -o "'[^']*'" | tr -d "'")
  echo "ladder: e2e 候选口令 ${#LADDER[@]} 项（含 seed 值）来源=${WEB_SETUP_SRC#"$PROJECT_DIR"/}"
else
  echo "ladder: 未找到 ${WEB_SETUP_SRC#"$PROJECT_DIR"/}，本轮只试 seed 公开值 1 项"
fi

if [[ "$LIST_ONLY" -eq 1 ]]; then
  printf 'list: 可尝试项=%s（未发任何请求）\n' "${#LADDER[@]}"
  exit 0
fi

command -v curl >/dev/null 2>&1 || die2 "找不到 curl"

# code_of：口令从 stdin 进 curl（不进 argv，避开 ps 可见面），JSON 里的反斜杠与双引号先转义
# （纯 bash 替换，不用 read 兜转义 —— 无换行输入会让 read 返回 1，在 set -e 下把调用点带崩）。
code_of() {
  local pw="$1"
  pw="${pw//\\/\\\\}"
  pw="${pw//\"/\\\"}"
  printf '{"username":"%s","password":"%s"}' "$USER" "$pw" \
    | curl -s -o /dev/null -w '%{http_code}' -m "${PROBE_TIMEOUT:-10}" -X POST "${BASE_URL}${LOGIN_PATH}" \
          -H 'Content-Type: application/json' --data-binary @-
}

# ---- 预检：'{}' 应当在绑定校验处 400，据此证明"服务可达且路径对"，且不消耗爆破计数 ----
PREFLIGHT="$(printf '{}' | curl -s -o /dev/null -w '%{http_code}' -m "${PROBE_TIMEOUT:-10}" \
  -X POST "${BASE_URL}${LOGIN_PATH}" -H 'Content-Type: application/json' --data-binary @-)"
case "$PREFLIGHT" in
  400) : ;;
  000) die2 "服务不可达：${BASE_URL}${LOGIN_PATH}（先起服务，或把 PROBE_BASE_URL 指对）" ;;
  429) die2 "预检就被防爆破锁住（Retry-After 未取）——等锁过期再跑，别在锁窗口内下结论" ;;
  *)   die2 "预检期望 400、实得 ${PREFLIGHT} —— 登录路径或请求形状变了，本探针的判据已失效" ;;
esac
echo "preflight: ${BASE_URL}${LOGIN_PATH} 空体返回 400（可达、未计入爆破次数）"

ATTEMPTS=("${LADDER[@]}")
if [[ "$FULL_LADDER" -eq 0 ]]; then
  ATTEMPTS=("${LADDER[0]}")
  echo "mode: 单次探针（只试 seed 公开值；另有 $(( ${#LADDER[@]} - 1 )) 项 e2e 候选未试，需 --full-ladder）"
else
  echo "mode: --full-ladder，最多 ${#ATTEMPTS[@]} 次尝试（防爆破口径 5 次/15m ⇒ 触发即锁 30m）"
fi

for i in "${!ATTEMPTS[@]}"; do
  pw="${ATTEMPTS[$i]}"
  n=$((i + 1))
  code="$(code_of "$pw")"
  echo "probe-${n}: 口令长度=${#pw} http=${code}"
  case "$code" in
    200)
      echo "RESULT: RED —— 超管账号 ${USER} 仍可用仓库公开的口令登录（第 ${n} 项命中）。"
      echo "改法：bash scripts/rotate-admin-password.sh --with-env 换掉库里的哈希与 .env 的 SEED_PASSWORD；"
      echo "      若这套实例本不该对外，另把 SERVER_HOST 收回回环（docs/DEPLOYMENT_GUIDE.md §6.2 路线①）。"
      exit 1 ;;
    401) ;;
    429)
      echo "RESULT: 未得出结论 —— 第 ${n} 次尝试触发防爆破锁（rc=2，不猜绿）。等锁过期后复跑。"
      exit 2 ;;
    000) echo "RESULT: 未得出结论 —— 第 ${n} 次尝试连不上服务（进程/网络中途变了）"; exit 2 ;;
    *)
      echo "RESULT: 未得出结论 —— 第 ${n} 次尝试返回 ${code}（既非 200 也非 401/429），判据不覆盖这个形状。"
      exit 2 ;;
  esac
done

echo "RESULT: GREEN —— ${#ATTEMPTS[@]} 次公开口令尝试全部 401，这套实例不认仓库公开的口令。"
exit 0

# =============================================================
# 反向测试（改完本脚本必须逐格真跑；本批实测 9/9 按判据收口）
#
# 夹具是一个"只做登录判定的假服务"，三条分流必须都实现，否则下面几格会假绿：
#   1) path != /api/auth/login        → 404   （喂 R7）
#   2) body == '{}'（Content-Length 2）→ 400   （预检档，不占爆破计数）
#   3) 其余按 password 是否在白名单 → 200/401；另留一个"非空体一律 429 + Retry-After"开关（喂 R6）
# 端口用 bind(0) 现取，就绪以"预检拿到 400"为信号（别 sleep 猜）。
#
#   R1 白名单为空                     期望 rc=0 / 印 RESULT: GREEN
#   R2 白名单={seed 公开值}           期望 rc=1 / 印 RESULT: RED 且点名 rotate-admin-password.sh
#   R3 白名单={某个 e2e 候选} 单次    期望 rc=0 —— 这条是"默认档覆盖面"的诚实读数，不是绿
#   R3b 同状态加 --full-ladder        期望 rc=1 —— 证明候选确实是从 spec 源码抽出来的（不是写死）
#   R4 SEED_SRC=/dev/null             期望 rc=2 "缺 seed 源"
#   R4b 把 seed_users.go 里那行 const 改名后存副本喂 SEED_SRC   期望 rc=2 "没抽出 seedPasswordDefault"
#       （变异要断言真的改动了源文本，否则等于没变异）
#   R5 PROBE_BASE_URL 指向无人监听的端口  期望 rc=2 且全输出里不得出现 GREEN
#   R6 打开 429 档                    期望 rc=2 "触发防爆破锁"
#   R7 PROBE_LOGIN_PATH=/api/not-found 期望 rc=2 "预检期望 400"
# R2/R3b 若不红、R4/R4b/R5/R6/R7 若不 rc=2 ⇒ 判据没牙，别提交。
# 注意 R1 与 R3 都是 rc=0：本探针绿只说明"试过的这几项不认"，别把它读成"口令已换"。
