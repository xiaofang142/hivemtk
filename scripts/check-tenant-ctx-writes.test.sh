#!/usr/bin/env bash
# check-tenant-ctx-writes.test.sh —— 证明 `scripts/check-tenant-ctx-writes.py` 能红。
#
# 门本身绿不算数：恒绿的门与坏掉的门在 CI 输出上长得一样（本仓 §23.16 第 5 条）。
# 这里用**假树夹具**逐条注入违规，要求门逐条开火；最后用真仓对账一次，
# 防"正则失效退化成恒绿"。
#
# 用例（每格只注入一个违规，避免两个判据互相掩盖）：
#   G1  有注册点、登记表为空              ⇒ NEW 红（点名该任务）
#   G2  补上正确登记（锚点真存在）        ⇒ 绿
#   G3  新增第二个任务但未登记            ⇒ NEW 红；补 tenant-free 登记 ⇒ 绿
#   G4  fanout 锚点指向不存在的字面量     ⇒ STALE 红
#   G4b 扇出实现还在、但任务不再调它      ⇒ BROKEN 红（只查锚点会误绿的形态）
#   G5  任务被删但登记还在                ⇒ STALE 红
#   G6  seam 首参不是 context.Context     ⇒ SEAM 红
#   G7  缺登记表                          ⇒ rc=2（区分"没扫"与"零命中"）
#   G8  真仓库：门绿且注册点数 = 7
#   G10 隐藏上层目录检出仍扫描违规          ⇒ SEAM 红
#   G11 仓库内隐藏目录与依赖目录仍排除      ⇒ 绿
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT/scripts/check-tenant-ctx-writes.py"
REAL_REG="$ROOT/scripts/tenant-ctx-cron.registry"
PORT=0

fail() { echo "FAIL[$PORT] $*" >&2; exit 1; }
pass() { echo "PASS[$PORT] $*"; }

[ -f "$GATE" ] || fail "门脚本不存在: $GATE"
[ -f "$REAL_REG" ] || fail "登记表不存在: $REAL_REG"

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
FIX="$TMP/fixture"
mkdir -p "$FIX/scripts" "$FIX/user-server/internal/pkg/cron" "$FIX/user-server/internal/service"

CRON_GO="$FIX/user-server/internal/pkg/cron/cron.go"
SEAM_GO="$FIX/user-server/internal/service/seam.go"
REG="$FIX/scripts/tenant-ctx-cron.registry"
ANCHOR='tenant.Fanout(context.Background(), nil, func(ctx context.Context, _ uint) (int64, error) {'

cat > "$CRON_GO" <<'GO'
package cron

import "context"

func InitCron() {
	mgr := GetTaskManager()
	_, _ = mgr.AddTask("0 * * * * *", func() {
		RegisteredCron()
	})
}

func RegisteredCron() {
	_, _ = tenant.Fanout(context.Background(), nil, func(ctx context.Context, _ uint) (int64, error) {
		return 0, nil
	})
}
GO

cat > "$SEAM_GO" <<'GO'
package service

import "context"

var GlobalGood func(ctx context.Context, x string)
GO

run_gate() { python3 "$GATE" --repo "$FIX" >/dev/null 2>&1; echo $?; }
gate_out() { python3 "$GATE" --repo "$FIX" 2>&1; }

# ---------- G1：有注册点、登记表为空 ⇒ NEW ----------
: > "$REG"
PORT=1
rc=$(run_gate); [ "$rc" = "1" ] || fail "注册点无登记应 rc=1，实际 $rc"
out=$(gate_out); grep -qF "NEW  未登记的后台任务 'RegisteredCron'" <<<"$out" || fail "G1 未点名未登记任务"
pass "注册点无登记 ⇒ NEW 红"

# ---------- G2：补上正确登记 ⇒ 绿 ----------
PORT=2
printf 'RegisteredCron\tfanout\tuser-server/internal/pkg/cron/cron.go::%s\n' "$ANCHOR" > "$REG"
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "补齐登记后应 rc=0，实际 $rc"; }
pass "补上 fanout 登记（锚点存在）⇒ 绿"

