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
用法：python3 scripts/mut_startup_hook_p702.py   （须能连测试库）
"""
import hashlib
import os
import subprocess
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
SERVER = os.path.join(ROOT, "user-server")
SRC = os.path.join(SERVER, "internal/pkg/db/migrate.go")
TEST = "TestLegacyExternalOrderKeyHookFailsLoudlyNotFatally"
DEFINE = "func postMigrateDropLegacyExternalOrderKey(db *gorm.DB) {"

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


def main() -> int:
    env = dict(os.environ)
    envfile = os.path.join(ROOT, ".env")
    with open(envfile, encoding="utf-8") as fh:
        for line in fh:
            if line.startswith("POSTGRES_PASSWORD="):
                env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
                break
    env["POSTGRES_TEST_PORT"] = env.get("POSTGRES_TEST_PORT") or "8232"
    env["GOFLAGS"] = "-mod=mod"

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
    if "no tests to run" in ctrl_out:
        print(f"CONTROL-BROKEN：{TEST} 不在树里或 -run 名单没匹配上 ⇒ 整轮判据是空的")
        return 7
    if ctrl.returncode != 0:
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
