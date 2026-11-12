#!/usr/bin/env python3
"""check-action-runtime.py —— action 运行时（runs.using）静态闸

为什么需要它（2026-09-22 第四十七轮 R33）：
  GitHub 在 2026-09-23 从 runner 上移除 node20。这个仓库里"哪些 action 还声明
  node20"**无法从工作流文件本身看出来** —— `uses: actions/checkout@v4` 里没有任何
  运行时信息，运行时写在那个 action 自己仓库的 `action.yml` 的 `runs.using` 字段里。
  上一轮的"干净文件已全部离开 node20"就是拿版本号新旧猜出来的，没逐条回读 action.yml，
  结果漏了 `softprops/action-gh-release@v2`（node20）和
  `slsa-verifier/actions/installer@v2.7.1`（node20）两处。
  CI 日志里的那句 "Node 20 is being deprecated" 也**不能**当判据：它只在
  **真正执行到的** action 上才印 —— 只在 tag 上跑的 release 流水线，push 日志里
  永远 0 条告警，看起来像已迁移。（runner 现在其实是"降级放行 + 告警"，不是硬失败。）

判据：
  R1 每个 `uses:`（job 级与 step 级都算）的 action，其 `runs.using` 必须在
     RUNTIMES 表里。查不到 ⇒ rc=2，要求作者去读那个 tag 的 action.yml 再登记，
     而不是放它静默通过。
  R2 `runs.using` ∈ BAD_RUNTIMES（node16/node20）⇒ rc=1，除非该 (文件名, pin)
     在 GRANDFATHER 里且站点数 ≤ 上界。
  R3 GRANDFATHER 条目在该文件里匹配不到任何站点 ⇒ STALE，rc=1。豁免账本必须
     随迁移一起收，不许留着空条目把"已迁完"伪装成"仍豁免"。
  R4 站点数 > 上界 ⇒ rc=1。上界是**只降不升**的棘轮：迁走站点会绿，新增会红。
  R5 可复用工作流（值含 `.yml`）、本地 action（`./`）、容器 action（`docker://`）
     没有 node 运行时 ⇒ 跳过，不计入。
  R6 一个工作流文件都没扫到 ⇒ rc=2（缺产物是 SKIP 不是 PASS）。

用法： check-action-runtime.py [--repo ROOT] [--grandfather FILE:PIN=N] [工作流文件...]
退出码：0=干净 / 1=有命中 / 2=输入无效（宁可报错也不静默零扫描）
"""
from __future__ import annotations

import argparse
import collections
import os
import sys

try:
    import yaml
except ImportError:  # pragma: no cover
    print("❌ 需要 PyYAML（pip install pyyaml）", file=sys.stderr)
    sys.exit(2)

# GitHub 在 job 里把 on 解析成 True（YAML 1.1 布尔），统一还原成字符串键
TRUTHY = {True: "on", False: "off", None: "null"}

BAD_RUNTIMES = {"node16", "node20"}

# pin -> runs.using。回读方式：
#   gh api repos/<owner>/<repo>/contents/<action.yml 或子路径>/action.yml?ref=<tag> --jq .content | base64 -d
# 全部为 2026-09-22 逐个真读所得；composite / 子路径 action 也要读，不能按版本号推。
RUNTIMES = {
    "DavidAnson/markdownlint-cli2-action@v24": "node24",
    "actions/checkout@v4": "node20",
    "actions/checkout@v7": "node24",
    "actions/configure-pages@v6": "node24",
    "actions/deploy-pages@v5": "node24",
    "actions/download-artifact@v8": "node24",
    "actions/setup-go@v5": "node20",
    "actions/setup-go@v7": "node24",
    "actions/setup-node@v4": "node20",
    "actions/setup-node@v7": "node24",
    "actions/setup-python@v7": "node24",
    "actions/upload-artifact@v4": "node20",
    "actions/upload-artifact@v7": "node24",
    "actions/upload-pages-artifact@v3": "composite",
    # v5 的 runs.using 于 2026-10-14 现读 v5 tag 的 action.yml（runs: using: composite），
    # 非按"v5 比 v3 新所以大概已迁 node24"推出来：它是 composite，本来就没有 node 运行时，
    # 抬 major 号不会改变这一格。website-pages.yml 已在合并 upstream/master 时从 v3 抬到 v5。
    "actions/upload-pages-artifact@v5": "composite",
    "codecov/codecov-action@v4": "node20",
    "codecov/codecov-action@v7": "composite",
    "gitleaks/gitleaks-action@v2": "node20",
    # v3 的 runs.using 于 2026-10-13 现读 v3 tag 的 action.yml（runs: using: "node24"）。
    "gitleaks/gitleaks-action@v3": "node24",
    "golangci/golangci-lint-action@v8": "node20",
    # v9 的 runs.using 取自 v9 tag 的 action.yml（runs: using: "node24"），非按版本号推断。
    # user-server-ci.yml 已在 2026-10-08 合并 upstream/master 时从 v8 抬到 v9。
    "golangci/golangci-lint-action@v9": "node24",
    "lycheeverse/lychee-action@v2": "composite",
    "release-drafter/release-drafter@v7": "node24",
    "slsa-framework/slsa-verifier/actions/installer@v2.7.1": "node20",
    "softprops/action-gh-release@v3": "node24",
    # —— SHA pin 条目（2026-11-13 逐个真读该 SHA 的 action.yml 所得，非按版本推断）：
    # Dependabot 已把全部 uses 抬成 SHA 锁定，tag 形条目对站点匹配失效，逐条登记。
    "DavidAnson/markdownlint-cli2-action@21c1be1b93ad9ed58fa840aacc3f279cde2a72ff": "node24",
    "actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1": "node24",
    "actions/download-artifact@9000827ccba6bdab643e8b6fd33ac0654aef8333": "node24",
    "actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e": "node24",
    "actions/setup-node@949feb2413d6458794dcd2491c4babbbce0c15c1": "node24",
    "actions/setup-python@6d4fd22b20465ccc0f7354476588e8df7c9ca0e0": "node24",
    "actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9": "node24",
    "codecov/codecov-action@303a32d7a59b442fa8d48b6a1cc6825c09c847a5": "composite",
    "gitleaks/gitleaks-action@e0c47f4f8be36e29cdc102c57e68cb5cbf0e8d1e": "node24",
    "golangci/golangci-lint-action@ba0d7d2ec06a0ea1cb5fa41b2e4a3ab91d21278a": "node24",
    "lycheeverse/lychee-action@e7477775783ea5526144ba13e8db5eec57747ce8": "composite",
    "release-drafter/release-drafter@978bb0b30c5ee8cdd332ced253758157150d43f0": "node24",
    "slsa-framework/slsa-verifier/actions/installer@ea584f4502babc6f60d9bc799dbbb13c1caa9ee6": "node20",
    "softprops/action-gh-release@8a0cd651e5b2365bd640854a3fa5dfc70e4875f3": "node24",
}

