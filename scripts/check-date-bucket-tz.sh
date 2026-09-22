#!/usr/bin/env bash
# =============================================================================
# check-date-bucket-tz.sh —— 「日期键/日期窗口必须按业务日口径」守卫
#
# 立项原因（2026-09-20 第二十六轮审计 A8/A9）：本仓所有 PostgreSQL 连接串都把会话
# 时区钉在 Asia/Shanghai（pkg/db/db.go 生产、pkg/testutil 测试库），于是 SQL 侧的
# `DATE(ts)` / `::date` 分桶、以及把 'YYYY-MM-DD' 字符串当参数下推时的解释，一律按
# CST；而 Go 进程自己的 time.Now() 跟宿主机时区（CI runner 与多数镜像是 UTC）。
# 两者在 UTC 16:00–23:59（= CST 次日 00:00–07:59）之间正好差一个日历日，于是：
#   - 写库的日期键落到**昨天**的行上（geo stat_date、daily_card_uv_stats.Date、…）；
#   - `created_at >= 'YYYY-MM-DD'` 的"今日"统计恒漂 8 小时（漏算或多数）；
#   - 日趋势图的刻度标签与 SQL 分桶对不齐，同一天既出现又缺席。
# 这类缺陷**在开发机上永远看不到**（开发机时区恰好等于会话时区），只能靠 CI 或本守卫拦。
#
# 正确写法：internal/pkg/timeutil 的 BusinessDate / BusinessToday /
# ParseBusinessDate / StartOfBusinessDay（口径与 DB 会话时区一致）。
#
# 判据是「棘轮」：命中数只许降不许升。基线单独存文件，收敛一批就把基线改小，
# 新增一行就把门变红 —— 不要求一次性清零，但要求任何回退都被点名。
#
# 用法：bash scripts/check-date-bucket-tz.sh
#   rc=0 通过；rc=1 命中数超过基线（会打印逐条落点）；rc=2 环境/判据本身有问题
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# TARGET 定位与 check-architecture.sh 同口径：按候选顺序取第一个真正含 internal/ 的
# 目录，避免在影子克隆（/tmp/xxx）里退化成「目标目录无效」的假红。
TARGET=""
for _cand in "$SCRIPT_DIR/../user-server" "$SCRIPT_DIR/../../hivemtk/user-server"; do
  if [ -d "$_cand/internal" ]; then
    TARGET="$(cd "$_cand" && pwd)"
    break
  fi
done
[ -n "$TARGET" ] || TARGET="$PROJECT_ROOT/user-server"
if [ ! -d "$TARGET/internal" ]; then
  echo "::error::待检目录无效：${TARGET}（找不到 internal/）"
  exit 2
fi

BASELINE_FILE="$SCRIPT_DIR/date-bucket-tz.baseline"
if [ ! -f "$BASELINE_FILE" ]; then
  echo "::error::缺少基线文件 $BASELINE_FILE —— 没有基线时本门等于零覆盖"
  exit 2
fi
BASELINE="$(tr -dc '0-9' < "$BASELINE_FILE")"
if [ -z "$BASELINE" ]; then
  echo "::error::基线文件里没有数字：$BASELINE_FILE"
  exit 2
fi

# 命中形态：
#   X.Format("2006-01-02")            —— 跟随宿主机时区的日期键/窗口串
#   time.Parse("2006-01-02", …)       —— 返回 UTC 零点，与 CST 会话时区差 8h
# 放行形态（同口径的显式写法，不算违规）：
#   X.In(<时区>).Format("2006-01-02")  —— 转换紧贴 Format，已显式钉过时区
#   X.UTC().Format("2006-01-02")       —— 第三方签名域（如腾讯云 TC3 scope）必须用 UTC
#   注释行（含把反例写进注释的文档）
# 注意：放行判据写成「紧贴」而不是「同一行出现过 .In(」，否则一行代码尾部挂个
# 提到 cstZone 的行内注释就能把自己豁免掉；同理不豁免「行内出现 timeutil.」——
# 正确使用 timeutil 的行根本不含 Format("2006-01-02")，豁免它只会开出绕过口子。
set +e
HITS="$(
  cd "$TARGET"
  grep -rn --include='*.go' -E 'Format\("2006-01-02"\)|time\.Parse\("2006-01-02"' internal cmd \
    | grep -v '_test\.go:' \
    | grep -v 'internal/pkg/timeutil/' \
    | grep -vE '\.In\([^"]*\)\.Format\("2006-01-02"\)|\.UTC\(\)\.Format\("2006-01-02"\)' \
    | awk -F: '
        {
          # 把 file:line:content 的 content 还原出来（content 里可能还有冒号）
          content = $0
          sub("^[^:]*:[0-9]+:", "", content)
          stripped = content
          gsub(/^[ \t]+/, "", stripped)
          if (stripped ~ /^\/\// || stripped ~ /^\*/) next
          print $0
        }
      '
)"
scan_rc=$?
set -e
# grep 的两级退出码必须分开看：1 = 没有命中（正常），>1 = grep 自己出错
# （路径不存在、正则非法）。把两者都当成"零命中"会让门在判据坏掉时恒绿。
if [ "$scan_rc" -gt 1 ]; then
  echo "::error::扫描失败 rc=${scan_rc}（目标：${TARGET}）—— 判据坏掉时宁红不放"
  exit 2
fi

COUNT=0
if [ -n "$HITS" ]; then
  COUNT="$(printf '%s\n' "$HITS" | wc -l | tr -d ' ')"
fi

echo "日期口径命中：$COUNT 处（基线 $BASELINE 处）"

# 零命中而基线非零 ⇒ 大概率是判据本身坏了（目标目录没匹配到、grep 失败、正则被改错），
# 而不是 21 处一夜之间全收敛。gitleaks 那次的教训就是「配置一坏，门恒绿等于零覆盖」，
# 这里宁可报错让人来看一眼。真要清零就手动把基线改成 0，那是一次显式决策。
if [ "$COUNT" -eq 0 ] && [ "$BASELINE" -gt 0 ]; then
  echo "::error::命中数为 0 但基线是 $BASELINE —— 判定前先确认扫描目标与正则仍然有效（${TARGET}）"
  exit 2
fi

if [ "$COUNT" -gt "$BASELINE" ]; then
  echo "::error::宿主机时区日期口径违规增加了 $((COUNT - BASELINE)) 处（基线 $BASELINE → 现值 ${COUNT}）"
  echo "新增/现存落点："
  printf '%s\n' "$HITS" | sed 's/^/  /'
  echo "改法：日期键与日期窗口一律走 internal/pkg/timeutil"
  echo "  （BusinessDate / BusinessToday / ParseBusinessDate / StartOfBusinessDay）"
  exit 1
fi

if [ "$COUNT" -lt "$BASELINE" ]; then
  echo "提示：现值已低于基线，请把 $BASELINE_FILE 改小到 ${COUNT}，把收敛固化下来。"
fi

echo "✅ 日期口径守卫通过（未新增宿主机时区依赖）"
