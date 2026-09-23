#!/usr/bin/env bash
# check-ci-gate-paths.py 的用例：假 gh 不需要、不联网，全在临时目录里做。
#
# 四格，缺一格就是"改了一半"：
#   G1 缺 paths ⇒ 必须红，且红因点名"哪个作业、缺哪一行"（本门的存在理由就是这条）；
#   G2 补齐 ⇒ 必须绿；
#   G3 真仓库（本泳道树）⇒ 必须绿，且自证计数器要能对上独立复算的数目；
#   G4 工作流目录不存在 ⇒ rc=2（没跑过，不是绿）。
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHECKER="$ROOT/scripts/check-ci-gate-paths.py"
FAIL=0
WORK=$(mktemp -d /tmp/ci-gate-paths-test.XXXXXX)
trap 'rm -rf "$WORK"' EXIT

ok()  { echo "  ✓ $1"; }
bad() { echo "  ✗ $1"; FAIL=$((FAIL + 1)); }

[ -f "$CHECKER" ] || { echo "FATAL: 找不到 $CHECKER"; exit 1; }

# ---- 公共夹具：一份门脚本（自带基线引用）+ 一份只跑它的工作流 -----------------
seed() {  # seed <目标目录>
  local d="$1"
  mkdir -p "$d/.github/workflows" "$d/scripts"
  cat > "$d/scripts/check-fake.py" <<'PY'
#!/usr/bin/env python3
BASELINE = "scripts/check-fake.baseline"
PY
  : > "$d/scripts/check-fake.baseline"
}

wf() {  # wf <目标目录> <push paths 段|__NONE__>
  local d="$1" paths="$2"
  {
    echo 'name: Fake CI'
    echo 'on:'
    echo '  push:'
    echo '    branches: [main]'
    if [ "$paths" = "__NONE__" ]; then
      echo '  pull_request:'
      echo '    branches: [main]'
    else
      echo "$paths"
      echo '  pull_request:'
      echo '    branches: [main]'
      echo "$paths"
    fi
    echo 'jobs:'
    echo '  gates:'
    echo '    runs-on: ubuntu-latest'
    echo '    steps:'
    echo '      - name: fake gate'
    echo '        run: python3 scripts/check-fake.py'
  } > "$d/.github/workflows/fake.yml"
}

echo "G1 缺 paths ⇒ 红"
d="$WORK/g1"; seed "$d"
wf "$d" "    paths:
      - 'user-server/**'"
out=$(cd "$d" && python3 "$CHECKER" --repo . 2>&1); rc=$?
if [ "$rc" -eq 1 ]; then ok "rc=1"; else bad "rc=$rc（期望 1）"; fi
echo "$out" | grep -q 'scripts/check-fake.py' \
  && ok '红因点名门脚本' || bad '红因没点名门脚本'
echo "$out" | grep -q 'scripts/check-fake.baseline' \
  && ok '红因点名基线（从门源码派生，不是手抄）' || bad '红因没点名基线'
echo "$out" | grep -q 'fake.yml' && ok '红因点名工作流' || bad '红因没点名工作流'

echo "G2 补齐 ⇒ 绿"
d="$WORK/g2"; seed "$d"
wf "$d" "    paths:
      - 'user-server/**'
      - 'scripts/check-fake.py'
      - 'scripts/check-fake.baseline'"
out=$(cd "$d" && python3 "$CHECKER" --repo . 2>&1); rc=$?
if [ "$rc" -eq 0 ]; then ok 'rc=0'; else bad "rc=$rc（期望 0）"; echo "$out" | sed 's/^/      /'; fi
echo "$out" | grep -qE '无 paths 过滤' && bad 'G2 里明明有 paths，却报了"无过滤"' || ok '未误报"无过滤"'

echo "G2b 完全没有 paths（每次触发）⇒ 放行，但必须明说"
d="$WORK/g2b"; seed "$d"
wf "$d" "__NONE__"
out=$(cd "$d" && python3 "$CHECKER" --repo . 2>&1); rc=$?
if [ "$rc" -eq 0 ]; then ok 'rc=0'; else bad "rc=$rc（期望 0）"; echo "$out" | sed 's/^/      /'; fi
echo "$out" | grep -qE '无 paths 过滤' && ok '打印了"无 paths 过滤⇒每次触发"' \
  || bad '放行却没说明理由（静默通过＝恒绿的另一种形态）'

echo "G3 真仓库 ⇒ 绿 + 计数器对得上"
out=$(cd "$ROOT" && python3 "$CHECKER" --repo . 2>&1); rc=$?
if [ "$rc" -eq 0 ]; then ok 'rc=0'; else bad "rc=$rc（期望 0）"; echo "$out" | sed 's/^/      /'; fi
# BSD sed 不认 `\+`（GNU 才支持）：这里用 grep -E 抽自证数，否则恒抽成空串 ⇒ 假红
scanned=$(printf '%s\n' "$out" | grep -oE '工作流 [0-9]+ 份' | head -1 | grep -oE '[0-9]+')
real=$(ls "$ROOT/.github/workflows" | grep -cE '\.ya?ml$')
if [ -n "$scanned" ] && [ "$scanned" = "$real" ]; then ok "自证 scanned=$scanned = 独立复算 $real"; else bad "自证 '$scanned' ≠ 独立复算 $real"; fi
# 派生集不许是空表：空表意味着正则失效，本门会退化成恒绿
printf '%s\n' "$out" | grep -qE '派生判据文件 [1-9][0-9]* 份' \
  && ok '派生集非空' || bad '派生集为空/异常（判据文件没被抓到）'

echo "G4 工作流目录不存在 ⇒ rc=2"
d="$WORK/g4"; mkdir -p "$d/scripts"
out=$(cd "$d" && python3 "$CHECKER" --repo . 2>&1); rc=$?
if [ "$rc" -eq 2 ]; then ok 'rc=2（没跑过 ≠ 绿）'; else bad "rc=$rc（期望 2）"; fi

echo
if [ "$FAIL" -eq 0 ]; then echo "全部通过"; exit 0; else echo "失败 $FAIL 格"; exit 1; fi
