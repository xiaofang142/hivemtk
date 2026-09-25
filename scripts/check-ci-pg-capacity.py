#!/usr/bin/env python3
"""CI 的 PG 名额与代码侧连接池上界必须同源：谁改了另一边就得红。

为什么需要这道门（渠道审计批M-11 后续 2 · §23.21 第 8 段⑤）：
  CI 的 `services.postgres` 从不写名额 ⇒ 用镜像默认 **100**；而 `internal/config` 的
  `DefaultPoolConfig.MaxOpenConns` 是 **200**、`testutil` 夹具是 **32**。整片跑一旦越过 100
  就抛 `53300 too many clients`，表现为"最后一个开连接的用例"随机红（§23.21 第 2／3 段：
  同一份无上限字节在 500 名额的开发容器上 0 红、在 100 名额的 CI 容器上必红）。
  这条分歧本地按不下去，此前只有两种修法：靠人记得 YAML 里写 400，或者靠事后翻 CI 日志。
  本门把两者都换成判据——并且把"YAML 写了 400"与"容器真的以 400 起来了"分开钉：
  前者是静态一致性（本门），后者由工作流里那一步 `show max_connections` 在跑测试之前现测。

判据（任一不成立即 rc=1；读不到源即 rc=2）：
  C1 每枚带 `services.postgres` 的作业，服务块必须有显式 `command: '-c max_connections=<N>'`；
  C2 所有作业声明的 N 必须相等（不等＝某些作业仍在 100 名额上跑，红只落在一部分作业上，最难归因）；
  C3 每枚带 postgres 服务的作业必须有一步现测 `show max_connections`；
  C4 该现测步骤必须把实测值与本工作流里声明的值**比对**并判等（只 echo 不判＝没有门）；
  C5 N ≥ 生产默认池上界 + 测试夹具池上界（两个数从 Go 源现读，不是手抄）；
  C6 全文里 `max_connections=<数字>` 的出现次数必须恰等于服务块声明数——现测步骤是
     `grep -m1` 从本文件派生声明值的，注释里多写一处（哪怕只是举一个例子）就会让 CI 读到错的 want。

口径边界（把"没数到"和"没问题"分开写）：
  - 只认 `max_connections=<数字>` 写在 `command` 串里这一种形态。若有人改往 `options:` 塞
    server 参数：`options` 是 docker create 侧的旗标，postgres 进程读不到 ⇒ 本门按"没声明"判红，
    这正是它该红的时候（`services.<id>` 的键集合里有 command／entrypoint，没有 args）。
  - 名额是**每作业一枚容器**，跨作业不叠加（四枚 job 各自 100 ⇒ 各自撞墙，不是合计 400）。
  - C5 的下限只含"单个测试二进制可能同时持有的连接上界"（生产默认＋夹具），
    不含 `-p 1` 之外的人为并发；真值以第 2 段那台 100 名额容器的实测峰值为准。
  - 读不到 Go 侧那两个形状 ⇒ rc=2 并点名形状（缺产物＝SKIP，不是 PASS）。
  - 只扫 `.github/workflows/user-server-ci.yml` 一份：platform 侧另有工作流与另外的容器，
    本门的对象集合不含它（要扩面得先确认那边的池上界来源）。

执行入口：本地 `python3 scripts/check-ci-pg-capacity.py --repo .`；已注册进 `make audit`
与本工作流的 `static-gates` 作业。牙齿：`bash scripts/check-ci-pg-capacity.test.sh`
（F1 齐全绿／F2 摘 command／F3 名额不等／F4 缺现测步骤／F5 只印不判／F6 低于池上界之和／
F7 源文件不在退 2／F7b 上界形状不在退 2／F8 注释污染派生 grep 红／REAL 真仓库绿）。
"""

from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path

try:
    import yaml
except ImportError:  # pragma: no cover
    print("❌ 需要 PyYAML（pip install pyyaml）；本门不降级为跳过", file=sys.stderr)
    raise SystemExit(2)

WORKFLOW = ".github/workflows/user-server-ci.yml"
PROD_POOL = "user-server/internal/config/server.go"
TEST_POOL = "user-server/internal/pkg/testutil/testdb.go"
CAP_RE = re.compile(r"max_connections=(\d+)")
PROBE = "show max_connections"


def norm(node):
    """YAML 1.1 把 `on:` 读成布尔 True 键，工作流本体要按字符串 "on" 找。"""
    if isinstance(node, dict):
        return {("on" if k is True else k): norm(v) for k, v in node.items()}
    if isinstance(node, list):
        return [norm(x) for x in node]
    return node


