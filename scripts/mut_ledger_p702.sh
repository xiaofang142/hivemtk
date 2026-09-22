#!/usr/bin/env bash
# mut_ledger_p702.sh —— T-P7-02 台账格子的反向探针（5 刀：23e 翻转 + 24a–24d）。
#
# 判据：台账是**源码 grep 门**，它自己必须被证伪一次 —— 每一刀把那一格的构造点/调用点
# 改名（等价于"摘掉那一行"），断言台账 rc=1 且**唯一那条**"回退"行指向被改的这一格。
# 只改影子树，绝不碰工作树：整棵 user-server 用 `cp -al` 硬链接过去（瞬间、不占盘），
# 每格改完即从工作树 cp 回来，末了逐文件 cmp 自证影子树已还原、工作树 md5 未变。
#
# 为什么不在工作树上就地改：这一族改的是**装配点本身**（router.go / payment_wiring.go 那几行），
# 摘掉之后 Go 侧编译不过，而本树与并行会话共用 —— 影子树是唯一不会牵连别人的跑法。
#
# 用法：bash scripts/mut_ledger_p702.sh
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
US="$ROOT/user-server"
LEDGER_SRC="$ROOT/scripts/check-unwired-assets.sh"

WORK="$(mktemp -d)" || exit 9
trap 'rm -rf "${WORK:?}"' EXIT
SHADOW="$WORK/hivemtk"
mkdir -p "$SHADOW"
if ! cp -al "$US" "$SHADOW/user-server"; then
  echo "cp -al 失败（影子树要与工作树同盘才建得起硬链接）"
  exit 9
fi
mkdir -p "$SHADOW/scripts"
cp "$LEDGER_SRC" "$SHADOW/scripts/"
LEDGER="$SHADOW/scripts/check-unwired-assets.sh"
SHADOW_US="$SHADOW/user-server"

# 标签 | 相对路径 | perl 替换（表达式内不出现 | 字符） | 台账描述里用来认这一行的关键字
PROBES=(
  "23e|internal/service/payment.go|s/\\.bills\\.UpdateStatus\\(/.bills.UpdateStatusRenamed(/|账单状态跃迁口的生产调用方"
  "24a|internal/app/payment_wiring.go|s/NewPaymentRepositoryWithDB\\(/NewPaymentRepositoryWithDBRenamed(/|回款仓储的装配入口"
  "24b|internal/service/payment.go|s/model\\.Payment\\{/model.PaymentRenamed{/|回款行的生产写入点"
  "24c|internal/router/router.go|s/app\\.InitPaymentRuntime\\(/app.InitPaymentRuntimeRenamed(/|回款腿与对账读腿在启动路径上的装配点"
  "24d|internal/service/integration.go|s/GlobalPaymentService\\(\\)/globalPaymentSvcLoaded(/|回款腿交给集成服务的那一次交接"
)

run_ledger() {  # 跑影子台账，回两行：状态码 + 干净输出
  local o rc
  o=$(bash "$LEDGER" 2>&1); rc=$?
  printf '%s\n%s' "$rc" "$(printf '%s' "$o" | sed 's/\x1b\[[0-9;]*m//g')"
}

# 控制组：影子树未变异时必须与登记一致（rc=0），否则下面所有的红都可能是环境给的
ctrl=$(run_ledger); ctrl_rc=${ctrl%%$'\n'*}
if [[ "$ctrl_rc" != "0" ]]; then
  echo "CONTROL-RED 未放刀时台账就不是 rc=0，先归因环境再谈判据"
  printf '%s\n' "$ctrl" | tail -6
  exit 8
fi
echo "CONTROL-GREEN $(printf '%s\n' "$ctrl" | grep '与登记一致' | head -1)"

TOTAL=0; KILLED=0; ALIVE=0; BROKEN=0; BROKEN_LIST=""
for probe in "${PROBES[@]}"; do
  IFS='|' read -r tag rel expr needle <<< "$probe"
  TOTAL=$((TOTAL + 1))
  file="$SHADOW_US/$rel"; real="$US/$rel"
  if [[ ! -f "$file" || ! -f "$real" ]]; then
    printf '[%s] BROKEN 文件缺失\n' "$tag"; BROKEN=$((BROKEN+1)); BROKEN_LIST="$BROKEN_LIST $tag:missing-file"; continue
  fi
  before_real=$(md5 -q "$real")

  if ! perl -pe "$expr" "$file" > "$file.new" 2>/dev/null; then
    printf '[%s] PERL-DIED\n' "$tag"; BROKEN=$((BROKEN+1)); BROKEN_LIST="$BROKEN_LIST $tag:perl"; rm -f "$file.new"; continue
  fi
  # 变异必须"只改一行且真的改了"：0 行 = 锚点漂了，多行 = 打偏会连累别的判据
  changed=$(diff "$real" "$file.new" | grep -c '^<' || true)
  if [[ "$changed" != "1" ]]; then
    printf '[%s] ANCHOR-BAD 改动行数=%s（期望 1）\n' "$tag" "$changed"
    diff "$real" "$file.new" | head -6
    BROKEN=$((BROKEN+1)); BROKEN_LIST="$BROKEN_LIST $tag:anchor($changed)"; rm -f "$file.new"; continue
  fi
  mv -f "$file.new" "$file"

  out=$(run_ledger); rc=${out%%$'\n'*}; body=${out#*$'\n'}
  redlines=$(printf '%s\n' "$body" | grep -c '回退（登记为已接线却无调用点）' || true)
  pointed=$(printf '%s\n' "$body" | grep '回退（登记为已接线却无调用点）' | grep -cF "$needle" || true)
  if [[ "$rc" == "1" && "$redlines" == "1" && "$pointed" == "1" ]]; then
    printf '[%s] KILLED rc=1 唯一那条回退行指向本格\n' "$tag"; KILLED=$((KILLED+1))
  else
    printf '[%s] ALIVE rc=%s 回退行=%s 指向本格=%s\n' "$tag" "$rc" "$redlines" "$pointed"
    printf '%s\n' "$body" | grep -E '回退|与登记一致|与基线不符|定义缺失' | head -5
    ALIVE=$((ALIVE+1))
  fi

  cp -f "$real" "$file.tmp"; mv -f "$file.tmp" "$file"
  [[ "$before_real" == "$(md5 -q "$real")" ]] || {
    printf '[%s] REAL-TREE-CHANGED %s\n' "$tag" "$rel"; BROKEN=$((BROKEN+1)); BROKEN_LIST="$BROKEN_LIST $tag:real-tree"; }
done

final=$(run_ledger); frc=${final%%$'\n'*}
printf 'restored rc=%s :: %s\n' "$frc" "$(printf '%s\n' "$final" | grep -E '与登记一致|回退' | head -1)"
for probe in "${PROBES[@]}"; do
  IFS='|' read -r tag rel _ <<< "$probe"
  cmp -s "$SHADOW_US/$rel" "$US/$rel" || { printf 'SHADOW-NOT-RESTORED %s\n' "$tag"; BROKEN=$((BROKEN+1)); }
done

echo "────── total=$TOTAL killed=$KILLED alive=$ALIVE broken=$BROKEN${BROKEN_LIST:+  [$BROKEN_LIST]}"
if [[ $ALIVE -eq 0 && $BROKEN -eq 0 && $KILLED -eq $TOTAL && "$frc" == "0" ]]; then
  echo "ALL-CASES-KILLED-AND-SOURCE-RESTORED"
else
  echo "NOT-CLEAN"; exit 1
fi
