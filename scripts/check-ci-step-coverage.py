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

第二根轴（2026-09-23 第四十六轮补）：**整个作业**从来没跑过 —— NEVER_RUN_JOB。
  原判据只数 `job.steps`，而 API 对一个被整体跳过的作业给的 steps 是**空表**：
  这类作业在旧输出里一个字符都不存在。实测漏掉的就是 lint.yml 的
  `LICENSE Compliance Scan`（`if:` 只放 schedule / workflow_dispatch，cron 是每月 1 日，
  2026-09-15 才挂上 ⇒ 窗口内 schedule run 数为 **0**，本仓这道合规门**一次都没跑过**）。
  光加这根轴会把节奏门一起判红（月度门、tag 门本就不在 push 上跑），于是同时补上
  守卫识别：读 `.github/workflows/*.yml`，带 `if:` 或 `needs:` 链上带 `if:` 的作业
  单独印成「节奏门」不计红；读不到（缺 PyYAML／文件不在）按「守卫未知」**计红**。

已知局限：
  1. `if:` 守卫的合法跳过会被当成「门没跑」——**作业级与步骤级都已自动豁免**（见
     job_guards()/cadence_expr()）：条件是 `if:` 里写着 `github.event_name` / `github.ref`
     （＝"这次 run 有没有资格跑它"），命中的印成「节奏门 / 节奏步骤」不计红。
     `always()` / `success()` / `needs.*.outputs` **不算**豁免，因为那几个说的是"上游红了要不要
     继续"，被它们挡着从不执行恰恰是本脚本要抓的掩盖形状。
     仍会漏的：靠 `on.push.paths` / `branches` / `tags` 过滤而 `if:` 里看不出来的作业与步骤
     ⇒ 命中 NEVER_RUN 后先读 yml 再定性，别直接当缺陷修。
  1b. 两根轴的最小样本量**必须分开**（第四十六轮实测）：`--min-presence`（默认 3）管"出现过
     几次"，`--min-red`（默认 2）管"执行过几次且次次失败"。窗口是按 run 截的，本仓 14 个工作流
     在 30 个 run 里只出现约 2 次，共用 3 这个阈值会让一条 100% 失败的步骤（实测＝Lint 的
     `Run ESLint (errors block...)`）从 ALWAYS_RED 名单里静默消失，脚本退 0 说"每个步骤都至少
     成功过一次"——而同一份输出的那一行明明标着「← 从未通过」。
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
退出码：0 = 无 NEVER_RUN / NEVER_RUN_JOB / ALWAYS_RED；1 = 有命中；2 = 取数失败或零覆盖。

定位：**人工核查用的诊断脚本，故意不挂进 CI** —— 节奏门/节奏步骤已自动豁免（局限 1），
但 `on.push.paths` / `branches` 过滤掉的作业它看不出来；更要紧的是窗口里只要有一条真·常亮红灯
（第四十六轮实测＝Lint 的 ESLint 步，2 次执行次次败）它就退 1 ⇒ 挂成门＝把"另一条泳道正在修"
变成全仓红灯，正是它要查的那个形状。需要它时本地跑，或在临时 job 里 `|| true` 只看输出。
进 CI 的是**它自己的用例**：`bash scripts/check-ci-step-coverage.test.sh`（假 `gh` 喂夹具，
不联网、不需要 gh 登录；正向钉"死门要点名＋节奏门不连坐＋两次全败就算常亮红灯"，
反向钉"摘掉 yml 里的 if: 豁免就失效""阈值抬回 3 那条红就该消失"）。
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import sys
from collections import defaultdict

try:  # 守卫分类要读工作流 yml；读不了就一律按"守卫未知"计红（见 job_guards），不许静默放行
    import yaml
except ImportError:  # pragma: no cover - 取决于本机 Python
    yaml = None

# `git remote -v` 的行尾还挂着 " (fetch)" / " (push)"，所以不能拿 $ 收尾锚定。
GITHUB_SSH = re.compile(r"github\.com[:/]([\w.-]+)/([\w.-]+?)(?:\.git)?(?=\s|\()")

# GitHub 自己注入的 housekeeping 步，不是 authored 门禁（见 docstring 局限 2）。
HOUSEKEEPING_EXACT = {"Set up job", "Initialize containers", "Stop containers", "Complete job"}


def housekeeping(step: str) -> bool:
    return step in HOUSEKEEPING_EXACT or step.startswith("Post ")


def cadence_expr(expr: object) -> bool:
    """这个 `if:` 是"什么时候才跑"（节奏），还是"跑不跑得好"（条件续跑）？

    只有前者能豁免判红：`github.event_name` / `github.ref` 决定的是**这次 run 有没有资格跑它**
    （月度 cron、只在 tag 上跑），窗口里没有那种事件时它恒 skipped，那是设计而非缺陷。
    `always()` / `success()` / `needs.*.outputs` 不算 —— 那几个说的是"上游红了要不要继续"，
    一个只被这类条件挡着的步骤从没执行，恰恰是我们要抓的"被上游掩盖"形状。
    """
    text = str(expr or "")
    return "github.event_name" in text or "github.ref" in text


