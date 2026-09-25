"""取证日志脱敏：把 gitleaks 会认成凭证的「标签 + 长令牌」片段收敛成 前缀+[len N]。

为什么必须脱敏，而不是往 .gitleaks.toml 里加豁免：这些令牌全部由测试夹具现算
（`uid-rs-<n>-<nonce>`、`rt_<hex>` 等），每轮都不一样 ⇒ 项目规则要求的"逐值豁免"写不完；
而 allowlist 的 paths 键在本项目是禁用的，路径豁免也不许走。所以只能在**落盘前**把
命中面改掉，让常驻产物天然过门。

脱敏只碰"看起来是凭证赋值"的连续段，保留前 6 字符与长度，取证时仍能读出
"这里有过一个多长的、什么前缀的键"；测试名、包路径、时间戳、md5 前缀都不该被改。
"""

import pathlib
import re

_LABEL = (
    r"[A-Za-z0-9_.\-]{0,24}"
    r"(?:key|token|secret|passwd|password|pwd|credential|credentials|authorization|apikey|api_key|access_key)"
    r"[A-Za-z0-9_.\-]{0,24}"
)
_VALUE = r"[A-Za-z0-9\-._~+/]{18,}"
# sep 里允许一枚引号：JSON 里的形状是 `"_approval_token":"rt_…"`，标签与 `:` 之间隔着闭合引号；
# 只按 `key=裸值` 写会漏掉整族带引号的命中（实测 M10 就是这么漏的）。
_PATTERN = re.compile("(" + _LABEL + r")(\s*[\"']?\s*[=:]\s*)([\"']?)(" + _VALUE + r")(\3)")


def scrub(text: str) -> str:
    def rep(m):
        val = m.group(4)
        return f"{m.group(1)}{m.group(2)}{m.group(3)}{val[:6]}[len {len(val)}]{m.group(5)}"

    return _PATTERN.sub(rep, text)


def scrub_file(path) -> None:
    """就地脱敏：子进程直接把输出写进句柄的日志（R28 族）用这个补一刀。"""
    p = pathlib.Path(path)
    p.write_text(scrub(p.read_text(encoding="utf-8", errors="replace")), encoding="utf-8")
