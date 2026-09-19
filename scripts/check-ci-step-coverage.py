#!/usr/bin/env python3
"""CI 步骤执行覆盖率：找出「写好了但从来没跑过」的门禁步骤。

为什么需要它（2026-09-19 第二十五轮）：user-server 的 golangci-lint 步骤因
pin 的二进制构建版本低于 config 目标版本，从 2026-08-11 起恒定 exit 3；它排在
go build / 单测 / 覆盖率之前，于是那几步在 master 上一直是 skipped —— 五周里
「CI 在红」掩盖了「CI 有一半根本没跑」。这种形状靠人翻日志未必想得到，跑一次本脚本就能列出来。

判据（对每个步骤统计窗口内所有 run 的 conclusion 分布）：
  NEVER_RUN   —— 出现过 >=--min-presence 次，但一次都没执行过（全是 skipped / 缺失）
  ALWAYS_RED  —— 执行过，且每次都失败（等于一个常亮的红灯，多半在掩盖下游）
  OK          —— 至少成功过一次
另：本脚本自身零覆盖也直接判错（拿不到 run / 拿不到 job 一律 rc=2），
    不允许「没数据」被印成「没问题」。

已知局限（2026-09-20 在 hivemtk@master 实测窗口内跑出 8 个 NEVER_RUN，其中 2 个是假阳）：
  1. `if:` 守卫的合法跳过会被当成「门没跑」。例：SBOM workflow 的
     `Attach SBOM to release (only on tag)` 只在打 tag 时执行，主干 run 上恒 skipped。
     这类只能靠人判 —— 判据（conclusion 分布）看不到 workflow 里的 `if:`，
     所以命中 NEVER_RUN 后要先读 yml 再定性，别直接当缺陷修。
     （housekeeping 步已在代码里排除，见 housekeeping()；`if:` 守卫的没法自动排除。）
  2. GitHub 自己注入的 housekeeping 步（`Set up job` / `Post *` / `Initialize containers` /
     `Complete job` …）不是 authored 门禁。例：`Post Set up Go` 在 5 次 skipped 里长得和
     真死掉的门一模一样，但它 skip 只说明上游 `Set up Go` 没成功。已按名称排除出
     NEVER_RUN（仍照常打印；它们的 failure 仍算 ALWAYS_RED，因为 Post 步失败是真失败）。
  3. 取数依赖 `gh auth` 已登录 **且** 仓库有 GitHub 远端 ——
     hivemtk-platform（只有 gitee origin）与 assetdpo 的 runs 不在 GitHub Actions 上，
     对它们跑本脚本只会得到 rc=2 的「零覆盖」，那不是缺陷，是没有数据。

用法：
  python3 scripts/check-ci-step-coverage.py --repo .            # 从 git remote 推 GitHub slug
  python3 scripts/check-ci-step-coverage.py --slug owner/repo --runs 40
退出码：0 = 无 NEVER_RUN / ALWAYS_RED；1 = 有命中；2 = 取数失败或零覆盖。

定位：**人工核查用的诊断脚本，故意不挂进 CI** —— 本仓有合法 `if:` 跳过的步骤
（局限 1），挂成门就是永远红，等于亲手再造一个「常亮红灯掩盖下游」（正是它要查的形状）。
需要它时本地跑，或在临时 job 里跑 `|| true` 只看输出。
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
from collections import defaultdict

# `git remote -v` 的行尾还挂着 " (fetch)" / " (push)"，所以不能拿 $ 收尾锚定。
GITHUB_SSH = re.compile(r"github\.com[:/]([\w.-]+)/([\w.-]+?)(?:\.git)?(?=\s|\()")

# GitHub 自己注入的 housekeeping 步，不是 authored 门禁（见 docstring 局限 2）。
HOUSEKEEPING_EXACT = {"Set up job", "Initialize containers", "Stop containers", "Complete job"}


def housekeeping(step: str) -> bool:
    return step in HOUSEKEEPING_EXACT or step.startswith("Post ")


def gh(*args: str) -> object:
    """跑 gh api，失败返回 None（由调用方按零覆盖处理）。"""
    try:
        out = subprocess.run(("gh", "api", *args), capture_output=True, text=True, timeout=120)
    except (OSError, subprocess.TimeoutExpired) as exc:
        print(f"  gh api 调用异常：{exc}", file=sys.stderr)
        return None
    if out.returncode != 0:
        print(f"  gh api {' '.join(args)} 失败：{out.stderr.strip()[:200]}", file=sys.stderr)
        return None
    try:
        return json.loads(out.stdout or "null")
    except json.JSONDecodeError:
        print("  gh api 返回非 JSON", file=sys.stderr)
        return None


def guess_slug(repo: str) -> str | None:
    out = subprocess.run(("git", "-C", repo, "remote", "-v"), capture_output=True, text=True)
    for line in out.stdout.splitlines():
        # `git remote -v` 每行形如 "upstream  git@github.com:owner/repo.git (fetch)"，
        # 所以按整行匹配，别去取最后一个 token（那是 fetch/push）。
        m = GITHUB_SSH.search(line)
        if m:
            return f"{m.group(1)}/{m.group(2)}"
    return None


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--repo", default=".", help="仓库根（用于从 remote 推 GitHub slug）")
    ap.add_argument("--slug", help="owner/repo；给出后忽略 --repo 的推断")
    ap.add_argument("--runs", type=int, default=30, help="回看最近多少次 run（默认 30）")
    ap.add_argument("--min-presence", type=int, default=3, help="至少出现几次才判 NEVER_RUN（默认 3）")
    args = ap.parse_args()

    slug = args.slug or guess_slug(args.repo)
    if not slug:
        print("判定失败：既没给 --slug，也没在 git remote 里找到 GitHub 地址", file=sys.stderr)
        return 2

    runs_doc = gh(f"repos/{slug}/actions/runs?per_page=100")
    runs = (runs_doc or {}).get("workflow_runs") or []
    runs = [{k: r.get(k) for k in ("id", "name", "head_branch", "created_at")} for r in runs]
    if not runs:
        print(f"零覆盖：{slug} 一个 run 都没取到（gh 未登录？仓库无 GitHub 远端？）", file=sys.stderr)
        return 2

    runs = sorted(runs, key=lambda r: r.get("created_at") or "", reverse=True)[: args.runs]
    # 只统计默认分支的 push，避免 PR/特性分支的偶发 run 干扰「master 门禁是否跑过」的判断
    branch = subprocess.run(("git", "-C", args.repo, "rev-parse", "--abbrev-ref", "HEAD"),
                            capture_output=True, text=True).stdout.strip() or "master"
    runs = [r for r in runs if r.get("head_branch") == branch] or runs
    print(f"══════ CI 步骤执行覆盖率（{slug}，最近 {len(runs)} 次 {branch} run）══════")
    if not runs:
        print("零覆盖：窗口内没有可统计的 run", file=sys.stderr)
        return 2

    stats: dict[tuple[str, str, str], dict[str, int]] = defaultdict(lambda: defaultdict(int))
    for r in runs:
        jobs_doc = gh(f"repos/{slug}/actions/runs/{r['id']}/jobs?per_page=100")
        jobs = (jobs_doc or {}).get("jobs") or []
        if not jobs:
            continue
        for j in jobs:
            jn = j.get("name") or "?"
            for s in j.get("steps") or []:
                sn = s.get("name") or "?"
                stats[(r.get("name") or "?", jn, sn)][s.get("conclusion") or "none"] += 1

    if not stats:
        print("零覆盖：取到了 run，却一个 job/step 都没读到", file=sys.stderr)
        return 2

    dead, always_red = [], []
    by_job: dict[str, list] = defaultdict(list)
    for (wf, jn, sn), cnt in stats.items():
        executed = sum(v for k, v in cnt.items() if k not in ("skipped", "none"))
        ok = cnt.get("success", 0)
        fail = sum(v for k, v in cnt.items() if k in ("failure", "timed_out", "cancelled"))
        present = executed + cnt.get("skipped", 0)
        by_job[f"{wf} / {jn}"].append((sn, executed, ok, fail, cnt.get("skipped", 0)))
        if executed == 0 and present >= args.min_presence and not housekeeping(sn):
            dead.append((wf, jn, sn, present))
        elif executed >= args.min_presence and ok == 0 and fail == executed:
            always_red.append((wf, jn, sn, fail))

    for job in sorted(by_job):
        print(f"\n[{job}]")
        for sn, executed, ok, fail, skipped in sorted(by_job[job]):
            line = f"  {sn[:52]:<52} 执行 {executed:>3}  成 {ok:>3}  败 {fail:>3}  跳 {skipped:>3}"
            if executed == 0:
                line += "   ← 从未执行" if not housekeeping(sn) else "   ← 未执行（housekeeping，不计门禁）"
            elif ok == 0:
                line += "   ← 从未通过"
            print(line)

    print(f"\n══════ 结论 ══════")
    print(f"  统计步骤 {len(stats)} 个；NEVER_RUN {len(dead)} 个；ALWAYS_RED {len(always_red)} 个")
    for wf, jn, sn, present in dead:
        print(f"  · NEVER_RUN  {wf} / {jn} / {sn}（出现 {present} 次，全被跳过）")
    for wf, jn, sn, fail in always_red:
        print(f"  · ALWAYS_RED {wf} / {jn} / {sn}（执行 {fail} 次，次次失败）")
    if dead or always_red:
        print("  ⇒ 这些门禁没有产生过任何证据；先判「门跑不起来」还是「门查出了问题」，再决定修门还是修存量。")
        return 1
    print("  ✓ 窗口内每个步骤都至少执行并成功过一次")
    return 0


if __name__ == "__main__":
    sys.exit(main())
