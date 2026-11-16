#!/usr/bin/env bash
# =============================================================
# check-secrets-patterns.test.sh —— 证明 scripts/check-secrets.sh 的 B 段「能红」
#
# 立项原因：这道门自登记以来只有"作者手动跑"一个入口，而手动跑的
# 代价是没人能说清它到底有没有牙。本档第一轮就撞出一枚实锤：B 段粗筛写的是
# `PG?PASSWORD`（读作 "P + 可选 G + PASSWORD"，实际只能匹配 PGPASSWORD/PPASSWORD），
# 于是最常见的 `POSTGRES_PASSWORD=<字面量>` 整类不进粗筛 —— 一枚合成假凭证在"绿"的门里
# 大摇大摆退 0。是这一族的反向格把它逼出来的，不是通读代码。
#
# 十三格，逐格断言的是"哪条判据分支该开火"，不是只看终值：
#   L1 正控制      干净树 --patterns-only ⇒ rc=0，且闸自己报的"候选文件 N 个"必须 N>0
#                  （枚举面为空时"零命中"是没扫到，不是干净）
#   L2 参数解析    未知参数 ⇒ rc=2，且出声点名那个参数（不能静默当 --verbose 吞掉）
#   L3 前提缺席    full 模式 + ENV_FILE 指向不存在 ⇒ rc=2 而不是 rc=1（"没跑过"与"有泄露"分开）
#   L4 夹具入场    种入合成假凭证 ⇒ rc=1，**且红因点名到探针文件**（退 1 但没说在哪＝红因丢了）
#                  同一格里先证探针真落盘、真没被 .gitignore 吃掉、真在 ls-files 清单里
#   L5 摘除复绿    删掉探针 ⇒ rc=0，且文件确实不在（rm -f 退 0 不证明删对了）
#   L6 豁免有牙    SECRET_ALLOWLIST_EXTRA 追加一条匹配探针的豁免 ⇒ rc=0；
#                  换成一条不匹配的 ⇒ 仍退 1 且点名探针（极性对，缺一半就是橡皮章）。仓内那张豁免表
#                  全程不被改写：追加语义既保住既有豁免，也不占共享工作树里的已跟踪文件
#   L7 A 段不空跑  本机仓根有 .env ⇒ full 模式必须印出"已载入 N 枚真值"且 N≥1；
#                  没有 .env（CI 的形状）⇒ 这一格打 SKIP 且**不许印绿**
#   L8 判据同源    scripts/check-secrets.sh 与 scripts/check-secrets-workspace.sh 的 B 段
#                  PATTERN 必须逐字一致；极性对＝把主闸那一格改坏后必须判出"不同"
#   L9 红因遮码    种入探针后 rc=1 且输出里有文件名，但**没有值本体**；极性对＝同一趟必须
#                  印出非空输出，否则"值没出现"是空输出而不是掩码（量的是 B 段那处 echo）
#   L10 A 段同遮   合成 ENV_FILE + 含该值的探针文件 ⇒ A 段 rc=1、点名文件、值本体不出屏
#                  （两处 echo 各自有一格，防止"改一处漏一处"的半修）
#   L11 B+裸值红   种入「名字含 secret 的点文件 + 单行 48 位 hex」⇒ rc=1 且点名探针
#                  （B 段对裸值文件天然失明 —— 2026-10-18 .merchant_api_secret.bak 事故就是
#                  这个形状进的仓；本格证明 B+ 段的嫌疑名单+整行精判有牙）
#   L12 B+同遮码   同一趟 B+ 红因里不得出现 hex 本体（与 L9 同口径，量的就是 B+ 那处 echo）
#   L13 B+不误伤   摘除裸值探针 ⇒ 复绿；再种「可疑名字 + 模板展开内容」⇒ 仍绿
#                  （嫌疑名单只圈文件名形状，内容精判放过 ${VAR} 形态——gitignored 的
#                  本地合法密钥根本进不了候选面，这里测的是形状内误报那一维）
#
# 夹具全程是**仓根未跟踪文件**，走 rm -f / trap 清理，不碰 git add / checkout / restore / stash
# —— 共享工作树里有别的泳道在途改动，任何 git 侧操作都可能把它们一起吞掉。
# 代价说明：跑这几秒里如果有人 push，pre-push 密钥闸会把探针当明文拦下；摘掉即复绿，
# 文件名带 secret-patterns-probe 字样，好让下一位一眼认领。
#
# 用法： bash scripts/check-secrets-patterns.test.sh
# 退出码：0=十格全过；1=至少一格断言失败（逐格打印红因）；2=装架前提坏了（不在仓内、找不到闸）
# =============================================================
set -uo pipefail

