#!/usr/bin/env python3
"""mut_db_poolcfg.py —— `db.go` 池参数那条接线的常驻杀伤电池（K1–K4，四刀）。

为什么要有这一份（§23.22 第 6 段③）：那"四刀 4/4 全杀"的读数原先由一枚一次性 `/tmp` 驱动产出
（产物在 `logs/CapAB/20260924-135922/`），驱动本身不在树里 ⇒ 下一位无从复跑，读数成了只能引用
不能核验的二手证据。本脚本把同一批刀、同一套判据常驻化，注码形状逐字照旧。

被钉的决定在 `user-server/internal/pkg/db/db.go`：`normalizePool()` 的三个兜底值与
`applyPool()` 里那句 `SetMaxOpenConns`。这一段原先只活在 `InitDB` 里、没有任何用例经过，
所以"配置文件写着 200"与"句柄生效 200"之间那段接线从未被断言过——`check-ci-pg-capacity.py`
的 C5 下限正是拿 `config.DefaultPoolConfig.MaxOpenConns` 算的：那行 Set 一旦被挪走，
门仍绿、真实句柄却掉回 Go 默认，表现为吞吐骤降而不是报错，属最难发现的那类回归。

四刀的落点与期望杀手（expect 是**实测**出来的格子名，不是拿"应该红在哪格"推的；
K4 原先被推在 else 腿那一格，实测红在第一格，订正见 `db_pool_bound_test.go` 头部）：
  K1 摘掉 `sqlDB.SetMaxOpenConns(...)` 整行        ⇒ TestPoolBoundLandsOnSQLHandle
  K2 `MaxOpenConns` 兜底 20⇒0（0＝不限连接）        ⇒ TestNormalizePoolFallbacks/只缺_MaxOpenConns⇒补_20
  K3 `ConnMaxLifetime` 兜底 30 分钟⇒60 秒           ⇒ TestNormalizePoolFallbacks/只缺_ConnMaxLifetime⇒补_30_分钟
  K4 摘掉 `if MaxIdleConns == 0 { 用默认表 }` 整条  ⇒ TestNormalizePoolFallbacks/整条留空⇒采用生产默认表

跑在哪棵树：只在 `git clone --shared` 出来的私有克隆里注码（共享工作树里有并行会话的提交），
基线字节＝克隆 HEAD，身份行现读 tip 短 SHA；危险 `--clone` 由 `mut_dispose.workdir()` 挡在装架之前。
两枚被跑的用例都不连库（`noDialConnector` 不拨号、表测是纯函数），所以本电池**不需要测试库**，
也不读 `.env`——它的红因此不能归因到"PG 没起"。工具链与盘满仍单独分一类 ENV-BROKEN：
本机 Xcode 许可未接受时 `xcrun` 退 69 ⇒ `SDKROOT` 空 ⇒ 链接器报 `library 'resolv' not found`，
那既不是判据没牙也不是树红（§23.21 第 5 段同族）。

判据：四格全 KILLED、每格 `ran` 与控制组实测条数相等、expect 逐格点名且存在于控制组的 PASS 名单
（防"expect 点到改过名的化石腿"）、末了 `db.go` 的 md5 与基线一致。任一条不成立退 1。
取证落点：控制组与逐刀原始输出写进 `docs/superpowers/specs/ledger/logs/DBPool/<趟次戳>/`，
判定行同时 tee 到该目录的 `00-run.log`（落盘前过 `redact.scrub`）。

用法：
  python3 scripts/mut_db_poolcfg.py                    # 四刀全族（要编 Go，不要库）
  python3 scripts/mut_db_poolcfg.py --check            # 锚点/用例名预检（要装架，不跑 go test）
  python3 scripts/mut_db_poolcfg.py --check-tree       # 同上但对着当前树：CI 的落点用这一枚
  python3 scripts/mut_db_poolcfg.py --selftest         # 五格内存反向格（不读源码、不跑 go）
  python3 scripts/mut_db_poolcfg.py --clone <仓库外空目录> [--keep]
"""
import argparse
import hashlib
import os
import re
import subprocess
import sys
import time
from pathlib import Path

from battlog import tee_to
from mut_dispose import dispose, workdir
from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）

ROOT = Path(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))).resolve()
SERVER = "user-server"
SRC_REL = "user-server/internal/pkg/db/db.go"
PKG_REL = "user-server/internal/pkg/db"
RUNNER = "TestPoolBoundLandsOnSQLHandle|TestNormalizePoolFallbacks"
LOGROOT = ROOT / "docs/superpowers/specs/ledger/logs/DBPool"
LOGDIR = None  # main() 里按趟次戳定；常驻件不许复用上一轮的目录（同路径＝抹掉旧读数）

