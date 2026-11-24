#!/usr/bin/env bash
# check-secrets.sh — 阻止明文凭证进入版本管理（T-P0-05 的防复发闸门）
#
# 三条判定，缺一不可：
#  A. 真值比对：把本机 .env 里 PASSWORD/SECRET/TOKEN 类键的**实际值**在待纳管文件中全量搜索，
#     命中即 FAIL。零误报，且正是 bulk_seed/geo_full_test/deep_lib 那批事故的确切成因。
#  B. 字面量模式：搜索 password/secret/token 等键被赋非变量、非占位符的长字面量，
#     命中列在 scripts/.secret-allowlist 之外的即 FAIL。
#  B+. 裸值文件：对「名字含 secret/token 的点文件、*.bak-YYYYMMDD、*.key/*.pem/…」圈出的
#     嫌疑文件做整行裸值精判——B 段键名形态的盲区（2026-10-18 .merchant_api_secret.bak
#     事故形状），见下方 B+ 段注释。
#
# 扫描范围 = `git ls-files --cached --others --exclude-standard`，即"已跟踪 + 已 add -N +
# 未跟踪但未被 ignore"的文件，**按工作区内容**逐个 grep。
# 这里刻意不用 `git grep`：git grep 读的是 index 里的 blob，而 `git add -N`（intent-to-add）
# 在 index 里存的是空 blob —— 用 git grep 时"刚新建、正要提交"的文件会整份漏检，
# 而那恰恰是本闸门唯一要拦的时机（反向测试证实过这个漏洞）。
#
# 两种模式：
#   （默认，full）A + B 两条都跑 —— 需要本机 .env，属**本机/预推送**层。
#   --patterns-only 只跑 B 段 —— 不读、也不要求 .env，属 **CI** 层：CI 拿不到真凭证，
#     也不该拿到（把真凭证推上 runner 等于第二次泄露），所以 A 段永不上 CI。
#   CI 侧这一维另有 gitleaks（.github/workflows/gitleaks-config.yml），它抓公开高置信度模式，
#   覆盖不到本仓自定义键名的字面量赋值 —— 两者并列，不互相替代。
#
# 执行入口（三处，缺一处就回到"只有作者手动跑"的老状态）：
#   本机：git config core.hooksPath .githooks 后由 .githooks/pre-push 调
#         scripts/pre-push-secrets.sh（直调脚本而**不走** make audit-secrets：
#         make 把 recipe 失败一律压成 rc=2，1 与 2 的分别在那一层就丢了）；
#   手动：bash scripts/check-secrets.sh（仓根或任意子目录均可）；
#   CI：.github/workflows/secret-patterns.yml 调 --patterns-only，种入／摘除／豁免
#       三格由常驻反向测试 scripts/check-secrets-patterns.test.sh 持有（本机 CI 同一批格子）。
#
# 红因口径：命中一律只报 `文件:行号` ＋ 键名／值长，**命中的字面量本体不出屏**（--verbose 同口径）。
# 这道门的输出会落在终端回滚、CI 作业日志和本仓 logs/ 取证日志里；命中项若确为真凭证，
# 打印原文等于把它从 .env 抄进三个更公开的位置。豁免表需要"值形状"时请打开那个文件看：
# 真凭证绝不该被抄进 scripts/.secret-allowlist（那才是把泄露固化入库）。
#
#   B+ 段（裸值文件，2026-11-16 审计补口）：B 段按「键名+[:=]+值」形态匹配，**裸密钥文件**
#   （单行 hex/base64、无键名，如 config/.merchant_api_secret.bak-20261018 那次事故）永不命中。
#   B+ 按「文件名形状」圈嫌疑面（名字含 secret/token 的点文件、*.bak-YYYYMMDD、*.key/*.pem/
#   *.p12/*.pfx/*.gpg），再对整文件做「整行裸值」精判（≥32 纯 hex、≥40 且字母数字都有 base64 形态）。
#   嫌疑名单天然不含 gitignored 的本地密钥（.merchant_api_secret 这类合法落盘已被
#   --exclude-standard 排除在候选面外），不会把本机正常配置误报成泄露。
#
# 环境变量：ENV_FILE= 指 .env 的坐标；SECRET_ALLOWLIST_EXTRA= 追加一条豁免表（只追加、不替换，
#                                          给 scripts/check-secrets-patterns.test.sh 的 L6 用）。
#
# 退出码：
#   0 = 干净；
#   1 = 发现明文凭证；
#   2 = 无法执行（full 模式缺 .env、提取不到任何凭证键、不在 git 仓内、未知参数）。
#   2 与 1 必须分开：2 是"这一维没跑过"，把它当绿读就是假绿。
set -uo pipefail

