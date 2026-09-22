#!/usr/bin/env bash
# check-action-runtime.test.sh — 给 scripts/check-action-runtime.py 做正反两向用例
#
# 为什么这道门必须有牙齿测试：它判的是「仓库里看不见的事实」——一个 `uses:` 声明
# 什么运行时，只有去读那个 action 自己仓库里的 action.yml 才知道。上一轮就是拿
# 版本号新旧猜「已迁完」，漏了 action-gh-release@v2 和 slsa-verifier installer 两处。
# 判据依赖一张手抄表（RUNTIMES）加一本手抄账（GRANDFATHER），那么「表和账本身写歪」
# 就是这道门最可能的失效方式 —— 表写歪会静默放行，账写歪会连坐成红。所以每一档
# 都要正反各钉一次：既钉「该红的红」，也钉「改回去就不红」。
#
# 口径：夹具全部现造（$TMP 下的 .yml），不联网、不读真仓库
#      （真仓库只在最后一格作为基线自洽性被跑一次）；
#      每条断言失败都打红因，收尾打印 PASS/FAIL 计数，有 FAIL 则 rc=1。
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DOOR="${HERE}/check-action-runtime.py"
[[ -f "${DOOR}" ]] || { echo "找不到被检脚本：${DOOR}"; exit 2; }
REPO_ROOT="$(cd "${HERE}/.." && pwd)"

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT
mkdir -p "${TMP}/.github/workflows"

PASS=0
FAIL=0
ok() {
  PASS=$((PASS + 1))
  printf '  ✓ %s\n' "${1}"
}
bad() {
  FAIL=$((FAIL + 1))
  printf '  ✗ %s\n      红因：%s\n' "${1}" "${2}"
}

expect_eq() { # $1 用例名 $2 期望 $3 实得 $4 说明
  if [[ "${2}" == "${3}" ]]; then
    ok "${1}"
  else
    bad "${1}" "${4}（期望 ${2}，实得 ${3}）"
  fi
}

expect_has() { # $1 用例名 $2 关键字 $3 文件
  if grep -qF -- "${2}" "${3}"; then
    ok "${1}"
  else
    bad "${1}" "输出里找不到「${2}」"
  fi
}

expect_absent() { # $1 用例名 $2 关键字 $3 文件
  if grep -qF -- "${2}" "${3}"; then
    bad "${1}" "输出里出现了「${2}」"
    grep -nF -- "${2}" "${3}" | sed 's/^/      /'
  else
    ok "${1}"
  fi
}

count_lines() { grep -c -- "${1}" "${2}" || true; }

write_yml() { # $1 路径 $2 job 名 $3.. pin 列表（每 pin 造一个 step 级 uses）
  local path="${1}" job="${2}" pin
  shift 2
  {
    echo "name: fixture"
    echo "on: push"
    echo "jobs:"
    echo "  ${job}:"
    echo "    runs-on: ubuntu-latest"
    echo "    steps:"
    local i=1
    for pin in "$@"; do
      echo "      - name: step${i}"
      echo "        uses: ${pin}"
      i=$((i + 1))
    done
  } > "${path}"
}

run_door() { # $1 输出文件 $2.. 传给被检脚本的参数
  python3 "${DOOR}" --repo "${TMP}" "${@:2}" > "${1}" 2>&1
  echo "${?}"
}

GREEN='✅ 无未豁免的弃用运行时站点'

echo "──── 格 1：全 node24 的夹具必须绿，且证明真扫到了文件 ────"
write_yml "${TMP}/.github/workflows/clean.yml" build \
  actions/checkout@v7 actions/setup-go@v7 actions/setup-node@v7
rc="$(run_door "${TMP}/g1.log" "${TMP}/.github/workflows/clean.yml")"
expect_eq "格1 rc=0" "0" "${rc}" "干净夹具退出码"
expect_has "格1 打印了扫描份数（空跑会被这行揭穿）" "扫描工作流 1 份" "${TMP}/g1.log"
expect_has "格1 三处站点全部可判定" "uses 站点 3 处" "${TMP}/g1.log"

echo "──── 格 2：未豁免的 node20 要点名到工位 ────"
write_yml "${TMP}/.github/workflows/dirty.yml" build actions/checkout@v4
rc="$(run_door "${TMP}/g2.log" "${TMP}/.github/workflows/dirty.yml")"
expect_eq "格2 rc=1" "1" "${rc}" "node20 未豁免退出码"
expect_has "格2 点名文件与 pin" "dirty.yml: actions/checkout@v4" "${TMP}/g2.log"
expect_has "格2 点名工位（job#step 序号）" "at dirty.yml:build#1" "${TMP}/g2.log"
expect_has "格2 印出实际 using 值" "using=node20" "${TMP}/g2.log"
# 反向：同一个夹具换成 node24 的那一档必须不红，证明红是运行时判据打的不是无条件打的
write_yml "${TMP}/.github/workflows/dirty24.yml" build actions/checkout@v7
rc="$(run_door "${TMP}/g2b.log" "${TMP}/.github/workflows/dirty24.yml")"
expect_eq "格2 反向：同形状换 v7 则绿" "0" "${rc}" "node24 夹具退出码"