ROOT=$(git rev-parse --show-toplevel 2>/dev/null || true)
if [[ -z $ROOT ]]; then
  echo "BROKEN: 不在 git 仓内，本反向测试没有可写夹具的树" >&2
  exit 2
fi
cd "$ROOT" || exit 2

GATE=scripts/check-secrets.sh
if [[ ! -f $GATE ]]; then
  echo "BROKEN: 找不到被测闸 ${GATE}（cwd=$(pwd)）" >&2
  exit 2
fi

# 合成假凭证：值是本文件里写死的哑串，不对应任何系统、不是任何真口令的片段。
# 选形要点：不含 NEGATE 里的占位词（change_me/example/test/...）、长度≥16、字母与数字都有，
# 否则"门没牙"与"夹具不满足判据前置"这两种红分不开。
PROBE_LINE='PROBE_PASSWORD = Sup3rcalifragilistic2026'
PROBE=".tmp-secret-patterns-probe-$$"
SYNTH_ALLOW=$(mktemp)
SYNTH_ALLOW_NEG=$(mktemp)
FAILS=0
RAN=0

cleanup() {
  rm -f "$PROBE" "$SYNTH_ALLOW" "$SYNTH_ALLOW_NEG" "${SYNTH_ENV:-}"
}
trap cleanup EXIT

ok() {
  RAN=$((RAN + 1))
  echo "  ✓ $1"
}
bad() { # $1=这格断言了什么 $2=红因（必须传：只印标题＝把红因扔掉）
  RAN=$((RAN + 1))
  FAILS=$((FAILS + 1))
  echo "  ❌ $1"
  [[ -n ${2:-} ]] && echo "     红因：$2"
  return 0
}

run_gate() { # $1=额外环境变量赋值（可空，形如 "ENV_FILE=/x"，值里不许有空格） $2…=传给闸的参数
  local assign=$1
  shift
  if [[ -z $assign ]]; then
    env bash "$GATE" "$@" </dev/null
  else
    # shellcheck disable=SC2086  # assign 就是"KEY=value"赋值串，刻意按空格拆成多枚
    env $assign bash "$GATE" "$@" </dev/null
  fi
}

echo "══════ scripts/check-secrets.sh B 段反向测试 ══════"

# ── L1 正控制：干净树绿，且枚举面非空 ──────────────────────────
out=$(run_gate "" --patterns-only 2>&1); rc=$?
n=$(printf '%s\n' "$out" | awk '/候选文件 [0-9]+ 个/ {print $2; exit}')
if [[ $rc != 0 ]]; then
  bad "L1 干净树 --patterns-only 应绿" "$(printf '%s' "$out" | tail -3 | tr '\n' ' ')"
elif [[ -z $n || $n -le 0 ]]; then
  bad "L1 闸没报出候选文件数（或数为 0）" "零命中可能只是没扫到；读数行：$(printf '%s' "$out" | grep -c . ) 行"
else
  ok "L1 干净树绿，扫描面非空（候选文件 ${n} 个）"
fi

# ── L2 未知参数必须退 2 并点名 ────────────────────────────────
out=$(run_gate "" --definitely-not-a-flag 2>&1); rc=$?
if [[ $rc != 2 ]]; then
  bad "L2 未知参数应退 2（装架参数解析没牙）" "实退 ${rc}，输出：$(printf '%s' "$out" | head -1)"
