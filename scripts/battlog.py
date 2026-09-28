"""把电池的汇总行落到与逐格产物同一个目录：`stdout` 与 `stderr` 逐行镜像进 `LOGDIR/00-run.log`。

为什么要这一刀（2026-09-23 R45 实测）：常驻化只搬了**逐格**产物，而判定行（`结论：判 30 格…全杀`、
`===== 电池判定：…`）只打在终端。后台跑的那趟终端进了 CLI 的任务文件，任务文件一回收，
19 份格子日志在手却没有任何判定可引 ⇒ 只能整趟重跑。口径：**证据随仓走**要求判定行本身也在仓里。

文件侧走 `redact.scrub`（`00-run.log` 是仓库常驻产物，终端侧保持原样便于人读）。

`identity()` 是同一条口径的另一半：产物目录名只带墙钟戳，不带"这轮读数测的是哪一笔字节"。
"""

import pathlib
import subprocess
import sys

from redact import scrub


class _Tee:
    def __init__(self, stream, fh):
        self.stream, self.fh = stream, fh

    def write(self, text):
        n = self.stream.write(text)
        if not self.fh.closed:  # 解释器收尾时会先关文件再冲一次 stdout
            self.fh.write(scrub(text))
            self.fh.flush()  # 每写即落盘：进程被打断时，判定行至少和已经打出来的一样多
        return n

    def flush(self):
        self.stream.flush()
        if not self.fh.closed:
            self.fh.flush()

    def isatty(self):
        return self.stream.isatty()

    def __getattr__(self, name):
        return getattr(self.stream, name)


def tee_to(path) -> None:
    """把 stdout **与 stderr** 同时镜像进那份产物。

    stderr 这一半是本轮补的：驱动停机走的是 `raise SystemExit("…")`，解释器把那句话打到
    stderr，而只镜像 stdout 的 tee 让它留在终端 ⇒ 产物里只剩一行身份行加一句"rc=1"，
    **红读到了、红因不在档**（2026-09-28 实测：R22 `--check` 停机那轮的 `00-check.log` 只有
    3 行，停机原因"脏文件清单为空"查不到）。句柄不在 `atexit` 里提前关：解释器是在
    `atexit` 之后才打 SystemExit 那句话的，先关就把刚补的这一半又丢了。
    """
    p = pathlib.Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)  # 多数驱动的 LOGDIR 要到第一次 dump() 才建，tee 比它更早
    fh = open(p, "w", encoding="utf-8")
    sys.stdout = _Tee(sys.stdout, fh)
    sys.stderr = _Tee(sys.stderr, fh)


def identity(root, *, label: str = "基线字节", extra: str = "", overlay=None) -> str:
    """印一行 `基线字节：<SHA>｜未入库字节 N 处…`，返回同一行供调用方留存。

    必须在 `tee_to()` **之后**调用：这一行是产物里唯一的字节身份，早于 tee 只会留在终端，
    常驻日志里查不到（2026-09-28 实测：AiTrigger 13 份、DBPool 14 份产物对 `基线字节` 零命中，
    而驱动里确实有这行 —— 就打印在 tee 之前）。

    未入库字节数与 SHA 一起报是必需的：只报 SHA 分不清"已入库字节"与"含并行会话未提交
    字节的活树"，而后者的读数不能当前者的证据用；`extra` 由调用方写明本轮究竟读哪一份。

    `overlay` 给的是**本轮装架时从工作树覆盖进克隆的那批相对路径**（如果有）。这一份不写
    进身份行就没法复算：同一句"读克隆 HEAD"在 b17/p503/R22 那三枚驱动里是假话——它们的
    `prepare()` 把来树字节 `copy2` 进了克隆，未入库改动**计入**本轮读数（本轮实测）。
    所以凡有覆盖就现场数份数、数其中未入库几份，让口径成为读数而不是声明。
    """

    def git(*args: str) -> str:
        try:
            out = subprocess.run(("git", "-C", str(root), *args), capture_output=True,
                                 text=True, timeout=60)
        except (OSError, subprocess.SubprocessError):
            return ""
        return out.stdout.strip() if out.returncode == 0 else ""

    sha = git("rev-parse", "--short", "HEAD")
    if not sha:
        line = f"{label}：{root} 查无 HEAD（非 git 树 ⇒ 本轮读数只能按目录名认）{extra}"
    else:
        dirty = len([ln for ln in git("status", "--porcelain").splitlines() if ln.strip()])
        seg = ""
        if overlay is not None:
            paths = [str(p) for p in overlay]
            if not paths:
                seg = "｜装架不覆盖来树字节 ⇒ 本轮读数＝克隆 HEAD 那一笔"
            else:
                try:
                    r = subprocess.run(("git", "-C", str(root), "status", "--porcelain", "--", *paths),
                                       capture_output=True, text=True, timeout=60)
                except (OSError, subprocess.SubprocessError):
                    r = None
                if r is None or r.returncode != 0:
                    # 分不清"0 份未入库"与"没问到"⇒ 不许印成一个像绿的数
                    seg = f"｜装架覆盖来树 {len(paths)} 份（未入库份数查不到，按含改动对待）"
                else:
                    n = len([ln for ln in r.stdout.splitlines() if ln.strip()])
                    tail = (f"其中 {n} 份未入库，计入本轮读数" if n
                            else "全部与 HEAD 同字节 ⇒ 本轮读数＝HEAD")
                    seg = f"｜装架覆盖来树 {len(paths)} 份（{tail}）"
        line = f"{label}：`{sha}`｜未入库字节 {dirty} 处{seg}{extra}"
    print(line)
    return line
