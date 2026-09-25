#!/usr/bin/env python3
"""#73：把 CI 专属的 `sorry, too many clients already (SQLSTATE 53300)` 换成本地可复现的 A/B 读数。

为什么要有这个文件：这条红**只在 CI 出现过**，本地 8232 容器把 max_connections 调到 500
（docker-compose.yml:69），而 CI 的 services.postgres 没写任何连接数参数 ⇒ 用镜像默认 100。
"本地跑不出来的红"等于没有回归门，所以本件在本地起一个同样 100 名额的容器
（见文档里的 docker run 命令），只换 `internal/pkg/testutil/testdb.go` 一份字节做 A/B：

  - A 相 = HEAD（泳道基线）那份**没有** SetMaxOpenConns 的 testdb.go；
  - B 相 = 本批改成的 SetMaxOpenConns(32)／SetMaxIdleConns(8)。

其余字节两相逐字相同（同一棵树、同一批脏文件覆盖），所以两相之差只能归因到那两行。

判据形状（都是"跑出来的读数"，不是推演）：
  1. A 相在 100 名额容器上必须出现 53300，且 B 相同容器零出现；
  2. `control-a` 把 A 相挪到 500 名额的开发容器上必须**也**零出现 ⇒ 证明分歧来自容量而不是别的；
  3. 每格现测峰值连接数（单条采样连接、0.2s 一问，`datname like '<dbname>%'` 计数含 idle、排除采样器自身），
     峰值是"这一相吃掉多少名额"的直接读数，与 53300 是否出现互相独立；
  4. 每格跑完回读 testdb.go 的 md5，必须等于本相注码时记下的值 ⇒ 证明"编译用的字节
     ＝跑的时候磁盘上的字节"（先编后改会造陈旧二进制的假绿）。

用法：
    python3 scripts/cap-ab-53300-probe.py narrow      # 三枚探针 + Audience，A/B ×ROUNDS 轮，100 名额
    python3 scripts/cap-ab-53300-probe.py control-a   # A 相 @500 名额开发容器，一轮
    python3 scripts/cap-ab-53300-probe.py full        # 整包 internal/service，CI 参数，A/B 各一轮
"""

import argparse
import hashlib
import os
import pathlib
import re
import subprocess
import sys
import time

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from redact import scrub  # noqa: E402

ROOT = pathlib.Path(__file__).resolve().parents[1]
US = ROOT / "user-server"
TESTDB = US / "internal/pkg/testutil/testdb.go"
REL = "user-server/internal/pkg/testutil/testdb.go"
PKG = "./internal/service/"
NARROW = ("TestSessionChainCSATResolvesDBHandleSynchronously|"
          "TestFallbackVersionResolvesDBHandleSynchronously|"
          "TestSessionEventDispatchResolvesDBHandleSynchronously|"
          "TestAudience_SelectBySegment")
ROUNDS = 3
# 计数口径必须锚到错误形状本身：裸 `53300` 会吃到两种无关行 ——
# ① 夹具自己造的 event_id `tg_upd_1_426533000`（整包趟里白送 2 行假命中，读数从 2 涨成 4）；
# ② 本件自己写在产物首行的身份行（含字面量 `53300/上限报错行数=`，事后复算会再多吃 1 行）。
CAP_RE = re.compile(r"too many clients|SQLSTATE 53300", re.I)
DBNAME = os.environ.get("POSTGRES_TEST_DBNAME", "user_db_test")
SAMPLE_DB = "postgres"      # 两相容器都在的库；采样器与容量读数都走它


def head_text() -> str:
    r = subprocess.run(["git", "-C", str(ROOT), "show", f"HEAD:{REL}"],
                       capture_output=True, text=True, timeout=300)
    if r.returncode != 0:
        raise SystemExit(f"拿不到 HEAD:{REL} rc={r.returncode} {r.stderr[-200:]}")
    return r.stdout


def md5(p: pathlib.Path) -> str:
    return hashlib.md5(p.read_bytes()).hexdigest()