elif ! printf '%s' "$out" | grep -q -- '--definitely-not-a-flag'; then
  bad "L2 退了 2 却没点名是哪个参数" "输出：$out"
else
  ok "L2 未知参数退 2 且点名"
fi

# ── L3 缺 .env 的 full 模式：2 而不是 1 ───────────────────────
out=$(run_gate "ENV_FILE=$ROOT/.definitely-no-such-env-file" 2>&1); rc=$?
if [[ $rc != 2 ]]; then
  bad "L3 缺 .env 时 full 模式应退 2（没跑过）" "实退 $rc ⇒ 会把环境坏读成泄露或绿；输出：$(printf '%s' "$out" | head -2 | tr '\n' ' ')"
else
  ok "L3 缺 .env 时 full 模式明确退 2"
fi

# ── L4 种入合成假凭证：必须红，且红因点名探针 ─────────────────
printf '%s\n' "$PROBE_LINE" >"$PROBE"
if [[ ! -f $PROBE ]] || ! grep -qF -- "$PROBE_LINE" "$PROBE"; then
  bad "L4 夹具没落盘" "$PROBE 读不回那一行 ⇒ 后面任何读数都与探针无关"
else
  if git check-ignore -q "$PROBE"; then
    bad "L4 探针被 .gitignore 吃掉" "它进不了 ls-files 的扫描面 ⇒ '红'是别处来的、'绿'也没意义"
  else
    #  membership 判定用 grep -c 而不是 grep -q：本档开着 pipefail，`git ls-files | grep -q`
    #  里 grep 一命中就收摊，git 收到 SIGPIPE 退 141 ⇒ 管道整体非 0 ⇒ "在清单里"被读成"不在"。
    in_list=$(git ls-files --cached --others --exclude-standard | grep -cxF "$PROBE" || true)
    if [[ $in_list -lt 1 ]]; then
      bad "L4 探针不在待纳管清单里" "门压根没扫到它（清单精确匹配=${in_list}）"
    else
      out=$(run_gate "" --patterns-only 2>&1); rc=$?
      named=$(printf '%s\n' "$out" | grep -cF "$PROBE" || true)
      if [[ $rc != 1 ]]; then
        bad "L4 种入假凭证后应退 1" "实退 $rc ⇒ B 段判据没牙（PG?PASSWORD 那一类瞎法就长这样）"
      elif [[ $named -lt 1 ]]; then
        bad "L4 退了 1 但没点名探针文件" "红因丢失，下一位无从复算：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
      else
        ok "L4 假凭证让门变红，且红因点名到 ${PROBE}（命中 ${named} 行）"
      fi
    fi
  fi
fi

# ── L5 摘除后复绿 ────────────────────────────────────────────
rm -f "$PROBE"
if [[ -e $PROBE ]]; then
  bad "L5 探针没摘掉" "$PROBE 仍在 ⇒ 残迹会污染下一轮基线"
else
  out=$(run_gate "" --patterns-only 2>&1); rc=$?
  if [[ $rc != 0 ]]; then
    bad "L5 摘除探针后应复绿" "实退 ${rc}，剩余命中：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
  else
    ok "L5 摘除探针后复绿（夹具无残迹）"
  fi
fi

# ── L6 豁免表这条通路真的有在被读（极性对：加上就放行、换个不匹配的仍然红）──
# 追加表走 SECRET_ALLOWLIST_EXTRA：仓内那张**继续生效**，这里只加一条。整张替换会把仓内既有
# 豁免一起抹掉，那时测出来的红因就不是它声称的那一条（本档第一轮就是这么红的）。
printf '%s\n' ".*${PROBE}.*PROBE_PASSWORD.*Sup3rcalifragilistic2026" >"$SYNTH_ALLOW"
printf '%s\n' '__no_such_path_zzz.*PROBE_PASSWORD.*Sup3rcalifragilistic2026' >"$SYNTH_ALLOW_NEG"
printf '%s\n' "$PROBE_LINE" >"$PROBE"
out=$(run_gate "SECRET_ALLOWLIST_EXTRA=$SYNTH_ALLOW" --patterns-only 2>&1); rc=$?
if [[ $rc != 0 ]]; then
  bad "L6 追加豁免后应放行（rc=0）" "实退 $rc ⇒ 追加表根本没被 consult：$(printf '%s' "$out" | grep '❌' | head -1)"