# ---------- G3：新增第二个任务未登记 ⇒ NEW；补 tenant-free ⇒ 绿 ----------
PORT=3
python3 - "$CRON_GO" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace(
    '\t_, _ = mgr.AddTask("0 * * * * *", func() {\n\t\tRegisteredCron()\n\t})',
    '\t_, _ = mgr.AddTask("0 * * * * *", func() {\n\t\tRegisteredCron()\n\t})\n'
    '\t_, _ = mgr.AddTask("0 0 * * * *", func() {\n\t\tBrandNewCron()\n\t})')
s += "\nfunc BrandNewCron() {}\n"
p.write_text(s)
PY
rc=$(run_gate); [ "$rc" = "1" ] || fail "新增未登记任务应 rc=1，实际 $rc"
out=$(gate_out); grep -qF "BrandNewCron" <<<"$out" || fail "G3 未点名新增任务"
printf 'BrandNewCron\ttenant-free\t夹具：不碰库\n' >> "$REG"
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "补 tenant-free 后应 rc=0，实际 $rc"; }
pass "新增任务未登记 ⇒ 红；补 tenant-free ⇒ 绿"

# ---------- G4：fanout 锚点失效 ⇒ STALE ----------
PORT=4
cp "$REG" "$REG.bak"
{ head -1 "$REG.bak" | sed 's#::.*#::THIS ANCHOR DOES NOT EXIST#'; tail -n +2 "$REG.bak"; } > "$REG"
rc=$(run_gate); [ "$rc" = "1" ] || fail "锚点不存在应 rc=1，实际 $rc"
out=$(gate_out); grep -qF "扇出锚点" <<<"$out" || fail "G4 未打出锚点判词"
cp "$REG.bak" "$REG"; rm -f "$REG.bak"
pass "fanout 锚点失效 ⇒ STALE 红"

# ---------- G4b：扇出实现还在、但任务不再调它 ⇒ BROKEN ----------
# 门补得最晚也最值钱的一条：有人把调用改回不带租户的旧方法，只查锚点会误绿。
PORT=9
python3 - "$CRON_GO" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace(
    '\t_, _ = mgr.AddTask("0 * * * * *", func() {\n\t\tRegisteredCron()\n\t})',
    '\t_, _ = mgr.AddTask("0 * * * * *", func() {\n\t\tRegisteredCron()\n\t})\n'
    '\t_, _ = mgr.AddTask("0 1 * * * *", mgr.goTask("reverted_task", func(ctx context.Context) {\n'
    '\t\tlegacyService.RunOnce(ctx)\n\t}))')
p.write_text(s)
PY
printf 'reverted_task\tfanout\tuser-server/internal/pkg/cron/cron.go::%s\n' "$ANCHOR" >> "$REG"
rc=$(run_gate); [ "$rc" = "1" ] || fail "任务不再调扇出应 rc=1，实际 $rc"
out=$(gate_out); grep -qF "BROKEN 任务 'reverted_task'" <<<"$out" || fail "G4b 未打出 BROKEN 判词"
grep -v '^reverted_task' "$REG" > "$REG.tmp" && mv "$REG.tmp" "$REG"
python3 - "$CRON_GO" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace('\n\t_, _ = mgr.AddTask("0 1 * * * *", mgr.goTask("reverted_task", func(ctx context.Context) {\n\t\tlegacyService.RunOnce(ctx)\n\t}))', '')
p.write_text(s)
PY
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "撤掉该任务后应 rc=0，实际 $rc"; }
pass "扇出实现还在但任务不调它 ⇒ BROKEN 红"

# ---------- G5：任务被删但登记还在 ⇒ STALE ----------
PORT=5
python3 - "$CRON_GO" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1]); s = p.read_text()
s = s.replace('\t_, _ = mgr.AddTask("0 0 * * * *", func() {\n\t\tBrandNewCron()\n\t})\n', '')
s = s.replace('\nfunc BrandNewCron() {}\n', '\n')
p.write_text(s)
PY
rc=$(run_gate); [ "$rc" = "1" ] || fail "任务被删但登记还在应 rc=1，实际 $rc"
out=$(gate_out); grep -qF "STALE 登记表里的任务 'BrandNewCron'" <<<"$out" || fail "G5 未打出 STALE 判词"
grep -v '^BrandNewCron' "$REG" > "$REG.tmp" && mv "$REG.tmp" "$REG"
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "划掉登记后应 rc=0，实际 $rc"; }
pass "登记项无注册点 ⇒ STALE 红；划掉 ⇒ 绿"

