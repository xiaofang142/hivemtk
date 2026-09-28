#!/usr/bin/env bash
# 全族反向测（`mut-dispose-guard.test.sh` 那条行为面的夹具）：让每一枚带 git-clone 装架的电池
# 在 checkout 那一步当场失败，断言 ① 它确实按"克隆之后中止"的判据红（bail 开火，不是别的分支），
#           ② 交来的 --clone 目录还在、但里面的 clone/ 已被收尾闸带走（不留 70M 残骸）。
#
# 为什么它不注册进任何门／CI：一趟要 26 次真克隆、要改 PATH 挂假 git、JS 那几枚还要本地
# node_modules 才进得了装架——挂进 CI 只会得到一台恒红的机器。它是**手动复跑的取证件**：
# 门注释里那句"克隆之后真的回收了"的读数就是从这一趟来的，改了 bail/dispose/prepare 的写路径
# 之后应当在这里现取一次，而不是只信门的静态面（静态面只证 raises 都写了 bail(...)，不证带得走）。
#
# 用法：bash scripts/probe-fleet-bail-reverse.sh <影子克隆根> <带 .env 的主树根>
#   第一参数得是 <ws>/hivemtk 那一层（里有 scripts/）；第二参数只用来取库口令的值，值不打印。
#   只复跑其中几枚：ONLY="mut_bill_p701.py mut_db_poolcfg.py" bash ... （子集读数不是全族读数，
#   末行会照样写明这一趟跑了多少枚）。
# 退出码：0 全绿｜1 有 FAIL（含计数对不上账）｜2 没有 FAIL 但有 ENV-BROKEN 未取证的枚子。
#
# 两处第一版踩过的坑（都在这版里修掉，别改回去）：
#  - 加 --check 是错的：26 枚里 16 枚没有这个 flag（argparse rc=2，夹具从没进 prepare），
#    另外 4 枚的 --check 明确"只静态预检、不装架"⇒ 同样不走 prepare。必须不带 --check，
#    让流程真进装架；夹具杀了 checkout，所以走不到跑用例那一步，仍然便宜。
#  - 每个判据分支的产物要留在磁盘上：第一版 rm 掉了单枚输出，20 枚红只能猜是"电池漏"
#    还是"探针假红"。这版全部存 LOGDIR，FAIL 当场打印红因。
set -u
WS=${1:?用法：bash scripts/probe-fleet-bail-reverse.sh <影子克隆根> [带 .env 的主树根]}
SB="$WS/scripts"
[ -d "$SB" ] || { echo "找不到 $SB"; exit 1; }

# 控制组不成立的最小前提：库口令给上（值不打印），否则有些电池会先报 ENV-BROKEN，
# 那量到的是环境而不是 bail。影子克隆是 git 克隆、没有 .env（口令在库外），
# 所以要显式指到主树；找不到就出声继续跑——那时未取证的枚子会归进 ENV-BROKEN 那一类，
# 而不是冒充 PASS。
ENVSRC=${2:-}
if [ -z "$ENVSRC" ]; then
  for c in "$WS" "$(dirname "$WS")"; do
    [ -f "$c/.env" ] && { ENVSRC="$c"; break; }
  done
fi
if [ -n "$ENVSRC" ] && [ -f "$ENVSRC/.env" ]; then
  pw=$(awk -F= '/^POSTGRES_PASSWORD=/{print $2; exit}' "$ENVSRC/.env")
  [ -n "$pw" ] || { echo "取不到 $ENVSRC/.env 里的 POSTGRES_PASSWORD 值"; exit 1; }
  export POSTGRES_PASSWORD="$pw" POSTGRES_TEST_PASSWORD="$pw"
  export POSTGRES_TEST_HOST=127.0.0.1 POSTGRES_TEST_PORT=8232
  unset pw
else
  echo "缺 .env（第二个参数指到带 .env 的主树根）⇒ 可能有电池报 ENV-BROKEN 而不是走到夹具"
fi

