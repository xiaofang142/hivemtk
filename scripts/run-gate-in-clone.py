#!/usr/bin/env python3
"""把闭合门搬进**私有 --shared 克隆**再跑一遍：证明它只读文本、不连库、不靠本会话的残留状态。

为什么要有这一趟（协议 §3「门的性质」/台账 P-26 的那格证据）：闭合门跑在与并行泳道**同一棵工作树**
上，而它引用的证据（spec、日志、fixture）与用例名单都住在同一棵树里。只在工作树里跑绿，说明不了
"换一棵树还跑得出同一个数"——本仓已经栽过一次：门禁脚本用 `scripts/../..` 推项目根、又硬编码仓名，
在改名克隆里 rc=1 零输出。这一趟就是把那类病**主动撞一遍**。

三条口径：
1. 克隆只含 HEAD ⇒ 本泳道的未提交改动以**覆盖**方式装进去（`lane_overlays`），否则门读到的是旧字节，
   绿得毫无意义；
2. 外域未跟踪的 `_test.go` 不装（并行泳道按 TDD 写的测试文件引用还没落地的符号，装进来只会让那一包
   `go test -list` 编译不过——那是别人的在途状态，不是门的缺陷）。**这一条是有意不对称的**：外域未跟踪的
   **实现**文件照装——已跟踪的代码可能引用它（少装 ⇒ 那一包 `[build failed]`，门会报"腿所在包跑不出名单"，
   红因指向门而不是指向事实）。代价也说清：**排除外域测试会让那些用例名在克隆的 `-list` 里消失**，
   所以台账的 Go 腿一旦点到"只活在别人未跟踪文件里的用例"，这一趟就会在克隆侧单独红——那是台账写错了
   引用（腿该指自己泳道能复现的用例），不是门的缺陷，报告里要按这个口径归因。
3. 子进程环境里**主动抹掉所有 POSTGRES_/DB 变量**：门若偷偷连库，这一趟会红，而不是靠人读代码断定它没连。

用法：
    python3 scripts/run-gate-in-clone.py                    # 两份台账 + --selftest 都在克隆里跑
    python3 scripts/run-gate-in-clone.py docs/.../R22.jsonl # 只跑点名的台账
"""
from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
GATE = "scripts/check-review-closeout.py"
LOGS_REL = Path("docs/superpowers/specs/ledger/logs")
DEFAULT_LEDGERS = ["docs/superpowers/specs/ledger/R22.jsonl",
                   "docs/superpowers/specs/ledger/R22-protocol.jsonl"]

# 外域（支付/webhook 泳道）未跟踪的测试文件：按**文件名特征**排除，而不是按目录——同一目录里
# 两泳道的文件混住（`internal/service/` 既有我的 inbox_ingress_r23_* 也有对方的 order_webhook_payment_*）。
FOREIGN_UNTRACKED_TESTS = ("payment", "order_webhook", "external_order_key")


