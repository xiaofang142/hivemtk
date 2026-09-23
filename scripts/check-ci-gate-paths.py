#!/usr/bin/env python3
"""CI 触发面守卫：被作业调用起来的门，它自己的判据文件必须在该作业的 `paths:` 里。

为什么需要这道门（渠道审计批M-11 · #63；教训来路两处）：
  1. platform 仓 `docs-link-check.yml` 那次：门接进了 CI，但 `on.push.paths` 只登记了
     业务目录，没登记门脚本自己 ⇒ "只改判据/只放宽基线"这类提交根本不重跑这道门，
     于是得到一道**看起来在跑、实际永远绿**的门。
  2. 本仓 §23.16 第 5 条把同一句话写成了 `scripts/check-test-nil-deref.py` 接 CI 的前置条件，
     而"这次接对了"没有任何东西守着——下一次有人复制粘贴作业、或嫌 paths 长删两行，
     缺陷就回来了。本门把"接 CI 时记得写 paths"从口头规矩换成每次提交都跑的判据。

判据（任一不成立即 rc=1）：
  1. 逐份工作流扫出所有"步骤里真跑了某个 `scripts/**.py|sh`"的站点；
  2. 每个站点的门脚本自身路径，**以及**从该门源码里派生出的判据文件
     （字面量形如 `xxx.baseline` / `xxx.registry`，且在磁盘上真存在）——
     必须逐条出现在该工作流 `on.push.paths` 与 `on.pull_request.paths` 里；
  3. 该事件若**整体没有** `paths:` 过滤（＝每次 push 都触发）⇒ 判为满足，但必须打印出来，
     不许静默通过。

派生而不是手抄：判据集合从门脚本源码现抓（同 [[feedback-mutation-battery-hygiene]] 的
"还原表由 patch() 自动登记而非手写"、[[project-gate-scope-blind-spots]] ㉗ 的教训——
判据依赖手抄表时，最先失效的一定是表本身）。派生结果为 0 份时本门会打印数额，
用例 G3 断言它非空，防"正则失效退化成恒绿"。

口径边界（把"没数到"和"没问题"分开写）：
  - 只认**字面**文件名：门若用变量拼基线名（`BASE_DIR / f"{name}.baseline"`）抓不到 ⇒ 漏锁；
  - 输入在 `docs/`、`user-server/` 等业务树里的，**不要求**进 `paths:`：加 `docs/**` 会让每次
    文档提交拉起整条 CI 链（十几个作业），那是成本判断不是漏洞，本门只把它们打印成"输入面（不要求）"；
  - `paths-ignore` 不参与判定（本仓无一处使用，见打印的 `paths-ignore 站点 0`）；
  - 只扫 `.github/workflows/*.yml|*.yaml`；可复用工作流的 `uses:` 调用不在本门视野内。

执行入口：本地 `python3 scripts/check-ci-gate-paths.py`；已接 CI（`lint.yml` 的
`Workflow refs integrity` 作业，与 `check-action-runtime`／`check-ci-step-coverage` 同处）。
牙齿：`bash scripts/check-ci-gate-paths.test.sh`（G1 摘掉 paths 必红／G2 补齐必绿／
G2b 无过滤须明说／G3 真仓库计数对账／G4 缺目录退 2）。
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # 环境前提缺位 ≠ 判据成立
    print("ENV-BROKEN：缺 PyYAML（判据无法解析工作流，本趟未判定）", file=sys.stderr)
    sys.exit(2)

# 步骤里"真跑了某个脚本"的站点：只认 repo 相对路径写法，避免把注释里的文件名数进来
STEP_SCRIPT = re.compile(r"(?:^|[\s\"'&|(])((?:scripts|[\w.\-/]+)/[\w.\-]+\.(?:py|sh))\b")
# 门源码里的判据文件字面量（基线／注册表），按 [[feedback-cli-toolchain-gotchas]] 锚死后缀
JUDGMENT_FILE = re.compile(r"[\w.\-]+\.(?:baseline|registry)\b")
# 输入面（只打印、不要求进 paths）
DOC_INPUT = re.compile(r"[\w.\-/]+\.md\b")


def paths_of(handler: dict | None) -> tuple[bool, list[str]]:
    """返回 (该事件是否存在, paths 列表)。列表为空且事件存在＝无过滤（每次触发）。"""
    if not isinstance(handler, dict):
        return False, []
    p = handler.get("paths")
    if p is None:
        return True, []
    return True, [str(x) for x in p]


def covered(needle: str, patterns: list[str]) -> bool:
    """保守匹配：字面相等，或 `dir/**` 前缀包含。别的 glob 一律不算覆盖（宁可多要求一行）。"""
    for pat in patterns:
        if pat == needle:
            return True
        if pat.endswith("/**") and needle.startswith(pat[: -len("/**")] + "/"):
            return True
    return False


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=".", help="仓库根（用于推导 .github/workflows 与 scripts）")
    args = ap.parse_args()

    root = Path(os.path.abspath(args.repo))
    wdir = root / ".github" / "workflows"
    if not wdir.is_dir():
        print(f"❌ 工作流目录不存在：{wdir}（没跑过，不是绿）", file=sys.stderr)
        return 2

    wfiles = sorted(p for p in wdir.iterdir() if p.suffix in (".yml", ".yaml"))
    if not wfiles:
        print("❌ 零个工作流文件被扫到（SKIP 不算 PASS）", file=sys.stderr)
        return 2

    sites = 0
    derived_total = 0
    doc_inputs = 0
    ignore_sites = 0
    problems: list[str] = []
    notes: list[str] = []

    for wf in wfiles:
        try:
            doc = yaml.safe_load(wf.read_text(encoding="utf-8"))
        except Exception as exc:  # noqa: BLE001
            problems.append(f"{wf.name}: YAML 解析失败 ⇒ {exc}")
            continue
        if not isinstance(doc, dict):
            problems.append(f"{wf.name}: 顶层不是映射")
            continue
        # `on` 在 YAML 1.1 里会被解析成布尔 True——本仓所有工作流都吃到这个坑，必须两头都认
        trig = doc.get("on", doc.get(True))
        if not isinstance(trig, dict):
            notes.append(f"{wf.name}: 无触发器，跳过")
            continue
        for ev in ("push", "pull_request"):
            if "paths-ignore" in (trig.get(ev) or {}):
                ignore_sites += 1

        handlers = {ev: paths_of(trig.get(ev)) for ev in ("push", "pull_request")}

        for job_id, job in (doc.get("jobs") or {}).items():
            if not isinstance(job, dict):
                continue
            for step in job.get("steps") or []:
                if not isinstance(step, dict):
                    continue
                run = step.get("run")
                if not isinstance(run, str):
                    continue
                for gate in sorted(set(STEP_SCRIPT.findall(run))):
                    gate_path = root / gate
                    if not gate_path.is_file():
                        # 步骤里写了个不存在的路径（多半是子目录脚本或注释残留）：只记不判
                        notes.append(f"{wf.name}:{job_id} 引用 {gate} 不在磁盘上（不判定）")
                        continue
                    sites += 1
                    required = [gate]
                    src = gate_path.read_text(encoding="utf-8", errors="replace")
                    for name in sorted(set(JUDGMENT_FILE.findall(src))):
                        cand = gate_path.parent / name
                        if not cand.is_file():
                            cand = root / "scripts" / name
                        if cand.is_file():
                            required.append(str(cand.relative_to(root)))
                    for name in sorted(set(DOC_INPUT.findall(src))):
                        if (root / name).is_file():
                            doc_inputs += 1
                    derived_total += len(required) - 1

                    for ev, (present, pats) in handlers.items():
                        if not present:
                            continue
                        if not pats:
                            notes.append(f"{wf.name}:{job_id} [{ev}] 无 paths 过滤⇒每次触发（{gate} 天然被覆盖）")
                            continue
                        for need in required:
                            if not covered(need, pats):
                                problems.append(
                                    f"{wf.name} 的 {ev}.paths 缺 `{need}`"
                                    f"（该路径被作业 {job_id} 的步骤实际执行 ⇒ 改它不会重跑这道门）"
                                )

    print(f"工作流 {len(wfiles)} 份 · 门站点 {sites} 处 · 派生判据文件 {derived_total} 份"
          f" · 输入面 md 引用 {doc_inputs} 处（不要求进 paths）· paths-ignore 站点 {ignore_sites}")
    for n in sorted(set(notes)):
        print(f"  · {n}")
    if problems:
        print("❌ 触发面缺口：")
        for p in sorted(set(problems)):
            print(f"   {p}")
        return 1
    print("绿：每个被作业实际执行的门，其判据文件都在触发 paths 里（或该事件本就无过滤）")
    return 0


if __name__ == "__main__":
    sys.exit(main())
