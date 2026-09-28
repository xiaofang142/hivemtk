#!/usr/bin/env python3
"""mut_startup_hook_p702.py —— 给 `TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally` 验牙（2 刀）。

被钉的决定在 `internal/pkg/db/migrate.go` 的 postMigrateDropLegacyExternalOrderKey：
这一格失败只 Warn、不 panic（panic 的后果是整个服务起不来，而它拦的是"改键之前的既有行为"）。
那条决定今天只写在注释里 ⇒ 注释不算判据，判据是那两臂 recover。本脚本把两处失败处置
分别改坏，断言那条用例必须红。

锚点必须**限定在钩子函数体内**：`if db == nil {` 在 migrate.go 里出现 5 次、
`\n\t\treturn\n\t}\n\tconst ddl` 出现 2 次，整文件替换会打到别人的分支上，
那时"红"证的是别的钩子，这一格就是假杀。

判据：killed == 2 且 alive == broken == 0，末了源文件 md5 与开头一致。
取证落点：控制组与两刀的原始输出逐格写进 `docs/superpowers/specs/ledger/logs/P702hook/<趟次戳>/`。
影子克隆里没有 `ROOT/.env` ⇒ 由调用方导出 `POSTGRES_TEST_PASSWORD`（否则控制组红是 ENV-BROKEN）。
本脚本**就地注码** `internal/pkg/db/migrate.go`（用完每刀立刻还原并比 md5）⇒ 只在私有树里跑，
绝不与他人共用一棵工作树；中途被 kill 会把注码留在树上，恢复方式是按 md5 认回基线字节。
用法：python3 scripts/mut_startup_hook_p702.py            （须能连测试库）
      python3 scripts/mut_startup_hook_p702.py --check   （只验两刀的锚点与用例名，只读不写、不连库）
"""
import hashlib
import os
import re
import subprocess
import sys
import time
from pathlib import Path

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SERVER = os.path.join(ROOT, "user-server")
SRC = os.path.join(SERVER, "internal/pkg/db/migrate.go")
# 逐格原始输出落进仓库树（带趟次戳、不复用）：原先只随 stdout 走、由调用方重定向到 /tmp，
# 重启即蒸发 ⇒ 台账里的读数没有产物可对。`.gitignore` 需为本轮次开例外。
LOGDIR = Path(ROOT) / "docs/superpowers/specs/ledger/logs/P702hook" / time.strftime("%Y%m%d-%H%M%S")
TEST = "TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally"
DEFINE = "func postMigrateDropLegacyExternalOrderKey(db *gorm.DB) {"
# 控制组红要先分"树的红"和"环境的红"：本电池连的是 TCP 测试库，影子克隆里没有 `.env`，
# 不导出 POSTGRES_TEST_PASSWORD 时红因是 SASL 认证失败——那既不是判据没牙，也不是树红，
# 把它报成"先修树"会把人支去改没坏的代码（实测：本轮反向趟就是这么被误判的）。
ENV_SIGNS = ("failed SASL auth", "password authentication failed",
             "failed to connect to", "connect: connection refused",
             "too many clients", "dial tcp")


from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）
def dump(tag, out):
    LOGDIR.mkdir(parents=True, exist_ok=True)
    with open(LOGDIR / (re.sub(r"[^A-Za-z0-9_.-]", "-", tag) + ".log"), "w", encoding="utf-8") as fh:
        fh.write(scrub(out))


CASES = [
    ("nil 守卫短路", "if db == nil {", "if db == nil && false {"),
    ("DROP 失败改 panic", "model.ExternalOrderLegacyOrderIDIndex, err))\n\t\treturn",
     'model.ExternalOrderLegacyOrderIDIndex, err))\n\t\tpanic(err)'),
]


def hook_span(text: str):
    """返回钩子函数体的 (起, 止) 字节区间；找不到或有第二个 } 提前收尾都算锚点失守。"""
    i = text.index(DEFINE)
    j = text.index("\n}\n", i)
    return i, j + 3


