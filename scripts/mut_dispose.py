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

调用方形态（所有带 `--clone` 面的常驻电池都是这三件；枚数由
`mut-dispose-guard.test.sh` 的 REAL 静态面现取，写死的那个数每加一枚电池就过期一次）：
    tmp, owned = workdir(args.clone, prefix="p703mut-", repo_root=ROOT)
    clone = prepare(tmp, owned)
    dispose_at_exit(tmp, owned=owned, keep=args.keep, repo_root=ROOT)   # 兜底，紧随装架那一行
    ...
    dispose(tmp, owned=owned, keep=args.keep, repo_root=ROOT)

**每处装架之后各挂一次**，不是"每枚一次"：一枚电池如果有多条装架路径（`--check` 一条、
真跑用例一条，`mut_actionability_b17.py` 就是这个形状），注册只写在第一处后面，真跑那条路
就全程没闸——那行注册压根没执行到。注册排在装架**之后**是必须的（克隆还没建起来就挂，
"克隆失败"那条豁免路会被兜底当成现场去处理）；代价是 prepare **函数体内**的死路兜不到
（注册还没执行），那一半由 `bail()` 负责，两面各有各的门腿。

三道闸各挡一面：`workdir()` 挡在装架之前（危险 `--clone` 当轮退，工作树分毫未动）；
`dispose()` 是显式收尾出口，只回收本电池自己 `git clone --shared` 出来的 `clone/`；
`dispose_at_exit()` 挂在进程退出路径上，兜住"装架之后有人忘了接闸"的整类中止路——
2026-09-28 现扫：带克隆面的 27 枚电池里，装架函数体之外还可达 134 条退出，而门原先
只走 `prepare`/`go_prepare` 的函数体，那一半从没被量过，`mut_reach_p503.py` 的控制组停机
实测留下 73M 残骸。只留前两者的话，`--clone .` 会先写进调用方的树、而停机照样漏盘。
"现场只活在克隆里"那类豁免路（还原后 md5 不一致）改走 `leave_for_evidence(why)` 打标记，
兜底闸读到它就只出声不回收。

牙齿：`bash scripts/mut-dispose-guard.test.sh`（G1 交来的目录只回收 clone/／G2 owned 整体带走／
G3 仓库根拒删且出声／G4 上级拒删／G5 无 .git 的同名 clone/ 不碰／G6 --keep 全留／
G7–G9 入口闸：仓库根·上级·/tmp 拒、合法入参照样放行／
G10–G12 兜底闸：没接闸的退出路照样回收／已回收过则安静／打过让路标记则只出声不删／
REAL 静态面：--clone 面 == 入口闸面 == 收尾闸面，兜底闸**逐处装架**都有注册（注册次数与装架
点数一处对一处），且每个让路豁免点都带 `leave_for_evidence`，
并不留 `Path(args.clone` 直连赋值与裸 rmtree 站点）。
"""

from __future__ import annotations

import atexit
import shutil
import sys
import tempfile
from pathlib import Path

FORBIDDEN = {Path("/"), Path(tempfile.gettempdir()), Path("/tmp")}

# 唯一一处"让收尾闸让路"的开关：现场只活在克隆里的那几条例外路（与豁免三形同源）打上它，
# `dispose_at_exit()` 读到它就只出声不回收。
_EVIDENCE: dict[str, str] = {"why": ""}


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


def leave_for_evidence(why: str) -> None:
    """给"现场只活在克隆里"那几条例外路打标记：之后的兜底闸读到它就让路。

    与豁免三形同源的一条取舍：`还原后 md5 不一致` 那类停机意味着克隆里那份字节既不是
    HEAD 也不是注码版，删了就只剩一句"当时红过"，与 `main()` 侧还原校验同一取舍。
    """
    _EVIDENCE["why"] = why


def dispose_at_exit(tmp: Path, *, owned: bool, keep: bool = False,
                    repo_root: Path | None = None) -> None:
    """把收尾闸挂到进程退出路径上：克隆建起来**之后**任何一条没先过闸的退出也照样回收。

    为什么不是"逐处插 sweep()"：一枚电池的 main 里克隆之后的退出有 6–9 条，还有一部分
    从它调用的辅助函数（`sub_once`／`lane_overlays`／`apply_js`）里冒出来——那些函数不知道
    克隆在哪，逐站插要么改签名要么漏。2026-09-28 现扫：27 枚带克隆面的电池里，装架函数体
    之外的可达退出 134 条，而常驻门的"中止路腿"用 AST 只走 `prepare`/`go_prepare` 函数体，
    那一半从没被量过；`mut_reach_p503.py` 的 `控制组[service] 不干净` 就是这么实测留下 73M
    残骸（读数见 `docs/superpowers/specs/ledger/logs/P503/20260928-161751/00-residue.log`：
    那一趟的 `du -sk` = 75196 KiB = 73.43 MiB，中止 1h32m 后仍原样在盘上），它的红因句子与仓库红一模一样。

    与显式出口共存是设计不是冗余：正常收尾与 `bail()` 都已调过 `dispose()`，兜底这一脚先看
    `clone/` 还在不在——不在就安静退出，绝不再印第二句"收尾"，免得取证日志里出现两条互相
    矛盾的回收行。这一条也排在"让路"之前：让路那句话的前提是"有一份克隆值得留"，装架之前
    就停的路（入口闸、`--clone` 目录已存在）根本没这份克隆，跟着出声会把人引去查一个不存在现场。
    """
    def _hook() -> None:
        if not (Path(tmp) / "clone").exists():
            return  # 已经有人收过了（正常出口或 bail），或者压根还没装架：都不出声
        if _EVIDENCE["why"]:
            print(f"收尾闸让路：{_EVIDENCE['why']}——现场只活在克隆里，本次不回收")
            return
        print("收尾闸：这条中止路没接显式收尾（dispose），已由兜底闸回收；要把现场留成取证，走 --keep")
        dispose(tmp, owned=owned, keep=keep, repo_root=repo_root)

    atexit.register(_hook)


if __name__ == "__main__":
    print(__doc__)