# (格名, 这一刀改坏的是什么, old, new, 期望点名的杀手（整行名，含父/子）)
CELLS = [
    ("K1", "摘掉 SetMaxOpenConns ⇒ 上界不落句柄",
     "\tsqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)\n", "",
     "TestPoolBoundLandsOnSQLHandle"),
    ("K2", "上界兜底 20⇒0（0＝不限连接）",
     "\t\tpoolConfig.MaxOpenConns = 20", "\t\tpoolConfig.MaxOpenConns = 0",
     "TestNormalizePoolFallbacks/只缺_MaxOpenConns⇒补_20"),
    ("K3", "lifetime 兜底 30 分钟⇒60 秒",
     "\t\tpoolConfig.ConnMaxLifetime = int((30 * time.Minute).Seconds())",
     "\t\tpoolConfig.ConnMaxLifetime = 60",
     "TestNormalizePoolFallbacks/只缺_ConnMaxLifetime⇒补_30_分钟"),
    ("K4", "摘掉 MaxIdleConns==0 的默认表兜底",
     "\tif poolConfig.MaxIdleConns == 0 {\n\t\tpoolConfig = config.DefaultPoolConfig\n\t}\n", "",
     "TestNormalizePoolFallbacks/整条留空⇒采用生产默认表"),
]

TALLY = ["KILLED", "SURVIVED", "RED-UNNAMED", "BUILD-BROKEN", "ENV-BROKEN", "NO-RUN"]

# 环境红与树红必须分开：报成"先修树"会把人支去改没坏的文件。本电池不连库，所以别的电池里
# 那批 SASL／连接类签名在这里用不上，换成本电池真实的前置：工具链（Xcode 许可⇒SDKROOT 空⇒
# 链接器缺库）与写满的盘。
ENV_SIGNS = ("library 'resolv' not found", "no space left on device", "clang: error",
             "cannot find package", "cannot find GOROOT", "signal: killed")

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def dump(tag: str, out: str) -> None:
    LOGDIR.mkdir(parents=True, exist_ok=True)
    name = re.sub(r"[^A-Za-z0-9_.-]", "-", tag) + ".log"
    (LOGDIR / name).write_text(scrub(out), encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:80]!r}")
    return text.replace(old, new, 1)