else
  out=$(run_gate "SECRET_ALLOWLIST_EXTRA=$SYNTH_ALLOW_NEG" --patterns-only 2>&1); rc2=$?
  named=$(printf '%s\n' "$out" | grep -cF "$PROBE" || true)
  if [[ $rc2 != 1 ]]; then
    bad "L6 反向半边：换成不匹配的豁免表时应退 1" "实退 $rc2 ⇒ L6 的绿可能只是门一直绿"
  elif [[ $named -lt 1 ]]; then
    bad "L6 反向半边退了 1 却没点名探针" "红因不是这格放下去的那枚夹具：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
  else
    ok "L6 同一枚夹具：追加匹配豁免⇒绿，追加不匹配豁免⇒红并点名探针（极性对上了）"
  fi
fi
rm -f "$PROBE"
[[ -e $PROBE ]] && bad "L6 收尾：探针残留" "$PROBE 还在树上"

# ── L7 A 段在本机不空跑 ──────────────────────────────────────
if [[ -f $ROOT/.env ]]; then
  out=$(run_gate "" --verbose 2>&1); rc=$?
  nv=$(printf '%s\n' "$out" | awk '/已载入 [0-9]+ 枚真值/ {print $2; exit}')
  if [[ $rc != 0 ]]; then
    bad "L7 本机 full 模式应绿（树里若真有明文另说）" "实退 ${rc}：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
  elif [[ -z $nv || $nv -lt 1 ]]; then
    bad "L7 A 段没比任何真值" "输出里没有「已载入 N 枚真值」或 N<1 ⇒ 这一维没跑过，rc=0 是假绿"
  else
    ok "L7 A 段实比 ${nv} 枚本机真值（非空跑）"
  fi
else
  echo "  SKIP L7：本机仓根无 .env（CI 的形状）⇒ A 段这一维没有读数，别把上面的绿引成'真值也比过'"
fi

# ── L8 同族两份 B 段判据逐字同源 ─────────────────────────────
# 为什么钉这条：B 段那个 PG?PASSWORD 瞎法同时存在于 scripts/check-secrets.sh 与
# scripts/check-secrets-workspace.sh（后者扫的是不进任何 git 仓的顶层目录，正是 2026-09-19
# 一次性揪出 6 个明文凭证文件的那一层）。只修一份 = 另一半继续瞎，而且它更隐蔽：
# 工作区门手动跑起来也"绿"。判据是复制粘贴的，漂移就是默认结局，所以每次跑都逐字比一次。
PAT_MAIN=$(awk '/^PATTERN=/{print; exit}' "$GATE")
PAT_WS=$(awk '/^PATTERN=/{print; exit}' scripts/check-secrets-workspace.sh)
if [[ -z $PAT_MAIN || -z $PAT_WS ]]; then
  bad "L8 没能从两份脚本里各取到 PATTERN 行" "主闸命中=${#PAT_MAIN} 字符、工作区闸=${#PAT_WS} 字符 ⇒ 提取式随赋值语句形状而失效，比对本身没跑"
elif [[ "$PAT_MAIN" == "$PAT_WS" ]]; then
  # 极性对：同一套提取式在"刻意改坏一格"的副本上必须判出不同，否则这条绿只是"两次都取空"
  sed 's/PASSW\[A-Z\]\*/PASSW/' "$GATE" >"${GATE}.l8mut"
  PAT_MUT=$(awk '/^PATTERN=/{print; exit}' "${GATE}.l8mut")
  rm -f "${GATE}.l8mut"
  if [[ -z $PAT_MUT || "$PAT_MUT" == "$PAT_WS" ]]; then
    bad "L8 反向半边：改坏主闸 PATTERN 后仍与工作区闸相同" "提取式没有牙（PAT_MUT=${PAT_MUT}）"
  else
    ok "L8 两份 B 段判据逐字一致，且改坏一格就能判出不同（比对有牙）"
  fi
