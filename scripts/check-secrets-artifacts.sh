#!/usr/bin/env bash
# check-secrets-artifacts.sh — 把 check-secrets 的判据用到「构建产物」上（第二十二轮补盲区）
#
# 为什么单独一个脚本：产物是"秘密真正对外公开的那一刻"，而现有两道闸都刻意看不见它：
#   - check-secrets.sh 扫 git 索引 ⇒ 产物被 .gitignore 排除，永远扫不到；
#   - check-secrets-workspace.sh 的 find 里 `-path '*/dist/*' -prune` ⇒ 主动跳过。
#   于是被 vite 编进 bundle 的真凭证（来自 .env.local / .env.production 的 VITE_* 等）
#   没有任何闸拦得住 —— 尤其发行包要交给客户时。
#
# CI 里跑它没有意义（runner 的 checkout 不含本机产物），所以定位是「打包/交付前手动闸门」，
# 与 check-secrets-workspace.sh 同一族：只护本机，勿在 CI 里假装它跑过了。
#
# 附带两项产物专属判据：source map 是否随包落盘（等于把源码一起交付）、
# sourceMappingURL 是否悬空（浏览器控制台会去拉 .map，指向不存在的文件）。
#
# 用法： bash scripts/check-secrets-artifacts.sh <env文件> <产物目录...>
#   例： bash scripts/check-secrets-artifacts.sh ../user-web/.env.local user-web/dist embed-sdk/dist
# 退出码：0=干净 1=命中 2=用法/输入无效（宁可报错不可静默零扫描）
set -uo pipefail

ENV_FILE=${1:-}
[[ -n $ENV_FILE && -f $ENV_FILE ]] || { echo "❌ 用法: $0 <env文件> <产物目录...>；env 文件不存在: $ENV_FILE"; exit 2; }
shift
[[ $# -ge 1 ]] || { echo "❌ 未给产物目录"; exit 2; }
for d in "$@"; do [[ -d $d ]] || { echo "❌ 产物目录不存在: $d"; exit 2; }; done

FAIL=0
PAIRS=$(mktemp); PATTERNS=$(mktemp); FILELIST=$(mktemp)
trap 'rm -f "$PAIRS" "$PATTERNS" "$FILELIST"' EXIT

# ── A. 真凭证值比对：与仓内两个脚本同源（键名后缀判据 + 长度门槛）──
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
[[ $nv == 0 ]] && { echo "❌ 未能从 $ENV_FILE 提取到任何凭证键，检查形同虚设（rc=2）"; exit 2; }

find "$@" -type f \( -path '*/node_modules/*' -o -path '*/.git/*' \) -prune -o -type f -print >"$FILELIST"
NF=$(wc -l <"$FILELIST" | tr -d ' ')
[[ $NF == 0 ]] && { echo "❌ 待扫产物为空，检查形同虚设（rc=2）"; exit 2; }

echo "══════ 构建产物扫描 ══════"
echo "  env 真值：$nv 个凭证键　产物文件：$NF 个"

echo "────── A. .env 真凭证是否被编进产物 ──────"
AHITS=$(xargs grep -InH -F -f "$PATTERNS" -- <"$FILELIST" 2>/dev/null || true)
if [[ -n $AHITS ]]; then
  while IFS= read -r h; do
    [[ -z $h ]] && continue
    keyname="<未知>"
    while IFS=$'\t' read -r k v; do
      [[ $h == *"$v"* ]] && { keyname=$k; break; }
    done <"$PAIRS"
    echo "  ❌ [$keyname] ${h:0:160}"
    FAIL=1
  done <<<"$AHITS"
else
  echo "  ✓ 产物中不含 $ENV_FILE 里的真实凭证"
fi

echo "────── B. source map 是否随产物落盘 ──────"
MAPS=$(grep -E '\.map$' "$FILELIST" || true)
if [[ -n $MAPS ]]; then
  while IFS= read -r m; do echo "  ❌ 产物含 source map（全量源码可还原）：$m"; done <<<"$MAPS"
  FAIL=1
else
  echo "  ✓ 产物不含 .map"
fi

echo "────── C. 产物内引用了 .map 但文件不存在（ sourceMappingURL 悬空）──────"
DANGLE=$(xargs grep -IlH 'sourceMappingURL=' <"$FILELIST" 2>/dev/null || true)
MAPREFS=0
if [[ -n $DANGLE ]]; then
  while IFS= read -r f; do
    url=$(grep -o 'sourceMappingURL=[^'"'"' ")]*' "$f" | head -1)
    [[ -z $url ]] && continue
    # 只认「.map 结尾」的引用：产物里内联的正则/字符串（如剥 map 的构建代码本身含
    # sourceMappingURL=(\S+…）不是真引用，实测会误报一次，故加此后缀门槛
    [[ ${url##*=} == *.map ]] || continue
    MAPREFS=$((MAPREFS+1))
    base=$(basename "${url#sourceMappingURL=}")
    if ! grep -q "/${base}$" "$FILELIST"; then
      echo "  ⚠ 悬空 sourceMappingURL：$f -> $url"
    else
      echo "  ❌ 产物运行时可拉取 map：$f -> $url"
      FAIL=1
    fi
  done <<<"$DANGLE"
fi
[[ $MAPREFS == 0 ]] && echo "  ✓ 产物无有效 sourceMappingURL（指向 .map 的引用）"

echo "════════════════════════════════════"
[[ $FAIL == 1 ]] && { echo "❌ 产物扫描未通过"; exit 1; }
echo "✅ 产物扫描通过"