def prepare(dst: Path) -> Path:
    """私有 `--shared` 克隆。本卡四刀全打在已入库的 `db.go`，无未提交面依赖 ⇒ 不覆盖脏文件。

    为什么反而更严：克隆里的字节就是 HEAD，锚点若跟着工作树的未提交字节走会量出一个 HEAD
    上不存在的世界；预检因此对着 HEAD 验，锚点失守时该改的是脚本（跟着 tip 走），
    不是拿脏树冒充基线。
    """
    clone = dst / "clone"
    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        raise SystemExit("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    return clone


def tip(clone: Path) -> str:
    return subprocess.run(["git", "rev-parse", "--short", "HEAD"], cwd=clone,
                          capture_output=True, text=True).stdout.strip()


def defined_tests(pkg: Path) -> set:
    """`pkg` 里 `func TestXxx(` 定义的名字集合：expect 的父用例必须在此，否则那一格红在
    `no tests to run`——那是用例改名留下的化石，不是判据开火。"""
    names = set()
    for f in sorted(pkg.glob("*_test.go")):
        names |= set(re.findall(r"^func (Test[A-Za-z0-9_]*)\(", read(f), re.M))
    return names


def check_cells(text: str, cells, tests: set):
    """预检内核（纯函数，`--check` 与 `--selftest` 共用同一份判据）。三查：
    ① 锚点在整份文件里命中恰好 1 次（多处＝判据会打到别人的分支上，那是假杀）；
    ② 注码真让字节变了（改名式变异／old==new 的永不开火格）；
    ③ expect 的父用例名在本包有定义。
    返回 (坏格数, 逐行读数)。"""
    lines, bad = [], 0
    for code, desc, old, new, expect in cells:
        hits = text.count(old)
        if hits != 1:
            bad += 1
            lines.append(f"  ✗ {code} 锚点命中 {hits} 次（要求恰好 1 次）：{old[:70]!r}")
            continue
        if text.replace(old, new, 1) == text:
            bad += 1
            lines.append(f"  ✗ {code} 注码打完了而字节没变（这一格永不开火）")
            continue
        parent = expect.split("/")[0]
        if parent not in tests:
            bad += 1
            lines.append(f"  ✗ {code} 期望的杀手 {parent} 在本包 *_test.go 里查无定义"
                         f" ⇒ 这一格会红在 no tests to run，不是杀在判据上")
            continue
        lines.append(f"  ✓ {code} {desc}｜杀手 {expect}")
    return bad, lines


def go_run(clone: Path):
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOFLAGS", "-mod=mod")
    # 私有 GOCACHE：与 mut_egress_pool_r30.py 同一枚（文档里记为"有意保留"的那份），
    # 不复用系统缓存 ⇒ 本电池的红不会随别人清缓存而变，也不把共享盘写满。
    env.setdefault("GOCACHE", os.environ.get("R45_MUT_GOCACHE", "/tmp/gocache-r45mut"))
    try:
        p = subprocess.run(["go", "test", "./internal/pkg/db/", "-run", RUNNER, "-count=1", "-v"],
                           cwd=clone / SERVER, capture_output=True, text=True, timeout=1800, env=env)
        out, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out, rc = (e.stdout or "") + (e.stderr or ""), -9
    except FileNotFoundError:
        return -127, [], 0, "go：命令不在 PATH ⇒ 本电池没跑过任何东西", []
    out = ANSI.sub("", out)
    # 名单按整行取：`(\S+)` 能吃下 `父/子` 这种带斜杠的子用例名。先按 `[^\s/]+` 切会把子用例
    # 截成父名，expect 就永远对不上（本卡的 expect 三条打的是子用例）。
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = sorted(set(re.findall(r"^    --- PASS: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- PASS: (\S+)", out, re.M)))
    ran = (len(re.findall(r"^=== RUN\s+\S+", out, re.M)) +
           len(re.findall(r"^    === RUN\s+\S+", out, re.M)))
    return rc, killed, ran, out, passed


def classify(rc: int, killed: list, ran: int, out: str, expect: str) -> str:
    if any(s in out for s in ENV_SIGNS) or rc in (-127, -9):
        return "ENV-BROKEN"
    if "[build failed]" in out or "undefined:" in out or "cannot use " in out:
        return "BUILD-BROKEN"
    if "no tests to run" in out or ran == 0:
        return "NO-RUN"
    if expect in killed:
        return "KILLED"
    if killed:
        return "RED-UNNAMED"
    return "SURVIVED"


def do_check(base: Path) -> int:
    """锚点预检的公共出口：`base` 是"要对着哪一份字节验"——克隆（`--check`，HEAD）或当前树
    （`--check-tree`，CI 用，不装架不跑 go）。两者判据同一条，只是对象不同。"""
    tests = defined_tests(base / PKG_REL)
    bad, lines = check_cells(read(base / SRC_REL), CELLS, tests)
    for ln in lines:
        print(ln)
    print(f"锚点校验：{len(CELLS)} 格，{bad} 格有问题")
    return 1 if bad else 0


def do_selftest() -> int:
    """五格内存反向格：不读盘、不起 `go test`、不需要克隆 ⇒ 任何有 python3 的地方都能跑。

    为什么常驻件自己也要被反向测：`check_cells` 是本电池"锚点失守／注码不落地／expect 是化石"
    三种失效的唯一出口。它若恒报 0 格有问题，那 `--check` 的绿就是装饰——
    所以每一格既断言坏格数，也断言**点名的行数**（只数坏格数会放过"报了数却没报是谁"）。
    """
    good = "\n".join(c[2] for c in CELLS)          # 四枚锚点各出现恰好一次
    # 口径边界：S1 的文本是**由 CELLS 自己拼出来的**，自洽 ⇒ 它只证判据内核的分支走向，
    # 不证"这四枚锚点真在 `db.go` 里"——后者由 `--check`（克隆 HEAD）／`--check-tree`（当前树）判。
    # 别把这里的绿读成"锚点还活着"：锚点搬走时 S1 照样绿。
    tests = {"TestPoolBoundLandsOnSQLHandle", "TestNormalizePoolFallbacks"}
    cases = [
        ("S1 好锚点＋用例有定义 ⇒ 0 格有问题", good, CELLS, tests, 0),
        ("S2 锚点被搬走 ⇒ 点名 K2", good.replace(CELLS[1][2], "\t\tpoolConfig.MaxOpenConns = 21"),
         CELLS, tests, 1),
        ("S3 注码不落地（old==new）⇒ 点名那一格", good,
         [CELLS[0], ("K2x", "空补丁", CELLS[1][2], CELLS[1][2], CELLS[1][4]),
          CELLS[2], CELLS[3]], tests, 1),
        ("S4 expect 是用例改名后的化石 ⇒ 点名那一格", good,
         [CELLS[0], CELLS[1], CELLS[2],
          ("K4x", "化石 expect", CELLS[3][2], "", "TestRenamedAway/整条留空⇒采用生产默认表")],
         tests, 1),
        ("S5 锚点在文件里出现两次 ⇒ 点名（判据会打到别人身上）",
         good + "\n\t\tpoolConfig.MaxOpenConns = 20\n", CELLS, tests, 1),
    ]
    failed = 0
    for label, text, cells, tset, want in cases:
        bad, lines = check_cells(text, cells, tset)
        named = sum(1 for ln in lines if ln.startswith("  ✗"))
        if bad != want or named != want:
            failed += 1
            print(f"  ✗ {label}：实得 {bad} 格有问题／点名 {named} 行，期望 {want}")
        else:
            print(f"  ✓ {label}：坏格数 {bad}＝点名行数 {named}")
    print(f"===== 预检自测：{len(cases)} 格，失败 {failed} 格 =====")
    return 1 if failed else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只验锚点与用例名（要装架，不跑 go test）")
    ap.add_argument("--check-tree", action="store_true",
                    help="同上，但对着**当前树**的字节验：不装架、不跑 go（CI 落点用这一枚）")
    ap.add_argument("--selftest", action="store_true", help="纯内存反向格（不读盘、不跑 go）")
    ap.add_argument("--clone", default="", help="私有作业目录（必须是仓库外的空目录）")
    ap.add_argument("--keep", action="store_true", help="收尾不回收，原地留证")
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    args = ap.parse_args()

    global LOGDIR
    LOGDIR = LOGROOT / args.tag

    if args.selftest:
        return do_selftest()
    if args.check_tree:
        print(f"基线字节：当前工作树 {ROOT}（`--check-tree`，不装架、不落产物）")
        return do_check(ROOT)

    tmp, owned = workdir(args.clone or None, prefix="dbpoolmut-", repo_root=ROOT)
    try:
        clone = prepare(tmp)
        print(f"基线字节：克隆 HEAD `{tip(clone)}`｜作业目录 {tmp}")
        if args.check:
            tee_to(LOGDIR / "00-check.log")
            return do_check(clone)
        tee_to(LOGDIR / "00-run.log")

        src = clone / SRC_REL
        original = read(src)
        orig_md5 = md5_bytes(src)
        bad, lines = check_cells(original, CELLS, defined_tests(clone / PKG_REL))
        if bad:
            for ln in lines:
                print(ln)
            print("PREFLIGHT-FAILED：先修锚点再放刀（放刀路径上这些格会变成 BROKEN/NO-RUN）")
            return 6

        rc, killed, ran, out, passed = go_run(clone)
        dump("00-control", out)
        if rc != 0:
            kind = "CONTROL-ENV-BROKEN" if any(s in out for s in ENV_SIGNS) else "CONTROL-RED"
            print(f"{kind}：没放刀就红 ⇒ 判据未验证（红因末 8 行）")
            print("\n".join(out.splitlines()[-8:]))
            return 8
        expect_ran = ran
        print(f"CONTROL-GREEN ✅ ran={ran} 名单={'、'.join(passed)}")
        for _c, _d, _o, _n, expect in CELLS:
            if expect not in passed:
                print(f"CONTROL-BROKEN：期望杀手 {expect} 不在控制组的 PASS 名单里 ⇒ 这条腿没跑到")
                return 6

        tally = {k: 0 for k in TALLY}
        problems = []
        for code, desc, old, new, expect in CELLS:
            try:
                src.write_text(sub_once(original, old, new, code), encoding="utf-8")
            except SystemExit as e:
                problems.append(str(e))
                tally["BUILD-BROKEN"] += 1
                continue
            rc, killed, ran, out, _p = go_run(clone)
            dump(code, out)
            v = classify(rc, killed, ran, out, expect)
            tally[v] += 1
            if v == "KILLED" and ran != expect_ran:
                problems.append(f"{code} 杀了但 ran={ran}≠控制组的 {expect_ran}"
                                f"：疑似 panic 带走别的腿，红因要人工看")
            if v == "KILLED":
                print(f"{code:<4} {desc[:38]:<40} 杀掉 by {expect}｜红名单 {len(killed)} 条 ran={ran}")
            else:
                problems.append(f"{code} 判为 {v}（不是干净的杀掉）：{desc}")
                print(f"{code:<4} {desc[:38]:<40} {v} rc={rc} ran={ran}")
                print(out[-1500:])
            src.write_text(original, encoding="utf-8")
            if md5_bytes(src) != orig_md5:
                print("RESTORE-MISMATCH：还原后字节与基线不同，停机")
                return 8

        print("\n计数：", " ".join(f"{k}={tally[k]}" for k in TALLY))
        print(f"全部格子已还原（{SRC_REL} md5={orig_md5}）")
        if problems:
            print("\n===== 电池判定：有洞 =====")
            for x in problems:
                print("  ✗", x)
            return 1
        print(f"\n===== 电池判定：{len(CELLS)} 格逐格核验，无未登记存活 =====")
        return 0
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)


if __name__ == "__main__":
    sys.exit(main())
