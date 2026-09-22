#!/usr/bin/env bash
# check-ci-step-coverage.test.sh — 给 scripts/check-ci-step-coverage.py 做正反两向用例
#
# 为什么这道门要有用例：它是本仓唯一会回答"哪道门从来没跑过"的工具，而它自己
# 原先只数 **步骤**（`job.steps`），于是"整个作业被跳过"这类死门在它的输出里
# **一个字符都不存在** —— 实测形状就是 lint.yml 的 `LICENSE Compliance Scan`
# （2026-09-15 起挂月度 cron，窗口内 0 次 schedule run，作业级 skipped、steps 为空）。
# 判据本身有洞时，"没报"与"没有"看起来一模一样，所以这里把两向都钉成可跑的断言。
#
# 口径：全程用假 `gh`（$TMP/bin/gh）喂夹具 JSON，**不联网、不需要 gh 登录**；
#      每条断言失败都打红因，收尾打印 PASS/FAIL 计数，有 FAIL 则 rc=1。
set -uo pipefail

DOOR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts/check-ci-step-coverage.py"
[[ -f "${DOOR}" ]] || { echo "找不到被检脚本：${DOOR}"; exit 2; }

TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT
mkdir -p "${TMP}/bin" "${TMP}/.github/workflows" "${TMP}/fx"

cat > "${TMP}/bin/gh" <<'SHIM'
#!/usr/bin/env bash
# 只认本用例喂的那两条 API 路径；其余一律非零退出 —— 假 gh 静默返回空会把"没取到"
# 伪装成"取到了但没问题"，那是这道门最不该有的失效方式。
if [[ "${1:-}" != "api" ]]; then
  echo "fake gh: 不认识的调用：$*" >&2
  exit 7
fi
path="${2:-}"
case "${path}" in
  */actions/runs?per_page=100) cat "${GH_FIXTURE_RUNS}" ;;
  */jobs?per_page=100)
    # 允许按 run id 发不同的 job 表：没有"窗口里只跑了 2 次"的形状，就证不了
    # "两次全败"这一档该不该判红。
    rid="$(printf '%s' "${path}" | sed -n 's#.*/runs/\([0-9]\{1,\}\)/jobs.*#\1#p')"
    per="${GH_FIXTURE_DIR}/jobs_run${rid}.json"
    if [[ -f "${per}" ]]; then cat "${per}"; else cat "${GH_FIXTURE_JOBS}"; fi
    ;;
  *)
    echo "fake gh: 不认识的 API 路径：${path}" >&2
    exit 7
    ;;
esac
SHIM
chmod +x "${TMP}/bin/gh"

cat > "${TMP}/fx/runs.json" <<'JSON'
{"workflow_runs":[
 {"id":1,"name":"Demo","head_branch":"master","created_at":"2026-09-20T00:00:00Z"},
 {"id":2,"name":"Demo","head_branch":"master","created_at":"2026-09-21T00:00:00Z"},
 {"id":3,"name":"Demo","head_branch":"master","created_at":"2026-09-22T00:00:00Z"}
]}
JSON

cat > "${TMP}/fx/jobs.json" <<'JSON'
{"jobs":[
 {"name":"Guarded door","conclusion":"skipped","steps":[]},
 {"name":"Depends on guarded","conclusion":"skipped","steps":[]},
 {"name":"Dead door","conclusion":"skipped","steps":[]},
 {"name":"Healthy door","conclusion":"success","steps":[
   {"name":"Set up job","conclusion":"success"},
   {"name":"Report","conclusion":"skipped"},
   {"name":"Complete job","conclusion":"success"}]}
]}
JSON

# 控制版：Dead door 真的执行并成功 ⇒ 作业级红必须消失（证明红不是无条件打出来的）
cat > "${TMP}/fx/jobs_alive.json" <<'JSON'
{"jobs":[
 {"name":"Guarded door","conclusion":"skipped","steps":[]},
 {"name":"Depends on guarded","conclusion":"skipped","steps":[]},
 {"name":"Dead door","conclusion":"success","steps":[
   {"name":"Set up job","conclusion":"success"},
   {"name":"Check","conclusion":"success"},
   {"name":"Complete job","conclusion":"success"}]},
 {"name":"Healthy door","conclusion":"success","steps":[
   {"name":"Set up job","conclusion":"success"},
   {"name":"Report","conclusion":"skipped"},
   {"name":"Complete job","conclusion":"success"}]}
]}
JSON

