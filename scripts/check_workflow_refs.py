#!/usr/bin/env python3
"""check_workflow_refs.py —— 工作流引用完整性静态闸（本地/CI 共用）

为什么需要它（2026-09-19 第二十二轮）：
  CI 与真实仓库之间没有编译期。工作流里写死的文件路径、step id、needs 作业名、
  artifact 名字，**只在触发那一刻才会被发现是错的**，而 release 这类工作流只在打 tag
  时才跑 —— 于是一条流水线可以在仓库里"看起来在护着发布"好几个月，实际一跑就炸。
  本轮实测到的真实命中：release.yml 构建 `./user-server/Dockerfile`，而该文件已在
  94415060（宿主机部署重构）里删掉；同一文件里的 cosign 步骤引用了不存在的
  `steps.buildkit.outputs.digest`。两者都能静态判出来，不需要跑。

判据（只判能静态确定的，避免主观）：
  R1 with.file / with.context / working-directory 指向的路径必须存在
  R2 run 步骤里调用的仓内脚本（bash/sh/python3/node/go run <path>）必须存在
  R3 `steps.<id>.` 引用的 <id> 必须在同一 job 内有 `id: <id>`
     （步骤体与 job 级的 environment/if/concurrency 都算 —— 后者的空串同样静默）
  R4 `needs:` 引用的作业名必须在同一文件里有同名 job
  R5 download-artifact 的 name 必须在同一工作流里有对应的 upload-artifact name
     （跨工作流共享 artifact 用 `-- 允许名单` 注释显式声明后方可放行）

用法： check_workflow_refs.py [--repo ROOT] [--allow R5:name ...] [工作流文件...]
退出码：0=干净 / 1=有命中 / 2=输入无效（宁可报错也不静默零扫描）
"""
from __future__ import annotations

import argparse
import os
import re
import sys

try:
    import yaml
except ImportError:  # pragma: no cover
    print("❌ 需要 PyYAML（pip install pyyaml）", file=sys.stderr)
    sys.exit(2)

# GitHub 在 job 里把 on 解析成 True（YAML 1.1 布尔），统一还原成字符串键
TRUTHY = {True: "on", False: "off", None: "null"}


def norm_key(k):
    return TRUTHY.get(k, k)


def norm_tree(node):
    if isinstance(node, dict):
        return {norm_key(k): norm_tree(v) for k, v in node.items()}
    if isinstance(node, list):
        return [norm_tree(v) for v in node]
    return node


# run 步骤里"执行仓内脚本"的形态：bash scripts/x.sh / python3 scripts/y.py --strict / node a/b.cjs
SCRIPT_RE = re.compile(
    r"(?m)(?:^|[\s;&|(`])(?:bash|sh|zsh|python3|python|node|deno|ts-node|go\s+run|./)\s+"
    r"(?P<path>(?:\./)?(?:[\w.\-@/]+\.(?:sh|bash|py|cjs|mjs|js|ts)))\b"
)
STEPREF_RE = re.compile(r"steps\.([A-Za-z0-9_\-]+)\.")

# 只能挂在 step/job 上的属性：出现在 with 里说明作者把它当成了 action 输入（action 会静默忽略）。
# 只收"绝不可能是 action 输入"的两个，免得误伤 upload-artifact 的 with.name 一类合法输入。
STEP_ONLY_PROPS = {"continue-on-error", "timeout-minutes"}


def path_exists(token: str, repo_root: str, step_base: str, with_: dict | None) -> bool:
    """工作流里的相对路径基准不止一个：步骤级 working-directory、仓库根、
    以及 docker 构建的 context（build-push-action 的 file 既可写仓库根相对、也可写 context 相对）。
    只要任一基准下存在就不算命中 —— 闸要抓"文件根本不在仓里"，不要抓"作者用哪种相对写法"。"""
    cands = [os.path.join(step_base, token), os.path.join(repo_root, token)]
    if isinstance(with_, dict) and isinstance(with_.get("context"), str):
        ctx = os.path.join(repo_root, with_["context"].lstrip("./"))
        cands.append(os.path.join(ctx, token))
    return any(os.path.exists(os.path.normpath(c)) for c in cands)