# ---------- G6：seam 首参不是 context.Context ⇒ SEAM ----------
PORT=6
printf '\nvar GlobalBad func(x string)\n' >> "$SEAM_GO"
rc=$(run_gate); [ "$rc" = "1" ] || fail "seam 首参缺 ctx 应 rc=1，实际 $rc"
out=$(gate_out); grep -qE "SEAM .*GlobalBad" <<<"$out" || fail "G6 未点名 seam"
python3 - "$SEAM_GO" <<'PY'
import sys, pathlib
p = pathlib.Path(sys.argv[1]); p.write_text(p.read_text().replace("var GlobalBad func(x string)", "var GlobalBad func(ctx context.Context, x string)"))
PY
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "seam 补 ctx 后应 rc=0，实际 $rc"; }
pass "seam 首参不是 context.Context ⇒ SEAM 红；补 ctx ⇒ 绿"

# ---------- G7：缺登记表 ⇒ rc=2 ----------
PORT=7
mv "$REG" "$REG.hidden"
rc=$(run_gate); [ "$rc" = "2" ] || fail "缺登记表应 rc=2，实际 $rc"
mv "$REG.hidden" "$REG"
pass "缺登记表 ⇒ rc=2（未判定）"

# ---------- G10：检出上层隐藏目录不能使扫描面归零 ----------
PORT=10
HIDDEN_FIX="$TMP/.workspace/fixture"
mkdir -p "$(dirname "$HIDDEN_FIX")"
cp -R "$FIX" "$HIDDEN_FIX"
out=$(python3 "$GATE" --repo "$HIDDEN_FIX" 2>&1); rc=$?
[ "$rc" = "0" ] || { echo "$out" >&2; fail "隐藏上层检出合法夹具应绿，实际 rc=$rc"; }
printf '\nvar GlobalHiddenCheckoutBad func(x string)\n' >> "$HIDDEN_FIX/user-server/internal/service/seam.go"
out=$(python3 "$GATE" --repo "$HIDDEN_FIX" 2>&1); rc=$?
[ "$rc" = "1" ] || fail "隐藏上层检出仍应发现 seam 违规，实际 rc=$rc"
grep -qE "SEAM .*GlobalHiddenCheckoutBad" <<<"$out" || fail "隐藏上层检出漏扫违规 seam"
pass "隐藏上层检出仍扫描真实源文件并发现违规"

# ---------- G11：只排除仓库内隐藏目录与依赖目录 ----------
PORT=11
for ignored in .generated vendor node_modules; do
  mkdir -p "$FIX/user-server/internal/service/$ignored"
  printf 'package service\nvar GlobalIgnoredBad func(x string)\n' > "$FIX/user-server/internal/service/$ignored/seam.go"
done
rc=$(run_gate); [ "$rc" = "0" ] || { gate_out >&2; fail "仓库内隐藏/依赖目录应排除，实际 rc=$rc"; }
pass "仓库内隐藏目录、vendor 与 node_modules 仍排除"

# ---------- G8：真仓库（门绿 + 注册点计数对账） ----------
PORT=8
out=$(python3 "$GATE" 2>&1); rc=$?
[ "$rc" = "0" ] || { echo "$out" >&2; fail "真仓库应绿，实际 rc=$rc"; }
n=$(printf '%s' "$out" | grep -oE '注册点 [0-9]+ 个' | grep -oE '[0-9]+' | head -1)
[ -n "$n" ] || fail "G8 未能从门输出解析注册点数（正则失效？）"
[ "$n" -ge 7 ] || fail "真仓库注册点数应 ≥ 7，实际 $n"
grep -q '	fanout	' "$REAL_REG" || fail "真登记表里没有 fanout 条目"
grep -q '	tenant-free	' "$REAL_REG" || fail "真登记表里没有 tenant-free 条目（两种口径都要有样例）"
pass "真仓库绿且注册点 $n 个（fanout / tenant-free 两类登记都在）"

echo "PASS check-tenant-ctx-writes.test.sh（11 枚控制：NEW/补齐转绿/新增任务/锚点 STALE/BROKEN/条目 STALE/seam/缺表 rc=2/隐藏上层/内部排除/真仓对账）"
