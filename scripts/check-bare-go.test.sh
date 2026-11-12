#!/usr/bin/env bash
# check-bare-go.test.sh —— 证明 scripts/check-bare-go.py 能红能绿。
#
# 门本身绿不算数：恒绿的门与坏掉的门在 CI 输出上长得一样（本仓 §23.16 第 5 条）。
# 这里用**假树夹具**逐条注入违规，要求门逐条开火；最后用真仓对账一次，
# 防"正则失效退化成恒绿"（check-bare-go.py 曾因缺 re.MULTILINE / FUNC_DEF 结构
# / interface{} 当函数体三层 bug 全部恒绿——本夹具就是防它们复发的回归网）。
#
# 用例（每格只注入一个违规，避免判据互相掩盖）：
#   B1  裸 go func 无 recover              ⇒ NEW 红
#   B2  go func 体带 defer recover         ⇒ 绿
#   B3  go ident 目标无 recover            ⇒ 红
#   B4  go ident 目标自带 recover          ⇒ 绿
#   B5  go ident 目标跨文件（同文件无定义） ⇒ 红
#   B6  注释/字符串里的 `go func(`、`recover()` 不算语句也不算兜底 ⇒ B6a 绿 / B6b 红
#   B7  _test.go 裸 go 不计               ⇒ 绿
#   B8  缺基线文件                        ⇒ rc=2
#   B9  基线外文件有站点                  ⇒ NEW 红
#   B10 基线登记 > 现算                    ⇒ STALE 红
#   B11 基线登记 < 现算                    ⇒ OVER 红
#   B12 基线与现算一致                     ⇒ 绿
#   B13 签名含 interface{} 的目标函数（recover.go 曾踩坑的形状）⇒ B13a 绿 / B13b 红
#   B14 utils.SafeGo 调用不是 go 语句（不该误计）⇒ 绿
#   B15 真仓对账：门绿且站点数 = 基线总数 91
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT/scripts/check-bare-go.py"
REAL_BASE="$ROOT/scripts/bare-go.baseline"

fail() { echo "FAIL[$1] $2" >&2; exit 1; }
pass() { echo "PASS[$1] $2"; }

[ -f "$GATE" ] || fail 0 "门脚本不存在: $GATE"
[ -f "$REAL_BASE" ] || fail 0 "基线不存在: $REAL_BASE"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
FIX="$TMP/fixture"
mkdir -p "$FIX/scripts" "$FIX/user-server/internal/demo"

run_gate() { # $1=repo $2=baseline; 输出 rc
  python3 "$GATE" --repo "$1" --baseline "$2" >/dev/null 2>&1
  echo $?
}

# ---------- 夹具 Go 源 ----------

cat > "$FIX/user-server/internal/demo/bare.go" <<'GO'
package demo

import "context"

// RunBare 裸 go func 无 recover（B1）
func RunBare() {
	go func() {
		doWork()
	}()
}

func doWork() {}

// RunSafe go func 体带 defer recover（B2）
func RunSafe() {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				_ = r
			}
		}()
		doWork()
	}()
}

// RunTargetBare go ident 目标无 recover（B3）
func RunTargetBare() {
	go helperNoRecover()
}

func helperNoRecover() {}

// RunTargetSafe go ident 目标自带 recover（B4）
func RunTargetSafe() {
	go helperWithRecover()
}

func helperWithRecover() {
	defer func() {
		_ = recover()
	}()
}

// RunCrossFile go ident 目标跨文件（B5，同文件无定义）
func RunCrossFile() {
	go helperElsewhere()
}

// RunCommentOnly 注释里的形状（B6a：应为绿——注释不是语句）
func RunCommentOnly() {
	// go func() { }()  这行是注释
	s := "go func() { recover() }()" // 字符串里的 recover 不是兜底
	_ = s
}