class Cells:
    """A/B 两相的注码与还原。进场 cp 式全量写回，出场写回 B 相 —— 绝不 git checkout/restore。"""

    def __init__(self):
        self.batch = TESTDB.read_text(encoding="utf-8")
        self.head = head_text()
        if self.batch == self.head:
            raise SystemExit("A/B 两相字节相同：本树里没有池上界改动，测了也是白测")
        if "SetMaxOpenConns" not in self.batch:
            raise SystemExit("B 相（工作树字节）里没有 SetMaxOpenConns，身份判不出来")
        if "SetMaxOpenConns" in self.head:
            raise SystemExit("A 相（HEAD 字节）里已有 SetMaxOpenConns，身份判不出来")
        self.md5 = {"A": hashlib.md5(self.head.encode()).hexdigest(),
                    "B": hashlib.md5(self.batch.encode()).hexdigest()}

    def arm(self, cell: str) -> str:
        TESTDB.write_text(self.head if cell == "A" else self.batch, encoding="utf-8")
        got = md5(TESTDB)
        if got != self.md5[cell]:
            raise SystemExit(f"注码未落地：{cell} 期望 {self.md5[cell][:12]} 实得 {got[:12]}")
        return got

    def restore(self) -> str:
        TESTDB.write_text(self.batch, encoding="utf-8")
        got = md5(TESTDB)
        if got != self.md5["B"]:
            raise SystemExit("还原失败：B 相字节没写回，别接着跑")
        return got


def sample_conn(port: str, outfile: pathlib.Path):
    """单条采样连接、0.2s 一问。多开连接采样会自己挤占名额，读数就不干净了。"""
    user = os.environ.get("POSTGRES_TEST_USER", "admin")
    env = {**os.environ, "PGPASSWORD": os.environ["POSTGRES_TEST_PASSWORD"]}
    proc = subprocess.Popen(
        # -d postgres：两相容器都有这个库。第一版拿用户名顶库名（-d admin），在 8232 上
        # 直接 FATAL: database "admin" does not exist ⇒ 采样器静默零读数。
        ["psql", "-h", "127.0.0.1", "-p", port, "-U", user, "-d", SAMPLE_DB, "-tA", "-q", "-f", "-"],
        stdin=subprocess.PIPE, stdout=open(outfile, "w"), stderr=subprocess.STDOUT,
        text=True, env=env)
    # 计数含 idle：本用例的失效形状就是"连接开着不还、攒在池里"，只数 active 会把
    # 真正的用量（几十条 idle）读成 1，那是采样器自己的错，不是被测方的。
    proc.stdin.write(f"\\pset footer off\nSELECT count(*) FROM pg_stat_activity "
                     f"WHERE datname LIKE '{DBNAME}%' AND pid <> pg_backend_pid();\n\\watch 0.2\n")
    proc.stdin.flush()
    return proc


def peak_of(path: pathlib.Path) -> int:
    vals = [int(m.group(1)) for m in
            re.finditer(r"^(\d+)\s*$", path.read_text(errors="replace"), re.M)]
    return max(vals) if vals else -1


def go_env(port: str) -> dict:
    env = dict(os.environ)
    env.update({"GIN_MODE": "test", "GOFLAGS": "-mod=mod",
                "POSTGRES_TEST_HOST": "127.0.0.1", "POSTGRES_TEST_PORT": port})
    env.setdefault("POSTGRES_TEST_USER", "admin")
    return env


