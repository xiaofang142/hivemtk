#!/usr/bin/env python3
"""批19g 变异电池：数据保留裁剪的「分批 + 谓词 + 收敛」三件事，两侧各自逐格验牙。

为什么要有这条电池：本批改的是一条**注释承诺**（分批 5000）与实现不符。
裁剪这种「后台每小时悄悄跑一次」的代码，测试全绿只说明今天清得干净，说明不了：
- 批还在不在（H1/H4）：把批放大到 100 倍 = 事实上的整表一条语句，正是本批修掉的病；
- cutoff 还作不作数（H2/H5）：谓词写恒真会把未到期审计一起删；判据写反会只删未到期的——
  两种都「清空了行数」，只有「未超期那 2 行还在不在」这条腿看得见；
- 循环收不收敛（H3/H6）：只跑一批就 return，日志里看着成功、库里欠着全量，
  下一轮才继续——而 retention 每小时才动一次，欠账没人知道。
command_log（删行）与 llm_plans（清文本）是两处实现，两侧各四格（批/cutoff 写反/cutoff 恒真/提前收工）；
只有一侧有牙的话，另一侧被"顺手简化"就再没人拦。

口径（沿用批16/17/18/19 各电池）：控制组必须 rc==0、ran==2、skip==0；每格写完即还原并比 md5；
锚点命中恰好一次；注码先过 gofmt -e（编译红不算杀）；红了必须点出**这一格该红的那条腿**。
注码一律保持「占位符数 = 实参数」，否则红是 gorm 报参数不匹配、不是判据错——那种红点不出病。
只在私有 --shared 克隆里注码，绝不碰共享工作树（并行会话在里面提交）。

用法：python3 scripts/mut_prune_batch_b19g.py [--keep] [--clone DIR]
"""
from __future__ import annotations

import argparse
import hashlib
import os
import re
import shutil
import subprocess
import sys
from pathlib import Path
from mut_dispose import dispose, dispose_at_exit, leave_for_evidence, workdir

ROOT = Path(__file__).resolve().parent.parent
REPO = "internal/browser_automation/repository"
GO_OVERLAY = [
    f"user-server/{REPO}/command_log.go",
    f"user-server/{REPO}/llm_plan.go",
    f"user-server/{REPO}/retention_b19g_test.go",
]
GO_PKG = REPO
LOG = "TestB19GCommandLogPruneIsBatched"
PLAN = "TestB19GPlanSnapshotPruneIsBatched"
GO_RUN = "|".join([LOG, PLAN])
RUN_N = 2

ANSI = re.compile(r"\x1b\[[0-9;]*m")

BATCH = ".Limit(pruneBatchRows)"
BATCH_BIG = ".Limit(pruneBatchRows * 100)"
CONVERGE = "\t\tif res.RowsAffected == 0 {\n\t\t\treturn total, nil\n"
CONVERGE_TRUE = "\t\tif true {\n\t\t\treturn total, nil\n"
LOG_WHERE = ('Where("created_at < ? AND id IN (?)", cutoff,\n'
             '\t\t\t\tr.db.Model(&model.BrowserCommandLog{}).Where("created_at < ?", cutoff).Order("id ASC")')
LOG_WHERE_FLIPPED = ('Where("created_at > ? AND id IN (?)", cutoff,\n'
                     '\t\t\t\tr.db.Model(&model.BrowserCommandLog{}).Where("created_at > ?", cutoff).Order("id ASC")')
# 「cutoff 判成恒真」写成「时间判据整条撤掉」，不写成 `... OR 1=1`：
# gorm 把裸 SQL 条件用 AND 串起来时不额外补括号，`A < ? OR B AND id IN (?)` 会按
# A OR (B AND id IN) 解析——那样整表落在一条语句里吃完、清不干净就永远返回非 0，
# 跑出来是 9 分钟超时 panic（没有 --- FAIL 行），电池只会判「无法判定」。
LOG_WHERE_TRUTHY = ('Where("id IN (?)",\n'
                    '\t\t\t\tr.db.Model(&model.BrowserCommandLog{}).Where("id > 0").Order("id ASC")')
