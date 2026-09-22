#!/usr/bin/env bash
# check-secrets.sh — 阻止明文凭证进入版本管理（T-P0-05 的防复发闸门）
#
# 两条判定，缺一不可：
#  A. 真值比对：把本机 .env 里 PASSWORD/SECRET/TOKEN 类键的**实际值**在待纳管文件中全量搜索，
#     命中即 FAIL。零误报，且正是 bulk_seed/geo_full_test/deep_lib 那批事故的确切成因。
#  B. 字面量模式：搜索 password/secret/token 等键被赋非变量、非占位符的长字面量，
#     命中列在 scripts/.secret-allowlist 之外的即 FAIL。
#
# 扫描范围 = `git ls-files --cached --others --exclude-standard`，即"已跟踪 + 已 add -N +
# 未跟踪但未被 ignore"的文件，**按工作区内容**逐个 grep。
# 这里刻意不用 `git grep`：git grep 读的是 index 里的 blob，而 `git add -N`（intent-to-add）
# 在 index 里存的是空 blob —— 用 git grep 时"刚新建、正要提交"的文件会整份漏检，
# 而那恰恰是本闸门唯一要拦的时机（反向测试证实过这个漏洞）。
#
# 用法： bash scripts/check-secrets.sh          # 在仓根或任意子目录均可
#       bash scripts/check-secrets.sh --verbose
# 退出码：0=干净，1=发现明文凭证，2=无法执行（缺 .env 等）
set -uo pipefail

VERBOSE=0
[[ "${1:-}" == "--verbose" ]] && VERBOSE=1

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "❌ 不在 git 仓内"; exit 2; }
cd "$ROOT" || exit 2

ENV_FILE=${ENV_FILE:-$ROOT/.env}
ALLOWLIST=$ROOT/scripts/.secret-allowlist
FAIL=0

[[ -f "$ENV_FILE" ]] || { echo "❌ 找不到 ${ENV_FILE}（可用 ENV_FILE=/path/to/.env 指定）"; exit 2; }

# 从 allowlist 读取豁免正则（每行一条，# 开头为注释）
ALLOW_RE=""
if [[ -f "$ALLOWLIST" ]]; then
  while IFS= read -r line; do
    [[ -z $line || $line == \#* ]] && continue
    ALLOW_RE="${ALLOW_RE:+$ALLOW_RE|}($line)"
  done < "$ALLOWLIST"
fi

is_allowed() { # $1 = "path:lineno:content"
  [[ -z $ALLOW_RE ]] && return 1
  printf '%s' "$1" | grep -qE "$ALLOW_RE"
}

echo "══════ A. 本机 .env 真值比对（${ENV_FILE}）══════"
PAIRS=$(mktemp)
PATTERNS=$(mktemp)
trap 'rm -f "$PAIRS" "$PATTERNS"' EXIT

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
done < <(grep -E '^[[:space:]]*(export[[:space:]]+)?[A-Za-z0-9_]+=' "$ENV_FILE" | sort -u)
if [[ $nv == 0 ]]; then
  echo "  ❌ 未能从 $ENV_FILE 提取到任何凭证键，检查形同虚设（rc=2）"
  exit 2
fi
[[ $VERBOSE == 1 ]] && echo "  已载入 $nv 个真值待比对"

# 待纳管文件清单（含未跟踪），一次生成两处复用
FILELIST=$(mktemp)
trap 'rm -f "$PAIRS" "$PATTERNS" "$FILELIST"' EXIT
git ls-files --cached --others --exclude-standard >"$FILELIST"

scan_literal() { # $1=ERE 正则；扫描待纳管文件的工作区内容
  [[ -s $FILELIST ]] || return 0
  xargs grep -InHi -E -- "$1" <"$FILELIST" 2>/dev/null || true
}
scan_values() { # $1=字面量清单文件；逐个真值精确匹配
  [[ -s $FILELIST ]] || return 0
  xargs grep -InH -F -f "$1" -- <"$FILELIST" 2>/dev/null || true
}

AHITS=""
if [[ -s $PATTERNS ]]; then
  AHITS=$(scan_values "$PATTERNS")
fi
if [[ -n $AHITS ]]; then
  while IFS= read -r h; do
    [[ -z $h ]] && continue
    keyname="<未知>"
    while IFS=$'\t' read -r k v; do
      [[ $h == *"$v"* ]] && { keyname=$k; break; }
    done <"$PAIRS"
    if is_allowed "$h"; then
      [[ $VERBOSE == 1 ]] && echo "  (allow) $h"
    else
      echo "  ❌ [$keyname] $h"
      FAIL=1
    fi
  done <<<"$AHITS"
else
  echo "  ✓ 待纳管文件中不含本机 .env 的任何真实凭证"
fi

echo "══════ B. 字面量赋值模式扫描 ══════"
# 先粗筛候选行，再对"等号/冒号右侧的值"本身做精判：
#   长度>=16、同时含字母与数字、不是变量展开、不含占位符关键词。
# 字符类刻意不含 "." 与 "$"，从而不匹配 Go 的 cfg.APIKey = x.Y.APIKey 字段传递，
# 也不匹配 PGPASSWORD="$POSTGRES_PASSWORD" 这类已改为环境注入的写法。
PATTERN='(PG?PASSWORD|PASSWD|SECRET|_TOKEN|api_key)[a-z_]*["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9@#_-]{16,}'
NEGATE='change_?me|placeholder|your_|_here|example|xxxx|replace|redacted|dummy|sample|min_len|for_[a-z_]*|<[a-z_]+>|^\$|^\{\$|[-_]test|test[-_]'
while IFS= read -r h; do
  [[ -z $h ]] && continue
  val=$(printf '%s' "$h" | sed -E 's/.*[:=][[:space:]]*["'"'"']?//; s/["'"'"',;) ].*$//')
  [[ ${#val} -ge 16 ]] || continue
  [[ $val =~ [0-9] ]] || continue
  [[ $val =~ [A-Za-z] ]] || continue
  # 整行右侧是模板/变量展开（${VAR:default}、$VAR）而非字面量，直接跳过
  if printf '%s' "$h" | grep -qE '[:=][[:space:]]*["'"'"']?\$[{A-Za-z]'; then
    [[ $VERBOSE == 1 ]] && echo "  (模板展开，跳过) ${h:0:110}"
    continue
  fi
  if printf '%s' "$val" | grep -qiE "$NEGATE"; then
    [[ $VERBOSE == 1 ]] && echo "  (占位符/展开，跳过) ${h:0:110}"
    continue
  fi
  if is_allowed "$h"; then
    [[ $VERBOSE == 1 ]] && echo "  (allow) ${h:0:120}"
  else
    echo "  ❌ ${h:0:160}"
    FAIL=1
  fi
done < <(scan_literal "$PATTERN")

echo "════════════════════════════════════"
if [[ $FAIL == 1 ]]; then
  cat <<'MSG'
❌ 明文凭证检查未通过。
   凭证只允许来自环境变量 / gitignored 的 .env。若命中项确为公开常量（如演示自举口令），
   请把它加入 scripts/.secret-allowlist 并写明理由，而不是直接放行。
MSG
  exit 1
fi
echo "✅ 明文凭证检查通过"
