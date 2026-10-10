#!/usr/bin/env bash
# check-config-param-readpoints.py 的用例：全在临时目录里造树，不碰真仓库、不要数据库。
#
# 为什么单独有一份用例：这道门的判据是"文案里那句「未接线」"与"代码里那行 Get*(…)"两边对账，
# 任何一边写歪都会把 114 条参数中心条目判成假绿或假红（比如把只被测试引用的键当成生产读取点，
# 于是"改了不生效"重新回到运维面前）。所以每格都同时断言 rc **与**"哪一句判据开火"——
# 只判 rc 的话，缺文件退 2 与判据不成立退 1 会被混成一格。
#
# 格子：
#   F1 齐全 ⇒ 绿（正控制：证明夹具确实满足每一格的前置，否则后面的红可能是假阳）
#   F2 没读取点也没挂「未接线」 ⇒ 红，UNDECLARED 点名那个 key
#   F3 有读取点但仍挂着「未接线」 ⇒ 红，STALE 点名那个 key
#   F4 整条挤成一行（Name 与 Description 同行） ⇒ 仍要枚举到 ⇒ 绿。真仓库里 `38ea7489` 那两条
#      lead 条目就是这个形状：旧枚举源只认"Name 之后紧跟换行 + Description"，把它们当成不存在，
#      于是那两条的接线状态从没人核过（本格守的是"看不见 ≠ 没问题"）
#   F4b 条目头真的漂走（Key 挪到 Name 之后，`Key:` 行照旧数得到） ⇒ rc=2，说对账失败
#   F5 种子文件不在 ⇒ rc=2，不许把"没对象"印成"没问题"
#   F6 读取点走常量（IDENT = "key" ＋ Get*(…, IDENT, …)） ⇒ 绿（证明门认得这条读取路径）
#   F7 读取点只出现在 *_test.go ⇒ 红，UNDECLARED 点名（测试引用不算生产读取点）
#   REAL 真仓库 ⇒ 必须绿
set -u

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CHECKER="$ROOT/scripts/check-config-param-readpoints.py"
WORK=$(mktemp -d /tmp/config-param-readpoints-test.XXXXXX)
FAIL=0
N=0
trap 'rm -rf "$WORK"' EXIT

ok() {
  echo "  ✓ $*"
  N=$((N + 1))
}
bad() {
  echo "  ✗ $*"
  N=$((N + 1))
  FAIL=$((FAIL + 1))
}

if [ ! -f "$CHECKER" ]; then
  echo "FATAL: 找不到 $CHECKER"
  exit 1
fi
PYBIN="${PYTHON:-}"
if [ -z "$PYBIN" ]; then
  if command -v python3 >/dev/null 2>&1; then
    PYBIN=python3
  else
    echo "FATAL: 没有 python3，本用例无法执行（不作绿灯判定）"
    exit 2
  fi
fi

# seed_tree <目录>：造一棵最小树——种子目录两条条目（一条接线、一条未接线且已声明）
# ＋一份生产 Go 文件（把 wired_threshold 真读走）。
seed_tree() {
  d="$1"
  mkdir -p "$d/user-server/internal/service" "$d/user-server/internal/bridge"
  cat >"$d/user-server/internal/service/config_param_seeds.go" <<'GO'
package service

type ParamDef struct {
	Group, Key, Name, Description, ValueType, DefaultValue string
}

func DefaultParamDefs() []ParamDef {
	return []ParamDef{
		{Group: "demo", Key: "wired_threshold", Name: "演示阈值",
			Description: "演示用：生产代码真会读它",
			ValueType:   "int", DefaultValue: "5"},
		{Group: "demo", Key: "sleepy_ttl", Name: "演示 TTL（未接线）",
			Description: "【当前不生效，改了也没人读】演示用：没有任何读取点",
			ValueType:   "duration", DefaultValue: "60"},
	}
}
GO
  cat >"$d/user-server/internal/bridge/handler.go" <<'GO'
package bridge

func readThresh(svc interface{ GetInt(a, b, c string) int }) int {
	return svc.GetInt("demo", "wired_threshold", 5)
}
GO
}