else
  bad "L8 两份 B 段判据已漂移" "主闸：${PAT_MAIN} ／ 工作区闸：${PAT_WS}"
fi

# ── L9 红因不吐凭证本体 ──────────────────────────────────────
# 这道门的输出会落进终端回滚、CI 作业日志和本仓 logs/ 取证日志。命中项按定义就是"疑似真凭证"，
# 直出原文等于把它从 .env 抄进三个更公开的位置（本仓还有把门禁读数归档入库的习惯）。
# 断言的是"值不出现在 stdout"而不是"格式变短了"——后者会随措辞漂移。
printf '%s\n' "$PROBE_LINE" >"$PROBE"
out=$(run_gate "" --verbose --patterns-only 2>&1); rc=$?
leaked=$(printf '%s\n' "$out" | grep -cF 'Sup3rcalifragilistic2026' || true)
named=$(printf '%s\n' "$out" | grep -cF "$PROBE" || true)
printed=$(printf '%s\n' "$out" | grep -c . || true)
if [[ $rc != 1 ]]; then
  bad "L9 夹具没被 B 段抓住" "实退 $rc ⇒ 这一格量的不是掩码，而是压根没命中"
elif [[ $printed -lt 1 ]]; then
  bad "L9 闸没有任何输出" "零输出时'值没出现'是空话，不是掩码生效"
elif [[ $named -lt 1 ]]; then
  bad "L9 红因没点名探针文件" "掩码不该把位置也一起遮掉：$(printf '%s' "$out" | grep '❌' | head -1)"
elif [[ $leaked -gt 0 ]]; then
  bad "L9 红因里出现了凭证字面量本体" "命中 ${leaked} 行 ⇒ 命中行被原文打印；见脚本头部「红因口径」"
else
  ok "L9 红因只报位置＋值长，探针本体不在输出里（点名 ${named} 行、遮码 ${leaked} 处）"
fi
rm -f "$PROBE"
[[ -e $PROBE ]] && bad "L9 收尾：探针残留" "$PROBE 还在树上"

# ── L10 A 段同样不吐值本体（合成 ENV_FILE，全程不读本机 .env）──
# L9 只量到 B 段那一处 echo；A 段的打印点是另一处，"改一处漏一处"是本族最常见的半修形态。
# 夹具用合成 .env（值是本文件里编的哑串）⇒ 既证 A 段掩码生效，又不必把任何真凭证写进树上。
SYNTH_ENV=$(mktemp)
printf 'PROBE_API_KEY=SyntheticAValue2026NotReal\n' >"$SYNTH_ENV"
printf 'PROBE_API_KEY=SyntheticAValue2026NotReal\n' >"$PROBE"
out=$(run_gate "ENV_FILE=$SYNTH_ENV" 2>&1); rc=$?
leaked=$(printf '%s\n' "$out" | grep -cF 'SyntheticAValue2026NotReal' || true)
named=$(printf '%s\n' "$out" | grep -cF "$PROBE" || true)
if [[ $rc != 1 ]]; then
  bad "L10 合成真值没被 A 段抓住" "实退 $rc ⇒ 夹具不满足 A 段前置（键名要过 CRED_KEY_RE、值长≥8），这一格量不到东西"
elif [[ $named -lt 1 ]]; then
  bad "L10 A 段退了 1 却没点名探针文件" "红因：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
elif [[ $leaked -gt 0 ]]; then
  bad "L10 A 段红因里出现了值本体" "命中 ${leaked} 行 ⇒ B 段遮了、A 段没遮"
else
  ok "L10 A 段红因同样只报位置＋值长（点名 ${named} 行、泄漏 ${leaked} 处）"