def md5(path: str) -> str:
    with open(path, "rb") as fh:
        return hashlib.md5(fh.read()).hexdigest()


def check_anchors() -> int:
    """锚点预检（`--check`）：只读不写、一次 `go test` 都不跑，因此也不需要测试库。

    必要性：两刀的锚点都限定在钩子函数体内，钩子一搬家、守卫一行文一改成整文件形状，
    放刀路径上得到的是 ANCHOR-MISS ⇒ broken，而它和"判据没牙"混在同一份报告里；
    用例一改名则是两刀全红在 `no tests to run`。这一趟在几毫秒内把这两种失效先点出来。
    """
    with open(SRC, encoding="utf-8") as fh:
        original = fh.read()
    bad = 0
    try:
        start, end = hook_span(original)
    except ValueError:
        print(f"  ✗ 钩子边界没找到：{DEFINE!r} 不在 {SRC}")
        print(f"锚点校验：{len(CASES) + 1} 格，1 格有问题")
        return 1
    body = original[start:end]
    for label, old, new in CASES:
        hits = body.count(old)
        if hits != 1:
            bad += 1
            print(f"  ✗ [{label}] 钩子体内锚点命中 {hits} 次（期望 1 ⇒ 判据会打到别人身上）")
            continue
        if original[:start] + body.replace(old, new) + original[end:] == original:
            bad += 1
            print(f"  ✗ [{label}] 补丁没落地（字节未变，这一刀永不开火）")
    pkg = Path(SERVER) / "internal" / "pkg" / "db"
    hosts = [q.name for q in sorted(pkg.glob("*_test.go"))
             if f"func {TEST}(" in q.read_text(encoding="utf-8")]
    if not hosts:
        bad += 1
        print(f"  ✗ 用例 {TEST} 在 {pkg} 的 *_test.go 里查无定义 ⇒ 两刀都会红在 no tests to run")
    else:
        print(f"  用例定义于 {', '.join(hosts)}")
    print(f"锚点校验：{len(CASES) + 1} 格，{bad} 格有问题")
    return 1 if bad else 0