def pool_bound(repo, rel, pattern, shape):
    """从 Go 源现读一枚池上界；读不到即返回 None（调用方按 rc=2 处理）。"""
    path = repo / rel
    if not path.is_file():
        print(f"❌ 读不到 {rel}（源文件缺失 ⇒ 判据无法计算，按未跑过处理）", file=sys.stderr)
        return None
    m = re.search(pattern, path.read_text(encoding="utf-8"))
    if not m:
        print(f"❌ {rel} 里没有 {shape} 这个形状 ⇒ 池上界读不出来，按未跑过处理", file=sys.stderr)
        return None
    return int(m.group(1))


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--repo", default=".")
    args = ap.parse_args()
    repo = Path(args.repo).resolve()

    wf_path = repo / WORKFLOW
    if not wf_path.is_file():
        print(f"❌ 找不到 {WORKFLOW} ⇒ 本门什么都没测到（rc=2，不是绿）", file=sys.stderr)
        return 2
    raw = wf_path.read_text(encoding="utf-8")
    doc = norm(yaml.safe_load(raw) or {})
    jobs = doc.get("jobs") or {}

    prod_bound = pool_bound(repo, PROD_POOL,
                            r"DefaultPoolConfig\s*=\s*PoolConfig\{[^}]*?MaxOpenConns:\s*(\d+)",
                            "DefaultPoolConfig…MaxOpenConns")
    test_bound = pool_bound(repo, TEST_POOL, r"testDBMaxOpenConns\s*=\s*(\d+)",
                            "testDBMaxOpenConns")
    if prod_bound is None or test_bound is None:
        return 2
    floor = prod_bound + test_bound

    services_with_pg, declared, measured, problems = 0, [], 0, []

    for job_name, job in jobs.items():
        if not isinstance(job, dict):
            continue
        svc = (job.get("services") or {}).get("postgres")
        steps = job.get("steps") or []
        probes = [s for s in steps if isinstance(s, dict) and PROBE in str(s.get("run") or "")]
        if not isinstance(svc, dict):
            if probes:
                print(f"  · 作业 {job_name} 没有 postgres 服务却带现测步骤（不影响判据，仅登记）")
            continue
        services_with_pg += 1

        cmd = svc.get("command")
        cap = CAP_RE.search(cmd) if isinstance(cmd, str) else None
        if cap is None:
            problems.append(f"C1 作业 {job_name} 的 services.postgres 没有显式名额"
                            f"（command={cmd!r}）⇒ 容器会用镜像默认 100")
        else:
            declared.append((job_name, int(cap.group(1))))

        if not probes:
            problems.append(f"C3 作业 {job_name} 带 postgres 服务却没有现测 `{PROBE}` 的步骤"
                            "⇒ YAML 里写的名额从没被验证过容器真的起来了")
            continue
        measured += 1
        body = str(probes[0].get("run") or "")
        # 比对的两半：① 有一个 `!=` 判定；② 声明值从本工作流派生（那枚 `max_connections=` 的 grep
        # 模式串），而不是把 400 再手抄一遍进 shell——手抄的那份改了 YAML 不会跟着改，正是本门要防的。
        # 注意不能用 CAP_RE 量这里：夹具里那串是 `max_connections=[0-9]+`，`=` 后面跟的是方括号。
        if "!=" not in body or "max_connections=" not in body:
            problems.append(f"C4 作业 {job_name} 的现测步骤只印不判：既没把实测值与声明值比对的"
                            " `!=`，也没从本工作流派生声明值 ⇒ 名额漂了不会红")

    values = sorted({v for _, v in declared})
    if len(values) > 1:
        problems.append("C2 各作业声明的名额不等："
                        + "、".join(f"{j}={v}" for j, v in declared)
                        + " ⇒ 低的那几枚作业仍在 100 名额上跑，53300 只会红一部分作业，最难归因")
    for job_name, value in declared:
        if value < floor:
            problems.append(f"C5 作业 {job_name} 声明名额 {value} < 代码侧池上界之和 {floor}"
                            f"（生产默认 MaxOpenConns {prod_bound} ＋ 测试夹具 {test_bound}）")
    text_caps = CAP_RE.findall(raw)
    if len(text_caps) != len(declared):
        problems.append(f"C6 全文有 {len(text_caps)} 处 `max_connections=<数字>`，服务块只声明 {len(declared)} 处"
                        "⇒ 现测步骤那枚 `grep -m1` 会派生出错的 want（注释里举的例子也算一处）")

    print(f"postgres 服务 {services_with_pg} 处 · 显式名额声明 {len(declared)} 处 · "
          f"现测步骤 {measured} 处 · 代码侧下限 {floor}")
    if services_with_pg and len(declared) != services_with_pg:
        problems.append(f"C1 汇总：{services_with_pg} 枚 postgres 服务里只有 {len(declared)} 枚声明了名额")
    if services_with_pg and measured != services_with_pg:
        problems.append(f"C3 汇总：{services_with_pg} 枚 postgres 服务里只有 {measured} 枚作业现测了名额")

    if problems:
        for p in problems:
            print(f"✗ {p}", file=sys.stderr)
        return 1
    print("✅ 名额声明／现测／池上界三者同源，CI 侧不会再以 53300 的形态把红留给别人")
    return 0


if __name__ == "__main__":
    sys.exit(main())
