"""脚本侧文件读写统一路径闸（Mimosa path-traversal 修复共用）。

用法：
    from path_guard import safe_resolve, safe_open

    p = safe_resolve(user_input, REPO_ROOT)            # 拒绝绝对路径/..//符号链接逃逸
    with safe_open(user_input, "w", root=OUT_DIR) as f: ...

判据：realpath 解析后必须落在 root 内（允许等于 root 的子路径本身不算文件，
open 一个目录会自然报错）；root 自身也经 realpath 规范化。绝不接受用户提供的
绝对路径或 `..` 段——即便 resolve 后仍在 root 内也拒绝，避免口径含糊。
"""

from __future__ import annotations

import os
from pathlib import Path


def safe_resolve(path, root, *, allow_absolute: bool = False) -> Path:
    """把路径约束在 root 内，返回解析后的绝对路径。

    - 拒绝空路径、绝对路径（默认）、含 `..` / `\\..` 段的路径；
    - realpath 解析后与 root 取 commonpath，越界即 ValueError（符号链接逃逸同样拦）。
    """
    root_real = os.path.realpath(os.fspath(root))
    name = os.fspath(path)
    if not name:
        raise ValueError("路径不能为空")
    if not allow_absolute and os.path.isabs(name):
        raise ValueError(f"禁止绝对路径: {name!r}")
    parts = name.replace("\\", "/").split("/")
    if ".." in parts:
        raise ValueError(f"禁止路径穿越（..）: {name!r}")
    resolved = os.path.realpath(os.path.join(root_real, name) if not os.path.isabs(name) else name)
    if resolved == root_real or os.path.commonpath([root_real, resolved]) != root_real:
        raise ValueError(f"路径越出允许目录 {root_real!r}: {name!r}")
    return Path(resolved)


def safe_open(path, mode="r", *, root, allow_absolute: bool = False, **kwargs):
    """safe_resolve + open 的合成入口（所有用户提供的文件路径均经此打开）。"""
    return open(safe_resolve(path, root, allow_absolute=allow_absolute), mode, **kwargs)
