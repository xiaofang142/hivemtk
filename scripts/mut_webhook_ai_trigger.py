#!/usr/bin/env python3
"""mut_webhook_ai_trigger.py —— 「一条入站消息的 AI 回复只有一个归属人」这条守卫的常驻杀伤电池。

为什么要有这一份：`user-server/internal/service/webhook.go` 里那句
`if triggerAI && channel != ChannelQQ` 是全渠道"AI 触发归属"的唯一收口点 ——
QQ 的归属在中台 Ingress，其余渠道的归属在 handleJob。它一旦被摘掉半句或整段短路，
表现是"客户收不到回复"或"同一条消息收到两份回复"，而不是报错，属最难发现的那类回归。
该行的三臂用例（`webhook_batchk_qq_dual_trigger_test.go`）原先只钉住了 QQ 与抖音两家；
企微 / 飞书 / WhatsApp 三家「账号级 AI 开关 → handleJob 触发」的端到端臂是后补的
（`webhook_batchk_nonqq_trigger_test.go`，登记见审计文档 §19.5-1）。
补腿只做一次性 `/tmp` 驱动的话，读数就成了只能引用、无法复跑的二手证据，
所以五刀与判据常驻在本脚本里。

五刀的落点与期望（expect 是实测出来的名单，不是"应该红在哪格"推的）：
  V1 整段短路（`if false && triggerAI && …`）      ⇒ 企微／飞书／WhatsApp 三子例全红，且 QQ 用例里那条抖音臂也红，TG 的私聊正控制格也红
  V2 只把企微除名（`&& channel != ChannelWeCom`）   ⇒ 只有 WeCom 子例红，另两家与 QQ、TG 两格必须仍绿
  V3 只把飞书除名（`&& channel != ChannelFeishu`）  ⇒ 只有 Feishu 子例红，另两家与 QQ、TG 两格必须仍绿
  V4 只把 WhatsApp 除名（`&& channel != ChannelWhatsapp`）⇒ 只有 WhatsApp 子例红，另两家与 QQ、TG 两格必须仍绿
  V5 删掉 `tgExtra.GateHandled` 整段守卫块              ⇒ 只有 TG 的 `/start` 那一格红（它期望 0 变 1），
     私聊正控制格与另四臂必须仍绿 —— 这一刀打在守卫块上而不是触发行上，是另一处承重墙。

V1 与 V2–V4 打在**同一行源码**却红在**不同断言集合**，V5 打在**紧邻上方的守卫块**却只红在**「不该触发」那一格**：
"一处符号多处消费要逐格拆刀"与"抑制臂只能用反向格杀"两条口径在这里分别是判据。
V2–V4 的"另几家必须仍绿"是本电池比常规"点名杀手"多出来的一条硬判据：
表驱动用例最常见的失效是几条腿其实走的是同一条路径（渠道分支没真分开），
只断言"某一格红"证不出各臂有主 —— 必须同时证明其余臂在这一刀下不受影响。

跑在哪棵树：只在 `git clone --shared` 出来的私有克隆里注码（共享工作树里有并行会话的字节），
基线字节＝克隆 HEAD，身份行现读 tip 短 SHA ⇒ 本电池断言的是**已入库**的字节；
危险 `--clone` 由 `mut_dispose.workdir()` 挡在装架之前。
与不连库的那批电池不同，这一族跑的是 `internal/service` 的 handleJob 端到端腿，**必须要测试库**：
`POSTGRES_TEST_PORT`（默认 8232）与 `POSTGRES_TEST_PASSWORD` 都由本脚本备好再注入子进程环境 ——
`testutil` 只读进程环境（`POSTGRES_TEST_PASSWORD`，回落进程里的 `POSTGRES_PASSWORD`，
见 `internal/pkg/testutil/testdb.go:351`），**不会自己去读 `user-server/.env`**，
缺它时用例红在 `failed SASL auth`，那是环境不是判据。取值只进环境，不打印、不入档
（工具链与盘满同样单列 ENV-BROKEN）。

判据：控制组必须绿且 ran 名单含全部八枚名字（父用例与子用例都点名）；五格全 KILLED；每格 `ran` 与控制组相等；
每格的"必须仍绿"名单在红名单里零命中；末了 `webhook.go` 的 md5 与基线一致。
任一条不成立退 1。判据类别缺一格也算不成立（SURVIVED／RED-UNNAMED／BUILD-BROKEN／
ENV-BROKEN／NO-RUN 都记账，不在"杀了 4/5"时退 0）。

取证落点：控制组与逐刀原始输出写进 `docs/superpowers/specs/ledger/logs/AiTrigger/<趟次戳>/`，
判定行同时 tee 到该目录的 `00-run.log`（落盘前过 `redact.scrub`）。

用法：
  python3 scripts/mut_webhook_ai_trigger.py                    # 五刀全族（要编 Go，要测试库）
  python3 scripts/mut_webhook_ai_trigger.py --check            # 锚点/用例名预检（要装架，不跑 go test）
  python3 scripts/mut_webhook_ai_trigger.py --check-tree       # 同上但对着当前树：CI 的落点用这一枚
  python3 scripts/mut_webhook_ai_trigger.py --selftest         # 六格预检反向＋四腿归类反向（不读源码、不跑 go）
  python3 scripts/mut_webhook_ai_trigger.py --clone <仓库外空目录> [--keep]
"""
import argparse
import hashlib
import os
import re
import subprocess
import sys
import time
from pathlib import Path