fi
rm -f "$PROBE" "$SYNTH_ENV"
[[ -e $PROBE || -e $SYNTH_ENV ]] && bad "L10 收尾：夹具残留" "PROBE=${PROBE} SYNTH_ENV=${SYNTH_ENV}"

# ── L11 B+ 段：裸值文件（无键名单行 hex）必须能红 ──────────────
# 合成值：48 位 hex 哑串（纯编造，不对应任何系统、不是任何真凭证的片段）。
# 探针名以 .tmp-bare-secret 开头 → 命中 B+ 的嫌疑名单形状（点文件名含 secret）。
BARE_PROBE=".tmp-bare-secret-probe-$$"
BARE_HEX='0011223344556677aabbccddeeff0011223344556677aabb'
printf '%s\n' "$BARE_HEX" >"$BARE_PROBE"
out=$(run_gate "" --patterns-only 2>&1); rc=$?
named=$(printf '%s\n' "$out" | grep -cF "$BARE_PROBE" || true)
if [[ $rc != 1 ]]; then
  bad "L11 裸值文件应被 B+ 段抓住（rc=1）" "实退 $rc ⇒ B 段的键名形态盲区在 B+ 段依旧存在（.bak 事故同形状）"
elif [[ $named -lt 1 ]]; then
  bad "L11 B+ 退了 1 却没点名裸值探针" "红因：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
else
  ok "L11 裸值文件被 B+ 抓红并点名 ${BARE_PROBE}"
fi

# ── L12 B+ 红因同样遮码 ──────────────────────────────────────
leaked=$(printf '%s\n' "$out" | grep -cF "$BARE_HEX" || true)
if [[ $rc != 1 ]]; then
  bad "L12 这一格量不到遮码（L11 已红，此处引用同一趟输出）" "见 L11 红因"
elif [[ $leaked -gt 0 ]]; then
  bad "L12 B+ 红因里出现了裸值本体" "命中 ${leaked} 行 ⇒ B+ 的 echo 没走「只报位置＋值长」口径"
else
  ok "L12 B+ 红因只报位置＋值长，hex 本体不在输出里"
fi
rm -f "$BARE_PROBE"

# ── L13 B+ 不误伤：摘除复绿 + 可疑名字的正常内容仍绿 ───────────
if [[ -e $BARE_PROBE ]]; then
  bad "L13 收尾：裸值探针残留" "$BARE_PROBE 还在树上 ⇒ 残迹会污染下一轮基线"
else
  out=$(run_gate "" --patterns-only 2>&1); rc=$?
  if [[ $rc != 0 ]]; then
    bad "L13 摘除裸值探针后应复绿" "实退 ${rc}：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
  else
    ok "L13 摘除裸值探针后复绿"
  fi
fi
# 嫌疑名单形状内、内容是模板展开 ⇒ B+ 精判必须放行（形状≠内容，不冤枉正常文件）。
BARE_OK=".tmp-bare-secret-template-probe-$$"
printf 'MERCHANT_API_SECRET=${MERCHANT_API_SECRET}\n' >"$BARE_OK"
out=$(run_gate "" --patterns-only 2>&1); rc=$?
if [[ $rc != 0 ]]; then
  bad "L13 可疑名字+模板内容应放行" "实退 $rc ⇒ B+ 精判没放过 ${VAR} 形态：$(printf '%s' "$out" | grep '❌' | head -2 | tr '\n' ' ')"
else
  ok "L13 可疑名字 + 模板展开内容不误报（形状≠内容）"
fi
rm -f "$BARE_OK"
[[ -e $BARE_OK ]] && bad "L13 收尾：模板探针残留" "$BARE_OK 还在树上"

echo "══════════════════════════════════════════════════════"
if [[ $FAILS != 0 ]]; then
  echo "❌ 反向测试：${RAN} 格里 ${FAILS} 处断言失败"
  exit 1
fi
echo "✅ 反向测试：${RAN} 处断言全过（L7 若为 SKIP，上面已明说）"