# (工作流文件名, pin) -> (原因, 站点数上界)。
# 豁免账本必须随迁移一起收（R3）：2026-11-13 user-server-ci.yml 两处 codecov 已从
# v4(node20) 抬到 v7(composite，与 ci-bridge.yml 同 pin)，条目随之删除——不再留空账。
# 仅剩 slsa-verifier installer 一条：上游 v2.7.1 的 runs.using 仍是 node20（2026-11-13
# 现读该 tag action.yml 证实），无 node24 可抬目标。
# 每条都必须有真站点对着：站点被抬走而不删条目，R3 会直接报 STALE（曾有
# actions/setup-go@v5 那一条，user-server-ci.yml 全量升到 v7 后账本没跟着收）。
GRANDFATHER = {
    ("slsa.yml", "slsa-framework/slsa-verifier/actions/installer@ea584f4502babc6f60d9bc799dbbb13c1caa9ee6"):
        (1, "上游最新 tag 仍是 v2.7.1=node20，无可抬目标；等上游发 node24 版"),
}


def norm_key(k):
    return TRUTHY.get(k, k)


def norm_tree(node):
    if isinstance(node, dict):
        return {norm_key(k): norm_tree(v) for k, v in node.items()}
    if isinstance(node, list):
        return [norm_tree(v) for v in node]
    return node


def is_skippable(pin):
    """R5：没有 node 运行时可言的 uses 形态。"""
    return (".yml" in pin) or pin.startswith("./") or pin.startswith("docker://") \
        or pin.startswith("run:")


def iter_uses(doc):
    """产出 (job 名, step 序号或 '<job>', uses 值)。job 级 uses 也算。"""
    jobs = doc.get("jobs") or {}
    for jname, job in jobs.items():
        if not isinstance(job, dict):
            continue
        juses = job.get("uses")
        if isinstance(juses, str):
            yield jname, "<job>", juses
        for idx, step in enumerate(job.get("steps") or [], 1):
            if isinstance(step, dict) and isinstance(step.get("uses"), str):
                yield jname, str(idx), step["uses"]