// RunCommentRecover 体内只有注释 recover（B6b：应为红——注释不算兜底）
func RunCommentRecover() {
	go func() {
		// recover() 只是注释提及
		doWork()
	}()
}
GO

cat > "$FIX/user-server/internal/demo/other.go" <<'GO'
package demo

// helperElsewhere 定义在"另一个文件"（夹具里用这个文件模拟跨文件）。
// 注意：B5 需要目标在同文件搜不到——所以 helperElseonly 不能定义在 bare.go；
// 这里故意定义在 other.go，而 bare.go 的 RunCrossFile 调它 ⇒ 闸按"跨文件目标"计。
func helperElsewhere() {}
GO

cat > "$FIX/user-server/internal/demo/iface.go" <<'GO'
package demo

import "fmt"

// IfaceSafe 签名含 interface{} 且自带 recover（B13a：绿——recover.go 曾被当 {} 漏检）
func IfaceSafe() {
	go ifaceHelperSafe("x")
}

func ifaceHelperSafe(name string, extra ...interface{}) {
	defer func() {
		if r := recover(); r != nil {
			fmt.Println(r)
		}
	}()
	_ = name
}

// IfaceBare 签名含 interface{} 但无 recover（B13b：红）
func IfaceBare() {
	go ifaceHelperBare("x")
}

func ifaceHelperBare(name string, extra ...interface{}) {
	_ = name
}
GO

cat > "$FIX/user-server/internal/demo/safego.go" <<'GO'
package demo

import "context"

// utils.SafeGo 调用不是 go 语句（B14）
func RunViaSafeGo() {
	utilsSafeGoSim("demo.run", func(ctx context.Context) { doWork() })
}

func utilsSafeGoSim(name string, fn func(ctx context.Context)) {
	_ = name
	fn(context.Background())
}
GO

cat > "$FIX/user-server/internal/demo/bare_test.go" <<'GO'
package demo

import "testing"

// _test.go 里的裸 go 不计（B7）
func TestBare(t *testing.T) {
	go doWork()
}
GO

# ---------- 用例 ----------

# B1..B7：无基线时应报出 4 个裸站点（B1/B3/B5/B6b/B13b=5 处，分布在 3 个文件）
BASE_EMPTY="$FIX/scripts/empty.baseline"
touch "$BASE_EMPTY"
rc=$(run_gate "$FIX" "$BASE_EMPTY")
[ "$rc" = "1" ] || fail 1 "空基线应 rc=1（NEW 红），实际 rc=$rc"
out=$(python3 "$GATE" --repo "$FIX" --baseline "$BASE_EMPTY" 2>&1)
echo "$out" | grep -q 'bare.go:7' || fail 1 "应点名 bare.go:7（B1 裸 go func）: $out"
echo "$out" | grep -q 'bare.go:28' || fail 1 "应点名 bare.go:28（B3 go ident 目标无 recover）: $out"
echo "$out" | grep -q 'bare.go:46' || fail 1 "应点名 bare.go:46（B5 跨文件目标）: $out"
echo "$out" | grep -q 'bare.go:58' || fail 1 "应点名 bare.go:58（B6b 注释 recover 不算兜底）: $out"
echo "$out" | grep -q 'iface.go:21' || fail 1 "应点名 iface.go:21（B13b interface{} 签名无 recover）: $out"
echo "$out" | grep -q 'iface.go:7' && fail 1 "iface.go:7（B13a interface{} 签名自带 recover）不该被点名: $out"
echo "$out" | grep -q 'bare.go:16' && fail 1 "bare.go:16（B2 体带 recover）不该被点名: $out"
echo "$out" | grep -q 'bare.go:35' && fail 1 "bare.go:35（B4 目标自带 recover）不该被点名: $out"
echo "$out" | grep -q 'bare_test.go' && fail 1 "_test.go 不该被扫（B7）: $out"
echo "$out" | grep -q 'safego.go' && fail 1 "SafeGo 调用不该被计（B14）: $out"
pass 1 "B1-B7/B13/B14：违规逐条开火，保护形态逐条放行"

