#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""按 CI 口径复现 lychee --offline：存在性以 git 索引（= 干净 checkout）为准。

为什么不能只看文件系统：工作区里存在、但被 .gitignore 挡掉 / 从未 add 的文件，
在 GitHub runner 的 checkout 里根本不存在。本轮 7 处断链全部是这一类。
"""
import os
import re
import subprocess
import sys

# workflow 里的 --exclude-path（对端仓那份副本必须与此完全一致）
EXCLUDE = ("node_modules", "dist", "pg_data", "internal-docs")
LINK = re.compile(r"\[[^\]]*\]\(\s*([^)\s]+)(?:\s+[\"'][^\"']*[\"'])?\s*\)")


def main(root):
    tracked = set(subprocess.run(
        ["git", "-C", root, "-c", "core.quotePath=false", "ls-files", "-z"],
        capture_output=True, text=True, check=True).stdout.split("\0"))
    broken = []
    # 只扫「已入仓」的 md：未追踪文件在任何干净 checkout / CI 里都不存在，
    # 扫它们只会造成本地与 CI 判定不一致（本轮 3 处误报即由此而来）。
    md_files = sorted(fp for fp in tracked if fp.endswith(".md"))
    n = 0
    for rel in md_files:
        if any(rel.startswith(e + "/") or "/" + e + "/" in "/" + rel for e in EXCLUDE):
            continue
        fp = os.path.abspath(os.path.join(root, rel))
        n += 1
        fence = False
        for lineno, line in enumerate(open(fp, encoding="utf-8", errors="replace"), 1):
            # 代码块内的示例链接不是引用（lychee 按 markdown AST 解析，同样跳过）
            if re.match(r"^\s*(```|~~~)", line):
                fence = not fence
                continue
            if fence:
                continue
            for m in LINK.finditer(line):
                tgt = m.group(1)
                if re.match(r"^[a-zA-Z][a-zA-Z0-9+.-]*:", tgt) or tgt.startswith("#"):
                    continue
                if "${" in tgt or "{{" in tgt or "<" in tgt or "`" in tgt:
                    continue
                path = tgt.partition("#")[0].partition("?")[0]
                if not path:
                    continue
                cand = os.path.normpath(os.path.join(os.path.dirname(fp), path))
                relc = os.path.relpath(cand, root)
                if relc.startswith(".."):
                    why = "跨出仓库（CI checkout 里没有对端仓）"
                elif relc in tracked:
                    continue
                elif any(t.startswith(relc.rstrip("/") + "/") for t in tracked):
                    continue
                elif os.path.exists(cand):
                    why = "本地存在但未纳入版本控制（.gitignore 或从未 add）"
                else:
                    why = "仓库内不存在"
                broken.append((rel, lineno, tgt, relc, why))
    print(f"──── 扫描 {n} 个 md（CI 口径：只认 git 索引）────")
    for f, l, t, c, why in sorted(broken):
        print(f"  ❌ {f}:{l} → {t}\n        {why}｜解析为 {c}")
    print(f"──── 断链 {len(broken)} 处 ────")
    return 1 if broken else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1] if len(sys.argv) > 1 else "."))