def git(*args: str) -> str:
    r = subprocess.run(["git", "-C", str(ROOT), *args], capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit(f"git {' '.join(args)} 失败：{r.stderr[-200:]}")
    return r.stdout


def lane_overlays() -> tuple[list[str], list[str], list[str]]:
    """(要覆盖进克隆的文件, 排除的外域测试, 克隆里要删掉的文件)。"""
    overlay, skip, deleted = [], [], []
    for line in git("status", "--porcelain", "-uall").splitlines():
        state, p = line[:2], line[3:].split(" -> ")[-1].strip().strip('"')
        if "D" in state:
            deleted.append(p)
            continue
        if state == "??" and is_foreign_untracked(p):
            skip.append(p)
            continue
        if (ROOT / p).is_file():
            overlay.append(p)
    return overlay, skip, deleted


def make_clone(dst: Path) -> None:
    r = subprocess.run(["git", "clone", "--shared", "--quiet", str(ROOT), str(dst)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("git clone --shared 失败：" + (r.stderr or r.stdout)[-300:])
    head = git("rev-parse", "HEAD").strip()[:12]
    clone_head = subprocess.run(["git", "-C", str(dst), "rev-parse", "HEAD"],
                                capture_output=True, text=True).stdout.strip()[:12]
    if head != clone_head:
        raise SystemExit(f"克隆 HEAD({clone_head}) ≠ 工作树 HEAD({head})——基准不对，读数无意义")


def is_foreign_untracked(p: str) -> bool:
    return p.endswith("_test.go") and any(f in p for f in FOREIGN_UNTRACKED_TESTS)


# 只抹**这一组**库连接参数：早先写成一条 `PG` 前缀的正则，本意是 `PGPORT`，实际把 `PGCODE*`
# 之外的 P 开头变量（PATH 不受影响，但 PGxxx 一律误伤）一起吞了，而日志里却写"只抹了 PG 端口"
# ——判据实际吞掉的东西与自述不一致，比吞多了更贵。名单在这里列全，日志按**本趟真抹掉的键名**印。
DB_VARS = ("POSTGRES", "PGHOST", "PGPORT", "PGUSER", "PGPASSWORD", "PGDATABASE",
           "PGSSLMODE", "PGAPPNAME", "DB_", "MYSQL", "REDIS", "ONEID")


def is_db_var(k: str) -> bool:
    return any(k.startswith(p) for p in DB_VARS)


def bare_env() -> dict:
    """只留与"门是否连库"无关的变量：PATH 一类要留着，否则子进程连 python 都起不来。"""
    return {k: v for k, v in os.environ.items() if not is_db_var(k)}


def run_gate(clone: Path, argv: list[str]) -> tuple[int, str]:
    """在克隆里跑门，且**抹掉一切库连接参数**：门连库就该在这一步露出来。"""
    env = bare_env()
    env["GIN_MODE"] = "test"
    p = subprocess.run([sys.executable, GATE, *argv], cwd=clone,
                       capture_output=True, text=True, timeout=1800, env=env)
    return p.returncode, p.stdout + p.stderr


def summaries(out: str, base: Path) -> list[str]:
    """取门打印的汇总行并**去掉基准目录的痕迹**：两侧唯一能逐字比的就是这个。

    不比字符串而比"rc 都是 0"是不够的——门换一棵树少抽了几条承诺、条目数从 38 掉到 20，
    照样可能退 0（覆盖文件少装了一片时就是这个形状）。
    """
    return [l.replace(str(base), "@BASE@") for l in out.splitlines()
            if "闭合判定" in l or "items=" in l]


def clone_agrees(wt_out: str, cl_out: str, worktree: Path, clone: Path) -> tuple[bool, list[str], list[str]]:
    """(两侧汇总行是否逐字一致, 克隆侧行, 工作树侧行)。

    为什么这段要从 main 里抽出来：早先 `same = bool(sum_cl) and sum_cl == sum_wt` 内联在 main，
    而 --selftest 是在测试代码里**手写同一条表达式**做比对的——把 main 那行注成恒 True，
    7/7 照样绿（lane 6 实测）。现在两侧共用这一个函数，"非空"那半句才有主。
    """
    sum_wt, sum_cl = summaries(wt_out, worktree), summaries(cl_out, clone)
    return bool(sum_cl) and sum_cl == sum_wt, sum_cl, sum_wt


def _env_case() -> bool:
    """临时把口令塞进 os.environ，再看 bare_env() 有没有真把它抹掉（不塞的话这一格恒真＝没牙）。"""
    os.environ["POSTGRES_TEST_PASSWORD"] = "sentinel-not-a-real-secret"
    try:
        e = bare_env()
        return "POSTGRES_TEST_PASSWORD" not in e and "PATH" in e
    finally:
        del os.environ["POSTGRES_TEST_PASSWORD"]


def _overlay_case() -> bool:
    """反向格：临时起一个 git 仓，把"改了 / 删了 / 未跟踪（外域＋自己）"四种形状各造一个，看三类分对没有。

    为什么这一格值当起一个真仓：`lane_overlays` 是整趟克隆面的**输入面**——
    把外域测试当成自己的覆盖进去，克隆会因为别人没提交的测试编译不过而红，红因却写成"门在克隆里跑不通"；
    反过来把自己的未跟踪测试当外域排掉，克隆侧就少抽几条承诺，而"两侧汇总一致"照样成立（两边一起少）。
    两侧都测：外域要被排、自己的要被留。
    """
    global ROOT
    tmp = Path(tempfile.mkdtemp(prefix="clone-selftest-"))
    saved = ROOT
    try:
        def g(*args: str) -> None:
            subprocess.run(["git", *args], cwd=tmp, capture_output=True, check=True)
        (tmp / "tracked.go").write_text("package p\n", encoding="utf-8")
        (tmp / "gone.go").write_text("package p\n", encoding="utf-8")
        g("init", "-q", ".")
        g("add", ".")
        g("-c", "user.email=self@test", "-c", "user.name=self", "commit", "-q", "-m", "base")
        (tmp / "tracked.go").write_text("package p // 我这趟改的\n", encoding="utf-8")
        (tmp / "gone.go").unlink()
        (tmp / "payment_test.go").write_text("package p\n", encoding="utf-8")
        (tmp / "inbox_ingress_r23_mine_test.go").write_text("package p\n", encoding="utf-8")
        ROOT = tmp
        overlay, skip, deleted = lane_overlays()
        return (sorted(overlay) == ["inbox_ingress_r23_mine_test.go", "tracked.go"]
                and skip == ["payment_test.go"] and deleted == ["gone.go"])
    except subprocess.CalledProcessError:
        return False
    finally:
        ROOT = saved
        shutil.rmtree(tmp, ignore_errors=True)


def write_clone_report(logdir: Path, rc_all: int, lines: list[str]) -> str:
    """判决行**并进正文一起落盘**：台账钉的是这个脚本写出去的那份文件里的判定行。

    早先的顺序是 `log.write_text(out)` 在前、`print(判决行)` 在后 ⇒ 盘上那份永远缺这一行，
    于是"克隆面 rc=0"这条引用的 must_say 只能靠人手抄 stdout 补件才成立（本轮实测抓到）。
    自检两格都**读回磁盘**再断言，防止"注掉这个调用点"还能全绿。
    """
    verdict = ("===== 克隆面：rc=0，门在私有克隆里跑得出与工作树逐字一致的读数 ====="
               if rc_all == 0 else "===== 克隆面：有红，见上 =====")
    out = "\n".join([*lines, "", verdict]) + "\n"
    logdir.mkdir(parents=True, exist_ok=True)
    (logdir / "gate-in-shared-clone.log").write_text(out, encoding="utf-8")
    return out


def _disk_report(rc_all: int) -> str:
    """反向格夹具：真调生产写件路径，落到临时目录后**读回磁盘**（不读返回值、不读 stdout）。"""
    tmp = Path(tempfile.mkdtemp(prefix="clone-report-"))
    try:
        write_clone_report(tmp, rc_all, ["正文一行"])
        return (tmp / "gate-in-shared-clone.log").read_text(encoding="utf-8")
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def selftest() -> int:
    """反向测：这趟"克隆面"的判据本身要能被弄红，否则它是一句空话。

    最贵的一格是"两侧都零输出"那格：门在克隆里**零输出**（比如根目录推导撞墙、直接退出）时，
    两侧汇总行都是空列表，"逐字一致"会成立 ⇒ 空跑被记成绿。所以一致性的定义里必须带"非空"。

    本轮改这支撑时按同一口径抓到两种"看着有牙其实恒真"的形状，都留了格子：
    ① 判据写在测试里而不是生产代码里（把 main 那行注成恒 True，全绿）⇒ 现在四格都走 clone_agrees；
    ② 夹具没穿过真过滤器（喂给 summaries 的行不含标记关键字，两侧都被滤空再相等）
       ⇒ 现在喂真会被留下的行。
    """
    base = Path("/tmp/x-clone")
    other = Path("/other")
    cases = [
        # 四格都走 clone_agrees（生产那一条），不再在测试代码里手写同形表达式——
        # 早先的写法下，把 main 里的 `same = ...` 注成恒 True，这几格照样全绿。
        ("两侧汇总一致不该红",
         clone_agrees("闭合判定 items=38\n", "闭合判定 items=38\n", base, base)[0], True),
        ("克隆少抽几条要红",
         clone_agrees("闭合判定 items=38\n", "闭合判定 items=20\n", base, base)[0], False),
        ("两侧都零输出要红（空跑不是绿）",
         clone_agrees("啥也没打印\n", "啥也没打印\n", base, base)[0], False),
        ("工作树有汇总、克隆侧空要红",
         clone_agrees("闭合判定 items=38\n", "traceback...\n", base, base)[0], False),
        # 这一格原来是**空的**：喂进去的行既不含"闭合判定"也不含"items="，被 summaries 的过滤器
        # 滤成两个空列表再相等 ⇒ 恒 True，`[反向] 判对 ✓` 印了一路，路径抹平那段其实没被测过。
        # 现在喂真会被留下的行，且改走 clone_agrees（它要求非空，空对空不再算一致）。
        ("路径痕迹抹平后两侧才可比（不抹平就恒不等）",
         clone_agrees(f"闭合判定 读 {base}/a.md items=1\n",
                      f"闭合判定 读 {other}/a.md items=1\n", base, other)[0], True),
        ("外域未跟踪测试要被排除",
         is_foreign_untracked("user-server/internal/service/payment_test.go"), True),
        ("同目录自己的测试不许被连带排除",
         is_foreign_untracked("user-server/internal/service/inbox_ingress_r23_occurrence_test.go"),
         False),
        ("库连接参数要被抹掉、PATH 要留住", _env_case(), True),
        ("覆盖/删除/外域三类要分对", _overlay_case(), True),
        # 判决行不许只打在 stdout：台账钉的是**这个脚本自己写盘的那份取证**里的判定行。
        # 早先 `log.write_text(out)` 在 `print(判决行)` 之前 ⇒ 盘上那份永远缺这一行，
        # 而台账的 `must_say` 正是这一行——这条引用只可能靠"手抄 stdout 补进文件"兑现过。
        ("判决行要进脚本自己写的取证（绿侧）",
         "克隆面：rc=0，门在私有克隆里跑得出与工作树逐字一致的读数" in _disk_report(0), True),
        ("红侧判决行同样入件、且不许混进绿字样",
         ("克隆面：有红，见上" in _disk_report(1)) and ("rc=0，门在私有克隆" not in _disk_report(1)),
         True),
    ]
    n = 0
    for label, got, want in cases:
        ok = bool(got) == want
        n += 1 if ok else 0
        print(f"[反向] {label:<34} {'判对 ✓' if ok else '判错 ✗'}（实得 {got}，期望 {want}）")
    print(f"===== run-gate-in-clone --selftest：{n}/{len(cases)} 格判对 =====")
    return 0 if n == len(cases) else 1


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("ledgers", nargs="*", default=DEFAULT_LEDGERS)
    ap.add_argument("--round", default="R22-protocol")
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args()
    if a.selftest:
        return selftest()
    for rel in a.ledgers:
        if not (ROOT / rel).is_file():
            raise SystemExit(f"台账不存在，先跑 scripts/build-review-ledger.py 生成：{rel}")

    overlay, skip, deleted = lane_overlays()
    if not overlay:
        raise SystemExit("工作树没有任何未提交改动可装：这趟测的是 HEAD，不是本轮的字节——宁可不跑")
    lines = [f"$ python3 scripts/run-gate-in-clone.py {' '.join(a.ledgers)}",
             f"工作树 HEAD={git('rev-parse','--short','HEAD').strip()}  "
             f"装进克隆的覆盖文件 {len(overlay)} 个；排除外域未跟踪测试 {len(skip)} 个；"
             f"克隆里删除 {len(deleted)} 个",
             "排除的外域未跟踪测试：" + (" ".join(skip) or "（无）"),
             "子进程环境抹掉的连接参数（按键名，值不落日志）："
             + (" ".join(sorted(k for k in os.environ if is_db_var(k))) or "（本就没有）"), ""]
    tmp = Path(tempfile.mkdtemp(prefix="r22p-clone-"))
    clone = tmp / "gate-clone"
    rc_all = 0
    try:
        make_clone(clone)
        for p in overlay:
            d = clone / p
            d.parent.mkdir(parents=True, exist_ok=True)
            shutil.copy2(ROOT / p, d)
        for p in deleted:
            f = clone / p
            if f.is_file():
                f.unlink()
        for rel in a.ledgers:
            rc_wt = subprocess.run([sys.executable, GATE, rel], cwd=ROOT,
                                   capture_output=True, text=True)
            wt_out = rc_wt.stdout + rc_wt.stderr
            rc_cl, cl_out = run_gate(clone, [rel])
            same, sum_cl, sum_wt = clone_agrees(wt_out, cl_out, ROOT, clone)
            ok = rc_wt.returncode == rc_cl == 0 and same
            rc_all |= 0 if ok else 1
            lines += [f"### {rel}",
                      f"  工作树 rc={rc_wt.returncode}  克隆 rc={rc_cl}  "
                      f"两侧汇总行逐字一致={same}（各 {len(sum_cl)}/{len(sum_wt)} 行）",
                      f"  克隆目录名 {clone.name}（≠ 仓名，顺带验项目根推导不靠硬编码仓名）"]
            lines += [f"  | {l}" for l in sum_cl] or ["  | （门没打印汇总行——那本身就是红）"]
            if not same:
                lines += ["  两侧不一致：工作树侧 →"] + [f"  W {l}" for l in sum_wt]
            if not ok:
                lines += ["  红因读数（克隆侧前 12 行）："] \
                    + [f"  > {l}" for l in cl_out.splitlines()[:12]]
        rc_st, st_out = run_gate(clone, ["--selftest"])
        lines += ["", f"### {GATE} --selftest（克隆里）", f"  rc={rc_st}",
                  "  " + (st_out.splitlines()[-1] if st_out.splitlines() else "（零输出）")]
        rc_all |= 0 if rc_st == 0 else 1
    finally:
        shutil.rmtree(tmp, ignore_errors=True)
        lines += ["", f"克隆已删除（{clone} 不留残迹；本轮零 commit，取证只在仓内日志里）"]

    log = ROOT / LOGS_REL / a.round
    out = write_clone_report(log, rc_all, lines)
    sys.stdout.write(out)
    return rc_all


if __name__ == "__main__":
    sys.exit(main())