def iter_steps(job):
    for st in job.get("steps") or []:
        if isinstance(st, dict):
            yield st


def check_workflow(path: str, repo_root: str, allow: set[str]) -> list[str]:
    hits: list[str] = []
    with open(path, encoding="utf-8") as f:
        raw = f.read()
    try:
        doc = norm_tree(yaml.safe_load(raw))
    except yaml.YAMLError as e:
        return [f"{path}: YAML 解析失败 ⇒ 整个工作流永远不会跑：{e}"]
    if not isinstance(doc, dict):
        return [f"{path}: 顶层不是映射，非合法工作流"]

    jobs = doc.get("jobs") or {}
    if not isinstance(jobs, dict) or not jobs:
        return [f"{path}: 没有 jobs ⇒ 空工作流"]

    rel = os.path.relpath(path, repo_root)

    # R4: needs 指向的作业必须存在
    for jname, job in jobs.items():
        if not isinstance(job, dict):
            hits.append(f"{rel}: job '{jname}' 不是映射")
            continue
        needs = job.get("needs")
        if isinstance(needs, str):
            needs = [needs]
        if isinstance(needs, dict):
            needs = list(needs.keys())
        for n in needs or []:
            if n not in jobs:
                hits.append(f"{rel}: job '{jname}' needs '{n}' —— 该作业不存在，此 job 永不运行")

        step_ids = {st.get("id") for st in iter_steps(job) if st.get("id")}
        job_wd = None
        dflt = job.get("defaults")
        if isinstance(dflt, dict) and isinstance(dflt.get("run"), dict):
            job_wd = dflt.get("run").get("working-directory")
        if not isinstance(job_wd, str):
            job_wd = job.get("working-directory") if isinstance(job.get("working-directory"), str) else None
        job_runs = "\n".join(st.get("run") for st in iter_steps(job) if isinstance(st.get("run"), str))

        def wd_hit(wd: str, where: str) -> bool:
            """working-directory 指不存在的目录：若该目录名在本 job 的 run 里被造过
            （mkdir -p dist、go build -o ../dist/…、path: dist/x.zip），它是构建期现造的产物目录，
            不是引用悬空 —— 闸门不能把运行时目录判成静态错误。"""
            if "${{" in wd or os.path.isdir(os.path.join(repo_root, wd)):
                return False
            leaf = wd.rstrip("/").split("/")[-1]
            if leaf and re.search(rf"(?:mkdir|cd |-o |--output-dir|path:)\s*\S*{re.escape(leaf)}\b", job_runs):
                return False
            hits.append(f"{rel}: job '{jname}' {where} working-directory='{wd}' —— 目录不存在且无步骤创建它")
            return True

        if isinstance(job_wd, str):
            wd_hit(job_wd, "job 级")
        for st in iter_steps(job):
            uses = st.get("uses") or ""
            with_ = st.get("with") or {}
            # 步骤级 working-directory 决定该步骤内所有相对路径的基准
            st_wd = st.get("working-directory")
            if not isinstance(st_wd, str):
                st_wd = job_wd
            elif "working-directory" in st:
                wd_hit(st_wd, f"step '{st.get('name') or uses}'")
            step_base = os.path.join(repo_root, st_wd) if isinstance(st_wd, str) and "${{" not in st_wd else repo_root

            # R1: 显式路径字段
            if isinstance(with_, dict):
                is_artifact = "artifact" in uses
                for key in ("file", "context", "path"):
                    val = with_.get(key)
                    if not isinstance(val, str) or "${{" in val:
                        continue
                    if key == "path" and is_artifact:
                        continue  # upload/download-artifact 的 path 是运行时输出目录
                    for token in [t.strip() for t in val.splitlines() if t.strip()]:
                        if not path_exists(token, repo_root, step_base, with_):
                            hits.append(
                                f"{rel}: job '{jname}' step '{st.get('name') or uses}' with.{key}='{token}' "
                                f"—— 路径在仓库里不存在"
                            )
                # R6: 步骤级属性被写进 with: —— action 会静默忽略，作者意图落空
                for key in STEP_ONLY_PROPS & set(with_):
                    hits.append(
                        f"{rel}: job '{jname}' step '{st.get('name') or uses}' with.{key} —— "
                        f"'{key}' 是步骤级属性，写在 with 里会被 action 静默忽略（该 action 无此输入）"
                    )
            # R3: steps.<id> 引用必须已声明
            blob = yaml.safe_dump(st, sort_keys=False) if (st.get("with") or st.get("run") or st.get("env")) else ""
            for sid in set(STEPREF_RE.findall(blob)):
                if sid not in step_ids:
                    hits.append(
                        f"{rel}: job '{jname}' step '{st.get('name') or uses or st.get('id') or '?'}' "
                        f"引用 steps.{sid}. —— 该 job 内没有 id:'{sid}' 的步骤，取到的是空串"
                    )
            run = st.get("run")
            if isinstance(run, str):
                for m in SCRIPT_RE.finditer(run):
                    p = m.group("path")
                    if "${{" in p or "/" not in p or p.startswith("/"):
                        continue  # 只判带仓内目录前缀的，忽略绝对路径与系统命令
                    if not path_exists(p, repo_root, step_base, None):
                        hits.append(
                            f"{rel}: job '{jname}' step '{st.get('name') or '?'}' 调用仓内脚本 '{p}' —— 文件不存在"
                        )

        # R3b: job 级字段里的 steps.<id> 同样要已声明。R3 只扫步骤体，漏掉 environment.url
        # 这类 job 级引用 —— 而它正是"取到空串"最典型的落点：URL 写错 id 不报错，
        # 部署环境只是没链接，Pages 发布看起来一切正常。
        for where in ("environment", "if", "concurrency"):
            jblob = yaml.safe_dump(job.get(where), sort_keys=False)
            for sid in set(STEPREF_RE.findall(jblob)):
                if sid not in step_ids:
                    hits.append(
                        f"{rel}: job '{jname}' {where} 引用 steps.{sid}. —— "
                        f"该 job 内没有 id:'{sid}' 的步骤，取到的是空串"
                    )

    # R5: artifact 名配对
    uploaded, downloaded = set(), []
    for jname, job in jobs.items():
        if not isinstance(job, dict):
            continue
        for st in iter_steps(job):
            uses = st.get("uses") or ""
            w = st.get("with") or {}
            name = w.get("name") if isinstance(w, dict) else None
            if not isinstance(name, str) or "${{" in name:
                continue
            if "upload-artifact" in uses:
                uploaded.add(name)
            elif "download-artifact" in uses:
                downloaded.append((jname, name))
    for jname, name in downloaded:
        if name not in uploaded and f"R5:{name}" not in allow:
            hits.append(
                f"{rel}: job '{jname}' download-artifact name='{name}' —— 本工作流内没有任何 job 上传过它"
            )
    return hits


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=".", help="仓库根（相对路径的解析基准）")
    ap.add_argument("--allow", action="append", default=[], help="放行项，如 R5:<artifact name>")
    ap.add_argument("files", nargs="*")
    args = ap.parse_args()

    repo_root = os.path.abspath(args.repo)
    files = args.files
    if not files:
        wdir = os.path.join(repo_root, ".github", "workflows")
        if not os.path.isdir(wdir):
            print(f"❌ 既没给文件也找不到 {wdir}", file=sys.stderr)
            return 2
        files = sorted(os.path.join(wdir, n) for n in os.listdir(wdir) if n.endswith((".yml", ".yaml")))
    if not files:
        print("❌ 待检查工作流为 0 个 ⇒ 检查形同虚设", file=sys.stderr)
        return 2

    allow = set(args.allow)
    all_hits: list[str] = []
    for fp in files:
        all_hits += check_workflow(fp, repo_root, allow)

    print(f"══════ 工作流引用完整性（{len(files)} 个文件）══════")
    if all_hits:
        for h in sorted(set(all_hits)):
            print(f"  ❌ {h}")
        print(f"════ {len(set(all_hits))} 条命中 ════")
        return 1
    print("  ✓ 路径 / step id / needs / artifact 配对全部可解析")
    return 0


if __name__ == "__main__":
    sys.exit(main())