echo "──── 格 3：豁免要放行，且放行的是「这一档」而不是整道门 ────"
rc="$(run_door "${TMP}/g3.log" --grandfather "dirty.yml:actions/checkout@v4=1" \
  "${TMP}/.github/workflows/dirty.yml")"
expect_eq "格3 豁免内 rc=0" "0" "${rc}" "豁免后退出码"
expect_has "格3 计数把豁免单独列出来" "豁免内 1" "${TMP}/g3.log"
expect_absent "格3 豁免后不再打未豁免红因" "无豁免" "${TMP}/g3.log"
# 反向：豁免写的是别的 pin，就不能把这一档放行
rc="$(run_door "${TMP}/g3b.log" --grandfather "dirty.yml:actions/setup-node@v4=1" \
  "${TMP}/.github/workflows/dirty.yml")"
expect_eq "格3 反向：豁免记错 pin 则仍红" "1" "${rc}" "错配豁免退出码"

echo "──── 格 4：上界只降不升（棘轮）────"
write_yml "${TMP}/.github/workflows/grew.yml" build \
  actions/checkout@v4 actions/checkout@v4 actions/checkout@v4
rc="$(run_door "${TMP}/g4.log" --grandfather "grew.yml:actions/checkout@v4=2" \
  "${TMP}/.github/workflows/grew.yml")"
expect_eq "格4 超上界 rc=1" "1" "${rc}" "站点数超豁免上界退出码"
expect_has "格4 报出实有与上界" "实有 3 > 上界 2" "${TMP}/g4.log"
# 反向：上界抬到 3 就不红 —— 证明红来自比较式，不是「进了豁免表就红」
rc="$(run_door "${TMP}/g4b.log" --grandfather "grew.yml:actions/checkout@v4=3" \
  "${TMP}/.github/workflows/grew.yml")"
expect_eq "格4 反向：上界=3 时绿" "0" "${rc}" "上界相等退出码"

echo "──── 格 5：豁免账本留空条目要红（防止「已迁完」被伪装成「仍豁免」）────"
write_yml "${TMP}/.github/workflows/migrated.yml" build actions/checkout@v7
rc="$(run_door "${TMP}/g5.log" --grandfather "migrated.yml:actions/checkout@v4=5" \
  "${TMP}/.github/workflows/migrated.yml")"
expect_eq "格5 STALE rc=1" "1" "${rc}" "豁免条目匹配不到站点"
expect_has "格5 点名 STALE 条目" "migrated.yml: actions/checkout@v4" "${TMP}/g5.log"
expect_has "格5 给出处置动作" "请删条目" "${TMP}/g5.log"
# 反向：条目对应真在用的 pin 就不该报 STALE
rc="$(run_door "${TMP}/g5b.log" --grandfather "migrated.yml:actions/checkout@v7=1" \
  "${TMP}/.github/workflows/migrated.yml")"
expect_eq "格5 反向：条目对得上则绿" "0" "${rc}" "有效豁免退出码"

echo "──── 格 6：表里没有的 pin 一律 rc=2（不许凭版本号推）────"
write_yml "${TMP}/.github/workflows/newpin.yml" build someorg/newaction@v9
rc="$(run_door "${TMP}/g6.log" "${TMP}/.github/workflows/newpin.yml")"
expect_eq "格6 未知 pin rc=2" "2" "${rc}" "未登记 pin 退出码"
expect_has "格6 点名未登记 pin" "someorg/newaction@v9" "${TMP}/g6.log"
expect_has "格6 指出去读 action.yml" "去读该 tag 的 action.yml" "${TMP}/g6.log"
# 未知 pin 的红不许被「未豁免=0」的绿象盖掉
expect_absent "格6 未知时不许宣称干净" "${GREEN}" "${TMP}/g6.log"
# 退出码优先级：同一趟里既有未知 pin（2）又有 STALE 豁免（1）时，报 2 不许被压成 1，
# 否则「表写歪了」会伪装成「只是有站点没豁免」——两者处置动作完全不同。
rc="$(run_door "${TMP}/g6c.log" --grandfather "newpin.yml:actions/checkout@v4=1" \
  "${TMP}/.github/workflows/newpin.yml")"
expect_eq "格6 未知+STALE 同趟时 rc 仍为 2" "2" "${rc}" "退出码优先级"
expect_has "格6 同趟里 STALE 也要一并报出" "请删条目" "${TMP}/g6c.log"

