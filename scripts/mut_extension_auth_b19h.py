#!/usr/bin/env python3
"""批19h 变异电池（JS 侧）：扩展的「静默续期」与 popup 的「渲染出口转义」逐消费点验牙。

为什么要有这条电池（而不是"两条测试文件全绿"就算完）：
- 令牌那半边锁的是**时序**（一次性轮换下"续期成功"≠"这次请求用的令牌还活着"），
  这种锁最容易写成"看起来对但换回旧实现照样绿"的形状。A1~A4 四刀分别对应四种偷懒形态：
  续了期仍发旧令牌 / 401 兜底不看盘就清盘 / 换来的新令牌不落盘 / 续期窗口写反。
- 转义那半边有 **11 个插值 + 5 个转义分支**，"整段都 escapeHtml 了"这句话只要有一处漏掉
  就不再是真的。R1~R11 逐插值一刀（含 `step_index` 这种"看着一定是数字"的一处），
  R12/R13 砍 escapeHtml 表里的一个分支——引号分支只有属性位能看见，& 分支只有"不吞字"能看见。

已知等价（这两格**预期存活**，理由写在这里而不是事后编）：
- A5：401 兜底用 `{...auth, token}`（实际发出的那把）而不是 `auth`（本地那把）。
  一次性轮换语义下 auth.token 必然是 token 的前身 ⇒ 前身必已拉黑，两把都救不了，
  这条区别在本仿真服务端里观察不到。它守的是「别哪天服务端改成双令牌窗口时无从下手」，
  不是当下行为，所以不为它补用例（补出来的用例只能测仿真服务端，是自证）。
- R13：escapeHtml 表的 `'→&#39;` 分支。模板属性一律双引号，单引号在双引号属性里
  本就劈不开标签 ⇒ 当下不可观测。保留分支的理由是"不许依赖模板的引号风格"，属防御性。

口径（沿用批17 电池）：控制组必须 rc==0、ran==12、skip==0，否则整轮判"无法判定"直接停机；
每个变异体 cp 备份 + 逐次 md5 比对还原；注码必须断言命中恰好一次；
「红了」不算杀——必须点到本格预期那条用例名（中文用子串匹配）；
等价格外若真被杀掉，同样只报告不判失败（说明上面的论证错了，得回头看）。

用法：python3 scripts/mut_extension_auth_b19h.py [--keep] [--clone DIR]
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
from mut_dispose import dispose, workdir

ROOT = Path(__file__).resolve().parent.parent          # <repo>/scripts/ → 上一级
WEB = ROOT / "user-web" / "browser_automation"
API = Path("src/core/api-client.js")
RENDER = Path("src/core/render.js")
TESTS = ["test/batch19h-token-rotation.test.js", "test/batch19h-render-escape.test.js"]
RUN_N = 12                                              # 3 条令牌轮转 + 9 条渲染转义

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def read(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    """恰好命中一次，否则当场死——静默的"没改到"是最便宜的假绿。"""
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:90]!r}")
    return text.replace(old, new, 1)


def js_prepare(dst: Path) -> Path:
    work = dst / "web"
    work.mkdir(parents=True)
    for item in ("src", "test", "package.json", "vitest.config.js"):
        s = WEB / item
        if not s.exists():
            raise SystemExit(f"缺少 {s}")
        (shutil.copytree if s.is_dir() else shutil.copy2)(s, work / item)
    nm = WEB / "node_modules"
    if not nm.is_dir():
        raise SystemExit(f"{nm} 不存在——先在 user-web/browser_automation 里 npm install")
    (work / "node_modules").symlink_to(nm.resolve())
    return work


def js_run(work: Path):
    p = subprocess.run(["npx", "vitest", "run", *TESTS], cwd=work,
                       capture_output=True, text=True, timeout=900, env=os.environ)
    out = ANSI.sub("", p.stdout + p.stderr)
    killed = []
    for ln in out.splitlines():
        if "×" not in ln:
            continue
        name = re.sub(r"\s+\d+(\.\d+)?(ms|s)$", "", ln.split("×", 1)[1].strip())
        killed.append(name)
    m = re.search(r"Tests\s+(\d+) passed", out)
    ran = int(m.group(1)) if m else 0
    m = re.search(r"(\d+) skipped", out)
    skipped = int(m.group(1)) if m else 0
    return p.returncode, killed, ran, skipped, out


# ------------------------------------------------------------------ 变异表
# (格号, 说明, 文件, 锚点, 替换, 预期被杀用例名的子串 / None=已论证等价)
def cells():
    return [
        ("A1", "续期成功却仍发本地那把（已拉黑的旧令牌）", API,
         "  const token = await ensureFreshToken(auth);\n",
         "  await ensureFreshToken(auth);\n  const token = auth.token;\n",
         "exp 剩余 <1h"),
        ("A2", "401 兜底不再先看盘（把赢家换来的令牌当失效证据抹掉）", API,
         "    // 先看盘：令牌跟我们用的这把不一样 ⇒ 另有在途调用刚完成轮换，把我们这把拉黑了。\n"
         "    // 拿它重试一次即可——这一腿必须排在清盘之前，否则并发的一方会把另一方换来的\n"
         "    // 有效令牌当作「已失效」的证据抹掉（一次性轮换 + 并发在途是常态，不是边角）。\n"
         "    const cur = await loadAuth();\n"
         "    if (cur.token && cur.token !== token) {\n"
         "      return await apiCall(path, { method, body, retry: false });\n"
         "    }\n",
         "",
         "两个并发调用"),
        ("A3", "换来的新令牌不落盘（下次 loadAuth 仍读旧的）", API,
         "\n    await saveAuth({ ...auth, token: body.data.token, exp });",
         "",
         "exp 剩余 <1h"),
        ("A4", "续期窗口写反（充裕时猛刷 / 该刷时不刷）", API,
         "auth.exp * 1000 >= Date.now() + 3600 * 1000",
         "auth.exp * 1000 <= Date.now() + 3600 * 1000",
         "令牌充裕时不发 refresh"),
        ("A5", "401 兜底改用本地 auth 里的旧值去续期", API,
         "    if (await refreshTokenOnce({ ...auth, token })) {",
         "    if (await refreshTokenOnce(auth)) {",
         None),
        # ---- 渲染出口：逐插值一刀（"少一层 escapeHtml"，其余不变）
        ("R1", "任务标题不转义（库里文本当作 HTML 进 DOM）", RENDER,
         "#${escapeHtml(t.id)} ", "#${t.id} ", "id 进属性位"),
        ("R2", "data-run 属性位不转义（引号劈开标签）", RENDER,
         'data-run="${escapeHtml(t.id)}"', 'data-run="${t.id}"', "id 进属性位"),
        ("R3", "status 标签文本不转义", RENDER,
         '<span class="tag ${tagCls}">${escapeHtml(t.status)}</span>',
         '<span class="tag ${tagCls}">${t.status}</span>', "任务卡片的"),
        ("R4", "task_type 不转义", RENDER,
         "<span>${escapeHtml(t.task_type)}", "<span>${t.task_type}", "任务卡片的"),
        ("R5", "会话状态不转义", RENDER,
         "`<b>${escapeHtml(sess.status)}</b>`", "`<b>${sess.status}</b>`", "会话状态与错误文案"),
        ("R6", "错误文案不转义", RENDER,
         "` · ${escapeHtml(sess.error_msg)}`", "` · ${sess.error_msg}`", "步骤行的"),
        ("R7", "步骤动作名不转义（Brain 动作名 = LLM 原文）", RENDER,
         ". ${escapeHtml(s.action)} ", ". ${s.action} ", "步骤行的"),
        ("R8", "步骤状态不转义", RENDER,
         "${escapeHtml(s.status)}", "${s.status}", "步骤行的"),
        ("R9", "编号位不转义（step_index 也是 JSON 字段）", RENDER,
         "${escapeHtml(s.step_index + 1)}.", "${s.step_index + 1}.", "编号位"),
        ("R10", "name 不转义", RENDER,
         " ${escapeHtml(t.name)}</div>", " ${t.name}</div>", "任务卡片的"),
        ("R11", "escapeHtml 少转义双引号（属性位破门）", RENDER,
         "'&': '&amp;', '<': '&lt;', '>': '&gt;', '\"': '&quot;', \"'\": '&#39;'",
         "'&': '&amp;', '<': '&lt;', '>': '&gt;', \"'\": '&#39;'",
         "引号在属性位"),
        ("R12", "escapeHtml 少转义 `&`（实体被二次解码）", RENDER,
         "'&': '&amp;', '<': '&lt;'", "'<': '&lt;'", "& 分支不是摆设"),
        ("R13", "escapeHtml 少转义单引号（预期存活：属性全双引号）", RENDER,
         ", \"'\": '&#39;'", "",
         None),
    ]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--keep", action="store_true")
    ap.add_argument("--clone", default="")
    args = ap.parse_args()

    tmp, owned = workdir(args.clone or None, prefix="b19hjs-", repo_root=ROOT)
    tmp.mkdir(parents=True, exist_ok=True)
    print(f"私有作业目录：{tmp}")

    work = js_prepare(tmp)
    files = {rel: work / rel for rel in (API, RENDER)}
    originals = {rel: read(p) for rel, p in files.items()}
    basemd5 = {rel: md5_bytes(p) for rel, p in files.items()}

    # 注码前置：所有锚点先在原文里命中恰好一次（跑到一半才发现锚写错＝白跑十几轮）
    for code, _desc, rel, old, new, _exp in cells():
        sub_once(originals[rel], old, new, code)
    print("[注码前置] 十三格锚点各命中一次")

    rc, killed, ran, skipped, out = js_run(work)
    print(f"\n[控制组] rc={rc} ran={ran} skip={skipped} 红名={killed}")
    if rc != 0 or ran != RUN_N or skipped > 0 or killed:
        print(out[-3000:])
        raise SystemExit(f"[控制组] 不干净（要求 rc=0 / ran={RUN_N} / skip=0）——后面所有红/绿都不可信")

    problems = []
    same_family = {}
    for code, desc, rel, old, new, exp in cells():
        files[rel].write_text(sub_once(originals[rel], old, new, code))
        rc, killed, ran, skipped, out = js_run(work)
        same_family[code] = tuple(sorted(killed))
        if exp is None:
            verdict = "等价（真被杀了→回去复核论证）" if rc != 0 else "等价（如期存活）"
            print(f"{code:<4} {desc[:52]:<54} {verdict} ｜ 红名={killed[:2]}")
        else:
            hit = [k for k in killed if exp in k]
            if rc != 0 and hit:
                verdict = "杀掉"
            elif rc != 0:
                verdict = f"红了但没点到 {exp!r}"
            else:
                verdict = "存活=洞"
            print(f"{code:<4} {desc[:52]:<54} {verdict:<10} pass={ran} skip={skipped} ｜ "
                  + " | ".join(k[:58] for k in killed[:2]))
            if verdict != "杀掉":
                problems.append(f"{code} {verdict}：{desc}")
                print(out[-2500:])
        files[rel].write_text(originals[rel])
        if md5_bytes(files[rel]) != basemd5[rel]:
            raise SystemExit(f"{code} 还原后 md5 不一致，停机")

    items = sorted(same_family.items())
    for i in range(len(items)):
        for j in range(i + 1, len(items)):
            if items[i][1] and items[i][1] == items[j][1]:
                print(f"  [同族] {items[i][0]} 与 {items[j][0]} 杀掉的用例集合逐字相同"
                      f"（{len(items[i][1])} 条）——两格盯的是同一句断言")

    print("\n[还原] 两个源文件全量还原，md5 一致")
    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
    if problems:
        print("===== 电池判定：有格未杀 =====")
        for p in problems:
            print(" - " + p)
        return 1
    print(f"===== 电池判定：{len(cells())} 格全部判定完毕（预期 16 杀 + 2 已论证等价），无意外存活 =====")
    return 0


if __name__ == "__main__":
    sys.exit(main())
