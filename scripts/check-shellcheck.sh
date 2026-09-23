#!/usr/bin/env bash
# =============================================================================
# check-shellcheck.sh —— 仓内 shell 文件的 shellcheck「error 级零容忍」闸
#
# 立项原因（2026-09-23）：本仓此前**没有任何一道门看过 shell 脚本的内容**——
# scripts/check-shell-cjk-expansion.sh 只判一种形状（$VAR 紧跟中文），它自己的头注释
# 就写着"也不证明 shellcheck 跑过"；`grep -rn shellcheck .github/workflows/` 在建本门
# 之前是 0 命中，本机也没装过这个工具。也就是说 136 个 shell 文件（其中相当一部分是
# 部署/装机/密钥轮换这类会动到生产和凭证的脚本）从来没被任何静态分析跑过一遍。
#
# 先量债再定档（口径：tracked + 未跟踪未忽略的 *.sh/*.bash，134 + 2 = 136 个文件，
# 用 shellcheck 0.11.0 实测）：955 条 findings 里 info 828 / warning 110 / style 16 /
# **error 1**。error 只有 1 处（scripts/inference-host/env.sh 缺 shell 声明，SC2148，
# 已在本批就地修掉）。所以本门取 **error 级零容忍、不带基线**：
#   - 为什么不钉 warning 棘轮：110 条 warning 铺在 104 个文件上，基线会是一张百行表，
#     且大头属别泳道的热文件——那正是"基线写满别人文件＝门永远没人动"的形状。
#     等 error 面长期为零后再按目录分批收紧，届时候补基线（一次显式决策）。
#   - 为什么不留基线文件：零债的棘轮没有意义，而"缺基线退 2"这类自指保护的活由
#     checked 对账承担（见下）。
#
# 三处判据不是"跑一下 shellcheck 看 rc"就能有的，都来自实测：
#   ① **含非法 UTF-8 字节 .sh 文件，shellcheck 直接退 0、0 条 finding**
#      （实测 `echo \xff\xfe` 那种文件：rc=0 命中=0）。所以"文件能按 UTF-8 读出来"
#      必须是本门自己的一条判据，否则绿读数里混着"根本没被分析"的文件。
#   ② shellcheck 的 rc 语义：0=无该级别及以上 finding、1=有、2=它自己没能检查
#      （实测对不存在的文件 rc=2 并往 stderr 打 openBinaryFile: does not exist）。
#      因此 rc=2 绝不能并进"绿"，也不能并进"红因"——它是环境坏了，走本门的 rc=2。
#   ③ 逐文件跑而非一次传 136 个参数：只有逐文件才拿得到"每个文件各一个 rc"，
#      从而能把 checked 对账成"枚举到的文件数"（任何被静默跳过的文件都会露出来）。
#
# 覆盖面与盲区（别让绿读数大于它的证明力）：
#   - 只判 error 级 ⇒ 954/955 条不在判据内；也不证明脚本"行为正确"，更不跑 bats 用例。
#   - 不跟随 source（SC1091 属 info）：跨文件的"读了个没定义的变量"这类判不出来。
#   - 扫描面 = `git ls-files --cached --others --exclude-standard` 里的 *.sh / *.bash，
#     与 check-shell-cjk-expansion.sh 同一条口径（为什么不用 os.walk：仓根 logs/ 是
#     gitignore 的取证快照树，实测同一棵树两次扫 136 → 538，红点全在别泳道刚丢下的
#     clone 副本里 ⇒ 本地恒红、CI 恒绿）。代价：被忽略的 shell 文件本门看不见。
#   - 版本：本机 0.11.0（pip 包 shellcheck-py==0.11.0.1），CI 里按同一版本装。
#     本门**不断言版本相等**（升版该由维护者决定），但会把版本印在首行——升版后
#     若新规则把某文件判成 error，这里是"红 + 版本号"，不是静默漂移。
#   - *.bats / Makefile recipe 不在面内（跟着那道姊妹门的口径）。
#
# 维护注记：本文件自己的头注释里，行首「# + shellcheck + 非指令文本」会被当成指令去解析，
# 报 SC1073/SC1072（本门第一刀就砍在自己身上：第 12 行那句「shellcheck 0.11.0 实测」，
# 第二次是这句注释换行后行首又长出 shellcheck）。别的 shell 文件写注释时同理。
#
# 用法：bash scripts/check-shellcheck.sh
#   rc=0 全面零 error；rc=1 有 error 级 finding（逐条 文件:行:列 SC 码 原文）；
#   rc=2 环境或判据本身有问题（没装 shellcheck、扫描根不对、枚举面骤减、
#   文件按 UTF-8 读不出、shellcheck 自己 rc=2、输出不是合法 JSON）
#
# 本地跑之前需要一次装工具（不动系统包）：
#   python3 -m pip install --user shellcheck-py==0.11.0.1   # 落到 ~/.local/bin
#   回滚：python3 -m pip uninstall -y shellcheck-py
# 反向测试（改完本脚本必须逐格真跑）：bash scripts/check-shellcheck.test.sh
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
SELF="$SCRIPT_DIR/check-shellcheck.sh"