VERBOSE=0
MODE=full
while [[ $# -gt 0 ]]; do
  case "$1" in
    --verbose) VERBOSE=1 ;;
    --patterns-only) MODE=patterns ;;
    --help | -h)
      # 头部注释即用法：印到第一条非注释行为止（写死行号的话，加两行说明就错位）
      awk 'NR > 1 && /^#/ { sub(/^# ?/, ""); print; next } !/^#/ { exit }' "$0"
      exit 0
      ;;
    *)
      echo "❌ 未知参数：$1（可用：--verbose / --patterns-only / --help）" >&2
      exit 2
      ;;
  esac
  shift
done

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "❌ 不在 git 仓内"; exit 2; }
cd "$ROOT" || exit 2

ENV_FILE=${ENV_FILE:-$ROOT/.env}
# 豁免表：仓内那张**永远生效**；SECRET_ALLOWLIST_EXTRA 只往里**追加**，不替换。
# （反向测试要靠这一维证"命中列在表内才放行"，而把临时豁免追加进仓内那张表，
#   等于占用共享工作树里一个已跟踪文件；整张替换掉又会把真表的既有豁免一起抹掉，
#   测出来的红因就不是它声称的那一条。）
ALLOWLIST=$ROOT/scripts/.secret-allowlist
ALLOW_EXTRA=${SECRET_ALLOWLIST_EXTRA:-}
FAIL=0

if [[ $MODE == full ]]; then
  [[ -f "$ENV_FILE" ]] || { echo "❌ 找不到 ${ENV_FILE}（可用 ENV_FILE=/path/to/.env 指定；CI 上没有真凭证，请改用 --patterns-only）"; exit 2; }
fi