def parse_grandfather(specs):
    """--grandfather FILE:PIN=N 覆盖内置账本（测试用）。返回 dict 或 None。"""
    if not specs:
        return None
    table = {}
    for spec in specs:
        left, _, cap = spec.rpartition("=")
        base, _, pin = left.rpartition(":")
        if not base or not pin:
            print(f"❌ --grandfather 形如 FILE:PIN=N，收到 {spec!r}", file=sys.stderr)
            sys.exit(2)
        try:
            table[(base, pin)] = (int(cap), "命令行覆盖")
        except ValueError:
            print(f"❌ --grandfather 上界不是整数：{spec!r}", file=sys.stderr)
            sys.exit(2)
    return table


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=".", help="仓库根（用于打印与推导相对路径）")
    ap.add_argument("--grandfather", action="append", default=[],
                    help="FILE:PIN=N，覆盖内置豁免账本（仅供测试）")
    ap.add_argument("files", nargs="*", help="不传则扫 <repo>/.github/workflows/*.yml")
    args = ap.parse_args()

    root = os.path.abspath(args.repo)
    print(f"项目根: {root}")

    if args.files:
        paths = args.files
    else:
        wdir = os.path.join(root, ".github", "workflows")
        if not os.path.isdir(wdir):
            print(f"❌ 工作流目录不存在：{wdir}", file=sys.stderr)
            return 2
        paths = sorted(
            os.path.join(wdir, f) for f in os.listdir(wdir)
            if f.endswith(".yml") or f.endswith(".yaml")
        )

    if not paths:  # R6
        print("❌ 零个工作流文件被扫到（SKIP 不算 PASS）", file=sys.stderr)
        return 2

    grandfather = parse_grandfather(args.grandfather) or GRANDFATHER

    sites = collections.Counter()      # (basename, pin) -> 次数
    located = collections.defaultdict(list)
    scanned_bases = set()
    unknown, skipped, scanned = set(), 0, 0
    broken = []

    for path in paths:
        base = os.path.basename(path)
        try:
            with open(path, "r", encoding="utf-8") as fh:
                doc = norm_tree(yaml.safe_load(fh))
        except Exception as exc:  # 解析失败不能当"没命中"
            broken.append(f"{base}: {exc}")
            continue
        if not isinstance(doc, dict):
            broken.append(f"{base}: 顶层不是 mapping")
            continue
        scanned += 1
        scanned_bases.add(base)
        for jname, idx, pin in iter_uses(doc):
            if is_skippable(pin):
                skipped += 1
                continue
            located[(base, pin)].append(f"{base}:{jname}#{idx}")
            sites[(base, pin)] += 1
            if pin not in RUNTIMES:
                unknown.add(pin)

    rc = 0

    def raise_rc(n):
        # 退出码只许往上走：未知 pin（2）不许被后面的 STALE/命中（1）盖掉，
        # 否则「表写歪了」会伪装成「只是有站点没豁免」，处置动作完全不同。
        nonlocal rc
        rc = max(rc, n)

    if broken:
        print("❌ 工作流解析失败（宁可判红也不静默放行）：", file=sys.stderr)
        for line in broken:
            print(f"   {line}", file=sys.stderr)
        return 2

    if unknown:
        print("❌ 以下 pin 不在 RUNTIMES 表里，无法判定其 runs.using：", file=sys.stderr)
        for pin in sorted(unknown):
            print(f"   {pin}", file=sys.stderr)
        print("   去读该 tag 的 action.yml（子路径 action 要读子路径下那份），"
              "把 runs.using 登记进表；不要凭版本号猜。", file=sys.stderr)
        raise_rc(2)

    bad = {k: v for k, v in sites.items() if RUNTIMES.get(k[1]) in BAD_RUNTIMES}

    over = []
    for key, count in sorted(bad.items()):
        cap = grandfather.get(key)
        if cap is None:
            continue
        if count > cap[0]:
            over.append((key, count, cap[0]))
    live_bad = []
    for key, count in sorted(bad.items()):
        if key in grandfather:
            continue
        live_bad.append((key, count))

    if live_bad:
        print(f"❌ {len(live_bad)} 个 pin 声明了会被移除的运行时且无豁免"
              f"（共 {sum(c for _, c in live_bad)} 处站点）：", file=sys.stderr)
        for (base, pin), count in live_bad:
            print(f"   {base}: {pin} ×{count} -> using={RUNTIMES[pin]}", file=sys.stderr)
            for where in located[(base, pin)]:
                print(f"      at {where}", file=sys.stderr)
        raise_rc(1)

    if over:
        print("❌ 豁免文件里新增了超出上界的 node20 站点（上界只降不升）：", file=sys.stderr)
        for (base, pin), count, cap in over:
            print(f"   {base}: {pin} 实有 {count} > 上界 {cap}", file=sys.stderr)
        raise_rc(1)

    stale = []
    for key, (cap, reason) in sorted(grandfather.items()):
        # 只对"本次真扫到的文件"要求账本对得上：按显式文件清单局部跑时，
        # 别的文件的豁免条目天然匹配不到，全量连坐会把一次正常的单文件检查变成红墙。
        if key[0] not in scanned_bases:
            continue
        if sites.get(key, 0) == 0:
            stale.append((key, reason))
    if stale:
        print("❌ 豁免账本里有匹配不到站点的条目（该文件已迁完或已改名，请删条目）：",
              file=sys.stderr)
        for (base, pin), reason in stale:
            print(f"   {base}: {pin}  （原由：{reason}）", file=sys.stderr)
        raise_rc(1)

    exempt_hits = sum(count for key, count in bad.items() if key in grandfather)
    print(f"扫描工作流 {scanned} 份；uses 站点 {sum(sites.values())} 处"
          f"（可判定），跳过 {skipped} 处（可复用工作流/本地/容器）")
    print(f"node20/node16 命中 {sum(bad.values())} 处："
          f"未豁免 {sum(c for _, c in live_bad)}，豁免内 {exempt_hits}")
    if rc == 0:
        print("✅ 无未豁免的弃用运行时站点")
    return rc


if __name__ == "__main__":
    sys.exit(main())
