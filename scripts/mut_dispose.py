#!/usr/bin/env python3
"""变异电池收尾用的删除闸：`dispose()` 是唯一允许带走私有克隆的出口。

为什么需要它（2026-09-24 实测事故）：常驻电池一直有
`tmp = Path(args.clone or tempfile.mkdtemp(...))` ＋ 出口 `shutil.rmtree(tmp)` 这一对，
于是 `python3 scripts/mut_collection_p703.py --clone . --check` 会在锚点预检跑完、
一切正常退出之后，把 `Path(".")` 连同 `.git` 一起删掉——**那正是调用方的工作树**
（本轮 r45-lane 与 18 项未提交字节就是这么没的）。旧写法的隐含假设是
"这个目录是我建的"，而 `--clone` 恰恰把"由谁建"交给了命令行。

本模块把删除动作收敛成一个带前提的出口：
  1. `--keep` ⇒ 一律不删（原地留证）。
  2. 目标若是仓库根本身、仓库根的任一上级、当前工作目录、`/` 或 `/tmp` 本身 ⇒ **拒删并出声**
     （静默跳过也是一种失效：下次照样有人传 `--clone .`，只是这次什么也没告诉他）。
  3. 其余情况只删 `<tmp>/clone`，且必须带 `.git`——那是电池自己 `git clone --shared` 出来的私有克隆；
     没有 `.git` 就说明同名目录不是本电池建的，不碰。
  4. `owned=True`（本次由 `tempfile.mkdtemp` 创建、且确实落在系统临时目录下）才整体带走。

调用方形态（六枚带 `--clone` 的常驻电池都已改成这一对）：
    tmp, owned = workdir(args.clone, prefix="p703mut-", repo_root=ROOT)
    ...
    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

两道闸缺一不可：`workdir()` 挡在装架之前（危险 `--clone` 当轮退，工作树分毫未动），
`dispose()` 兜在收尾（只回收本电池自己 `git clone --shared` 出来的 `clone/`）。
只留后者的话，`--clone .` 依然会在退出前把克隆与补丁写进调用方的树里。

牙齿：`bash scripts/mut-dispose-guard.test.sh`（G1 交来的目录只回收 clone/／G2 owned 整体带走／
G3 仓库根拒删且出声／G4 上级拒删／G5 无 .git 的同名 clone/ 不碰／G6 --keep 全留／
G7–G9 入口闸：仓库根·上级·/tmp 拒、合法入参照样放行／
REAL 静态面：--clone 面 == 入口闸面 == 收尾闸面，且不留 `Path(args.clone` 直连赋值与裸 rmtree 站点）。
"""

from __future__ import annotations

import shutil
import sys
import tempfile
from pathlib import Path

FORBIDDEN = {Path("/"), Path(tempfile.gettempdir()), Path("/tmp")}


def is_forbidden(target: Path) -> bool:
    """`/tmp` 在 mac 上是 `/private/tmp` 的软链：只比未解析值会漏判，
    漏判的后果是往下走到"上级"那条分支——同样拒，但报的原因是错的。"""
    return target in FORBIDDEN or target.resolve() in {p.resolve() for p in FORBIDDEN}


def _refuse(path: Path, why: str) -> None:
    print(f"拒绝删除 {path}：{why}（私有克隆请留在临时目录里，或显式手删）", file=sys.stderr)


def _bail(arg: str, path: Path, why: str) -> None:
    # 入口闸必须**退**，不能只打印：调用方拿到 rc=0 就会把这趟记成"跑过了"。
    raise SystemExit(
        f"拒绝 --clone {arg}（解析为 {path}）：{why}\n"
        f"        改法：不传 --clone（电池自动开私有临时目录），或传一个仓库外的空目录。"
        f"注意 `--check` 也一样要装架，别以为只读预检就能指着自己的树。"
    )


def workdir(clone_arg: str | None, *, prefix: str, repo_root: Path) -> tuple[Path, bool]:
    """入口闸：把 `--clone` 的解析收敛成一个带拒删的出口，返回 (作业目录, owned)。

    与 `dispose()` 的分工：`dispose()` 只管"收尾别删错"，等到那时危险目录早已经被
    塞进一个 `clone/`、打过补丁、跑过 `go test`。这一枚在**装架之前**退，
    所以工作树分毫未动。

    被拒的形态（本轮事故是第一种）：
      * `--clone .`／`--clone <仓库根>` —— 电池会往这里 `git clone --shared`；
      * `--clone <仓库根的上级>` —— 删它等于带走同一工作区里的别的树；
      * `--clone /`、`--clone /tmp`、`--clone <临时目录本身>`；
      * `--clone <当前工作目录>` —— 退掉自己站的地板。
    合法时不创建目录、不动目录里的任何东西：私有克隆由 `go_prepare` 自己去建，
    回收由 `dispose()` 负责。
    """
    if not clone_arg:
        return Path(tempfile.mkdtemp(prefix=prefix)), True

    target = Path(clone_arg).expanduser().resolve()
    root = Path(repo_root).resolve()

    if is_forbidden(target):
        _bail(clone_arg, target, "那是文件系统／临时目录本身")
    if target == root:
        _bail(clone_arg, target, "那就是仓库根，装架会直接写进你的工作树（`--clone .` 的形态）")
    if target in root.parents:
        _bail(clone_arg, target, "那是仓库根的上级，作业文件会散到同一工作区的别的树里")
    if target == Path.cwd().resolve():
        _bail(clone_arg, target, "那是当前工作目录，回收它等于抽掉自己站的地板")

    return target, False


def dispose(tmp: Path, *, owned: bool, keep: bool = False, repo_root: Path | None = None) -> None:
    target = Path(tmp).resolve()

    if keep:
        print(f"收尾：--keep，保留 {target}")
        return

    if is_forbidden(target):
        _refuse(target, "目标是文件系统／临时目录本身")
        return
    if repo_root is not None:
        root = Path(repo_root).resolve()
        if target == root:
            _refuse(target, "目标就是仓库根（`--clone .` 的经典形态）")
            return
        if target in root.parents:
            _refuse(target, "目标是仓库根的上级，删它会连别的树一起带走")
            return
    if target == Path.cwd().resolve():
        _refuse(target, "目标是当前工作目录")
        return

    clone = target / "clone"
    removed = []
    if clone.exists():
        # `.git` 是"这枚克隆是本电池 git clone 出来的"唯一的现场证据。
        if (clone / ".git").exists():
            shutil.rmtree(clone, ignore_errors=True)
            removed.append(str(clone))
        else:
            print(f"收尾：{clone} 里没有 .git ⇒ 不是本电池的私有克隆，不碰")
    elif clone.is_symlink():
        clone.unlink(missing_ok=True)
        removed.append(str(clone))

    if owned and target.is_dir():
        # `owned` 的语义就是"这一枚是本次 `tempfile.mkdtemp()` 建的"；调用方以
        # `owned = not args.clone` 供给，所以它不可能指向别人的目录。落在哪儿都带走——
        # 早先还要求它在 `tempfile.gettempdir()` 下面，那是把"谁建的"和"建在哪"混成一件事，
        # 结果 `TMPDIR` 被改过／显式传 `--clone` 到别的空目录时 owned 反而删不掉（G2 实测）。
        shutil.rmtree(target, ignore_errors=True)
        removed.append(str(target))

    print(f"收尾：带走 {'、'.join(removed) if removed else '（无可回收产物）'}")


if __name__ == "__main__":
    print(__doc__)