def job_guards(repo: str) -> tuple[dict, dict, str]:
    """读仓内工作流，给出 (作业级守卫, 步骤级守卫, 备注)。

    为什么要读文件而不是只看 API 结论：作业级"全被跳过"有两种完全不同的成因 ——
      ① 门坏了（needs 指向不存在的作业、路径写错、trigger 不匹配）；
      ② 节奏门本来就不在 push 上跑（月度 cron、只在 tag 上跑）。
    ②在 yml 里写着 `if:`，API 的 conclusion 里看不出来。不区分就把 lint.yml 的
    `LICENSE Compliance Scan`（月度）和 release.yml 的 tag 作业一起判成缺陷，
    那道门立刻变成"常亮红灯"—— 而常亮的门与坏掉的门在人的眼里是同一个东西。
    `needs:` 一个被守卫的作业，自己也永远跑不到，所以守卫沿 needs 传一遍闭包。
    步骤级同一回事（第四十六轮补）：`sbom.yml` 的 `Attach SBOM to release (only on tag)`
    在窗口里恒 skipped，是这道门自己产出过、并被文档承认的假阳。

    返回的映射里**没有的键**＝"守卫未知"（缺 PyYAML、工作流文件不在
    本地、或该作业是别处定义的），调用方按红处理 —— 宁可多问一句，不把"读不到"说成"没问题"。
    """
    wf_dir = os.path.join(repo, ".github", "workflows")
    if yaml is None:
        return {}, {}, "PyYAML 不可用，无法区分节奏门与坏门"
    if not os.path.isdir(wf_dir):
        return {}, {}, f"{wf_dir} 不存在，无法区分节奏门与坏门"
    guarded: dict[tuple[str, str], bool] = {}
    step_guarded: dict[tuple[str, str, str], bool] = {}
    for fn in sorted(os.listdir(wf_dir)):
        if not fn.endswith((".yml", ".yaml")):
            continue
        try:
            with open(os.path.join(wf_dir, fn), encoding="utf-8") as fh:
                doc = yaml.safe_load(fh)
        except (OSError, yaml.YAMLError) as exc:
            print(f"  读 {fn} 失败，其中作业的守卫状态按未知处理：{exc}", file=sys.stderr)
            continue
        if not isinstance(doc, dict):
            continue
        wf_name = str(doc.get("name") or os.path.splitext(fn)[0])
        jobs = doc.get("jobs")
        if not isinstance(jobs, dict):
            continue
        local: dict[str, bool] = {}
        deps: dict[str, list] = {}
        labels: dict[str, str] = {}
        steps_of: dict[str, list] = {}
        for jid, body in jobs.items():
            body = body if isinstance(body, dict) else {}
            labels[str(jid)] = str(body.get("name") or jid)
            local[str(jid)] = cadence_expr(body.get("if"))
            steps_of[str(jid)] = [(str(st["name"]), st.get("if"))
                                  for st in (body.get("steps") or [])
                                  if isinstance(st, dict) and st.get("name")]
            need = body.get("needs")
            if isinstance(need, str):
                need = [need]
            deps[str(jid)] = [str(x) for x in need] if isinstance(need, list) else []
        for _ in range(len(local) + 1):  # needs 链是有限长的，多传一轮即收敛
            changed = False
            for jid, need in deps.items():
                if local[jid]:
                    continue
                if any(local.get(n) or local.get(labels.get(n, ""), False) for n in need):
                    local[jid] = True
                    changed = True
            if not changed:
                break
        for jid, flag in local.items():
            guarded[(wf_name, labels[jid])] = flag
            if jid != labels[jid]:
                guarded[(wf_name, jid)] = flag
            for sname, expr in steps_of.get(jid, []):
                # 作业本身是节奏门时，里面每一步都跟着跑不到 —— 步骤轴也得豁免，
                # 否则同一件事在两根轴上各红一次。
                sflag = flag or cadence_expr(expr)
                step_guarded[(wf_name, labels[jid], sname)] = sflag
                if jid != labels[jid]:
                    step_guarded[(wf_name, jid, sname)] = sflag
    return guarded, step_guarded, ""


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
    ap.add_argument("--min-red", type=int, default=2,
                    help="至少执行几次、且次次失败才判 ALWAYS_RED（默认 2）")
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
    job_stats: dict[tuple[str, str], dict[str, int]] = defaultdict(lambda: defaultdict(int))
    for r in runs:
        jobs_doc = gh(f"repos/{slug}/actions/runs/{r['id']}/jobs?per_page=100")
        jobs = (jobs_doc or {}).get("jobs") or []
        if not jobs:
            continue
        for j in jobs:
            jn = j.get("name") or "?"
            job_stats[(r.get("name") or "?", jn)][j.get("conclusion") or "none"] += 1
            # 整个作业被跳过时 API 给的 steps 是空表 —— 只数步骤就永远看不见这类死门，
            # 所以作业级结论必须在进这个循环之前先记一笔。
            for s in j.get("steps") or []:
                sn = s.get("name") or "?"
                stats[(r.get("name") or "?", jn, sn)][s.get("conclusion") or "none"] += 1

    if not stats and not job_stats:
        print("零覆盖：取到了 run，却一个 job/step 都没读到", file=sys.stderr)
        return 2

    guards, step_guards, guard_note = job_guards(args.repo)
    if guard_note:
        print(f"  ⚠️ {guard_note} ⇒ 以下窗口内所有从未执行的作业都按红计", file=sys.stderr)

    dead, always_red, cadence_steps = [], [], []
    by_job: dict[str, list] = defaultdict(list)
    for (wf, jn, sn), cnt in stats.items():
        executed = sum(v for k, v in cnt.items() if k not in ("skipped", "none"))
        ok = cnt.get("success", 0)
        fail = sum(v for k, v in cnt.items() if k in ("failure", "timed_out", "cancelled"))
        present = executed + cnt.get("skipped", 0)
        by_job[f"{wf} / {jn}"].append((sn, executed, ok, fail, cnt.get("skipped", 0), cnt.get("none", 0)))
        # 阈值分开：NEVER_RUN 要的是"这个步骤确实存在于配置里"的把握（出现几次），
        # ALWAYS_RED 要的是"跑起来就红"的把握（执行几次）——窗口按 run 截，
        # 14 个工作流的仓在 30 个 run 里每个作业只出现 2 次，共用一个阈值会让
        # 一条次次失败的步骤从名单里静默消失（第四十六轮实测就是 Lint 的 ESLint 步）。
        if executed == 0 and present >= args.min_presence and not housekeeping(sn):
            if step_guards.get((wf, jn, sn)) is True:
                cadence_steps.append((wf, jn, sn, present))
            else:
                dead.append((wf, jn, sn, present))
        elif executed >= args.min_red and ok == 0 and fail == executed:
            always_red.append((wf, jn, sn, fail))

    dead_jobs, cadence_jobs = [], []
    for (wf, jn), cnt in job_stats.items():
        executed = sum(v for k, v in cnt.items() if k not in ("skipped", "none"))
        present = executed + cnt.get("skipped", 0)
        if executed or present < args.min_presence:
            continue
        flag = guards.get((wf, jn))
        if flag is True:
            cadence_jobs.append((wf, jn, present))
        else:
            why = "无 if: 守卫" if flag is False else "守卫未知"
            dead_jobs.append((wf, jn, present, why))

    for job in sorted(by_job):
        print(f"\n[{job}]")
        for sn, executed, ok, fail, skipped, pending in sorted(by_job[job]):
            line = f"  {sn[:52]:<52} 执行 {executed:>3}  成 {ok:>3}  败 {fail:>3}  跳 {skipped:>3}"
            if executed == 0 and skipped == 0 and pending:
                # 窗口里只读到 null 结论 = 取样时那个 run 还在跑（或那个作业根本没触发过）。
                # 判据靠 present >= --min-presence 已经把它排除在红之外，但文案不能再写
                # "从未执行"——那会让读者去修一道好门。
                line += f"   ← 无结论（窗口内 {pending} 次取样该步骤都还是 pending，不判死门）"
            elif executed == 0:
                line += "   ← 从未执行" if not housekeeping(sn) else "   ← 未执行（housekeeping，不计门禁）"
            elif ok == 0:
                line += "   ← 从未通过"
            print(line)

    print(f"\n══════ 结论 ══════")
    print(f"  统计步骤 {len(stats)} 个 / 作业 {len(job_stats)} 个；"
          f"NEVER_RUN {len(dead)} 个；NEVER_RUN_JOB {len(dead_jobs)} 个；ALWAYS_RED {len(always_red)} 个")
    for wf, jn, sn, present in dead:
        print(f"  · NEVER_RUN  {wf} / {jn} / {sn}（出现 {present} 次，全被跳过）")
    for wf, jn, present, why in dead_jobs:
        print(f"  · NEVER_RUN_JOB  {wf} / {jn}（出现 {present} 次，整个作业全被跳过，{why}）")
    for wf, jn, sn, fail in always_red:
        print(f"  · ALWAYS_RED {wf} / {jn} / {sn}（执行 {fail} 次，次次失败）")
    if cadence_steps:
        print("  节奏步骤（yml 里该步骤自带事件/ref 条件，或所在作业是节奏门，本就不在这类 run 上跑，不计红）：")
        for wf, jn, sn, present in sorted(cadence_steps):
            print(f"  · 节奏步骤     {wf} / {jn} / {sn}（窗口内出现 {present} 次，全被跳过）")
    if cadence_jobs:
        print("  节奏门（yml 里有 if: 守卫或 needs 继承自它，本就不在 push 上跑，不计红）：")
        for wf, jn, present in sorted(cadence_jobs):
            print(f"  · 节奏门     {wf} / {jn}（窗口内出现 {present} 次，全被跳过）")
    if dead or dead_jobs or always_red:
        print("  ⇒ 这些门禁没有产生过任何证据；先判「门跑不起来」还是「门查出了问题」，再决定修门还是修存量。")
        return 1
    print("  ✓ 窗口内每个作业与步骤都至少执行并成功过一次")
    return 0


if __name__ == "__main__":
    sys.exit(main())