echo "──── 格 7：job 级 uses 要参与判定，可复用工作流/本地形态要跳过 ────"
cat > "${TMP}/.github/workflows/mixed.yml" <<'YML'
name: fixture-mixed
on: push
jobs:
  caller:
    uses: ./.github/workflows/other.yml
    secrets: inherit
  reusable_from_elsewhere:
    uses: octo/repo/.github/workflows/x.yml@v2
  direct:
    uses: actions/checkout@v4
YML
rc="$(run_door "${TMP}/g7.log" "${TMP}/.github/workflows/mixed.yml")"
expect_eq "格7 job 级 node20 未豁免 rc=1" "1" "${rc}" "混合夹具退出码"
expect_has "格7 打出了跳过计数" "跳过 2 处" "${TMP}/g7.log"
expect_has "格7 job 级 uses 也被扫到（只数 steps 会漏它）" "mixed.yml:direct" "${TMP}/g7.log"
expect_has "格7 可判定站点只剩那 1 处" "uses 站点 1 处" "${TMP}/g7.log"
expect_absent "格7 .yml 形态不进 RUNTIMES 判定（不报未知）" "无法判定其 runs.using" "${TMP}/g7.log"
# 反向：把 job 级 pin 换成 node24，同一夹具必须绿
sed -i.bak 's#uses: actions/checkout@v4#uses: actions/checkout@v7#' \
  "${TMP}/.github/workflows/mixed.yml"
rc="$(run_door "${TMP}/g7b.log" "${TMP}/.github/workflows/mixed.yml")"
expect_eq "格7 反向：job 级换 v7 则绿" "0" "${rc}" "node24 混合夹具退出码"
expect_has "格7 反向仍计入跳过数" "跳过 2 处" "${TMP}/g7b.log"

echo "──── 格 8：坏 YAML 判红不判绿 ────"
printf 'name: broken\non: push\njobs:\n  a:\n   bad: [unclosed\n' \
  > "${TMP}/.github/workflows/broken.yml"
rc="$(run_door "${TMP}/g8.log" "${TMP}/.github/workflows/broken.yml")"
expect_eq "格8 解析失败 rc=2" "2" "${rc}" "不可解析工作流退出码"
expect_has "格8 点名坏文件" "broken.yml" "${TMP}/g8.log"
expect_absent "格8 坏文件不许带着绿象退出" "${GREEN}" "${TMP}/g8.log"

echo "──── 格 9：零输入是 SKIP 不是 PASS ────"
rc="$(run_door "${TMP}/g9.log" "${TMP}/.github/workflows/nope.yml")"
expect_eq "格9 文件不存在 rc=2" "2" "${rc}" "读不到文件退出码"
mkdir -p "${TMP}/empty/.github/workflows"
python3 "${DOOR}" --repo "${TMP}/empty" > "${TMP}/g9b.log" 2>&1
rc="${?}"
expect_eq "格9 空目录 rc=2" "2" "${rc}" "零个工作流退出码"
expect_has "格9 空目录点名零扫描" "零个工作流文件被扫到" "${TMP}/g9b.log"

echo "──── 格 10：出厂基线自洽（跑真仓库，表与账必须对得上磁盘）────"
python3 "${DOOR}" --repo "${REPO_ROOT}" > "${TMP}/g10.log" 2>&1
rc="${?}"
expect_eq "格10 真仓库 rc=0" "0" "${rc}" "出厂基线在真仓库上的退出码"
expect_has "格10 真仓库无 STALE/未豁免红因" "${GREEN}" "${TMP}/g10.log"
expect_has "格10 真仓库确实扫到了多份工作流" "扫描工作流 " "${TMP}/g10.log"
n="$(count_lines '扫描工作流' "${TMP}/g10.log")"
expect_eq "格10 汇总行只印一次" "1" "${n}" "扫描行出现次数"
# 真仓库的 node20 命中数不许是 0 —— 为 0 说明扫描面被换掉了（比如 glob 失效）
# 注意别拿 `grep -oE '[0-9]+'` 去整行里捞：那会先捞到 "node20" 里的 20。
hits="$(grep -oE '命中 [0-9]+ 处' "${TMP}/g10.log" | grep -oE '[0-9]+')"
if [[ -n "${hits}" && "${hits}" -gt 0 ]]; then
  ok "格10 真仓库基线非空（命中 ${hits} 处，全部落在豁免内）"
else
  bad "格10 真仓库基线非空" "命中数为 ${hits:-空}，说明扫描面或表被改坏"
fi

echo "════════════════════════════════════"
printf 'PASS=%s FAIL=%s\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]] || exit 1
echo "✅ check-action-runtime 的判据两向都有牙"