# 从豁免表读取正则（每行一条，# 开头为注释；仓内表 + 可选的追加表都往 ALLOW_RE 里并）
ALLOW_RE=""
load_allow() { # $1=豁免表路径
  [[ -f $1 ]] || return 0
  while IFS= read -r line; do
    [[ -z $line || $line == \#* ]] && continue
    ALLOW_RE="${ALLOW_RE:+$ALLOW_RE|}($line)"
  done <"$1"
}
load_allow "$ALLOWLIST"
[[ -n $ALLOW_EXTRA ]] && load_allow "$ALLOW_EXTRA"

is_allowed() { # $1 = "path:lineno:content"
  [[ -z $ALLOW_RE ]] && return 1
  printf '%s' "$1" | grep -qE "$ALLOW_RE"
}

echo "══════ 待纳管文件清单 ══════"
# 待纳管文件清单（含未跟踪），一次生成两处复用
FILELIST=$(mktemp)
PAIRS=$(mktemp)
PATTERNS=$(mktemp)
BARELIST=$(mktemp)
trap 'rm -f "$PAIRS" "$PATTERNS" "$FILELIST" "$BARELIST"' EXIT
git ls-files --cached --others --exclude-standard >"$FILELIST"
echo "  候选文件 $(wc -l <"$FILELIST" | tr -d ' ') 个（git ls-files --cached --others --exclude-standard）"

scan_literal() { # $1=ERE 正则；扫描待纳管文件的工作区内容
  [[ -s $FILELIST ]] || return 0
  xargs grep -InHi -E -- "$1" <"$FILELIST" 2>/dev/null || true
}
scan_values() { # $1=字面量清单文件；逐个真值精确匹配
  [[ -s $FILELIST ]] || return 0
  xargs grep -InH -F -f "$1" -- <"$FILELIST" 2>/dev/null || true
}

# 命中行的"位置"部分（grep -InH 的 file:line 前两段）。红因一律只报到这里为止：
# 命中的字面量本体绝不出屏。理由不是洁癖 —— 这道门的输出会被抄进终端回滚、
# CI 作业日志和本仓的取证日志（logs/），一旦某条命中真是生产凭证，
# 打印原文等于把它从 .env 复制到了四个更公开的地方。A 段本来就是这个口径
# （只印键名与值长），B 段之前直出 ${h:0:160}，属同一族里没拉平的那一格。
hit_loc() { printf '%s' "$1" | cut -d: -f1,2; }

if [[ $MODE == patterns ]]; then
  echo "  模式：patterns-only ⇒ **跳过 A 段**（CI 无真凭证，也不该有）"
else
  echo "══════ A. 本机 .env 真值比对（${ENV_FILE}）══════"
fi

# A 段的取值来源：patterns-only 时整块不跑。这里用空输入而不是把下面 40 行包进 if，
# 是为了让"跳过"这件事只有**一个**开关（下面 nv==0 与 A 段绿读数两处都按它分档），
# 漏改任何一处都会退化成"印了一句假绿"，而假绿正是本闸立项要拦的东西。
ENV_SRC=$ENV_FILE
if [[ $MODE == patterns ]]; then
  ENV_SRC=/dev/null
fi

# 只取看起来是凭证的键，跳过空值、模板变量与已知的公开自举常量。
# 键名匹配用"后缀族"而不是单个词：早期版本写的是 (PASSW|SECRET|TOKEN|KEY)$，
# 而 POSTGRES_PASSWORD 并不以 PASSW 结尾，导致最高危的那几个键被静默跳过——
# 反向测试（把 .env 真值塞进新建文件）才暴露出来。PASSW[A-Z]* 才能盖住 PASSWORD/PASSWD。
CRED_KEY_RE='(PASSW[A-Z]*|PASSPHRASE|_PWD|SECRET|TOKEN|API_?KEY|_KEY|CREDENTIAL)$'
nv=0
while IFS='=' read -r k v; do
  k=$(printf '%s' "${k%$'\r'}" | tr -d '[:space:]' | tr '[:lower:]' '[:upper:]')
  k=${k#EXPORT}
  v=${v%$'\r'}
  v=${v#[\"\\\']}
  v=${v%[\"\\\']}
  [[ $k =~ $CRED_KEY_RE ]] || continue
  [[ ${#v} -ge 8 ]] || continue
  [[ -z $v || $v == \$* ]] && continue
  printf '%s\t%s\n' "$k" "$v" >>"$PAIRS"
  printf '%s\n' "$v" >>"$PATTERNS"
  nv=$((nv + 1))
done < <(grep -E '^[[:space:]]*(export[[:space:]]+)?[A-Za-z0-9_]+=' "$ENV_SRC" | sort -u)
if [[ $nv == 0 && $MODE == full ]]; then
  echo "  ❌ 未能从 $ENV_FILE 提取到任何凭证键，检查形同虚设（rc=2）"
  exit 2
fi
AHITS=""
if [[ $MODE == full && -s $PATTERNS ]]; then
  AHITS=$(scan_values "$PATTERNS")
  echo "  已载入 $nv 枚真值，逐个在候选文件里精确匹配"
fi
if [[ -n $AHITS ]]; then
  while IFS= read -r h; do
    [[ -z $h ]] && continue
    keyname="<未知>"
    keylen=0
    while IFS=$'\t' read -r k v; do
      if [[ $h == *"$v"* ]]; then keyname=$k; keylen=${#v}; break; fi
    done <"$PAIRS"
    if is_allowed "$h"; then
      [[ $VERBOSE == 1 ]] && echo "  (allow) [$keyname] $(hit_loc "$h")"
    else
      echo "  ❌ [$keyname] $(hit_loc "$h") 命中（值长 ${keylen}，本体不打印）"
      FAIL=1
    fi
  done <<<"$AHITS"
else
  if [[ $MODE == patterns ]]; then
    echo "  （patterns-only：A 段未跑，本行不作出「干净」断言）"
  else
    echo "  ✓ 待纳管文件中不含本机 .env 的任何真实凭证"
  fi
fi

echo "══════ B. 字面量赋值模式扫描 ══════"
# 先粗筛候选行，再对"等号/冒号右侧的值"本身做精判：
#   长度>=16、同时含字母与数字、不是变量展开、不含占位符关键词。
# 字符类刻意不含 "." 与 "$"，从而不匹配 Go 的 cfg.APIKey = x.Y.APIKey 字段传递，
# 也不匹配以 env 方式注入 PGPASSWORD 的写法（参见 build 脚本里的 env "PGPASSWORD=$DB_PASS"）。
#
# 关键词这一族从 `PG?PASSWORD|PASSWD` 改成了 `PASSW[A-Z]*|PASSPHRASE`（2026-10-23 反向探针实测）：
# `PG?PASSWORD` 读作 "P + 可选 G + PASSWORD"，要匹配的其实是 `PPASSWORD`/`PGPASSWORD`，
# 于是最常见的 `POSTGRES_PASSWORD=<字面量>`（前面是下划线，不是 P）**整类不进粗筛**——
# 一句 `PROBE_PASSWORD = <16 位含数字字母的字面量>` 在本闸里 rc=0，与 A 段那个
# "(PASSW|…)$ 漏掉 POSTGRES_PASSWORD" 是同一形状的错，只是这次是 B 段。
# 覆盖面边界（本闸不声称覆盖，别把 rc=0 读成"全仓无明文口令"）：
#   裸 `TOKEN`（无下划线前缀，如 `Token: "…"`）不进粗筛；含 `.`/`$` 的值不进；
#   值长 <16、或不含数字、或命中 NEGATE 占位符词的，按精判放行。
PATTERN='(PASSW[A-Z]*|PASSPHRASE|SECRET|_TOKEN|api_key)[a-z_]*["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9@#_-]{16,}'
NEGATE='change_?me|placeholder|your_|_here|example|xxxx|replace|redacted|dummy|sample|min_len|for_[a-z_]*|<[a-z_]+>|^\$|^\{\$|[-_]test|test[-_]'
while IFS= read -r h; do
  [[ -z $h ]] && continue
  val=$(printf '%s' "$h" | sed -E 's/.*[:=][[:space:]]*["'"'"']?//; s/["'"'"',;) ].*$//')
  [[ ${#val} -ge 16 ]] || continue
  [[ $val =~ [0-9] ]] || continue
  [[ $val =~ [A-Za-z] ]] || continue
  # 整行右侧是模板/变量展开（${VAR:default}、$VAR）而非字面量，直接跳过
  if printf '%s' "$h" | grep -qE '[:=][[:space:]]*["'"'"']?\$[{A-Za-z]'; then
    [[ $VERBOSE == 1 ]] && echo "  (模板展开，跳过) $(hit_loc "$h")"
    continue
  fi
  if printf '%s' "$val" | grep -qiE "$NEGATE"; then
    [[ $VERBOSE == 1 ]] && echo "  (占位符/展开，跳过) $(hit_loc "$h")"
    continue
  fi
  if is_allowed "$h"; then
    [[ $VERBOSE == 1 ]] && echo "  (allow) $(hit_loc "$h")"
  else
    echo "  ❌ $(hit_loc "$h") 命中（值长 ${#val}，本体不打印）"
    FAIL=1
  fi
done < <(scan_literal "$PATTERN")

# ══════ B+ 段：裸值文件扫描（2026-11-16 审计 P0-1 补口）══════
# B 段要求「键名+[:=]+值」形态；裸密钥文件（单行 hex/base64、无键名）整类绕过。
# 先按文件名形状圈嫌疑面（误报面 = 名字可疑但内容正常的文件，由精判兜住），
# 再对嫌疑文件的每一行做「整行裸值」精判。gitignored 的本地密钥（合法落盘）
# 天然不在 FILELIST 候选面里，不会被本段误伤。
SUSPICIOUS_NAME_RE='(^|/)(\.[^/]*(secret|token)[^/]*|[^/]*\.bak-[0-9]{8}|[^/]*\.(key|pem|p12|pfx|gpg))$'
BARE_HEX_RE='^[0-9a-fA-F]{32,}$'
BARE_B64_RE='^[A-Za-z0-9+/]{40,}={0,2}$'
if [[ -s $FILELIST ]] && grep -E -- "$SUSPICIOUS_NAME_RE" "$FILELIST" >"$BARELIST" && [[ -s $BARELIST ]]; then
  echo "══════ B+. 裸值文件扫描（嫌疑文件 $(wc -l <"$BARELIST" | tr -d ' ') 个）══════"
  BAREHITS=""
  while IFS= read -r f; do
    [[ -f $f ]] || continue
    # 整行裸值精判：hex ≥32；base64 形态 ≥40 且字母与数字都有（防 aaaa…/1111… 平凡串）。
    lineno=0
    while IFS= read -r line; do
      lineno=$((lineno + 1))
      [[ -z $line ]] && continue
      if printf '%s' "$line" | grep -qE -- "$BARE_HEX_RE" \
        || { printf '%s' "$line" | grep -qE -- "$BARE_B64_RE" \
             && printf '%s' "$line" | grep -q '[0-9]' && printf '%s' "$line" | grep -q '[A-Za-z]'; }; then
        BAREHITS+="${f}:${lineno}:${line}"$'\n'
      fi
    done <"$f"
  done <"$BARELIST"
  if [[ -n $BAREHITS ]]; then
    while IFS= read -r h; do
      [[ -z $h ]] && continue
      if is_allowed "$h"; then
        [[ $VERBOSE == 1 ]] && echo "  (allow) $(hit_loc "$h")"
      else
        vlen=$(printf '%s' "$h" | cut -d: -f3- | tr -d '\n' | wc -c | tr -d ' ')
        echo "  ❌ [BARE] $(hit_loc "$h") 命中（整行裸值，长 ${vlen}，本体不打印）"
        FAIL=1
      fi
    done <<<"$BAREHITS"
  else
    echo "  ✓ 嫌疑文件中无整行裸值"
  fi
fi

echo "════════════════════════════════════"
if [[ $FAIL == 1 ]]; then
  cat <<'MSG'
❌ 明文凭证检查未通过。
   凭证只允许来自环境变量 / gitignored 的 .env。若命中项确为公开常量（如演示自举口令），
   请把它加入 scripts/.secret-allowlist 并写明理由，而不是直接放行。
   上面只给了「文件:行号 ＋ 键名/值长」：命中本体是刻意不打印的（见头部「红因口径」），
   要写豁免规则请打开那个文件看那一行，别把任何真实凭证值抄进豁免表或日志。
MSG
  exit 1
fi
echo "✅ 明文凭证检查通过（模式：${MODE}；A 段$([[ $MODE == full ]] && echo 已跑 || echo 未跑)）"
