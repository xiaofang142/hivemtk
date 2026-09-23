"""把电池的汇总行落到与逐格产物同一个目录：`stdout` 逐行镜像进 `LOGDIR/00-run.log`。

为什么要这一刀（2026-09-23 R45 实测）：常驻化只搬了**逐格**产物，而判定行（`结论：判 30 格…全杀`、
`===== 电池判定：…`）只打在终端。后台跑的那趟终端进了 CLI 的任务文件，任务文件一回收，
19 份格子日志在手却没有任何判定可引 ⇒ 只能整趟重跑。口径：**证据随仓走**要求判定行本身也在仓里。

文件侧走 `redact.scrub`（`00-run.log` 是仓库常驻产物，终端侧保持原样便于人读）。
"""

import atexit
import pathlib
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
    p = pathlib.Path(path)
    p.parent.mkdir(parents=True, exist_ok=True)  # 多数驱动的 LOGDIR 要到第一次 dump() 才建，tee 比它更早
    fh = open(p, "w", encoding="utf-8")
    sys.stdout = _Tee(sys.stdout, fh)
    atexit.register(fh.close)