run() { $PYBIN "$CHECKER" --repo "$1" 2>&1; }

# ── F1 齐全 ⇒ 绿 ──
T=$WORK/f1
seed_tree "$T"
OUT=$(run "$T")
RC=$?
if [ "$RC" = 0 ] && printf '%s\n' "$OUT" | grep -q '有读取点且未挂标注=1  未接线且已声明=1  未接线但未声明=0  声明过期=0'; then
  ok "F1 齐全夹具绿，且四个读数分别是 1/1/0/0"
else
  bad "F1 齐全夹具应绿且读数 1/1/0/0，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── F2 没读取点又没挂未接线 ⇒ 红，点名 sleepy_ttl ──
T=$WORK/f2
seed_tree "$T"
perl -0pi -e 's/演示 TTL（未接线）/演示 TTL/' "$T/user-server/internal/service/config_param_seeds.go"
perl -0pi -e 's/【当前不生效，改了也没人读】//' "$T/user-server/internal/service/config_param_seeds.go"
OUT=$(run "$T")
RC=$?
if [ "$RC" = 1 ] && printf '%s\n' "$OUT" | grep -q 'UNDECLARED: demo\.sleepy_ttl'; then
  ok "F2 撤掉标注后红在 UNDECLARED，且点名 demo.sleepy_ttl"
else
  bad "F2 应 rc=1 且点名 UNDECLARED: demo.sleepy_ttl，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── F3 接线了仍挂未接线 ⇒ 红，STALE 点名 ──
T=$WORK/f3
seed_tree "$T"
cat >>"$T/user-server/internal/bridge/handler.go" <<'GO'

func readTTL(svc interface{ GetDuration(a, b, c string) int }) int {
	return svc.GetDuration("demo", "sleepy_ttl", 60)
}
GO
OUT=$(run "$T")
RC=$?
if [ "$RC" = 1 ] && printf '%s\n' "$OUT" | grep -q 'STALE: demo\.sleepy_ttl'; then
  ok "F3 接线后不撤标注，红在 STALE 并点名 demo.sleepy_ttl"
else
  bad "F3 应 rc=1 且点名 STALE: demo.sleepy_ttl，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── F4 整条挤成一行 ⇒ 仍被枚举 ⇒ 绿 ──
# 这一格断的是"枚举源对形状免疫"：条目压成一行是运维侧真发生过的写法（`38ea7489`），
# 而门一旦看不见某条，那条的"改了没人读"就永远不会被报出来 —— 报不出来与全绿在输出上一样。
T=$WORK/f4
seed_tree "$T"
perl -0pi -e 's/\n\s*Description: "演示用：生产代码真会读它",/ Description: "演示用：生产代码真会读它",/' \
  "$T/user-server/internal/service/config_param_seeds.go"
if grep -q '^[[:space:]]*Description: "演示用' "$T/user-server/internal/service/config_param_seeds.go"; then
  bad "F4 注码没落到磁盘（Description 仍独占一行＝变异未生效），本格结论不作数"
else
  OUT=$(run "$T")
  RC=$?
  if [ "$RC" = 0 ] && printf '%s\n' "$OUT" | grep -q '种子条目 2 条' \
     && printf '%s\n' "$OUT" | grep -q '有读取点且未挂标注=1  未接线且已声明=1  未接线但未声明=0  声明过期=0'; then
    ok "F4 单行条目照样被枚举（种子条目 2 条）且判据正常开火"
  else
    bad "F4 应 rc=0 且枚举到 2 条、读数 1/1/0/0，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
  fi
fi