def main() -> int:
    from battlog import identity, tee_to  # 判定行与逐格产物同处一地（LOGDIR/00-run.log）
    check = "--check" in sys.argv   # 与 mut_ledger_b16* 同形：本脚本没有别的参数，不必上 argparse
    # tee 必须在 `--check` 分流之前：只读预检的末行也是常驻判据，早先它只进终端 ⇒ 预检的绿没有产物可对。
    tee_to(LOGDIR / ("00-check.log" if check else "00-run.log"))
    identity(ROOT, label="基线字节", extra="｜就地注码 ⇒ 未入库字节也在本轮读数里")
    if check:
        return check_anchors()
    env = dict(os.environ)
    envfile = os.path.join(ROOT, ".env")
    # .env 不进 git ⇒ 影子克隆里根本没有它；此时调用方必须自己导出 POSTGRES_TEST_PASSWORD，
    # 否则下面的控制组会以"连不上测试库"的形态红（那是 ENV-BROKEN，不是判据有牙）。
    if os.path.exists(envfile) and "POSTGRES_TEST_PASSWORD" not in env:
        with open(envfile, encoding="utf-8") as fh:
            for line in fh:
                if line.startswith("POSTGRES_PASSWORD="):
                    env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
                    break
    env["POSTGRES_TEST_PORT"] = env.get("POSTGRES_TEST_PORT") or "8232"
    env["GOFLAGS"] = "-mod=mod"
    print(f"逐格日志目录：{LOGDIR}")

    with open(SRC, encoding="utf-8") as fh:
        original = fh.read()
    orig_md5 = md5(SRC)
    killed = alive = broken = 0
    total = len(CASES)

    # 控制组：先证明**没放刀时这条用例是绿的**。少了这一步，下面每一格的 "KILLED"
    # 都可能只是树本来就红（测试库没起来、包编译不过），那与判据有没有牙无关。
    ctrl = subprocess.run(
        ["go", "test", "./internal/pkg/db/", "-run", f"^{TEST}$", "-count=1"],
        cwd=SERVER, env=env, capture_output=True, text=True)
    ctrl_out = ctrl.stdout + ctrl.stderr
    dump("00-control", ctrl_out)
    if "no tests to run" in ctrl_out:
        print(f"CONTROL-BROKEN：{TEST} 不在树里或 -run 名单没匹配上 ⇒ 整轮判据是空的")
        return 7
    if ctrl.returncode != 0:
        if any(s in ctrl_out for s in ENV_SIGNS):
            print("CONTROL-ENV-BROKEN：不是树的红，是连不上测试库 ⇒ 导出 "
                  "POSTGRES_TEST_PORT/POSTGRES_TEST_PASSWORD 后重跑（本趟未放刀，判据未验证）")
            print("\n".join(ctrl_out.splitlines()[-4:]))
            return 8
        print("CONTROL-RED：基线就是红的，先修树再放刀")
        print("\n".join(ctrl_out.splitlines()[-10:]))
        return 7
    print("CONTROL-GREEN ✅ 未放刀时该用例绿")

    for label, old, new in CASES:
        try:
            start, end = hook_span(original)
        except ValueError:
            print(f"[{label}] ANCHOR-MISS（找不到钩子边界，这一格无效）")
            broken += 1
            continue
        body = original[start:end]
        hits = body.count(old)
        if hits != 1:
            print(f"[{label}] ANCHOR-MISS（钩子体内锚点命中 {hits} 次，期望 1 ⇒ 判据会打到别人身上）")
            broken += 1
            continue
        mutated = original[:start] + body.replace(old, new) + original[end:]
        if mutated == original:
            print(f"[{label}] ANCHOR-MISS（补丁没落地）")
            broken += 1
            continue
        with open(SRC, "w", encoding="utf-8") as fh:
            fh.write(mutated)
        proc = subprocess.run(
            ["go", "test", "./internal/pkg/db/", "-run", f"^{TEST}$", "-count=1"],
            cwd=SERVER, env=env, capture_output=True, text=True)
        out = proc.stdout + proc.stderr
        dump(label, out)
        if "no tests to run" in out:
            print(f"[{label}] NO-TESTS-RAN（-run 名单没匹配上，这一格没跑）")
            broken += 1
        elif "[build failed]" in out or "cannot use" in out or "undefined:" in out:
            print(f"[{label}] BUILD-BROKEN（红因不是判据，先修刀）")
            print("\n".join(out.splitlines()[-8:]))
            broken += 1
        elif proc.returncode == 0:
            print(f"[{label}] ALIVE —— 改坏了它还是绿的")
            print("\n".join(out.splitlines()[-6:]))
            alive += 1
        else:
            reason = [ln for ln in out.splitlines() if "panic" in ln or "钩子" in ln]
            print(f"[{label}] KILLED rc={proc.returncode}")
            for ln in reason[:3]:
                print("      " + ln.strip())
            killed += 1

        with open(SRC, "w", encoding="utf-8") as fh:
            fh.write(original)   # 每刀用完立刻还原
        if md5(SRC) != orig_md5:
            print(f"[{label}] RESTORE-MISMATCH：这一格还原后的字节与基线不同，立刻停手")
            return 8

    final_md5 = md5(SRC)
    print("──────")
    print(f"total={total} killed={killed} alive={alive} broken={broken}")
    print(f"md5_orig={orig_md5} md5_after={final_md5}")
    if final_md5 != orig_md5:
        print("RESTORE-MISMATCH")
        return 8
    if alive or broken or killed != total:
        return 7
    print("ALL-CASES-KILLED-AND-SOURCE-RESTORED")
    return 0


if __name__ == "__main__":
    sys.exit(main())
