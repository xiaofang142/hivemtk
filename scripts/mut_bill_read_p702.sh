#!/bin/bash
# mut_bill_read_p702.sh —— T-P7-02 账单**读侧**控制器的反向探针（12 刀）。
#
# 为什么要有这个文件：读侧那几个处理器是"生产代码先落地、用例后补"的那一批
# （写侧那一批是先写用例的），绿用例本身不自证有牙。每一刀把 bill.go 改坏一处，
# 断言"被点名的那条用例必须红"；全绿就是判据在空转。
#
# 判据：killed == total 且 alive == broken == 0，且末了源文件 md5 与开头一致（源码还原）。
# 用法：bash scripts/mut_bill_read_p702.sh      （须能连测试库，环境变量在本脚本里设）
set -u

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT/user-server" || exit 9
SRC=internal/controller/bill.go
BAK="$(mktemp -t bill_p702_orig)"
trap 'rm -f "$BAK"' EXIT

export POSTGRES_TEST_PASSWORD="$(awk -F= '/^POSTGRES_PASSWORD=/{print $2; exit}' < ../.env)"
export POSTGRES_TEST_PORT="${POSTGRES_TEST_PORT:-8232}"
export GOFLAGS=-mod=mod

cp "$SRC" "$BAK" || exit 9
ORIG_MD5=$(md5 -q "$BAK")
if [ ! -s "$BAK" ]; then echo "BAK-EMPTY"; exit 9; fi

killed=0; alive=0; broken=0; total=0

while IFS='|' read -r label perl_expr test_name; do
  [ -z "$label" ] && continue
  total=$((total+1))
  cp "$BAK" "$SRC"
  perl -0pi -e "$perl_expr" "$SRC" || { echo "[$label] PERL-DIED"; broken=$((broken+1)); continue; }
  if cmp -s "$BAK" "$SRC"; then
    echo "[$label] ANCHOR-MISS（补丁没落地，这一格无效）"
    broken=$((broken+1)); continue
  fi
  if ! grep -q "func ${test_name}(" internal/controller/bill_test.go; then
    echo "[$label] TEST-MISSING（$test_name 不在树里，判据是空的）"
    broken=$((broken+1)); continue
  fi
  out=$(go test ./internal/controller/ -run "^${test_name}$" -count=1 2>&1)
  if printf '%s' "$out" | grep -q "no tests to run"; then
    echo "[$label] NO-TESTS-RAN（-run 名单没匹配上，这一格没跑）"
    broken=$((broken+1)); continue
  fi
  if printf '%s\n' "$out" | grep -qE '^(panic|FAIL.*\[build failed\])|cannot use|undefined:'; then
    echo "[$label] BUILD/TIME-BROKEN（红因不是判据，先修刀）"
    printf '%s\n' "$out" | tail -8 | sed 's/^/      /'
    broken=$((broken+1)); continue
  fi
  if printf '%s' "$out" | grep -q -- "--- FAIL"; then
    echo "[$label] KILLED by $test_name"
    killed=$((killed+1))
  else
    echo "[$label] ALIVE —— $test_name 仍绿"
    printf '%s\n' "$out" | tail -6 | sed 's/^/      /'
    alive=$((alive+1))
  fi
done <<'CASES'
读腿关闸只看指针|s/return c != nil && c\.read != nil && c\.read\.Available\(\)/return c != nil \&\& c.read != nil/|TestBillController_ReadLegUnassembledAnswersFiveOhThree
读侧503改成404|s/c\.unavailable\(ctx, msgReadUnavailable\)/response.Error(ctx, http.StatusNotFound, msgReadUnavailable, gin.H{"reason": billReasonNotFound})/|TestBillController_ReadLegUnassembledAnswersFiveOhThree
键不trim|s/key := strings\.TrimSpace\(raw\)/key := raw/|TestBillController_BlankPathKeyIsRejectedLocally
空键判据短路|s/if key == "" \{/if key == "" \&\& false {/|TestBillController_BlankPathKeyIsRejectedLocally
超宽键判据短路|s/if len\(key\) > billKeyMaxLen \{/if len(key) > billKeyMaxLen \&\& false {/|TestBillController_OverlongPathKeyIsRejectedAndNotEchoed
读侧偷偷清洗键|s/return key, true/return strings.ToLower(key), true/|TestBillController_PathKeyPassesThroughUnchanged
nil列表不再兜成数组|s/views = \[\]\*service\.BillStatementView\{\}/views = nil/|TestBillController_OfQuoteShape
count与列表脱钩|s/"count": len\(views\)/"count": len(views)+1/|TestBillController_OfQuoteShape
空结果放行|s/if st == nil \{/if st == nil \&\& false {/|TestBillController_ReadNilResultWithoutErrorIsFailure
404哨兵短路|s/case errors\.Is\(err, service\.ErrPaymentBillNotFound\):/case errors.Is(err, service.ErrPaymentBillNotFound) \&\& false:/|TestBillController_ReadSentinelsHaveDistinctOutcomes
500透出底层串|s/"读账单失败（底座或数据异常），本次未返回任何对账结果"/err.Error()/|TestBillController_ReadErrorDoesNotLeak
Available混进读腿|s/c\.derive != nil && c\.derive\.Available\(\)/c.read != nil \&\& c.read.Available()/|TestBillController_LegsFailIndependently
CASES

cp "$BAK" "$SRC"
NOW_MD5=$(md5 -q "$SRC")
echo "────── total=$total killed=$killed alive=$alive broken=$broken"
echo "md5_orig=$ORIG_MD5 md5_after=$NOW_MD5"
if [ "$killed" -eq "$total" ] && [ "$alive" -eq 0 ] && [ "$broken" -eq 0 ] && [ "$ORIG_MD5" = "$NOW_MD5" ]; then
  echo "ALL-CASES-KILLED-AND-SOURCE-RESTORED"
else
  echo "NOT-CLEAN"
  exit 1
fi