# ── F4b 条目头漂走（Key 挪到 Name 之后）⇒ rc=2 说对账失败 ──
# 与 F4 相反的那一侧：`Key:` 行还数得到、条目头解不出 ⇒ 两条计数必须对上，
# 否则"少枚举一条"会被读成"少一条要判的参数"（那就是把盲区放行的口子）。
T=$WORK/f4b
seed_tree "$T"
perl -0pi -e 's/\{Group: "demo", Key: "wired_threshold", Name: "演示阈值",/{Group: "demo", Name: "演示阈值", Key: "wired_threshold",/' \
  "$T/user-server/internal/service/config_param_seeds.go"
if ! grep -q '{Group: "demo", Name: "演示阈值", Key: "wired_threshold",' "$T/user-server/internal/service/config_param_seeds.go"; then
  bad "F4b 注码没落到磁盘（字段顺序未变＝变异未生效），本格结论不作数"
else
  OUT=$(run "$T")
  RC=$?
  if [ "$RC" = 2 ] && printf '%s\n' "$OUT" | grep -q '条目解析对账失败'; then
    ok "F4b 枚举源少解一条时 rc=2 并说「条目解析对账失败」，不是冒充绿"
  else
    bad "F4b 应 rc=2 且说对账失败，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
  fi
fi

# ── F5 种子文件不在 ⇒ rc=2 ──
T=$WORK/f5
seed_tree "$T"
rm -f "$T/user-server/internal/service/config_param_seeds.go"
OUT=$(run "$T")
RC=$?
if [ "$RC" = 2 ] && printf '%s\n' "$OUT" | grep -q '种子文件不存在'; then
  ok "F5 没有对象时 rc=2 并说「种子文件不存在」"
else
  bad "F5 应 rc=2 且说种子文件不存在，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── F6 读取点走常量 ⇒ 绿（wired_threshold 不再以字面量出现在 Get 行上） ──
T=$WORK/f6
seed_tree "$T"
cat >"$T/user-server/internal/bridge/handler.go" <<'GO'
package bridge

const wiredKey = "wired_threshold"

func readThresh(svc interface{ GetInt(a, b, c string) int }) int {
	return svc.GetInt("demo", wiredKey, 5)
}
GO
OUT=$(run "$T")
RC=$?
if [ "$RC" = 0 ] && printf '%s\n' "$OUT" | grep -q '有读取点且未挂标注=1'; then
  ok "F6 常量形态的读取点被认出来了（rc=0 且有读取点=1）"
else
  bad "F6 常量形态应认成读取点，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── F7 读取点只在 _test.go ⇒ 仍判未接线 ──
T=$WORK/f7
seed_tree "$T"
cat >"$T/user-server/internal/bridge/handler_test.go" <<'GO'
package bridge

func readTTL(svc interface{ GetDuration(a, b, c string) int }) int {
	return svc.GetDuration("demo", "sleepy_ttl", 60)
}
GO
perl -0pi -e 's/演示 TTL（未接线）/演示 TTL/' "$T/user-server/internal/service/config_param_seeds.go"
perl -0pi -e 's/【当前不生效，改了也没人读】//' "$T/user-server/internal/service/config_param_seeds.go"
OUT=$(run "$T")
RC=$?
if [ "$RC" = 1 ] && printf '%s\n' "$OUT" | grep -q 'UNDECLARED: demo\.sleepy_ttl'; then
  ok "F7 只有测试引用时不算生产读取点，红在 UNDECLARED"
else
  bad "F7 应 rc=1 且点名 UNDECLARED: demo.sleepy_ttl，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

# ── REAL 真仓库 ⇒ 必须绿 ──
OUT=$(run "$ROOT")
RC=$?
if [ "$RC" = 0 ]; then
  ok "REAL 真仓库绿：$(printf '%s\n' "$OUT" | grep '读数:')"
else
  bad "REAL 真仓库应绿，实际 rc=${RC}：$(printf '%s\n' "$OUT" | tr '\n' ' | ')"
fi

echo "断言处数=$N 失败=$FAIL"
if [ "$FAIL" -gt 0 ]; then
  echo "❌ check-config-param-readpoints 用例未通过"
  exit 1
fi
echo "✅ check-config-param-readpoints 用例通过"