FAKE=$(mktemp -d /tmp/fakegit.XXXXXX)
REALGIT=$(command -v git)
cat > "$FAKE/git" <<EOF
#!/bin/sh
# 只对 checkout 装死，其余原样转发（转发面必须可证：clone 那一步真的走的是真 git）
if [ "\$1" = "checkout" ]; then
  echo "FAKEGIT: 故意让 checkout 失败（反向测夹具）" >&2
  exit 123
fi
exec $REALGIT "\$@"
EOF
chmod +x "$FAKE/git"

LOGDIR=$(mktemp -d /tmp/fleetbail_logs_XXXXXX)
# ONLY=空格分隔的文件名 ⇒ 子集复跑。
ONLY=${ONLY:-}
[ -n "$ONLY" ] && echo "本趟是子集复跑 ONLY=${ONLY}（全族判读要看不带 ONLY 的那一趟）"
PASS=0; FAILN=0; SKIPN=0; SEEN=0; BLOCKN=0
for f in "$SB"/mut_*.py; do
  name=$(basename "$f")
  [ "$name" = "mut_dispose.py" ] && continue
  grep -q '"git", "clone"' "$f" || { SKIPN=$((SKIPN + 1)); continue; }
  if [ -n "$ONLY" ]; then
    case " $ONLY " in
      *" $name "*) : ;;
      *) continue ;;
    esac
  fi
  SEEN=$((SEEN + 1))
  # 夹具前提：这一枚真的走 clone(--no-checkout) → checkout 两步，且装了 bail。
  # 前提不成立就 SKIP 并写明缺哪一面，不许让它以"红因不是 checkout"混进 FAIL。
  pre=""
  grep -q -- '"--no-checkout"' "$f" || pre="${pre}no-checkout "
  grep -q '\["git", "checkout"' "$f" || pre="${pre}checkout-call "
  grep -q 'def bail(' "$f" || pre="${pre}bail-def "
  if [ -n "$pre" ]; then
    echo "  ⚠ ${name}：夹具前提不成立（缺 ${pre}）⇒ 本枚未取证"
    SKIPN=$((SKIPN + 1)); continue
  fi
  d=$(mktemp -d /tmp/fleetbail.XXXXXX)
  # mut_review_r22_teeth 的默认 --logs 目录在 HEAD 里就带 10 份已跟踪 .log ⇒ 任何干净克隆
  # 里不带参数跑都会先撞"复用旧目录"这条判据（它在 prepare 之前），量不到夹具。给它新目录。
  # 用普通字符串而非数组：本机 /bin/bash 是 3.2，set -u 下展开空数组会直接报 unbound。
  xlog=""
  case "$name" in
    mut_review_r22_teeth.py) xlog="$LOGDIR/r22fresh" ;;
  esac
  if [ -n "$xlog" ]; then
    PATH="$FAKE:$PATH" python3 "$f" --clone "$d" --logs "$xlog" > "$LOGDIR/$name.log" 2>&1
  else
    PATH="$FAKE:$PATH" python3 "$f" --clone "$d" > "$LOGDIR/$name.log" 2>&1
  fi
  rc=$?
  hit=$(grep -c "checkout 失败\|FAKEGIT" "$LOGDIR/$name.log" || true)
  inner=$(ls -A "$d" 2>/dev/null | tr '\n' ' ')
  # 允许的残骸按 mut_dispose.py 的契约判，不是一律"外层必须空"：
  #   · clone/ 必须被带走（那是电池自己 git clone 出来的，唯一现场证据是 .git）；
  #   · JS 装架（有 `dst / "web"` 面的那几枚）留下的 web/ 是**契约里写明不许回收**的——
  #     它把调用方自己的 node_modules 拷/链了进来，rmtree 会顺着走到别人的依赖树上去
  #     （见 mut_actionability_b17.py 里 js_prepare 那段"为什么不能顺手 rmtree(work)"）。
  #   所以 JS 枚的外层只允许恰好一个 web/，且体积有上界（拷贝跑偏要露出来）。
  webmb=0
  [ -d "$d/web" ] && webmb=$(du -m -s "$d/web" 2>/dev/null | awk 'NR==1{print $1}')
  if grep -q 'dst / "web"' "$f"; then
    expect_inner="web "
  else
    expect_inner=""
  fi
  if [ "$rc" = 0 ]; then
    echo "  ✗ ${name}：夹具没让它退（rc=0）——bail 那条路根本没走到"
    sed -n '1,3p' "$LOGDIR/$name.log" | sed 's/^/      /'; FAILN=$((FAILN + 1))
  elif [ "$hit" = 0 ]; then
    envb=$(grep -c '^ENV-BROKEN\|ENV-BROKEN：' "$LOGDIR/$name.log" || true)
    if [ "$rc" != 0 ] && [ "$envb" != 0 ] && [ "$inner" = "$expect_inner" ]; then
      # 这一枚被自己的环境前提闸拦在装架之前（判据写在 prepare 以前），夹具根本没进门。
      # 归 ENV-BROKEN 单列，不并进 PASS（它没被证明），也不并进 FAIL（没有证据说它漏）——
      # 与电池"环境前提不满足退 ENV-BROKEN 并不印全杀"同一条口径。
      echo "  ⛔ ${name}：装架前被自身环境前提拦下 ⇒ 本枚未取证"
      grep -m1 'ENV-BROKEN' "$LOGDIR/$name.log" | sed 's/^/      /'; BLOCKN=$((BLOCKN + 1))
    else
      echo "  ✗ ${name}：退了，但红因不是 checkout（判据分支没对上）"
      tail -3 "$LOGDIR/$name.log" | sed 's/^/      /'
      echo "      外层剩「${inner}」（契约允许「${expect_inner}」）"
      du -sh "$d"/* 2>/dev/null | sed 's/^/      /'; FAILN=$((FAILN + 1))
    fi
  elif [ ! -d "$d" ]; then
    echo "  ✗ ${name}：--clone 交来的外层目录被删了（越权）"; FAILN=$((FAILN + 1))
  elif [ "$inner" != "$expect_inner" ]; then
    echo "  ✗ ${name}：中止后外层剩「${inner}」，契约允许「${expect_inner}」"
    du -sh "$d"/* 2>/dev/null | sed 's/^/      /'; FAILN=$((FAILN + 1))
  elif [ "$expect_inner" = "web " ] && [ "$webmb" -gt 5 ]; then
    echo "  ✗ ${name}：web/ 超过 5 MB（拷贝跑偏了，不是正常残留）"
    du -sh "$d/web" | sed 's/^/      /'; FAILN=$((FAILN + 1))
  else
    echo "  ✓ ${name}：bail 开火、外层在、clone/ 已回收"
    PASS=$((PASS + 1)); rm -f "$LOGDIR/$name.log"
  fi
  rm -rf "$d"
done
echo
echo "判读：本趟入夹具 SEEN=${SEEN} 枚｜PASS=${PASS} FAIL=${FAILN} ENV-BROKEN 未取证=${BLOCKN} 夹具前提不成立/无 git-clone 面而跳过=${SKIPN}"
echo "红因留档：${LOGDIR}（每份＝对应枚「不带 --check、带 --clone 空目录」的完整 stdout+stderr）"
rm -rf "$FAKE"
# 计数自证：进了夹具的每一枚必须落在三类里之一，缺项＝有一枚既没绿也没红地掉了（假绿形状）。
if [ $((PASS + FAILN + BLOCKN)) != "$SEEN" ]; then
  echo "判据坏了：SEEN=${SEEN} 但对账 PASS+FAIL+BLOCK=$((PASS + FAILN + BLOCKN))——有枚子没归类"
  exit 1
fi
# ENV-BROKEN 让整趟退 2：它不是"验过了"，也不该被读成"漏了"。
if [ "$FAILN" != 0 ]; then exit 1; fi
if [ "$BLOCKN" != 0 ]; then exit 2; fi
exit 0