[ -d "$PROJECT_ROOT/scripts" ] || { echo "::error::扫描根不对（$PROJECT_ROOT 下没有 scripts/）"; exit 2; }
[ -f "$SELF" ] || { echo "::error::找不到闸自身 $SELF —— 判据文件不在原位，扫描面无从谈起"; exit 2; }

python3 - "$PROJECT_ROOT" <<'PY'
import json, os, re, shutil, subprocess, sys

root = sys.argv[1]
SHELL_EXT = (".sh", ".bash")
# SHELLCHECK_PATH 优先于 PATH 查找：反向测试要用一个假 shellcheck 喂特定形状
# （rc 与 finding 数矛盾 / 输出不是 JSON / 参数没带全），若让 which() 先赢，
# 开发机上装了真货时那几格会静默打到真工具上、证不到分支。
SC = os.environ.get("SHELLCHECK_PATH", "").strip() or (shutil.which("shellcheck") or "")
if not SC or not os.path.exists(SC):
    print("::error::找不到 shellcheck —— 本门零覆盖，不判绿。装法："
          "python3 -m pip install --user shellcheck-py==0.11.0.1（然后 PATH 里要有 ~/.local/bin）")
    sys.exit(2)

ver_out = subprocess.run([SC, "--version"], capture_output=True, text=True).stdout
ver = re.search(r"version:\s*(\S+)", ver_out)
print("shellcheck=%s（%s）" % (ver.group(1) if ver else "?", SC))


def git_shell_files(root_dir):
    """已跟踪的 + 未跟踪但没被 .gitignore 忽略的 shell 文件＝「提交前该拦下的那一面」。"""
    r = subprocess.run(["git", "-C", root_dir, "ls-files", "-z",
                        "--cached", "--others", "--exclude-standard"],
                       capture_output=True, text=True, encoding="utf-8", errors="surrogateescape")
    if r.returncode != 0:
        print("::error::git ls-files 失败（%s）—— 扫描面无法确定，宁可红也不判绿"
              % r.stderr.strip()[:200])
        sys.exit(2)
    return sorted(p for p in r.stdout.split("\0") if p.endswith(SHELL_EXT))


files = git_shell_files(root)
# 面骤减＝枚举坏了，不是"债清完了"。本仓已知量级 134（tracked）+ 未跟踪未忽略的新落点。
if len(files) < 50:
    print("::error::只枚举到 %d 个 shell 文件（已知量级 134）—— 枚举或 git 环境有问题，不判绿"
          % len(files))
    sys.exit(2)

findings, checked, broken = [], 0, []
for rel in files:
    path = os.path.join(root, rel)
    try:
        raw = open(path, "rb").read()
        raw.decode("utf-8")
    except (UnicodeDecodeError, OSError) as e:
        # 判据①：这种文件 shellcheck 会退 0，所以必须在这一层拦下来。
        broken.append("%s: %s" % (rel, e))
        continue
    r = subprocess.run([SC, "-f", "json", "-S", "error", "--", path],
                       capture_output=True, text=True)
    if r.returncode == 2:
        # 判据②：它自己没检查成，不是"没问题"。
        broken.append("%s: shellcheck rc=2 %s" % (rel, r.stderr.strip()[:160]))
        continue
    if r.returncode not in (0, 1):
        broken.append("%s: shellcheck 返回了未预期的 rc=%d" % (rel, r.returncode))
        continue
    try:
        items = json.loads(r.stdout or "[]")
    except ValueError:
        broken.append("%s: shellcheck 输出不是合法 JSON（换了 -f json 之外的形状？版本对得上吗）" % rel)
        continue
    # 判据③的对账：rc=0 时结果集必须为空，rc=1 时必须非空——反了说明参数没落进去。
    if (r.returncode == 0) != (len(items) == 0):
        broken.append("%s: rc=%d 与 finding 数 %d 自相矛盾" % (rel, r.returncode, len(items)))
        continue
    checked += 1
    for it in items:
        findings.append("%s:%d:%d SC%d %s"
                        % (rel, it["line"], it["column"], it["code"], it["message"]))

print("scanned=%d checked=%d error=%d" % (len(files), checked, len(findings)))
# 每条 continue 都同时往 broken 里记一笔，所以 broken 为空即 checked == len(files)；
# 这一条不变量不再单独判一次（写成判据反而会出现"永远走不到"的假门），靠上面那行读数对账：
# 反向测试 T0 拿独立的 git ls-files 计数比 scanned 与 checked 两个数。
if broken:
    print("::error::有 %d 个文件本门无法判它（不是「没问题」）：\n  %s"
          % (len(broken), "\n  ".join(broken)))
    sys.exit(2)

if findings:
    print("::error::shellcheck error 级 findings：")
    for f in findings:
        print("  " + f)
    print("红因逐条上面；改不动的不要加豁免，先看它是不是真 bug（本门当前零债、无基线）。")
    sys.exit(1)

print("✅ %d 个 shell 文件全部按 error 级检查过，0 条 finding" % checked)
PY