def run_cell(cell: str, port: str, rnd: str, full: bool, logdir: pathlib.Path,
             cells: Cells) -> str:
    name = f"{'full-' if full else ''}{cell}@{port}-r{rnd}"
    log = logdir / f"{name}.log"
    samp = logdir / f"{name}.samples.log"      # 采样读数随产物同地留档（峰值可复核，不留裸 .samples）
    armed = cells.arm(cell)
    proc = sample_conn(port, samp)
    args = ["go", "test", PKG, "-count=1", "-p", "1", "-short", "-race", "-v"]
    args += ["-timeout", "40m"] if full else ["-timeout", "25m", "-run", f"^({NARROW})$"]
    rc = -9
    try:
        p = subprocess.run(args, cwd=US, capture_output=True, text=True,
                           timeout=60 * (46 if full else 28), env=go_env(port))
        out, rc = p.stdout + p.stderr, p.returncode
    except subprocess.TimeoutExpired as e:
        out = (e.stdout or "") + (e.stderr or "") + "\n[驱动] TIMEOUT：整格超时截断\n"
    finally:
        # psql 的 \watch 循环不消费 stdin 的 \q ⇒ 只能收尾时直接终止采样连接；
        # 等它自己退出会把本格判成驱动崩溃（实测第一版就是这么死在 wait(timeout=30)）。
        try:
            proc.kill()
        except OSError:
            pass
        proc.wait(timeout=30)
    landed = md5(TESTDB)
    peak = peak_of(samp)
    head = (f"[{name}] 相={cell} testdb注码={armed[:12]} 跑后磁盘={landed[:12]}"
            f"{'一致' if landed == armed else '★不一致'} "
            f"53300/上限报错行数={sum(1 for _l in out.splitlines() if CAP_RE.search(_l))} "
            f"峰值连接(含idle)={peak}{'★SAMPLER-BROKEN' if peak < 0 else ''} "
            f"FAIL={len(re.findall('^--- FAIL: ', out, re.M))} "
            f"PASS={len(re.findall('^--- PASS: ', out, re.M))} go rc={rc}\n")
    log.write_text(scrub(head + out), encoding="utf-8")
    return head.strip()


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("mode", choices=["narrow", "control-a", "full"])
    ap.add_argument("--tag", default=time.strftime("%Y%m%d-%H%M%S"))
    ap.add_argument("--rounds", type=int, default=ROUNDS)
    args = ap.parse_args()

    if not os.environ.get("POSTGRES_TEST_PASSWORD"):
        print("ENV-BROKEN：没导出 POSTGRES_TEST_PASSWORD，本件不代读 .env（密钥不进代码）")
        return 4

    ports = {"narrow": [os.environ.get("CAPAB_PORT", "8234")],
             "control-a": [os.environ.get("CAPAB_DEV_PORT", "8232")],
             "full": [os.environ.get("CAPAB_PORT", "8234")]}[args.mode]
    logdir = ROOT / "docs/superpowers/specs/ledger/logs/CapAB" / args.tag
    logdir.mkdir(parents=True, exist_ok=True)
    summary = logdir / "00-summary.log"
    lines = [f"趟次 tag={args.tag} mode={args.mode}"]

    cells = Cells()
    lines.append(f"两相身份：A(HEAD 无上限)={cells.md5['A'][:12]} B(本批 32/8)={cells.md5['B'][:12]}")
    for port in ports:
        r = subprocess.run(["pg_isready", "-h", "127.0.0.1", "-p", port],
                           capture_output=True, text=True, timeout=30)
        cap = subprocess.run(["psql", "-h", "127.0.0.1", "-p", port, "-U",
                              os.environ.get("POSTGRES_TEST_USER", "admin"), "-d", SAMPLE_DB,
                              "-tA", "-c",
                              "show max_connections"],
                             capture_output=True, text=True, timeout=30,
                             env={**os.environ, "PGPASSWORD": os.environ["POSTGRES_TEST_PASSWORD"]})
        lines.append(f"端口 {port}: {r.stdout.strip()} rc={r.returncode} max_connections={cap.stdout.strip() or cap.stderr.strip()[-120:]}")
        if r.returncode != 0:
            summary.write_text(scrub("\n".join(lines) + "\n"), encoding="utf-8")
            print("ENV-BROKEN：端口没起来，本趟未放刀；产物 " + str(summary))
            return 4

    port = ports[0]
    try:
        if args.mode == "narrow":
            for rnd in range(1, args.rounds + 1):
                for cell in "AB":
                    line = run_cell(cell, port, str(rnd), False, logdir, cells)
                    lines.append(line)
                    print(line, flush=True)
        elif args.mode == "control-a":
            line = run_cell("A", port, "1", False, logdir, cells)
            lines.append(line)
            print(line, flush=True)
        else:
            for cell in "AB":
                line = run_cell(cell, port, "1", True, logdir, cells)
                lines.append(line)
                print(line, flush=True)
    finally:
        restored = cells.restore()

    def hits(phase: str) -> int:
        return sum(int(re.search(r"上限报错行数=(\d+)", l).group(1))
                   for l in lines if f"相={phase}" in l)

    a, b = hits("A"), hits("B")
    verdict = (f"判定：A 相上限报错 {a} 行／B 相 {b} 行 ⇒ " +
               ("分歧成立，53300 归因到连接池上界" if a and not b else "分歧未成立（逐格行见上）"))
    if args.mode == "control-a":
        verdict = (f"判定（A 相 @500 名额控制格）：上限报错 {a} 行 ⇒ " +
                   ("零 ⇒ 容量才是分歧来源" if a == 0 else "照旧红 ⇒ 分歧不在容量，本批归因要改"))
    dead = [l.split()[0] for l in lines if "SAMPLER-BROKEN" in l]
    if dead:
        verdict += f"｜★{len(dead)} 格采样器无读数（{', '.join(dead)}）⇒ 名额占用那一维本趟没测到"
    lines.append(verdict)
    lines.append(f"还原复测：testdb.go md5={restored[:12]}（应等于 B 相 {cells.md5['B'][:12]}）")
    summary.write_text(scrub("\n".join(lines) + "\n"), encoding="utf-8")
    print(verdict)
    print(f"产物：{summary}")
    return 5 if dead else 0


if __name__ == "__main__":
    raise SystemExit(main())