from battlog import identity, tee_to
from mut_dispose import dispose, dispose_at_exit, workdir
from redact import scrub  # 落盘前脱敏：常驻产物要过 gitleaks（见 scripts/redact.py 的 why）

ROOT = Path(os.path.dirname(os.path.dirname(os.path.abspath(__file__)))).resolve()
SERVER = "user-server"
SRC_REL = "user-server/internal/service/webhook.go"
PKG_REL = "user-server/internal/service"
RUNNER = ("TestBatchK_NonQQHomeChannelsTriggerAIExactlyOnce"
          "|TestBatchK_QQHandleJobDoesNotDoubleTriggerAI"
          "|TestTGGateHandledSuppressesSalesTrigger")
LOGROOT = ROOT / "docs/superpowers/specs/ledger/logs/AiTrigger"
LOGDIR = None  # main() 里按趟次戳定；常驻件不许复用上一轮的目录（同路径＝抹掉旧读数）

NQP = "TestBatchK_NonQQHomeChannelsTriggerAIExactlyOnce"
QQ = "TestBatchK_QQHandleJobDoesNotDoubleTriggerAI"
TG = "TestTGGateHandledSuppressesSalesTrigger"
WECOM, FEISHU, WAPP = f"{NQP}/WeCom", f"{NQP}/Feishu", f"{NQP}/WhatsApp"
# TG 那一枚是互为对照的两格：正控制格（私聊普通消息⇒恰好 1）与抑制格（/start⇒恰好 0）。
# 两格分属不同刀的判据集合：整段短路只红在正控制格，删守卫只红在抑制格。
TG_ON, TG_OFF = f"{TG}/PlainPrivateTriggers", f"{TG}/StartGateSuppresses"
OTHERS = sorted([NQP, WECOM, FEISHU, WAPP, QQ])

# 触发块那一行的原文（V1–V4 全打在这里，锚点必须命中恰好 1 次）
ANCHOR = "\tif triggerAI && channel != ChannelQQ {\n"
# 紧邻上方的 TG /start 网关抑制块（V5 的锚点，同样要求恰好 1 次）
GUARD = ("\tif channel == ChannelTelegram && tgExtra != nil && tgExtra.GateHandled {\n"
         "\t\tif tgExtra.GateMuted {\n"
         "\t\t\tlogger.Infof(\"[Webhook] TG 群门控互锁：发言人未通过验证，不触发 AI event=%s chat=%s sender=%s\",\n"
         "\t\t\t\tjob.event.EventID, payload.ChatID, payload.Sender)\n"
         "\t\t}\n"
         "\t\ttriggerAI = false // /start 网关验证已消费 / 门控群未验证成员发言\n"
         "\t}\n")