PLAN_WHERE = ('return r.db.WithContext(ctx).Model(&model.BrowserLLMPlan{}).\n'
              '\t\t\tWhere("created_at < ? AND snapshot <> \'\'", cutoff)')
PLAN_WHERE_FLIPPED = ('return r.db.WithContext(ctx).Model(&model.BrowserLLMPlan{}).\n'
                      '\t\t\tWhere("created_at > ? AND snapshot <> \'\'", cutoff)')
PLAN_WHERE_TRUTHY = ('return r.db.WithContext(ctx).Model(&model.BrowserLLMPlan{}).\n'
                     '\t\t\tWhere("snapshot <> \'\'")')


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def md5(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1）：{old[:90]!r}")
    return text.replace(old, new, 1)


def syntax_ok(src: str) -> tuple:
    p = subprocess.run(["gofmt", "-e"], input=src, capture_output=True, text=True, timeout=120)
    return p.returncode == 0, p.stderr.strip()


def cells():
    return [
        ("H1", "LOG", "删行侧把批放大到 100 倍（= 整表一条语句）",
         lambda s: sub_once(s, BATCH, BATCH_BIG, "H1"), LOG),
        ("H2", "LOG", "删行侧 cutoff 判据写反（只删未到期的）",
         lambda s: sub_once(s, LOG_WHERE, LOG_WHERE_FLIPPED, "H2"), LOG),
        ("H3", "LOG", "删行侧 cutoff 判成恒真（连未到期一起删）",
         lambda s: sub_once(s, LOG_WHERE, LOG_WHERE_TRUTHY, "H3"), LOG),
        ("H4", "LOG", "删行侧只跑一批就收工",
         lambda s: sub_once(s, CONVERGE, CONVERGE_TRUE, "H4"), LOG),
        ("H5", "PLAN", "清文本侧把批放大到 100 倍",
         lambda s: sub_once(s, BATCH, BATCH_BIG, "H5"), PLAN),
        ("H6", "PLAN", "清文本侧 cutoff 判据写反（清掉未到期的）",
         lambda s: sub_once(s, PLAN_WHERE, PLAN_WHERE_FLIPPED, "H6"), PLAN),
        ("H7", "PLAN", "清文本侧 cutoff 判成恒真（连未到期一起清）",
         lambda s: sub_once(s, PLAN_WHERE, PLAN_WHERE_TRUTHY, "H7"), PLAN),
        ("H8", "PLAN", "清文本侧只跑一批就收工",
         lambda s: sub_once(s, CONVERGE, CONVERGE_TRUE, "H8"), PLAN),
    ]


def go_prepare(dst: Path, owned: bool = False) -> Path:

    def bail(msg: str) -> None:
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声。

        收尾闸原先只接在 `main()` 的出口上，装架函数里克隆之后的每一条 raise 都把整份
        私有克隆留在临时目录（一轮 50–70MB，而磁盘常态 98% 满）。2026-09-28 在
        `mut_bill_p701.py` 上实测一次 DIRTY 停机留 72M，这一族按同一形状补齐。
        三条**不**走这里："已存在"（那份 clone/ 不是本电池建的）、"克隆失败"（目录归属
        还没定）、"md5 不一致"（"覆盖后还是不对"的字节只活在克隆里，删了就只剩一句
        "当时红过"——与 `main()` 侧还原校验同一取舍）。
        """
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)
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
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    for rel in GO_OVERLAY:
        src = ROOT / rel
        if not src.exists():
            bail(f"覆盖源缺失：{src}")
        tgt = clone / rel
        tgt.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, tgt)
        if md5(src) != md5(tgt):
            leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
            raise SystemExit(f"覆盖后 md5 不一致（装错树/写盘失败）：{rel}")
    hostenv = ROOT / "user-server" / ".env"
    if hostenv.exists():
        shutil.copy2(hostenv, clone / "user-server" / ".env")
    return clone


def go_run(clone: Path):
    root = clone / "user-server"
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOCACHE", "/tmp/gocache-b19gmut")
    envf = root / ".env"
    if envf.exists():
        for line in read(envf).splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    p = subprocess.run(["go", "test", "./" + GO_PKG + "/", "-run", GO_RUN, "-count=1", "-v"],
                       cwd=root, capture_output=True, text=True, timeout=1800, env=env)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    ran = len(re.findall(r"^=== RUN\s+(\S+)", out, re.M))
    skipped = len(re.findall(r"^--- SKIP: (\S+)", out, re.M))
    return p.returncode, killed, ran, skipped, out


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    # 作业目录必须由 workdir() 现开（不传 --clone 时是 mkdtemp 的独占目录）：上一轮没退干净就又起一轮时，
    # 两轮会共用同一个克隆——后起的那轮把前一轮的树建没了，前一轮转头就去注码**后一轮**的文件（实测两边结论全废）。
    # 早先这里靠自己拼 `/tmp/b19g-mut-<pid>` 求独占，那只挡住了"两轮"，没挡住 `--clone .`。
    tmp, owned = workdir(args.clone or None, prefix="b19g-mut-", repo_root=ROOT)
    print(f"私有作业目录：{tmp}", flush=True)

    clone = go_prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    u = clone / "user-server"
    TARGET = {"LOG": u / REPO / "command_log.go", "PLAN": u / REPO / "llm_plan.go"}
    originals = {p: md5(p) for p in TARGET.values()}
    texts = {k: read(v) for k, v in TARGET.items()}

    for tag, key, _desc, apply, _expect in cells():
        mutated = apply(texts[key])
        if mutated == texts[key]:
            raise SystemExit(f"{tag} 注码无效（替换后与原文件一致）")
        ok, err = syntax_ok(mutated)
        if not ok:
            raise SystemExit(f"{tag} 注码语法坏，跑出来只会是 build failed：{err[:200]}")
    print("[注码前置] 八格锚点各命中一次 + 注码后语法可解析\n", flush=True)

    rc, killed, ran, skipped, out = go_run(clone)
    if rc != 0 or ran != RUN_N or skipped != 0:
        print(out[-3000:])
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        raise SystemExit(f"控制组不成立 rc={rc} ran={ran}（要求 {RUN_N}）skip={skipped}——整轮判「无法判定」")
    print(f"[控制组] rc=0 ran={ran} skip=0 全绿\n", flush=True)

    survivors, broken = [], []
    try:
        for tag, key, desc, apply, expect in cells():
            tgt = TARGET[key]
            src = read(tgt)
            tgt.write_text(apply(src), encoding="utf-8")
            if md5(tgt) == originals[tgt]:
                broken.append(f"{tag} 注码未生效（文件与原内容一致）")
                continue
            rc, killed, ran, skipped, out = go_run(clone)
            tgt.write_text(src, encoding="utf-8")
            if md5(tgt) != originals[tgt]:
                leave_for_evidence("还原后 md5 不一致：现场只活在克隆里")
                raise SystemExit(f"{tag} 还原失败：md5 与原文件不一致，已停机（克隆保留 {clone}）")
            if rc == 0:
                survivors.append(tag)
                print(f"{tag:<4} {desc:<40} 存活", flush=True)
            elif expect not in killed:
                broken.append(f"{tag} 红了但没点出 {expect}（killed={killed or '空=编译红'}）")
                print(f"{tag:<4} {desc:<40} 无法判定\n{out[-1800:]}", flush=True)
            else:
                print(f"{tag:<4} {desc:<40} 杀掉  {' '.join(killed)}", flush=True)
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

    print()
    if broken:
        print("===== 电池判定：以下格无法判定，须先修注码 =====")
        for b in broken:
            print("  " + b)
        return 2
    if survivors:
        print(f"===== 电池判定：{len(survivors)} 格存活 = 裁剪有洞：{' '.join(survivors)} =====")
        return 1
    print(f"===== 电池判定：{len(cells())} 格逐格被杀，无存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
