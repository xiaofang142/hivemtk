#!/usr/bin/env bash
# L4 宽口径基线（scripts/arch-l4-getdb.baseline）的反向测试：证明这道新校验真有牙。
# 规矩来源：二次审核协议 §3「新校验不反向测＝没有校验」。2026-09-22 那轮它在 /tmp 里跑过一次，
# 而 /tmp 不算证据（协议 §2），故搬成常驻脚本，日志落 docs/superpowers/specs/ledger/logs/。
#
# 每格注一种坏基线 ⇒ 门必须红且红因点名到位；最后一格是不注码的控制组 ⇒ 门必须绿。
# 注码手法：cp 备份基线 → 改 → 跑门 → 还原 → 核 md5。基线还原失败即停机（绝不 git checkout）。
set -uo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
BASE="$REPO/scripts/arch-l4-getdb.baseline"
GATE="$REPO/scripts/check-architecture.sh"
KEY_LINE=$(grep -v '^#' "$BASE" | awk -F'\t' 'NF>=3 && $2+0>=2 {print; exit}')
KEY_FILE=$(printf '%s' "$KEY_LINE" | cut -f1)
KEY_COUNT=$(printf '%s' "$KEY_LINE" | cut -f2)
if [ -z "$KEY_LINE" ]; then echo "BROKEN: 基线里找不到一处处数 ≥2 的行，注不出「超出/收窄」两格"; exit 1; fi

BAK=$(mktemp)
cp "$BASE" "$BAK"
MD5_BEFORE=$(md5 -q "$BASE")
restore() { cp "$BAK" "$BASE"; MD5_AFTER=$(md5 -q "$BASE"); }
cleanup() { restore; rm -f "$BAK"; }
trap cleanup EXIT

# 只留这道校验自己的那几行：结论行 + 门点名到文件的红因行（红因是缩进的，别把它滤掉）。
run_gate() {
  (cd "$REPO" && bash "$GATE" 2>&1) \
    | grep -E '宽口径|^[[:space:]]+(新增|超出|收窄|失效条目)' || true
}

FAIL=0

# cell <格名> <期望红因关键字> <1=本格应绿>
cell() {
  local name=$1 want=$2 green=$3 out
  out=$(run_gate)
  if [ "$green" = 1 ]; then
    if printf '%s' "$out" | grep -q '✅' && [ -z "$(printf '%s' "$out" | grep '❌')" ]; then
      echo "[$name] 绿 ✓  $(printf '%s' "$out" | grep '✅')"
    else
      echo "[$name] 控制组本不该红：$out"; FAIL=$((FAIL+1))
    fi
    return
  fi
  # 变量一律带花括号：mac 自带 bash 3.2 在 UTF-8 下会把紧跟变量的中文字节读进变量名
  # （`$want」` → "unbound variable"），判红的那两格会在 stderr 上报错而不红。
  if printf '%s' "$out" | grep -q '❌' && printf '%s\n' "$out" | grep -q -- "$want"; then
    echo "[${name}] 红 ✓  红因命中「${want}」"
  else
    echo "[${name}] 判错：要求红且红因含「${want}」，实得："; printf '%s\n' "$out" | sed 's/^/    /'
    FAIL=$((FAIL+1))
  fi
}

# 格 1：删掉一个仍有命中的文件条目 ⇒ 「新增」
awk -F'\t' -v k="$KEY_FILE" '$1!=k {print}' "$BAK" > "$BASE"
cell "删条目→新增" "新增: $KEY_FILE" 0

# 格 2：把处数登记调小 ⇒ 「超出」
restore
awk -F'\t' -v k="$KEY_FILE" -v n="$KEY_COUNT" 'BEGIN{OFS="\t"} $1==k{$2=n-1} {print}' "$BAK" > "$BASE"
cell "登记调小→超出" "超出: $KEY_FILE" 0

# 格 3：把处数登记调大（已收口却不改表）⇒ 「收窄」
restore
awk -F'\t' -v k="$KEY_FILE" -v n="$KEY_COUNT" 'BEGIN{OFS="\t"} $1==k{$2=n+1} {print}' "$BAK" > "$BASE"
cell "登记调大→收窄" "收窄: $KEY_FILE" 0

# 格 4：理由列清空 ⇒ 该条不被加载 ⇒ 「新增」（空理由＝没登记）
restore
awk -F'\t' -v k="$KEY_FILE" 'BEGIN{OFS="\t"} $1==k{$3=""} {print}' "$BAK" > "$BASE"
cell "理由清空→新增" "新增: $KEY_FILE" 0

# 格 5：基线文件整体缺失 ⇒ 必须点名缺基线，不许静默空转
restore
rm -f "$BASE"
cell "基线缺失→判红" "宽口径基线文件缺失" 0

# 格 6：控制组（还原后）⇒ 绿
restore
cell "还原后控制组" "" 1

if [ "$MD5_BEFORE" != "$(md5 -q "$BASE")" ]; then
  echo "BROKEN: 基线当前 md5 与注码前不一致，请手工核对 $BASE"; exit 1
fi
if [ "$FAIL" -ne 0 ]; then echo "===== L4 基线反向测试：$FAIL 格判错 ====="; exit 1; fi
echo "===== L4 基线反向测试：6 格（5 种坏基线各自红且红因点名 + 还原后控制组绿），基线 md5 与注码前一致 ====="