# (格名, 这一刀改坏的是什么, old, new, 必须红的名单, 必须仍绿的名单)
CELLS = [
    ("V1", "整段短路 ⇒ 五家的触发归属一起没了",
     ANCHOR, "\tif false && triggerAI && channel != ChannelQQ {\n",
     sorted(OTHERS + [TG, TG_ON]), [TG_OFF]),
    ("V2", "只把企微从触发块除名 ⇒ 只有企微那条客户收不到回复",
     ANCHOR, "\tif triggerAI && channel != ChannelQQ && channel != ChannelWeCom {\n",
     [NQP, WECOM], sorted([FEISHU, WAPP, QQ, TG, TG_ON, TG_OFF])),
    ("V3", "只把飞书除名",
     ANCHOR, "\tif triggerAI && channel != ChannelQQ && channel != ChannelFeishu {\n",
     [NQP, FEISHU], sorted([WECOM, WAPP, QQ, TG, TG_ON, TG_OFF])),
    ("V4", "只把 WhatsApp 除名",
     ANCHOR, "\tif triggerAI && channel != ChannelQQ && channel != ChannelWhatsapp {\n",
     [NQP, WAPP], sorted([WECOM, FEISHU, QQ, TG, TG_ON, TG_OFF])),
    ("V5", "删掉 GateHandled 守卫 ⇒ /start 之后又追一份 AI 回复",
     GUARD, "",
     [TG, TG_OFF], sorted([TG_ON] + OTHERS)),
]

TALLY = ["KILLED", "SURVIVED", "RED-UNNAMED", "BUILD-BROKEN", "ENV-BROKEN", "NO-RUN"]

# 环境红与树红必须分开：报成"先修树"会把人支去改没坏的文件。这一族的前置是
# 测试库可达（SASL／端口／连接数）＋ Go 工具链＋可写的盘，三类签名各自成组。
ENV_SIGNS = ("failed SASL auth", "connection refused", "no such host",
             "too many clients", "does not exist (SQLSTATE 3D000)",
             "library 'resolv' not found", "no space left on device", "clang: error",
             "cannot find package", "cannot find GOROOT", "signal: killed")

ANSI = re.compile(r"\x1b\[[0-9;]*m")


def read(p: Path) -> str:
    return p.read_text(encoding="utf-8")


