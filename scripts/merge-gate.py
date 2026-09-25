#!/usr/bin/env python3
"""二次审核协议 §4 E 相 / §8 判据 3 的**常驻**合并门禁驱动器：一串全量步骤，逐步落日志、逐步判红。

为什么住在仓里而不是 /tmp 里一支一次性脚本：协议 §0.1 记的就是「规矩住在散文里，散文不是判据」，
而 §8.2/§7.28 那几轮把合并门禁写在 `/tmp/b22_gate.sh` 里跑 ⇒ 下一轮谁也复现不出"那 26 步到底是哪些步"，
读数只剩表格里那句转述。本脚本把自己变成判据：步骤名单在仓里，任何人 `--list` 就能看到本轮的门由什么组成。

三条不可商量的口径（都是本仓踩过之后写进来的）：
1. **每步都要"跑到"的证据**：光看 rc 会把「命令根本没执行」判成 PASS（npx 找不到依赖、脚本改名、
   `go test` 因为包不存在而 `no test files` 都会退 0）。所以每步除 rc==0 外还要满足一条形状判据：
   `empty`（输出必须为空，用于 gofmt -l 这类"有输出才是红"）、`re`（输出必须出现某行）、
   `nore`（输出不得出现某行，用于 `FAIL`）。
2. **一步红不许把后面的步骤吃掉**：`set -e` 式跑法在第一处红之后就停，于是"本轮还剩几条红"永远只有
   一个数。这里跑完全部步骤再汇总，红因逐步打印。
3. **红要能归因**：每步全量输出落在 `docs/superpowers/specs/ledger/logs/<round>/merge/<NN>-<name>.log`
   （仓内持久路径，`/tmp` 不作证据——协议 §2 的 fixed 形状）。摘要行写 步名 + rc + 秒数 + 判定。
4. **豁免表自己也必须有牙**（本轮加）：`DIAG` 里那一档"诊断步"照常跑、照常落全量日志、照常印红因，
   只是它的 rc 不进门红名单——`check-ci-step-coverage.py` 自述"人工核查用的诊断脚本，故意不挂进 CI"，
   把它当门就是犯它要查的那个病（见 DIAG 的注释）。`--list` 会把这一档标出来，汇总行分开两个分母，
   且 `check_diag_names()` 拦住"步骤改名后残留豁免挂到别的步上"——三格反向测在 --selftest 里各钉一次。

用法：
    python3 scripts/merge-gate.py --list                 # 看步骤名单（也是"名单非空"那档自检）
    python3 scripts/merge-gate.py                        # 全跑，日志落默认轮次目录
    python3 scripts/merge-gate.py --only markdownlint --only go-vet
    python3 scripts/merge-gate.py --round R22-protocol   # 指定取证目录
    python3 scripts/merge-gate.py --selftest             # 反向测：判据摘掉会不会漏红
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import subprocess
import sys
import tempfile
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
US = "user-server"
LOGS_REL = "docs/superpowers/specs/ledger/logs"

# vitest / 若干 CLI 即使 stdout 不是 tty 也照发 ANSI 颜色码：`\x1b[2m      Tests \x1b[22m \x1b[32m762 passed`
# 里 "Tests" 与 "762" 之间夹着转义序列，`Tests\s+\d+ passed` 一律匹配不上 ⇒ 762 条全绿被判成红。
# 判据读的是**去掉颜色后的文本**，日志里也存这份（否则人对不上门到底看见了什么）。
ANSI = re.compile(r"\x1b\[[0-9;]*m")


def db_env(env: dict) -> dict:
    """给需要真库的那几步补上测试连接参数——本机 PG 端口/口令与 CI 默认值不同。

    不补的代价是**假红**：本仓所有测试电池（mut_*.py）都自带这段口令注入，唯独合并门禁漏了，
    于是 `go test ./internal/service/` 整包 3000+ 条用例在 60 秒里全部以
    "failed SASL auth: password authentication failed" 红掉——看着像本轮改坏了，其实是门少喂了一个变量。
    口令只从 user-server/.env 读进子进程环境，**永不打印、永不落日志**。
    """
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    if "POSTGRES_TEST_PASSWORD" in env:
        return env
    envf = ROOT / US / ".env"
    if envf.is_file():
        for line in envf.read_text(encoding="utf-8", errors="replace").splitlines():
            if line.startswith("POSTGRES_PASSWORD=") and "POSTGRES_TEST_PASSWORD" not in env:
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
    return env

# (步名, 工作目录, 命令, 形状判据…)；形状判据：("empty"|"re"|"nore", 参数)
# 名单＝批22 那趟 26 步的可复现版本 + 本泳道本轮新增的门（闭合门 ×2、--selftest、seam/async 两扇门）。
STEPS: list[tuple[str, str, list[str], list[tuple[str, str]]]] = [
    ("go-build", US, ["go", "build", "./..."], []),
    ("go-vet", US, ["go", "vet", "./..."], []),
    # gofmt 是"有输出才是红"的那种命令：rc 恒 0，判据只能是输出为空。
    ("gofmt", ".", ["gofmt", "-l", "user-server/internal"], [("empty", "")]),
    ("go-test-browser-automation", US,
     ["go", "test", "-p", "1", "-count=1", "-timeout", "40m", "./internal/browser_automation/..."],
     [("re", r"^ok\s+\S+browser_automation"), ("nore", r"(^|\n)FAIL")]),
    # 宿主包整包跑：service 单包实测 450–880s 随负载摆动，超时按 40m 给（600s 是抽奖式假红）。
    ("go-test-host-packages", US,
     ["go", "test", "-p", "1", "-count=1", "-timeout", "40m",
      "./internal/service/", "./internal/repository/", "./internal/model/",
      "./internal/migration/...", "./internal/channelgw/"],
     [("re", r"^ok\s+hivemtk-user/internal/service"), ("nore", r"(^|\n)FAIL")]),
    ("vitest-bridge", "user-web/bridge", ["npx", "vitest", "run"],
     [("re", r"Tests\s+\d+ passed"), ("nore", r"failed")]),
    ("vitest-browser-automation", "user-web/browser_automation", ["npx", "vitest", "run"],
     [("re", r"Tests\s+\d+ passed"), ("nore", r"failed")]),
    # 这两道本地 ESLint 是本轮**补的腿**（不是放宽）：CI 里有 `ESLint (Bridge)` 与
    # `ESLint (user-web 主应用)` 两个作业，而合并门禁此前只跑 vitest ⇒ lint 只有推上去才知道红不红。
    # 实测形状：run 653（HEAD `1bce38c3`）user-web 报 2 errors 常红，而**工作树**同一条命令 0 errors
    # ——差的那两处（cdp/input.js 的 `{cause}`、primitives.js 的 useless-assignment）正是本泳道
    # 尚未提交的改动。历史读数不是本轮的代价，树内读数才是，所以门里放这两步。
    # 形状用 `ESLINT_RC=<rc>` 哨兵而不是数 "0 errors"：eslint 只在 problem 数 >0 时印汇总行，
    # 拿那句话当判据＝告警债还清的那天这道门自己会红；哨兵由 sh 打，`exit $rc` 把真码传回去
    # （只 `echo rc=$?` 会让末句的命令替掉被测命令的退出码）。
    ("lint-bridge", "user-web/bridge",
     ["sh", "-c", "npx eslint src test scripts --max-warnings=80; rc=$?; echo ESLINT_RC=$rc; exit $rc"],
     [("re", r"ESLINT_RC=0")]),
    ("lint-user-web", "user-web",
     ["sh", "-c", "npx eslint .; rc=$?; echo ESLINT_RC=$rc; exit $rc"],
     [("re", r"ESLINT_RC=0")]),
    ("markdownlint", ".", ["npx", "markdownlint-cli2"], [("re", r"Summary: 0 issue")]),
    # 架构脚本自带 rc 语义（❌ 计数>0 退 1），且每一层检查都打印 `[L4]` 字样——早先这里写
    # `nore: [L4]`，等于"只要跑到 L4 那几行就红"，绿的 ✅ 也照红：判据本身坏了。
    # 现在的形状＝rc=0 且整趟输出不出现 ❌（拦住"打印了 ❌ 却退 0"那一类闭源/漏 exit 的步）。
    ("arch-gate", ".", ["bash", "scripts/check-architecture.sh"], [("nore", "❌")]),
    # L4 宽口径基线是本轮**越界**加的一道共享门（§5 纪律）：只跑 arch-gate 证明它今天绿，
    # 不证明它有牙。这一格跑 5 种坏基线各自红（红因点名到文件）+ 还原后控制组绿。
    ("arch-l4-reverse", ".", ["bash", "scripts/reverse-test-l4-baseline.sh"],
     [("re", r"L4 基线反向测试：6 格"), ("nore", "判错|BROKEN|❌")]),
    ("fmt-check", ".", ["make", "fmt-check"], []),
    ("date-bucket-tz", ".", ["bash", "scripts/check-date-bucket-tz.sh"], []),
    ("unwired-assets", ".", ["bash", "scripts/check-unwired-assets.sh"], []),
    ("doc-consistency", ".", ["bash", "scripts/check-doc-consistency.sh"], []),
    ("feature-doc", ".", ["bash", "scripts/check-feature-doc.sh"], []),
    ("enum-consistency", ".", ["bash", "scripts/check-enum-consistency.sh"], []),
    ("no-xapptool", ".", ["bash", "scripts/check-no-xapptool.sh"], []),
    ("cross-package-ports", ".", ["bash", "scripts/audit-cross-package-ports.sh"], []),
    ("model-migration", ".", ["python3", "scripts/check_model_migration.py"], []),
    ("md-links-offline", ".", ["python3", "scripts/check-md-links-offline.py"], []),
    ("component-types", ".", ["python3", "scripts/check_component_types.py"], []),
    ("workflow-refs", ".", ["python3", "scripts/check_workflow_refs.py"], []),
    ("ci-step-coverage", ".", ["python3", "scripts/check-ci-step-coverage.py"], []),
    ("test-nil-deref", ".", ["python3", "scripts/check-test-nil-deref.py"], []),
    ("env-coverage", ".", ["python3", "scripts/check-env-coverage.py"], []),
    ("seam-guard", ".", ["python3", "scripts/check-seam-guard.py"], []),
    ("async-global-read", ".", ["python3", "scripts/check-async-global-read.py"], []),
    # 锚点预检：在工作树上把已登记电池的每条锚点各核一次「命中恰好一次 + 注码后语法可解析」，
    # 不建克隆、不跑用例（0.9s）。它原来是一份"人想起来才跑"的旁路脚本，且只认一种电池形状——
    # 于是"预检绿"被引用成了"全仓锚点已核"，而它当时读得了 32 份里的 1 份。现在它每次自报
    # `覆盖 X/Y 份电池`，读不了的名单是脚本里写死的 28 个名字，两份之和==目录现算数（硬门）。
    # 挂进步骤名单的理由同 merge-gate-selftest：判据坏了要在合并前拦住，而不是等人想起。
    ("anchor-preflight", ".", ["python3", "scripts/anchor-preflight.py"],
     [("re", r"已登记的 \d+ 份电池"), ("nore", "✗")]),
    # 预检自己的反向测（内存里造坏锚点/坏树，不碰工作树），10 格各自点名该开火的那条判据。
    # 分子必须等于分母且不为 0/0：反向格名单被清空时"全绿"是最容易伪装成通过的形状。
    ("anchor-preflight-selftest", ".", ["python3", "scripts/anchor-preflight.py", "--selftest"],
     [("re", r"反向测：([1-9]\d*)/\1 格"), ("nore", "✗")]),
    ("closeout-r22", ".", ["python3", "scripts/check-review-closeout.py",
                           "docs/superpowers/specs/ledger/R22.jsonl"],
     [("re", r"===== 闭合判定："), ("nore", "❌")]),
    ("closeout-r22-protocol", ".", ["python3", "scripts/check-review-closeout.py",
                                    "docs/superpowers/specs/ledger/R22-protocol.jsonl"],
     [("re", r"===== 闭合判定："), ("nore", "❌")]),
    ("closeout-selftest", ".", ["python3", "scripts/check-review-closeout.py", "--selftest"],
     [("re", r"selftest：\d+/\d+ 种坏形态"), ("nore", r"✗")]),
    # 门禁自己的反向测挂进名单：早先它只是"人想起来才跑一次"的旁路——`--selftest` 悄悄退 1
    # （判据坏了）在整趟汇总里是隐形的。挂成步之后，"判据坏"和"被测对象坏"一样会拦合并。
    # 判据用 ([1-9]\d*)/\1 而不是写死 16：分子必须等于分母（有一格判错就退 1 是 selftest 自己的事，
    # 但这一步不许把"0/0 种"读成绿——反向格名单被清空时那才是真的没跑），且不许出现"判错 ✗"。
    ("merge-gate-selftest", ".", ["python3", "scripts/merge-gate.py", "--selftest"],
     [("re", r"--selftest：([1-9]\d*)/\1 种"), ("nore", r"判错 ✗")]),
    # 克隆面的判据也是同一类：它判的是"两侧汇总行逐字一致 **且非空**"，判据坏了不会印红字，
    # 只会把空跑读成一致 ⇒ 必须有一格执行它的 --selftest，而不是等人想起来手跑。
    ("clone-selftest", ".", ["python3", "scripts/run-gate-in-clone.py", "--selftest"],
     [("re", r"--selftest：([1-9]\d*)/\1 格"), ("nore", r"判错 ✗")]),
    # 汇编取证的那件自己也有判据（成员零输出＝没跑过 ⇒ 红）。它的判据坏了，这份取证会绿着
    # 少一格读数，而台账钉的是"里面要有那几句"——所以生产方的反向测同样得有人执行。
    ("family-selftests-selftest", ".",
     ["python3", "scripts/run-gate-family-selftests.py", "--selftest"],
     [("re", r"--selftest：([1-9]\d*)/\1 格"), ("nore", r"判错 ✗")]),
]

# 这几步要真连测试库：口令拿不到时它们会整片假红（见 preflight）。
NEEDS_DB = {"go-test-browser-automation", "go-test-host-packages"}

# **诊断步**：照常跑、照常落全量日志、照常印红因，只是它的 rc 不进本趟的判定。
# 为什么 ci-step-coverage 必须在这一档而不是门里：它自己的 docstring 写明
# 「定位：人工核查用的诊断脚本，故意不挂进 CI —— 本仓有合法 `if:` 跳过的步骤，
# 挂成门就是永远红，等于亲手再造一个「常亮红灯掩盖下游」（正是它要查的形状）」。
# 把它当门＝犯它要查的那个病；本轮它红在"CI 历史里 user-web ESLint 从未通过"，
# 而树内那条命令 0 errors（见上面 lint-user-web 那两步）——那是历史窗口，不是本轮的代价。
# 这是一张**豁免表**，所以它自己必须有牙：名单里的名字必须真在步骤名单里
# （check_diag_names），诊断步的红不许悄悄消失（main 里仍印 `[诊断·红]` 与红因），
# 三格反向测在 --selftest 里各钉一次。
DIAG = {"ci-step-coverage"}

STEP_TIMEOUT = {"go-test-browser-automation": 2700, "go-test-host-packages": 3300,
                "vitest-bridge": 1200, "vitest-browser-automation": 1200,
                "env-coverage": 1800}


class Transcript:
    """驱动把自己的摘要**逐行**落盘，而不是跑完再一次性写。

    为什么：台账里引用"合并门禁那趟 rc=0"的条目，其证据文件名正是这份摘要；而那一趟里就含
    `closeout-r22-protocol` 这一步——闭合门检查证据文件时若它还不在盘上，这一条永远凑不齐
    （先有鸡还是先有蛋）。流式写让文件从第一步起就在、且在长大，摘要末尾的 rc 仍是本趟真读数。
    """

    def __init__(self, path: Path):
        path.parent.mkdir(parents=True, exist_ok=True)
        self.f = path.open("w", encoding="utf-8", buffering=1)

    def line(self, s: str) -> None:
        print(s)
        self.f.write(s + "\n")

    def close(self) -> None:
        self.f.close()


def check(rc: int, raw: str, conds: list[tuple[str, str]]) -> list[str]:
    """判据读的是**剥掉 ANSI 之后**的输出——先剥再判，否则颜色码会把绿读成红。"""
    return judge(rc, ANSI.sub("", raw), conds)


def judge(rc: int, out: str, conds: list[tuple[str, str]]) -> list[str]:
    """一步的判定：返回红因列表（空＝绿）。单独成函数是为了能被 --selftest 直接喂假读数。"""
    bad = []
    if rc != 0:
        bad.append(f"rc={rc}")
    for kind, arg in conds:
        if kind == "empty" and out.strip():
            bad.append(f"要求输出为空，实得 {len(out.strip().splitlines())} 行："
                       + out.strip().splitlines()[0][:80])
        elif kind == "re" and not re.search(arg, out, re.M):
            bad.append(f"输出里找不到判据行 {arg!r}（命令可能根本没跑到那一步）")
        elif kind == "nore" and re.search(arg, out, re.M):
            bad.append(f"输出里出现了禁止行 {arg!r}")
    return bad


def gate_reds(name: str, bad: list[str]) -> list[str]:
    """这一步的红算不算门红：DIAG 里的步返回空名单（红因照印，见 main 的 `[诊断·红]`）。"""
    return [] if name in DIAG else bad


def check_diag_names(steps: list[tuple]) -> None:
    """DIAG 里每个名字都必须真在步骤名单里。

    为什么这是必须的：豁免是按**步名**挂的。哪天 `ci-step-coverage` 改了名或被删掉，
    残留的那一项就会在下一步同名误挂时把一道真门悄悄降成诊断步——红靠改名溜走，
    而这正是本脚本要查的「常亮红灯掩盖下游」的反面形态。宁可开跑前停机。
    """
    stale = DIAG - {s[0] for s in steps}
    if stale:
        raise SystemExit(f"DIAG 里的步名不在步骤名单：{' '.join(sorted(stale))}"
                         "（步骤改名/删掉后门没跟着改 ⇒ 豁免会挂到别的步上）")


def filter_steps(steps: list[tuple], only: list[str]) -> list[tuple]:
    """--only 的过滤：步名打错必须停机，而不是跑成一趟 0 步的「门禁」。

    单独成函数是为了能被探针真喂（lane 6：这段原先内联在 main 里，12/12 里没有任何一格走得到它）。
    点名判定用**全量名单**而不是过滤后的子集——诊断步被 --only 排除时，它依然在 DIAG 里合法。
    """
    if not only:
        return steps
    missing = set(only) - {s[0] for s in steps}
    if missing:
        raise SystemExit(f"步名不存在：{' '.join(sorted(missing))}"
                         "（拼错步名会跑成一趟 0 步的「门禁」）")
    return [s for s in steps if s[0] in only]


def tally(name: str, bad: list[str], failed: list[str], diag_red: list[str]) -> str:
    """一步的读数落到哪个名单，返回汇总行该打的标签。

    为什么这段必须从 main 的循环里抽出来：早先 main 内联写了
    `(diag_red if name in DIAG else failed).extend(...)`，而 --selftest 直测的是**同名概念**的
    `gate_reds()`——main 压根没调它。lane 6 把内联那条注成恒走 `diag_red`，真红步全被折进
    "诊断不门控"，汇总照印「N/N 门步绿，无红」退 0，而 --selftest 仍 12/12 全绿。
    现在门控口径只有一个来源（gate_reds），且这段判断能被探针真喂一次。
    """
    gating = gate_reds(name, bad)
    if not bad:
        return "绿"
    if not gating:
        diag_red.append(name)
        return "诊断·红"
    failed.append(name)
    return "红"


def parse_ok(path: Path) -> str:
    """能编译才谈得上跑：返回 ''＝可解析，否则返回红因（带行列号）。

    本仓栽过的形状是**门禁脚本自己语法坏了**：它一旦加载失败，python 只退 1 并吐一段 traceback，
    在汇总里和"跑过了、真的判红"长得一模一样；而 bash/go 那几步会照常往下跑满 40 分钟，人才发现
    最后一步是空的。开跑前逐份 compile 一次，既点名到行列，也把"别为一支坏脚本重跑整包 Go 测试"省下。
    """
    try:
        src = path.read_text(encoding="utf-8")
    except FileNotFoundError:
        # 不兜这一手，"点名的脚本被改名"会变成一段 traceback（rc=1、红因是缺文件），
        # 而这段支路本该只负责"能不能编译"——两支混在一起，读日志的人分不清"没跑"和"判红"。
        return f"{path} 不存在（改名或被删，这一步等于没跑）"
    try:
        compile(src, str(path), "exec")
        return ""
    except (SyntaxError, ValueError, UnicodeDecodeError) as e:
        return f"{path} 第 {getattr(e, 'lineno', '?')} 行语法坏了：{e.msg or e}"


def preflight(steps: list[tuple]) -> None:
    """跑之前先证明"这趟真有东西可跑"：名单非空、工作目录在、命令点名的仓内脚本在且可解析、
    要连库的那几步真拿得到口令。

    口令那一格是本轮实测出来的：少了 POSTGRES_TEST_PASSWORD 时，service/browser_automation 两步
    会在 70 秒里把上千条用例全判红（SASL 认证失败），红因写着"password authentication failed"，
    而人看到的是"本轮改坏了"。宁可开跑前退，也不要跑完再归因。
    """
    if not steps:
        raise SystemExit("--only 过滤后一步都没有：那是 0 步的「门禁」，退 0 只会骗人")
    names = {s[0] for s in steps}
    if names & NEEDS_DB and not db_env(dict(os.environ)).get("POSTGRES_TEST_PASSWORD"):
        raise SystemExit(f"跑不出{'、'.join(sorted(names & NEEDS_DB))}的口令："
                         "user-server/.env 没有 POSTGRES_PASSWORD，"
                         "环境里也没有 POSTGRES_TEST_PASSWORD ⇒ 整包测试会假红，宁可不跑")
    for name, cwd, argv, _ in steps:
        d = (ROOT / cwd).resolve()
        if not d.is_dir():
            raise SystemExit(f"{name}：工作目录不存在 {d}")
        for a in argv[1:]:
            if "/" in a and (a.startswith("scripts/") or a.startswith("docs/")) \
                    and not (ROOT / a).exists():
                raise SystemExit(f"{name}：命令点名的仓内文件不存在 {a}（改名或被删，这一步等于没跑）")
            if a.endswith(".py"):
                why = parse_ok(ROOT / a)
                if why:
                    raise SystemExit(f"{name}：{why}（它退非 0 会被当成「判红了」，其实是没跑）")


def probe_empty_list() -> tuple[bool, str]:
    """反向格 1：--only 打错字过滤成 0 步 ⇒ preflight 必须停机，而不是跑一趟空门禁退 0。"""
    try:
        preflight([])
        return False, "判错：空名单被放行"
    except SystemExit as e:
        return True, str(e)[:20]


def probe_no_db_password() -> tuple[bool, str]:
    """反向格 2：要连库的步子拿不到口令 ⇒ 必须开跑前停机。

    取证要把"口令确实拿不到"这个前提做出来：临时把模块 ROOT 指到一棵空目录（没有 .env 可读）、
    从 os.environ 里抹掉口令键，喂一步 NEEDS_DB 的名单。**口令值只进 pop 的返回值，
    不进消息、不进日志**，探针结束立刻放回环境。
    """
    global ROOT
    tmp = Path(tempfile.mkdtemp(prefix="merge-gate-probe-"))
    saved_root, saved_pw = ROOT, os.environ.pop("POSTGRES_TEST_PASSWORD", None)
    ROOT = tmp
    try:
        preflight([("go-test-host-packages", ".", ["true"], [])])
        return False, "判错：没口令也放行"
    except SystemExit as e:
        return True, str(e)[:20]
    finally:
        ROOT = saved_root
        if saved_pw is not None:
            os.environ["POSTGRES_TEST_PASSWORD"] = saved_pw
        shutil.rmtree(tmp, ignore_errors=True)


def probe_bad_script() -> tuple[bool, str]:
    """反向格 3：坏脚本要**经 preflight**点名，合法脚本不许误伤。

    走 preflight 而不是直调 parse_ok，是为了让这一格开火的是"真在跑的那条支路"（argv 里出现 .py
    就 compile）——只测 parse_ok 的话，万一哪天忘了把它接进 preflight，这一格照样绿。
    两支（坏⇒停、好⇒放行）都测，缺一支就是"只在靶子上红"。
    """
    tmp = Path(tempfile.mkdtemp(prefix="merge-gate-probe-"))
    try:
        bad = tmp / "broken.py"
        bad.write_text("def f(:\n    return 1\n", encoding="utf-8")
        good = tmp / "fine.py"
        good.write_text("def f():\n    return 1\n", encoding="utf-8")
        for argv0, expect_stop in (([str(bad)], True), ([str(good)], False)):
            stopped = False
            try:
                preflight([("probe", ".", ["python3", *argv0], [])])
            except SystemExit as e:
                stopped = True
                why = str(e)
            if stopped != expect_stop:
                return False, f"判错：{bad.name if expect_stop else '合法脚本'}被{'放过' if not stopped else '误伤'}"
        return True, why[why.index("第"):][:20]
    finally:
        shutil.rmtree(tmp, ignore_errors=True)


def probe_diag_stale_name() -> tuple[bool, str]:
    """反向格 4：DIAG 点了个名单里没有的名字 ⇒ 必须停机；照抄真名单 ⇒ 放行。

    三支都测：DIAG 为空＝这格没有靶子（判错），真名单被误伤＝判错，残留名被放行＝判错。
    只测"红的那一侧"会漏掉恰好相反的形状——豁免表一旦写坏，误伤会让整趟门跑不起来。
    """
    if not DIAG:
        return False, "判错：DIAG 为空，这格没有靶子"
    try:
        check_diag_names(list(STEPS))
    except SystemExit as e:
        return False, f"判错：真名单本不该停（DIAG 已挂空：{str(e)[:24]}）"
    stale = [s for s in STEPS if s[0] in DIAG]
    if not stale:
        return False, "判错：步骤名单里没有 DIAG 那一步"
    renamed = [(f"{stale[0][0]}-renamed", ".", ["true"], [])]
    try:
        check_diag_names([s for s in STEPS if s[0] not in DIAG] + renamed)
        return False, "判错：改名后的残留豁免被放行"
    except SystemExit as e:
        return True, str(e)[:20]


def probe_only_typo() -> tuple[bool, str]:
    """反向格 5：--only 打错步名要停机点名，真名要选成子集，不给名要留全量。

    三支各钉一条支路：只测"打错要停"的话，把过滤整个写成 `return steps`（--only 形同虚设）照样绿；
    少"全量"那一支，把它写成 `return []` 也只在 preflight 的空名单格里才红——红因会张冠李戴。
    """
    real = STEPS[0][0]
    try:
        filter_steps(STEPS, [real, real + "-typo"])
        return False, "判错：拼错的步名被放行"
    except SystemExit as e:
        why = str(e)
    if "步名不存在" not in why:
        return False, f"判错：红因不是点名步名（{why[:24]}）"
    if [s[0] for s in filter_steps(STEPS, [real])] != [real]:
        return False, "判错：真名没被选成单步子集"
    if len(filter_steps(STEPS, [])) != len(STEPS):
        return False, "判错：不带 --only 时把全量过滤小了"
    return True, why[:20]


def probe_diag_routing() -> tuple[bool, str]:
    """反向格 6：门步的红必须进 failed，诊断步的红只进 diag_red，绿步两边都不进。

    这一格钉的是**落点**而不是 gate_reds 的返回值：上一轮 gate_reds 被 selftest 直测、
    main 却自己内联同一条判断，于是"内联那侧被注坏"在 12/12 里完全隐形（lane 6 实测注码后仍退 0）。
    """
    diag = next((s[0] for s in STEPS if s[0] in DIAG), None)
    gate = next((s[0] for s in STEPS if s[0] not in DIAG), None)
    if diag is None or gate is None:
        return False, "判错：DIAG 或步骤名单为空，这格没有靶子"
    for name, want_failed, want_diag, want_tag in ((gate, [gate], [], "红"),
                                                   (diag, [], [diag], "诊断·红"),
                                                   (gate, [], [], "绿")):
        failed: list[str] = []
        dr: list[str] = []
        tag = tally(name, [] if want_tag == "绿" else ["rc=1"], failed, dr)
        if (failed, dr, tag) != (want_failed, want_diag, want_tag):
            return False, (f"判错：{name} 的 {'红' if want_tag != '绿' else '绿'}读数落点不对"
                           f"（failed={failed} diag={dr} tag={tag}）")
    return True, f"{gate} 红→failed、{diag} 红→只印"


def probe_missing_cmd_file() -> tuple[bool, str]:
    """反向格 7：命令点名的 docs/ 路径不存在要停机、存在要放行，且红因必须点名"不存在"。

    为什么单列一格：preflight 那条分支的前缀是 `scripts/` **或** `docs/`，而四格老探针喂的都是
    临时目录里的绝对路径（压根走不到这条支路）——把 `docs/` 从判断里删掉，
    "台账被改名、闭合门那几步其实在读一份不存在的文件"就没人拦。
    红因认"不存在"而不是认关键字都行：两支（不存在 / 语法坏了）共用同一个 argv 循环，
    不绑红因就可能证明到错的那条支路。
    """
    real = next((a for _, _, argv, _ in STEPS for a in argv[1:] if a.startswith("docs/")), "")
    if not real:
        return False, "判错：步骤名单里没有 docs/ 路径参数，这格没有靶子"
    gone = real.replace(".jsonl", "-no-such-r99.jsonl")
    if not (ROOT / real).exists() or (ROOT / gone).exists():
        return False, f"判错：探针的靶子文件状态不对（{real}）"
    try:
        preflight([("probe", ".", ["true", real], [])])
    except SystemExit as e:
        return False, f"判错：真存在的 docs 路径被误伤（{str(e)[:24]}）"
    try:
        preflight([("probe", ".", ["true", gone], [])])
        return False, "判错：不存在的 docs 路径被放行"
    except SystemExit as e:
        why = str(e)
    if "不存在" not in why:
        return False, f"判错：开火的不是存在性那条支路（{why[:24]}）"
    return True, why[:20]


def run_step(name: str, cwd: str, argv: list[str], conds, logdir: Path) -> tuple[list[str], float]:
    t0 = time.time()
    env = db_env(dict(os.environ))
    env.setdefault("GIN_MODE", "test")
    try:
        p = subprocess.run(argv, cwd=ROOT / cwd, capture_output=True, text=True,
                           timeout=STEP_TIMEOUT.get(name, 1800), env=env)
        raw, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or b"").decode(errors="replace") + (e.stderr or b"").decode(errors="replace") \
            if isinstance(e.stdout, bytes) else str(e.stdout or "")
        raw, rc = f"{out}\n[TIMEOUT {STEP_TIMEOUT.get(name, 1800)}s]", 124
    out = ANSI.sub("", raw)
    dt = time.time() - t0
    logdir.mkdir(parents=True, exist_ok=True)
    (logdir / f"{name}.log").write_text(
            f"$ {' '.join(argv)}\n(cwd={cwd})\nrc={rc}  {dt:.1f}s\n\n{out}\n", encoding="utf-8")
    return check(rc, raw, conds), dt


def probe_transcript_name() -> tuple[bool, str]:
    """取证名两型都要判：子集跑写成全量名＝把别人的证据原地抹掉（本轮实测撞到的）；
    全量跑写不成全量名＝台账找不到它引用的那份东西。只测前一侧的话，
    把两个名字对调的改法也能"通过"。
    """
    if transcript_name([]) != "merge-gate-full.log":
        return False, "判错：全量跑没写 merge-gate-full.log"
    for only in (["go-build"], ["go-build", "gofmt"]):
        n = transcript_name(only)
        if n == "merge-gate-full.log":
            return False, f"判错：--only {' '.join(only)} 被允许写全量取证名"
        if "only" not in n:
            return False, f"判错：子集取证名没标出自己是子集：{n}"
    wide = transcript_name([f"step-{i}" for i in range(40)])
    if len(wide) > 80:
        return False, f"判错：名单过长会撞文件名上界：{len(wide)} 字"
    return True, "子集永不写 full 名"


def selftest() -> int:
    """反向测：把"看着绿其实没跑""看着红其实全绿"这两类读数喂给 check/preflight，各自必须判对。

    新校验不反向测＝没有校验（本仓口径）。这里不真跑任何门禁——判据本身才是被测对象。
    """
    cases = [
        ("rc≠0 要红", check(1, "", []), True),
        ("rc=0 但判据行没出现要红", check(0, "nothing relevant\n", [("re", r"^ok\s")]), True),
        ("rc=0 但出现 FAIL 要红", check(0, "ok x\nFAIL y\n", [("nore", r"(^|\n)FAIL")]), True),
        ("gofmt 有输出要红（rc 恒 0 那种）", check(0, "a.go\nb.go\n", [("empty", "")]), True),
        ("全绿不该红", check(0, "ok  hivemtk-user/internal/service\n",
                        [("re", r"^ok\s"), ("nore", r"(^|\n)FAIL")]), False),
        # vitest 即使输出重定向到文件也发 ANSI：剥色后 762 passed 才算数。这一格是"误伤"那半边——
        # 少了它，门会在别人的全绿结果上喊红，而喊红的门没人信。
        ("带 ANSI 颜色的绿读数不该红",
         check(0, "\x1b[2m      Tests \x1b[22m \x1b[32m762 passed | 7 skipped\x1b[39m (769)\n",
               [("re", r"Tests\s+\d+ passed"), ("nore", "failed")]), False),
        # 这两格钉的是 DIAG 这张**豁免表**：上一格红要算门红、诊断步的红不许算，
        # 少任何一侧，"只印不门"就既能悄悄吞掉真红、也能被反向滥用成"全都只印不门"。
        ("诊断步的红不门控（但照常印）", gate_reds("ci-step-coverage", ["rc=1"]), False),
        ("门步的红必须门控", gate_reds("go-build", ["rc=1"]), True),
    ]
    # 这七格不是"喂一段读数"能测的：被测对象是 preflight / parse_ok / DIAG / --only 过滤 / 落点分流
    # 这些**会停机或会放行**的判据，只能真调一次看它停不停，所以各配一个探针函数。
    # 「坏门禁脚本」那一格另有边界：它拦的是**别的**脚本坏了——本脚本自己坏的话，
    # 整支 --selftest 都不存在，任何一格都红不出来。所以 --selftest 本身也被挂进步骤名单
    # （merge-gate-selftest 那一格），退 1 会在合并那趟里被点名。
    probes = [("名单非空要拦空过滤", probe_empty_list),
              ("口令拿不到要停机", probe_no_db_password),
              ("坏门禁脚本要点名", probe_bad_script),
              ("诊断豁免指错步要停机", probe_diag_stale_name),
              ("--only 打错步名要停机", probe_only_typo),
              ("红落点：门控/只印/绿分流", probe_diag_routing),
              ("点名的仓内文件缺失要停机", probe_missing_cmd_file),
              ("子集跑不许写全量取证名", probe_transcript_name)]
    reds = 0
    for label, got, want_red in cases:
        red = bool(got)
        reds += 1 if red == want_red else 0
        # 判绿的那一侧也必须印出来：只印「红 ✓」会让人以为这些格都是"注进去红了"，
        # 而那格合法读数恰恰是"不许红"的那半边（缺它就等于把假红当门有牙）。
        tag = f"判对 ✓（{'红' if red else '绿'}）" if red == want_red \
            else f"判错 ✗（{'漏判' if not red else '误伤'}）"
        print(f"[反向] {label:<34} {tag}")
    for label, fn in probes:
        ok, why = fn()
        reds += 1 if ok else 0
        # 探针判错也要带 ✗：cases 那半边的失败行有 ✗、这半边只印散文，下游按关键字抓失败行就会
        # 把"探针判错"整类漏掉（本轮反向测驱动第一次就只抓到了 cases 那半边）。
        tag = f"判对 ✓（拦停：{why}）" if ok else f"判错 ✗（{why.removeprefix('判错：')}）"
        print(f"[反向] {label:<34} {tag}")
    total = len(cases) + len(probes)
    print(f"===== merge-gate --selftest：{reds}/{total} 种坏读数各自拦停/判红，绿读数不误伤 =====")
    return 0 if reds == total else 1


def transcript_name(only: list[str]) -> str:
    """取证文件名带上"这趟是什么形状的跑"——否则子集跑会把全量跑的证据**原地抹掉**。

    实测到的形状：`--round` 的默认值正是台账引用的那一轮（R22-protocol），而台账里
    "合并门禁全量 rc=0"那条（P-33）引用的 `merge-gate-full.log`，盘上内容已被某次合法的
    局部调试（`--only action-runtime`，2 步）覆盖成子集跑的汇总。闭合门只做**存在性**检查，
    所以它绿着，而它核的那份证据已经不再证明它承诺的那件事。文件名里的 "full" 是最容易骗人的字。
    规则：带 `--only` 的跑永不写 `merge-gate-full.log`（哪怕只筛掉一步）——宁可名字多起来，
    也不让"局部"有机会冒充"全量"。
    """
    if not only:
        return "merge-gate-full.log"
    tag = "+".join(only)
    return f"merge-gate-only-{tag}.log" if len(tag) <= 120 \
        else f"merge-gate-only-{len(only)}steps.log"


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--round", default="R22-protocol")
    ap.add_argument("--only", action="append", default=[])
    ap.add_argument("--list", action="store_true")
    ap.add_argument("--selftest", action="store_true")
    a = ap.parse_args()
    if a.selftest:
        return selftest()
    if a.list:
        for s in STEPS:
            print(s[0] + ("　〔诊断步：只印不门〕" if s[0] in DIAG else ""))
        return 0
    steps = filter_steps(STEPS, a.only)
    check_diag_names(STEPS)
    preflight(steps)
    logdir = Path(LOGS_REL) / a.round / "merge"
    failed, diag_red = [], []
    tpath = ROOT / LOGS_REL / a.round / transcript_name(a.only)
    t = Transcript(tpath)
    t.line(f"$ python3 scripts/merge-gate.py --round {a.round}"
           + "".join(f" --only {n}" for n in a.only))
    t.line(f"步骤名单（{len(steps)} 步，全程不带 -run）：{' '.join(s[0] for s in steps)}")
    t.line(f"合并门禁：{len(steps)} 步（其中诊断步 {len([s for s in steps if s[0] in DIAG])} 步只印不门，"
           f"见 DIAG），日志 → {logdir}/　取证 → {tpath.relative_to(ROOT)}")
    for name, cwd, argv, conds in steps:
        bad, dt = run_step(name, cwd, argv, conds, ROOT / logdir)
        # 诊断步也印"红"字与红因，只是不改判定：让豁免在日志里**看得见**，
        # 而不是把那一行折成绿（那才是真的假绿）。落点由 tally 一处决定（它同时是 --selftest 的靶子）。
        tag = tally(name, bad, failed, diag_red)
        t.line(f"[{tag}] {name:<28} {dt:>7.1f}s "
               + ("" if not bad else "｜ " + " ; ".join(bad)))
    ngate = len(steps) - len([s for s in steps if s[0] in DIAG])
    t.line(f"===== 合并门禁：{ngate - len(failed)}/{ngate} 门步绿"
          + ("，无红" if not failed else f"，红在：{' '.join(failed)}")
          + (f"；诊断步红 {len(diag_red)} 道不门控：{' '.join(diag_red)}" if diag_red
             else "；诊断步全绿")
          + " =====")
    rc = 1 if failed else 0
    t.close()
    return rc


if __name__ == "__main__":
    sys.exit(main())
