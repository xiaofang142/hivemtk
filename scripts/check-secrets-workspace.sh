#!/usr/bin/env bash
# check-secrets-workspace.sh — 把 check-secrets.sh 的判据扩到"不属于任何 git 仓"的顶层目录（审计会话C S1 收口）
#
# 为什么需要第二个脚本而不是改 check-secrets.sh：
#   check-secrets.sh 的扫描范围是 `git ls-files --cached --others --exclude-standard`，
#   天然扫不到 <工作区根>/scripts、<工作区根>/docs、<工作区根>/cold-start —— 这三层目录
#   不在任何 git 仓里（2026-09-19 审计 F2 就是在 scripts/ 里一次性揪出 6 个明文凭证文件）。
#   本脚本以同一套 A(本机 .env 真值比对)/B(字面量模式) 判据扫这三层目录。
#   不复用而是并列：check-secrets.sh 正被另一会话高频迭代（T-P0-05），新增零交集文件最稳。
#
# 与仓内闸门的分工：CI（GitHub runner）拿不到这三层目录，本闸门只护本机与共享工作区，
# 需人在提交前/排障时手动跑；建议纳入个人 pre-push 习惯，勿在 CI 里假装它跑过了。
#
# 用法： bash hivemtk/scripts/check-secrets-workspace.sh [--verbose] [目录...]
#       默认扫 <仓根>/../{scripts,docs,cold-start}；也可显式传目录覆盖。
# 退出码：0=干净，1=发现明文凭证，2=布局不符/缺 .env（检查形同虚设时宁可报错不可静默）
set -uo pipefail

VERBOSE=0
ARGS=()
for a in "$@"; do
  [[ $a == --verbose ]] && VERBOSE=1 || ARGS+=("$a")
done

ROOT=$(git rev-parse --show-toplevel 2>/dev/null) || { echo "❌ 不在 git 仓内（应在 hivemtk 仓下执行）"; exit 2; }
WS=$(dirname "$ROOT")   # 工作区根 = <ws>/hivemtk 的上一层
cd "$ROOT" || exit 2

ENV_FILE=${ENV_FILE:-$ROOT/.env}
ALLOWLIST=$ROOT/scripts/.workspace-secret-allowlist
[[ -f "$ENV_FILE" ]] || { echo "❌ 找不到 $ENV_FILE"; exit 2; }

# 待扫目录：默认三层非 git 目录；布局不符（如单仓 checkout）时宁可 rc=2 也不静默零扫描
if [[ ${#ARGS[@]} -gt 0 ]]; then
  TARGETS=("${ARGS[@]}")
else
  TARGETS=("$WS/scripts" "$WS/docs" "$WS/cold-start")
fi
PRESENT=()
for t in "${TARGETS[@]}"; do [[ -d $t ]] && PRESENT+=("$t"); done
[[ ${#PRESENT[@]} -gt 0 ]] || { echo "❌ 待扫目录全部不存在，检查形同虚设：${TARGETS[*]}"; exit 2; }

ALLOW_RE=""
if [[ -f "$ALLOWLIST" ]]; then
  while IFS= read -r line; do
    [[ -z $line || $line == \#* ]] && continue
    ALLOW_RE="${ALLOW_RE:+$ALLOW_RE|}($line)"
  done < "$ALLOWLIST"
fi
# 判据与仓内那道门（check-secrets.sh 同名函数）逐字一致：空表 ⇒ 一律不豁免，非空 ⇒ 整串命中 ERE 才豁免。
# 2026-09-21 实测本函数曾写成一行 `[[ -n $ALLOW_RE ]] && grep -qE ...; return 1` —— 末尾的
# return 1 无条件执行，函数恒返回"未豁免"，于是本脚本报错文案里"确属公开常量再加豁免表"这条
# 处置路径是死路（登记后门照样红）。改回多行形，别再压回去。
is_allowed() { # $1 = "path:lineno:content"
  [[ -z $ALLOW_RE ]] && return 1
  printf '%s' "$1" | grep -qE "$ALLOW_RE"
}

FAIL=0
PAIRS=$(mktemp); PATTERNS=$(mktemp); FILELIST=$(mktemp)
trap 'rm -f "$PAIRS" "$PATTERNS" "$FILELIST"' EXIT

echo "══════ 工作区凭证扫描（非 git 目录）══════"
for t in "${PRESENT[@]}"; do echo "  目标：$t"; done

find "${PRESENT[@]}" -type f \
  \( -path '*/node_modules/*' -o -path '*/.git/*' -o -path '*/__pycache__/*' \
     -o -path '*/dist/*' -o -path '*/.venv/*' -o -path '*/venv/*' \) -prune -o \
  -type f ! -size +8M -print >"$FILELIST"
NF=$(wc -l <"$FILELIST" | tr -d ' ')
echo "  文件数：$NF"

# ── A. 本机 .env 真值比对（判据与 check-secrets.sh 同源）──
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
[[ $nv == 0 ]] && { echo "  ❌ 未能从 $ENV_FILE 提取到任何凭证键，检查形同虚设（rc=2）"; exit 2; }
[[ $VERBOSE == 1 ]] && echo "  已载入 $nv 个真值待比对"

scan_values() {
  [[ -s $FILELIST ]] || return 0
  xargs grep -InH -F -f "$PATTERNS" -- <"$FILELIST" 2>/dev/null || true
}
scan_literal() {
  [[ -s $FILELIST ]] || return 0
  xargs grep -InHi -E -- "$1" <"$FILELIST" 2>/dev/null || true
}

echo "────── A. .env 真值比对 ──────"
AHITS=$(scan_values)
if [[ -n $AHITS ]]; then
  while IFS= read -r h; do
    [[ -z $h ]] && continue
    keyname="<未知>"
    while IFS=$'\t' read -r k v; do
      [[ $h == *"$v"* ]] && { keyname=$k; break; }
    done <"$PAIRS"
    if is_allowed "$h"; then
      [[ $VERBOSE == 1 ]] && echo "  (allow) ${h:0:120}"
    else
      echo "  ❌ [$keyname] ${h:0:160}"
      FAIL=1
    fi
  done <<<"$AHITS"
else
  echo "  ✓ 非 git 目录中不含本机 .env 的真实凭证"
fi

echo "────── B. 字面量模式 ──────"
PATTERN='(PG?PASSWORD|PASSWD|SECRET|_TOKEN|api_key)[a-z_]*["'"'"']?[[:space:]]*[:=][[:space:]]*["'"'"']?[A-Za-z0-9@#_-]{16,}'
NEGATE='change_?me|placeholder|your_|_here|example|xxxx|replace|redacted|dummy|sample|min_len|for_[a-z_]*|<[a-z_]+>|^\$|^\{\$|[-_]test|test[-_]|^\*\*$|^[0-9*]+$'
while IFS= read -r h; do
  [[ -z $h ]] && continue
  val=$(printf '%s' "$h" | sed -E 's/.*[:=][[:space:]]*["'"'"']?//; s/["'"'"',;) ].*$//')
  [[ ${#val} -ge 16 ]] || continue
  [[ $val =~ [0-9] ]] || continue
  [[ $val =~ [A-Za-z] ]] || continue
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
❌ 非 git 目录中发现明文凭证。
   这些文件虽不进版本库，但同在工作区里会被任何 agent/备份/打包脚本读到（F2 那批就是这么进仓的）。
   处置：改为环境变量注入后复跑本脚本；确属公开常量再加 scripts/.workspace-secret-allowlist 并写明理由。
MSG
  exit 1
fi
echo "✅ 工作区凭证扫描通过"