def md5_bytes(p: Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


def dump(tag: str, out: str) -> None:
    LOGDIR.mkdir(parents=True, exist_ok=True)
    name = re.sub(r"[^A-Za-z0-9_.-]", "-", tag) + ".log"
    (LOGDIR / name).write_text(scrub(out), encoding="utf-8")


def sub_once(text: str, old: str, new: str, tag: str) -> str:
    n = text.count(old)
    if n != 1:
        raise SystemExit(f"{tag} 锚点命中 {n} 次（要求恰好 1 次）：{old[:80]!r}")
    return text.replace(old, new, 1)


def prepare(dst: Path, owned: bool = False) -> Path:
    """私有 `--shared` 克隆，基线字节＝HEAD。

    为什么不打脏文件：本电池五刀全打在已入库的 `webhook.go`，杀手用例也必须是被提交的那份
    （覆盖工作树的未提交测试字节会量出一个 HEAD 上不存在的世界）。锚点失守时该改的是脚本
    （跟着 tip 走），不是拿脏树冒充基线。
    """
    clone = dst / "clone"

    def bail(msg: str) -> None:
        """克隆已经建起来之后的中止路：先回收私有克隆，再出声。

        收尾闸原先只接在 `main()` 的出口上，装架函数里克隆之后的每一条 raise 都把整份
        私有克隆留在临时目录（一轮 50–70MB，而磁盘常态 98% 满）。三条**不**走这里：
        "已存在"（那份 clone/ 不是本电池建的）、"克隆失败"（目录归属还没定）、
        "md5 不一致"（本电池没有该站点）。
        """
        if (dst / "clone").exists():
            dispose(dst, owned=owned, keep=False, repo_root=ROOT)
        raise SystemExit(msg)

    if clone.exists():
        raise SystemExit(f"{clone} 已存在（换 --clone 目录或先删）")
    r = subprocess.run(["git", "clone", "--shared", "--no-checkout", str(ROOT), str(clone)],
                       capture_output=True, text=True, timeout=900)
    if r.returncode != 0:
        raise SystemExit("克隆失败：" + (r.stdout + r.stderr)[-400:])
    b = subprocess.run(["git", "checkout", "-f", "master"], cwd=clone,
                       capture_output=True, text=True, timeout=900)
    if b.returncode != 0:
        bail("checkout 失败：" + (b.stdout + b.stderr)[-400:])
    return clone


def test_env(clone: Path) -> dict:
    """测试库参数只进子进程环境：口令值不打印、不入档（取值口径见 scripts/redact.py）。"""
    env = dict(os.environ)
    env.setdefault("GIN_MODE", "test")
    env.setdefault("GOFLAGS", "-mod=mod")
    env.setdefault("GOCACHE", os.environ.get("R45_MUT_GOCACHE", "/tmp/gocache-r45mut"))
    env.setdefault("POSTGRES_TEST_PORT", "8232")
    if not env.get("POSTGRES_TEST_PASSWORD"):
        for candidate in (clone / SERVER / ".env", ROOT / SERVER / ".env"):
            if not candidate.exists():
                continue
            for line in read(candidate).splitlines():
                if line.startswith("POSTGRES_PASSWORD="):
                    env["POSTGRES_TEST_PASSWORD"] = line.split("=", 1)[1].strip()
                    break
            if env.get("POSTGRES_TEST_PASSWORD"):
                break
    return env


def defined_tests(pkg: Path) -> set:
    """`pkg` 里 `func TestXxx(` 定义的名字集合：expect 的父用例必须在此，否则那一格红在
    `no tests to run`——那是用例改名留下的化石，不是判据开火。"""
    names = set()
    for f in sorted(pkg.glob("*_test.go")):
        names |= set(re.findall(r"^func (Test[A-Za-z0-9_]*)\(", read(f), re.M))
    return names


def check_cells(text: str, cells, tests: set):
    """预检内核（纯函数，`--check` 与 `--selftest` 共用同一份判据）。四查：
    ① 锚点在整份文件里命中恰好 1 次（多处＝一刀打到别人的分支上，那是假杀）；
    ② 注码真让字节变了（old==new 的永不开火格）；
    ③ expect 里每个父用例名在本包有定义；
    ④ "必须仍绿"名单与"必须红"名单不相交（相交＝这一格自己说不清该红还是该绿）。
    返回 (坏格数, 逐行读数)。"""
    lines, bad = [], 0
    for code, desc, old, new, red, green in cells:
        hits = text.count(old)
        if hits != 1:
            bad += 1
            lines.append(f"  ✗ {code} 锚点命中 {hits} 次（要求恰好 1 次）：{old[:70]!r}")
            continue
        if text.replace(old, new, 1) == text:
            bad += 1
            lines.append(f"  ✗ {code} 注码打完了而字节没变（这一格永不开火）")
            continue
        missing = sorted({n.split("/")[0] for n in list(red) + list(green)} - tests)
        if missing:
            bad += 1
            lines.append(f"  ✗ {code} 名单里的用例 {missing} 在本包 *_test.go 里查无定义"
                         f" ⇒ 这一格会红在 no tests to run，不是杀在判据上")
            continue
        overlap = sorted(set(red) & set(green))
        if overlap:
            bad += 1
            lines.append(f"  ✗ {code} 同一名字既被要求红又被要求绿：{overlap}")
            continue
        lines.append(f"  ✓ {code} {desc[:40]}｜红 {len(red)} 条／绿 {len(green)} 条")
    return bad, lines


def go_run(clone: Path, env: dict):
    try:
        p = subprocess.run(["go", "test", "./internal/service/", "-run", RUNNER,
                            "-count=1", "-v", "-timeout", "1200s"],
                           cwd=clone / SERVER, capture_output=True, text=True,
                           timeout=1800, env=env)
        out, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out, rc = (e.stdout or "") + (e.stderr or ""), -9
    except FileNotFoundError:
        return -127, [], 0, "go：命令不在 PATH ⇒ 本电池没跑过任何东西", []
    out = ANSI.sub("", out)
    # 名单按整行取：`(\S+)` 能吃下 `父/子` 这种带斜杠的子用例名。先按 `[^\s/]+` 切会把子用例
    # 截成父名，expect 就永远对不上（本卡四格的 expect 全是子用例）。
    killed = sorted(set(re.findall(r"^    --- FAIL: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- FAIL: (\S+)", out, re.M)))
    passed = sorted(set(re.findall(r"^    --- PASS: (\S+)", out, re.M)) |
                    set(re.findall(r"^--- PASS: (\S+)", out, re.M)))
    ran = (len(re.findall(r"^=== RUN\s+\S+", out, re.M)) +
           len(re.findall(r"^    === RUN\s+\S+", out, re.M)))
    return rc, killed, ran, out, passed


def classify(rc: int, killed: list, ran: int, out: str, red: list, green: list) -> str:
    if any(s in out for s in ENV_SIGNS) or rc in (-127, -9):
        return "ENV-BROKEN"
    if "[build failed]" in out or "undefined:" in out or "cannot use " in out:
        return "BUILD-BROKEN"
    if "no tests to run" in out or ran == 0:
        return "NO-RUN"
    hit = set(killed)
    if not hit:
        return "SURVIVED"
    if all(n in hit for n in red) and not (hit & set(green)):
        return "KILLED"
    return "RED-UNNAMED"


def do_check(base: Path) -> int:
    """锚点预检的公共出口：`base` 是"要对着哪一份字节验"——克隆（`--check`，HEAD）或当前树
    （`--check-tree`，CI 用，不装架不跑 go）。两者判据同一条，只是对象不同。"""
    tests = defined_tests(base / PKG_REL)
    bad, lines = check_cells(read(base / SRC_REL), CELLS, tests)
    for ln in lines:
        print(ln)
    print(f"锚点校验：{len(CELLS)} 格，{bad} 格有问题")
    return 1 if bad else 0


def do_selftest() -> int:
    """六格预检反向＋四腿归类反向：不读盘、不起 `go test`、不需要克隆 ⇒ 任何有 python3 的地方都能跑。

    为什么常驻件自己也要被反向测：`check_cells` 与 `classify` 是本电池"锚点失守／注码不落地／
    expect 是化石／把别人的红当自己的杀"四种失效的唯一出口。它们若恒报"没问题／KILLED"，
    那 `--check` 与五格的绿就是装饰 —— 所以每一格既断言坏格数，也断言**点名的行数**。

    `base` 把两处锚点都带上（V1–V4 打 ANCHOR、V5 打 GUARD）：只喂一份锚点的话，另一处
    会在每一格里被顺带点名成"锚点失守"，坏格数就对不上期望了（这是加第五刀时实测到的）。
    S3–S5 按 CELLS 动态拼，加第六刀时不必再改这一段的下标。
    """
    base_text = ANCHOR + GUARD
    tests = {NQP, QQ, TG}
    base = CELLS[1]
    cases = [
        ("S1 好锚点＋名单里的用例都有定义 ⇒ 0 格有问题", base_text, CELLS, tests, 0),
        ("S2 锚点被搬走 ⇒ 全部格点名（两处锚点都没了就没牙）", "", CELLS, tests, len(CELLS)),
        ("S3 注码不落地（old==new）⇒ 点名那一格", base_text,
         [CELLS[0]] + [(base[0], base[1], base[2], base[2], base[4], base[5])] + CELLS[2:],
         tests, 1),
        ("S4 expect 是用例改名后的化石 ⇒ 点名那一格", base_text,
         CELLS[:-1] + [("Vx", "化石 expect", base[2], "\t// x\n", [f"{NQP}RenamedAway"], [])],
         tests, 1),
        ("S5 红名单与绿名单相交 ⇒ 点名（这一格说不清该红该绿）", base_text,
         CELLS[:-1] + [("Vy", "自相矛盾", base[2], "\t// y\n", [NQP, WAPP], [WAPP])],
         tests, 1),
        # 相交判据在第五刀的名单形状上也要有一次反向：V5 的红名单只有"期望 0"的那格，
        # 若同一格被同时写进红与绿（把抑制格当成正控制格的那类笔误），必须由 ④ 点名。
        ("S6 V5 形状的名单自相矛盾 ⇒ 点名相交判据", base_text,
         CELLS[:-1] + [("V5z", "同一格又红又绿", GUARD, "",
                        [TG, TG_OFF], sorted([TG_OFF, TG_ON] + OTHERS))],
         tests, 1),
    ]
    failed = 0
    for label, text, cells, tset, want in cases:
        bad, lines = check_cells(text, cells, tset)
        named = sum(1 for ln in lines if ln.startswith("  ✗"))
        if bad != want or named != want:
            failed += 1
            print(f"  ✗ {label}：实得 {bad} 格有问题／点名 {named} 行，期望 {want}")
        else:
            print(f"  ✓ {label}：坏格数 {bad}＝点名行数 {named}")

    # classify 的"必须仍绿"那条腿单独反向：只断言"某个 expect 红了"的话，
    # 一刀把三家全打死（V1 的红名单是 V2 的红名单的超集）会被 V2 误判成 KILLED。
    # 第五刀的两条腿：V5 的红名单是"父用例＋期望 0 那一格"。若两格一起红（正控制格也被
    # 带走＝这一刀把整条腿打崩，不是精准杀掉），必须判 RED-UNNAMED 而不是 KILLED。
    # 四条探针摆成名单 ⇒ 末行分母由 `len(probes)` 现取，不再写死那个 `＋4`：写死＝下一位
    # 加一条反向探针，汇总行就少报一格（与收尾闸的"九格"、自测的"+3"同族）。
    v5 = CELLS[4]
    probes = [
        ("classify V2 反查：三家同红时",
         classify(1, sorted([NQP, WECOM, FEISHU, WAPP, QQ]), 5, "", CELLS[1][4], CELLS[1][5]),
         "RED-UNNAMED", "，不能算干净杀掉"),
        ("classify V2 正查：只企微红时",
         classify(1, [NQP, WECOM], 5, "", CELLS[1][4], CELLS[1][5]),
         "KILLED", ""),
        ("classify V5 反查：抑制格与正控制格同红时",
         classify(1, sorted([TG, TG_OFF, TG_ON]), 8, "", v5[4], v5[5]),
         "RED-UNNAMED", ""),
        ("classify V5 正查：只 /start 那一格红时",
         classify(1, [TG, TG_OFF], 8, "", v5[4], v5[5]),
         "KILLED", ""),
    ]
    for label, got, want, note in probes:
        print(f"  {label}判为 {got}（要求 {want}{note}）")
        if got != want:
            failed += 1
    print(f"===== 预检自测：{len(cases)}＋{len(probes)} 格，失败 {failed} 格 =====")
    return 1 if failed else 0


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--check", action="store_true", help="只验锚点与用例名（要装架，不跑 go test）")
    ap.add_argument("--check-tree", action="store_true",
                    help="同上，但对着**当前树**的字节验：不装架、不跑 go（CI 落点用这一枚）")
    ap.add_argument("--selftest", action="store_true", help="纯内存反向格（不读盘、不跑 go）")
    ap.add_argument("--clone", default="", help="私有作业目录（必须是仓库外的空目录）")
    ap.add_argument("--keep", action="store_true", help="收尾不回收，原地留证")
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    args = ap.parse_args()

    global LOGDIR
    LOGDIR = LOGROOT / args.tag

    if args.selftest:
        return do_selftest()
    if args.check_tree:
        identity(ROOT, extra="｜`--check-tree`：当前工作树，不装架、不落产物")
        return do_check(ROOT)

    tmp, owned = workdir(args.clone or None, prefix="whtrigmut-", repo_root=ROOT)
    try:
        clone = prepare(tmp, owned)
        dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)
        if args.check:
            tee_to(LOGDIR / "00-check.log")
        else:
            tee_to(LOGDIR / "00-run.log")
        # 身份行必须在 tee 之后：它是"这轮读数测的是哪一笔"的唯一记录，早于 tee 只留在终端。
        # 报的是**来树**的 HEAD 与未入库字节数——注码发生在 `--shared` 克隆里，未入库的那些不进本轮读数。
        identity(ROOT, label="基线字节", extra="｜本轮读私有 `--shared` 克隆的 HEAD（来树未入库字节不进本轮读数）"
                               f"｜作业目录 {tmp}")
        if args.check:
            return do_check(clone)

        src = clone / SRC_REL
        original = read(src)
        orig_md5 = md5_bytes(src)
        bad, lines = check_cells(original, CELLS, defined_tests(clone / PKG_REL))
        if bad:
            for ln in lines:
                print(ln)
            print("PREFLIGHT-FAILED：先修锚点再放刀（放刀路径上这些格会变成 BROKEN/NO-RUN）")
            return 6
        for ln in lines:
            print(ln)

        env = test_env(clone)

        rc, killed, ran, out, passed = go_run(clone, env)
        dump("00-control", out)
        if rc != 0:
            kind = "CONTROL-ENV-BROKEN" if any(s in out for s in ENV_SIGNS) else "CONTROL-RED"
            print(f"{kind}：没放刀就红 ⇒ 判据未验证（红因末 8 行）")
            print("\n".join(out.splitlines()[-8:]))
            return 8
        expect_ran = ran
        print(f"CONTROL-GREEN ✅ ran={ran} 名单={'、'.join(passed)}")
        want = {n for c in CELLS for n in c[4] + c[5]}
        absent = sorted(want - set(passed))
        if absent:
            print(f"CONTROL-BROKEN：名单里的腿没在控制组跑绿 {absent} ⇒ 四格的 expect 无从核验")
            return 6

        tally = {k: 0 for k in TALLY}
        problems = []
        for code, desc, old, new, red, green in CELLS:
            try:
                mutated = sub_once(original, old, new, code)
            except SystemExit as e:
                problems.append(str(e))
                tally["BUILD-BROKEN"] += 1
                continue
            src.write_text(mutated, encoding="utf-8")
            # 变异要断言落到磁盘：只 replace 不 write，或写错了对象，都会"以为在跑变异态"
            if read(src) != mutated:
                problems.append(f"{code} 注码没落到磁盘（写回后字节不等于变异文本）")
                tally["BUILD-BROKEN"] += 1
                src.write_text(original, encoding="utf-8")
                continue
            rc, killed, ran, out, _p = go_run(clone, env)
            dump(code, out)
            v = classify(rc, killed, ran, out, red, green)
            tally[v] += 1
            if v == "KILLED" and ran != expect_ran:
                problems.append(f"{code} 杀了但 ran={ran}≠控制组的 {expect_ran}"
                                f"：疑似 panic 带走别的腿，红因要人工看")
            if v == "KILLED":
                print(f"{code:<4} {desc[:36]:<38} 杀掉｜红 {len(killed)} 条（{len(red)} 条点名）"
                      f"绿名单零命中 ran={ran}")
            else:
                problems.append(f"{code} 判为 {v}（不是干净的杀掉）：{desc}")
                print(f"{code:<4} {desc[:36]:<38} {v} rc={rc} ran={ran} 红名单={killed}")
                print(out[-1500:])
            src.write_text(original, encoding="utf-8")
            if md5_bytes(src) != orig_md5:
                print("RESTORE-MISMATCH：还原后字节与基线不同，停机")
                return 8

        print("\n计数：", " ".join(f"{k}={tally[k]}" for k in TALLY))
        print(f"全部格子已还原（{SRC_REL} md5={orig_md5}）")
        if problems:
            print("\n===== 电池判定：有洞 =====")
            for x in problems:
                print("  ✗", x)
            return 1
        if tally["KILLED"] != len(CELLS):
            print(f"\n===== 电池判定：只杀掉 {tally['KILLED']}/{len(CELLS)} 格 =====")
            return 1
        print(f"\n===== 电池判定：{len(CELLS)} 格逐格核验（红名单点名＋绿名单零命中），无未登记存活 =====")
        return 0
    finally:
        dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)


if __name__ == "__main__":
    sys.exit(main())