write_yml() {  # $1 = 目标文件，$2 = guarded 作业是否带 if: 守卫，$3 = Report 步骤是否带 if:（节奏步骤）
  local out="${1}" with_if="${2}" step_if="${3:-no}" guard_line="" step_guard=""
  [[ "${with_if}" == "yes" ]] && guard_line="    if: github.event_name == 'schedule'"
  [[ "${step_if}" == "yes" ]] && step_guard="        if: startsWith(github.ref, 'refs/tags/')"
  cat > "${out}" <<YML
name: Demo
on:
  push:
    branches: [master]
jobs:
  guarded:
    name: Guarded door
${guard_line}
    runs-on: ubuntu-latest
    steps:
      - name: Scan
        run: echo ok
  downstream:
    name: Depends on guarded
    needs: [guarded]
    runs-on: ubuntu-latest
    steps:
      - name: Do
        run: echo ok
  dead:
    name: Dead door
    runs-on: ubuntu-latest
    steps:
      - name: Check
        run: echo ok
  healthy:
    name: Healthy door
    runs-on: ubuntu-latest
    steps:
      - name: Report
${step_guard}
        run: echo ok
  failing:
    name: Failing door
    runs-on: ubuntu-latest
    steps:
      - name: Check
        run: echo ok
YML
}

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

run_door() { # $1 输出文件 $2 jobs 夹具 $3.. 传给被检脚本的额外参数
  PATH="${TMP}/bin:${PATH}" GH_FIXTURE_RUNS="${TMP}/fx/runs.json" GH_FIXTURE_DIR="${TMP}/fx" \
    GH_FIXTURE_JOBS="${2}" python3 "${DOOR}" --slug o/r --repo "${TMP}" \
    --runs 3 --min-presence 3 "${@:3}" > "${1}" 2>&1
  echo "${?}"
}

count_lines() { grep -c -- "${1}" "${2}" || true; }

echo "──── 格 1：作业级死门要点名，节奏门不连坐，旧的步骤轴不许退化 ────"
write_yml "${TMP}/.github/workflows/demo.yml" yes
rc="$(run_door "${TMP}/g1.log" "${TMP}/fx/jobs.json")"
dead_n="$(count_lines '^  · NEVER_RUN_JOB ' "${TMP}/g1.log")"
cadence_n="$(count_lines '^  · 节奏门 ' "${TMP}/g1.log")"
expect_eq "格1 rc=1（有命中就该红）" "1" "${rc}" "run_door 退出码"
expect_eq "格1 作业级红恰 1 条" "1" "${dead_n}" "NEVER_RUN_JOB 行数"
expect_has "格1 点名 Dead door" "NEVER_RUN_JOB" "${TMP}/g1.log"
expect_has "格1 红因写明无 if: 守卫" "无 if: 守卫" "${TMP}/g1.log"
expect_eq "格1 节奏门恰 2 条（直接 if ＋ needs 继承）" "2" "${cadence_n}" "节奏门行数"
expect_has "格1 节奏门含 Guarded door" "Guarded door" "${TMP}/g1.log"
expect_has "格1 节奏门含 Depends on guarded" "Depends on guarded" "${TMP}/g1.log"
expect_has "格1 步骤轴仍报 Report（旧判据没被改没）" "NEVER_RUN  Demo / Healthy door / Report" "${TMP}/g1.log"

echo "──── 格 2（反向）：摘掉 yml 里那行 if: ⇒ 节奏门豁免必须失效 ────"
write_yml "${TMP}/.github/workflows/demo.yml" no
if grep -q "^    if:" "${TMP}/.github/workflows/demo.yml"; then
  bad "格2 变异落地" "夹具 yml 里仍留着 if: 行，豁免臂没被摘掉，这一格的读数无意义"
else
  ok "格2 变异落地（yml 已无 if:）"
fi
rc2="$(run_door "${TMP}/g2.log" "${TMP}/fx/jobs.json")"
dead2="$(count_lines '^  · NEVER_RUN_JOB ' "${TMP}/g2.log")"
expect_eq "格2 rc=1" "1" "${rc2}" "run_door 退出码"
expect_eq "格2 作业级红涨到 3 条" "3" "${dead2}" "摘掉 if: 后 Guarded/Depends/Dead 三条都该计红"
cad2="$(count_lines '^  · 节奏门 ' "${TMP}/g2.log")"
expect_eq "格2 节奏门段消失" "0" "${cad2}" "没有守卫可豁免时不该印节奏门"