# B8：缺基线 ⇒ rc=2
rc=$(run_gate "$FIX" "$FIX/scripts/absent.baseline")
[ "$rc" = "2" ] || fail 2 "缺基线应 rc=2，实际 rc=$rc"
pass 2 "B8：缺基线 rc=2（区分没扫与零命中）"

# B9：NEW（基线只有其它文件的条目）
printf 'user-server/internal/demo/other.go 0\n' > "$FIX/scripts/base_new.baseline"
rc=$(run_gate "$FIX" "$FIX/scripts/base_new.baseline")
[ "$rc" = "1" ] || fail 3 "NEW 应 rc=1，实际 rc=$rc"
pass 3 "B9：基线外文件出现站点 ⇒ NEW 红"

# B10：STALE（登记 > 现算）
printf 'user-server/internal/demo/bare.go 6\nuser-server/internal/demo/iface.go 1\n' > "$FIX/scripts/base_stale.baseline"
rc=$(run_gate "$FIX" "$FIX/scripts/base_stale.baseline")
[ "$rc" = "1" ] || fail 4 "STALE 应 rc=1，实际 rc=$rc"
stale_out=$(python3 "$GATE" --repo "$FIX" --baseline "$FIX/scripts/base_stale.baseline" 2>&1)
echo "$stale_out" | grep -q 'STALE' || fail 4 "应输出 STALE 红: $stale_out"
pass 4 "B10：登记 > 现算 ⇒ STALE 红"

# B11：OVER（登记 < 现算）
printf 'user-server/internal/demo/bare.go 3\nuser-server/internal/demo/iface.go 1\n' > "$FIX/scripts/base_over.baseline"
rc=$(run_gate "$FIX" "$FIX/scripts/base_over.baseline")
[ "$rc" = "1" ] || fail 5 "OVER 应 rc=1，实际 rc=$rc"
over_out=$(python3 "$GATE" --repo "$FIX" --baseline "$FIX/scripts/base_over.baseline" 2>&1)
echo "$over_out" | grep -q 'OVER' || fail 5 "应输出 OVER 红: $over_out"
pass 5 "B11：登记 < 现算 ⇒ OVER 红"

# B12：基线与现算一致（bare.go=4: B1/B3/B5/B6b；iface.go=1: B13b）⇒ 绿
printf 'user-server/internal/demo/bare.go 4\nuser-server/internal/demo/iface.go 1\n' > "$FIX/scripts/base_ok.baseline"
rc=$(run_gate "$FIX" "$FIX/scripts/base_ok.baseline")
[ "$rc" = "0" ] || fail 6 "基线一致应 rc=0，实际 rc=$rc"
pass 6 "B12：基线与现算一致 ⇒ 绿"

# B12b：基线格式坏 ⇒ rc=1
printf 'user-server/internal/demo/bare.go x\n' > "$FIX/scripts/base_bad.baseline"
rc=$(run_gate "$FIX" "$FIX/scripts/base_bad.baseline")
[ "$rc" = "1" ] || fail 7 "格式坏应 rc=1，实际 rc=$rc"
pass 7 "B12b：基线格式坏 ⇒ rc=1"

# B15：真仓对账——门绿且站点数与基线总数一致
rc=$(run_gate "$ROOT" "$REAL_BASE")
[ "$rc" = "0" ] || fail 8 "真仓门应绿（基线与现算一致），实际 rc=${rc}；若刚改了产码请同步基线"
real_sites=$(python3 "$ROOT/scripts/check-bare-go.py" --repo "$ROOT" --baseline "$REAL_BASE" 2>&1 | head -1)
echo "$real_sites" | grep -q "现算 83 处 / 基线 83 处" || fail 8 "真仓站点数对账失败: $real_sites"
pass 8 "B15：真仓门绿，站点数 83/83 与基线一致"

echo "ALL PASS"
exit 0
