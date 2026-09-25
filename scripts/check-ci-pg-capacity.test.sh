#!/usr/bin/env bash
# check-ci-pg-capacity.py 的用例：不联网、不要 docker、不要 runner，全在临时目录里造树。
#
# 为什么单独有一份用例：这道门的判据横跨三份文件（工作流的 services／steps ＋ 两处 Go 池上界），
# 任何一处漂移都只会以"CI 里 53300 假红"的形式出现在**别人**手里（§23.21 第 2 段那条本地按不下的
# 红就是它）。所以每格都必须同时断言 rc **与**"哪一句判据开火"——只判 rc 的话，
# 缺文件退 2 与判据不成立退 1 会被混成一格（[[feedback-cli-toolchain-gotchas]] "rc=2 是没跑过"）。
#
# 十格：
#   F1 齐全 ⇒ 绿（正控制：证明夹具满足每一格的前置，否则后面的红可能是假阳）
#   F2 摘掉一处 command ⇒ 红，点名那个 job
#   F3 两处声明值不等 ⇒ 红，说"不等"
#   F4 摘掉现测步骤 ⇒ 红，点名"没现测"
#   F5 现测步骤只 echo 不比对 ⇒ 红，说"只印不判"
#   F6 声明值低于代码侧池上界之和 ⇒ 红，点名两个数
#   F7 读不到 Go 侧上界（源文件改名） ⇒ rc=2，不许当成绿
#   F7b 源文件在、但那枚上界字面量被改没了 ⇒ 同样 rc=2，红因点名缺失的形状
#       （F7 与 F7b 是两条不同的分支：一条是"文件不在"，一条是"形状不在"，合起来才盖住 rc=2 这一类）
#   F8 注释里多写一处 max_connections=<数字> ⇒ 红，说派生 grep 会被污染（C6）
#   REAL 真仓库 ⇒ 必须绿（本卡就是要把它从红改绿的那笔 CI 字节的前置）
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHECKER="$ROOT/scripts/check-ci-pg-capacity.py"
FAIL=0
WORK=$(mktemp -d /tmp/ci-pg-capacity-test.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

ok()  { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$CHECKER" ] || { echo "FATAL: 找不到 $CHECKER"; exit 1; }

# ── 夹具：一份最小工作流（两枚 postgres 服务＋两枚现测步骤）＋两份带池上界的 Go 源 ──
# 名额 400，代码侧 200＋32＝232 ⇒ F1 天然有余量。
seed() {
  local d="$WORK/$1"
  mkdir -p "$d/.github/workflows" "$d/user-server/internal/config" "$d/user-server/internal/pkg/testutil"
  cat > "$d/user-server/internal/config/server.go" <<'GO'
package config

var DefaultPoolConfig = PoolConfig{
	MaxIdleConns:    50,
	MaxOpenConns:    200,
	ConnMaxIdleTime: 300,
}
GO
  cat > "$d/user-server/internal/pkg/testutil/testdb.go" <<'GO'
package testutil

const (
	testDBMaxOpenConns = 32
	testDBMaxIdleConns = 8
)
GO
  make_wf "$d" 400 400 both cmp
  echo "$d"
}

# make_wf <目录> <job1 名额> <job2 名额> <both|one> <cmp|echo>
#   both|one：两枚 job 都有现测步骤 / 只有第一枚有
#   cmp|echo：现测步骤真比对 / 只 echo（"印了但没判"）
make_wf() {
  local d="$1" n1="$2" n2="$3" measure="$4" judge="$5"
  {
    echo 'name: Fake CI'
    echo 'on:'
    echo '  push:'
    echo '    branches: [main]'
    echo 'jobs:'
    echo '  alpha:'
    echo '    services:'
    echo '      postgres:'
    echo '        image: pgvector/pgvector:pg15'
    if [ "$n1" != "none" ]; then echo "        command: '-c max_connections=$n1'"; fi
    echo '        options: >-'
    echo '          --health-cmd "pg_isready -U admin"'
    echo '    steps:'
    echo '      - name: Checkout'
    echo '        uses: actions/checkout@v4'
    if [ "$measure" = both ] || [ "$measure" = one ]; then step_body "$judge" "$n1"; fi
    echo '  beta:'
    echo '    services:'
    echo '      postgres:'
    echo '        image: pgvector/pgvector:pg15'
    if [ "$n2" != "none" ]; then echo "        command: '-c max_connections=$n2'"; fi
    echo '        options: >-'
    echo '          --health-cmd "pg_isready -U admin"'
    echo '    steps:'
    echo '      - name: Checkout'
    echo '        uses: actions/checkout@v4'
    if [ "$measure" = both ]; then step_body "$judge" "$n2"; fi
  } > "$d/.github/workflows/user-server-ci.yml"
}

step_body() {
  local judge="$1"
  echo '      - name: 现测 PG 名额'
  echo '        run: |'
  echo '          set -euo pipefail'
  if [ "$judge" = cmp ]; then
    echo "          want=\$(grep -m1 -oE 'max_connections=[0-9]+' .github/workflows/user-server-ci.yml | cut -d= -f2)"
    echo "          got=\$(psql -U admin -d marketing_tools -Atc 'show max_connections')"
    echo '          if [ "$got" != "$want" ]; then echo "::error::容器没按声明起来（$got ≠ $want）"; exit 1; fi'
  else
    echo "          psql -U admin -d marketing_tools -Atc 'show max_connections'"
  fi
}

run() { (cd "$1" && python3 "$CHECKER" --repo . 2>&1); }

# ---- F1 ------------------------------------------------------------------
echo "F1 齐全 ⇒ 绿"
d=$(seed f1); out=$(run "$d"); rc=$?
[ "$rc" = 0 ] && ok "rc=0" || bad "rc=${rc}（须 0）：$(printf '%s' "$out" | tail -3)"

# ---- F2 ------------------------------------------------------------------
echo "F2 摘掉一处 command ⇒ 红且点名 job"
d=$(seed f2); make_wf "$d" none 400 both cmp >/dev/null
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）"
printf '%s' "$out" | grep -q 'alpha' && ok '点名 alpha' || bad '没点名 alpha'
printf '%s' "$out" | grep -q '没有显式名额' && ok '红因＝缺 command' || bad '红因不是缺 command'

# ---- F3 ------------------------------------------------------------------
echo "F3 两处声明值不等 ⇒ 红"
d=$(seed f3); make_wf "$d" 400 800 both cmp >/dev/null
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）"
printf '%s' "$out" | grep -q '不等' && ok '红因＝名额不等' || bad '红因不是名额不等'

# ---- F4 ------------------------------------------------------------------
echo "F4 一枚 job 没有现测步骤 ⇒ 红"
d=$(seed f4); make_wf "$d" 400 400 one cmp >/dev/null
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）"
printf '%s' "$out" | grep -q 'beta' && ok '点名 beta' || bad '没点名 beta'
printf '%s' "$out" | grep -q '没有现测' && ok '红因＝缺现测步骤' || bad '红因不是缺现测步骤'

# ---- F5 ------------------------------------------------------------------
echo "F5 现测步骤只 echo 不比对 ⇒ 红"
d=$(seed f5); make_wf "$d" 400 400 both echo >/dev/null
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）"
printf '%s' "$out" | grep -q '只印不判' && ok '红因＝只印不判' || bad '红因不是只印不判'

# ---- F6 ------------------------------------------------------------------
echo "F6 名额低于代码侧池上界之和 ⇒ 红且点名两个数"
d=$(seed f6); make_wf "$d" 200 200 both cmp >/dev/null
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）"
printf '%s' "$out" | grep -q '232' && ok '红因点名所需名额 232' || bad '没点名所需名额'
printf '%s' "$out" | grep -q '200' && ok '红因点名声明名额 200' || bad '没点名声明名额'

# ---- F7 ------------------------------------------------------------------
echo "F7 Go 源文件不在 ⇒ rc=2（不许当绿）"
d=$(seed f7); mv "$d/user-server/internal/config/server.go" "$d/user-server/internal/config/server.go.hidden"
out=$(run "$d"); rc=$?
[ "$rc" = 2 ] && ok "rc=2" || bad "rc=${rc}（须 2）：$(printf '%s' "$out" | tail -3)"
printf '%s' "$out" | grep -q 'internal/config/server.go' && ok '红因点名缺失的文件' || bad '没点名文件'

echo "F7b 文件在但上界字面量被改没 ⇒ rc=2 且点名形状"
d=$(seed f7b); sed -i.bak '/MaxOpenConns:/d' "$d/user-server/internal/config/server.go"; rm -f "$d/user-server/internal/config/server.go.bak"
out=$(run "$d"); rc=$?
[ "$rc" = 2 ] && ok "rc=2" || bad "rc=${rc}（须 2）：$(printf '%s' "$out" | tail -3)"
printf '%s' "$out" | grep -q 'MaxOpenConns' && ok '红因点名读不到的形状' || bad '没点名形状'

echo "F8 注释里多一处名额 ⇒ 红（现测步骤的 grep -m1 会读错）"
# 插入用 python 不用 sed：BSD sed 的 `4i\ 文本` 形态在本机报
# `extra characters after \ at the end of i command`，注码没落地 ⇒ 门自然退 0，
# 一格"注了码而门不报"的假绿就是这么造出来的（§23.21 第 4 段 G3 同形）。
d=$(seed f8)
python3 - "$d/.github/workflows/user-server-ci.yml" <<'PY'
import sys
p = sys.argv[1]
lines = open(p, encoding="utf-8").read().splitlines(True)
lines.insert(3, "        # 举例：max_connections=999\n")
open(p, "w", encoding="utf-8").write("".join(lines))
PY
out=$(run "$d"); rc=$?
[ "$rc" = 1 ] && ok "rc=1" || bad "rc=${rc}（须 1）：$(printf '%s' "$out" | tail -3)"
printf '%s' "$out" | grep -q 'grep -m1' && ok '红因点名派生 grep' || bad '没点名派生 grep'

# ---- REAL ----------------------------------------------------------------
echo "REAL 真仓库 ⇒ 绿（本卡的 CI 字节必须同时满足这道门）"
out=$(run "$ROOT"); rc=$?
[ "$rc" = 0 ] && ok "rc=0" || bad "rc=${rc}（须 0）：$(printf '%s' "$out" | tail -6)"
printf '%s' "$out" | grep -qE 'postgres 服务 [0-9]+ 处' && ok '自证计数器在场' || bad '没有自证计数器'
printf '%s' "$out" | grep -qE '现测步骤 [0-9]+ 处' && ok '现测计数在场' || bad '没有现测计数'

echo
if [ "$FAIL" = 0 ]; then echo "===== 用例：十格全过（断言失败 0 处）====="; exit 0; fi
echo "===== 用例：$FAIL 处断言失败 ====="; exit 1