echo "──── 格 3（反向）：死门不在 yml 里（守卫未知）⇒ 不许静默放行 ────"
cat > "${TMP}/.github/workflows/other.yml" <<'YML'
name: Demo
on:
  push:
    branches: [master]
jobs:
  healthy:
    name: Healthy door
    runs-on: ubuntu-latest
    steps:
      - name: Report
        run: echo ok
YML
rm -f "${TMP}/.github/workflows/demo.yml"
rc3="$(run_door "${TMP}/g3.log" "${TMP}/fx/jobs.json")"
dead3="$(count_lines '^  · NEVER_RUN_JOB ' "${TMP}/g3.log")"
expect_eq "格3 rc=1" "1" "${rc3}" "run_door 退出码"
expect_eq "格3 三条全计红（守卫未知按红处理）" "3" "${dead3}" "NEVER_RUN_JOB 行数"
expect_has "格3 写明守卫未知" "守卫未知" "${TMP}/g3.log"

echo "──── 格 4（控制）：让 Dead door 真的跑起来 ⇒ 作业级红必须消失 ────"
write_yml "${TMP}/.github/workflows/demo.yml" yes
rm -f "${TMP}/.github/workflows/other.yml"
rc4="$(run_door "${TMP}/g4.log" "${TMP}/fx/jobs_alive.json")"
expect_eq "格4 rc=1（步骤轴的 Report 仍该红）" "1" "${rc4}" "run_door 退出码"
expect_absent "格4 作业级红消失" "· NEVER_RUN_JOB" "${TMP}/g4.log"
expect_has "格4 节奏门仍豁免两条" "Depends on guarded" "${TMP}/g4.log"

echo "──── 格 5：作业**还在跑**时读到的 null 结论，不许印成「从未执行」────"
# 实测形状（2026-09-23 第四十六轮）：对 in_progress 的 run 取 job，未跑完的步骤
# conclusion 是 null ⇒ 这一行印成 `执行 0 … 跳 0  ← 从未执行`，和"出现了 3 次全被跳过"
# 的死门在表里长得一样。判据本身没被骗（present=0 < --min-presence ⇒ 不计红），
# 但人是读表的：把"没数据"说成"没跑过"，下一轮就会有人去修一道好门。
cat > "${TMP}/fx/jobs_inflight.json" <<'JSON'
{"jobs":[
 {"name":"Guarded door","conclusion":"skipped","steps":[]},
 {"name":"Depends on guarded","conclusion":"skipped","steps":[]},
 {"name":"Dead door","conclusion":"skipped","steps":[]},
 {"name":"Healthy door","conclusion":"success","steps":[
   {"name":"Set up job","conclusion":"success"},
   {"name":"Report","conclusion":"skipped"},
   {"name":"In flight","conclusion":null},
   {"name":"Complete job","conclusion":"success"}]}
]}
JSON
write_yml "${TMP}/.github/workflows/demo.yml" yes
rm -f "${TMP}/.github/workflows/other.yml"
rc5="$(run_door "${TMP}/g5.log" "${TMP}/fx/jobs_inflight.json")"
inflight_line="$(grep -F -- 'In flight' "${TMP}/g5.log" | head -1)"
if [[ -z "${inflight_line}" ]]; then
  bad "格5 取证有效（表里有 In flight 这一行）" "输出里根本没有该步骤行，后面的断言全部无意义"
else
  ok "格5 取证有效（表里有 In flight 这一行）"
fi
case "${inflight_line}" in
  *无结论*) ok "格5 null 结论标成「无结论」" ;;
  *) bad "格5 null 结论标成「无结论」" "这一行仍是「${inflight_line}」" ;;
esac
case "${inflight_line}" in
  *从未执行*) bad "格5 不许把 null 说成从未执行" "这一行仍是「${inflight_line}」" ;;
  *) ok "格5 不许把 null 说成从未执行" ;;
esac
expect_eq "格5 rc 仍由真死门决定（红）" "1" "${rc5}" "run_door 退出码"
expect_absent "格5 null 步骤不进 NEVER_RUN 名单" "NEVER_RUN  Demo / Healthy door / In flight" "${TMP}/g5.log"
expect_has "格5 步骤轴对 Report 的判红不退化" "NEVER_RUN  Demo / Healthy door / Report" "${TMP}/g5.log"

echo "──── 格 6：步骤级 if: 的「节奏步骤」不许算 NEVER_RUN────"
# 真实假阳：sbom.yml 的 `Attach SBOM to release (only on tag)`（`if: startsWith(github.ref,
# 'refs/tags/')`）在只统计 push 的窗口里恒被跳过。作业轴早就按 if: 豁免了，步骤轴没有，
# 于是这道门自己产出过一条被文档承认是假阳的红。假阳的门会被人的手关掉，比不判还糟。
write_yml "${TMP}/.github/workflows/demo.yml" yes yes
rc6="$(run_door "${TMP}/g6.log" "${TMP}/fx/jobs.json")"
expect_eq "格6 rc=1（作业级 Dead door 仍该红）" "1" "${rc6}" "run_door 退出码"
expect_eq "格6 步骤轴 NEVER_RUN 归零" "0" "$(count_lines '^  · NEVER_RUN ' "${TMP}/g6.log")" "NEVER_RUN 行数"
expect_eq "格6 节奏步骤恰 1 条" "1" "$(count_lines '^  · 节奏步骤 ' "${TMP}/g6.log")" "节奏步骤行数"
expect_has "格6 节奏步骤点名 Report" "节奏步骤     Demo / Healthy door / Report" "${TMP}/g6.log"
expect_eq "格6 作业级红不受影响（仍 1 条）" "1" "$(count_lines '^  · NEVER_RUN_JOB ' "${TMP}/g6.log")" "NEVER_RUN_JOB 行数"

echo "──── 格 7：只出现 2 次、次次失败的步骤，就是常亮红灯────"
# 判红阈值原来与"至少出现 3 次"共用一个参数，而窗口是按 **run** 截的：本仓 14 个工作流，
# 30 个 run 只有约 2 次 push ⇒ 每个作业出现 2 次 ⇒ 一条 100% 失败的步骤（实测＝Lint 的
# `Run ESLint (errors block...)`，败 2 成 0）从 ALWAYS_RED 名单里静默消失，脚本退 0 说
# "每个步骤都至少成功过一次"。而同一份输出里那一行明明标着「← 从未通过」。
cat > "${TMP}/fx/jobs_run1.json" <<'JSON'
{"jobs":[
 {"name":"Failing door","conclusion":"failure","steps":[
   {"name":"Set up job","conclusion":"success"},
   {"name":"Check","conclusion":"failure"}]}
]}
JSON
cp "${TMP}/fx/jobs_run1.json" "${TMP}/fx/jobs_run2.json"
write_yml "${TMP}/.github/workflows/demo.yml" yes no
rc7="$(run_door "${TMP}/g7.log" "${TMP}/fx/jobs.json")"
expect_eq "格7 rc=1" "1" "${rc7}" "run_door 退出码"
expect_has "格7 两次全败即判 ALWAYS_RED" "ALWAYS_RED Demo / Failing door / Check（执行 2 次，次次失败）" "${TMP}/g7.log"
# 反向：把阈值抬回 3 ⇒ 这条必须消失，证明红是新阈值打出来的，不是无条件打的
rc7b="$(run_door "${TMP}/g7b.log" "${TMP}/fx/jobs.json" --min-red 3)"
expect_absent "格7 反向：--min-red 3 时这条不红" "ALWAYS_RED Demo / Failing door / Check" "${TMP}/g7b.log"
# 上那条豁免断言只在"这条步骤确实进了反向档"时才有意义（表格里的行只有步骤名，
# 作业名在上一行的 [Demo / Failing door] 表头里，别拿组合串去 grep 表体）
expect_has "格7 反向取证有效（反向档的表里有 Failing door 这个作业）" "[Demo / Failing door]" "${TMP}/g7b.log"

echo "════════════════════════════════════"
printf 'PASS=%s FAIL=%s\n' "${PASS}" "${FAIL}"
[[ "${FAIL}" -eq 0 ]] || exit 1
echo "✅ check-ci-step-coverage 的判据两向都有牙"
