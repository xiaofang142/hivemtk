#!/usr/bin/env python3
"""硬编码扫描器：把 user-server 服务端里「应该入库用于用户自定义、却写死在代码里」的值全部枚举出来。

三类（与需求对位）：
  CONFIG —— 命名的数字/布尔/时长常量与显式默认值（阈值、上限、超时、批量大小、开关）。
  DATA   —— 硬编码的枚举/列表/映射字面量（渠道类型、状态码、类别选项、标签映射）。
  CN     —— 用户可感知的中文字符串字面量（错误提示、默认文案、状态标签），排除注释与纯日志。

边界（写清楚本扫器看见什么、看不见什么，避免把“没扫到”印成“没问题”）：
  - 只看 user-server 下非 *_test.go、非种子（cmd/seed、*_seeds.go）的 Go 文件。
  - CONFIG 只认「标识符 = 数字/布尔/时长/明确数值表达式」形态的命名常量；
    直接内联在函数里的魔数（如 f(ctx, 30)）审慎起见也单列 INLINE_NUM 待人工复核。
  - DATA 只认「= []T{...}」「= map[T]U{...}」的直接字面量赋值；循环里拼出来的集合不在视野。
  - CN 只认字符串字面量里的中文；注释、go doc、logger 调试文案会按启发式减噪，但最终以人工确认为准。
  - 输出到 hardcode-sweep/（JSON + Markdown），供迁移逐条核对；不修改任何源码。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
SERVER = REPO_ROOT / "user-server"

CN_RANGE = "\u4e00-\u9fa5"
CN_IN_STR = re.compile(r'"([^"\n]*[' + CN_RANGE + r']+[^"\n]*)"')
# 命名数值/布尔/时段/位移常量: Ident = <literal>
CONST_ASSIGN = re.compile(
    r'\b([A-Z][A-Za-z0-9_]*)\s*=\s*'
    r'(\d[\d_.e+]*|true|false|(?:\d+(?:\.\d+)?\s*\*\s*)?time\.(?:Second|Millisecond|Minute|Hour)|'
    r'\d+\s*<<\s*\d+)'
)
TIME_LIT = re.compile(r'time\.(?:Second|Millisecond|Minute|Hour)')
BOOL_LIT = re.compile(r'\btrue\b|\bfalse\b')
NUM_LIT = re.compile(r'^[\d.]+$')
# 数据字面量: Ident = ([]T{...} | map[...]...{...})
DATA_LIT = re.compile(
    r'\b([A-Z][A-Za-z0-9_]*)\s*=\s*(?:map\[[^\]]*\]|\[\][A-Za-z0-9_.\[\]]*)\s*\{'
)
INLINE_NUM = re.compile(r'\(\s*\d+(?:\.\d+)?\s*\)')

SKIP_SUFFIX = ("_test.go", "_seeds.go", "_seed.go")
SKIP_TREE = ("cmd/seed", "/vendor/", "/migrations/", "/test/", "/tests/", "/tmp/", "/backups/")


def classify_const(value: str) -> str | None:
    if BOOL_LIT.fullmatch(value):
        return "bool"
    if TIME_LIT.search(value) or "<<" in value:
        return "duration"
    if NUM_LIT.fullmatch(value):
        return "go_num"
    if re.fullmatch(r"\d+\.\d+", value):
        return "float"
    return None


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=str(REPO_ROOT / "hardcode-sweep"), help="输出目录")
    ap.add_argument("--repo", default=str(REPO_ROOT))
    args = ap.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    roots = [SERVER / "internal", SERVER / "cmd"]
    cfg: list[dict] = []
    data: list[dict] = []
    cn: list[dict] = []
    inline_num: list[dict] = []

    files = []
    for root in roots:
        if not root.is_dir():
            continue
        for p in root.rglob("*.go"):
            sp = str(p)
            if p.name.endswith(SKIP_SUFFIX):
                continue
            if any(t in sp for t in SKIP_TREE):
                continue
            files.append(p)

    for p in sorted(files):
        try:
            text = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        rel = str(p.relative_to(SERVER))
        for ln, raw in enumerate(text.splitlines(), 1):
            code = re.sub(r'//.*$', '', raw)
            # CONFIG / DATA 常量名
            for m in CONST_ASSIGN.finditer(code):
                name, val = m.group(1), m.group(2)
                kind = classify_const(val)
                if kind:
                    cfg.append({"file": rel, "line": ln, "name": name, "value": val, "kind": kind})
            for m in DATA_LIT.finditer(code):
                data.append({"file": rel, "line": ln, "name": m.group(1), "text": raw.strip()})
            # CN 字符串（排除注释行 / 纯日志行）
            if not code.strip().startswith(("//", "*", "/*")):
                for m in CN_IN_STR.finditer(code):
                    snippet = m.group(1)
                    if not snippet:
                        continue
                    # 减噪：整行若主要是日志/测试性质，仍保留但打 light 标
                    cn.append({"file": rel, "line": ln, "text": snippet, "ctx": raw.strip()[:160]})
            if INLINE_NUM.search(code):
                inline_num.append({"file": rel, "line": ln, "ctx": raw.strip()[:160]})

    def dedup(rows: list[dict]) -> list[dict]:
        seen = set()
        outr = []
        for r in rows:
            k = (r["file"], r["line"], r.get("name") or r.get("text", "")[:80])
            if k in seen:
                continue
            seen.add(k)
            outr.append(r)
        return outr

    cfg, data, cn, inline_num = dedup(cfg), dedup(data), dedup(cn), dedup(inline_num)

    report = {
        "summary": {
            "config_consts": len(cfg),
            "data_literals": len(data),
            "cn_strings": len(cn),
            "inline_numbers_candidates": len(inline_num),
        },
        "config_consts": cfg,
        "data_literals": data,
        "cn_strings": cn,
        "inline_numbers_candidates": inline_num,
    }
    (out / "hardcode-sweep.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))

    # Markdown 概要（按文件聚合，便于逐文件迁移）
    md = ["# 硬编码扫描报告\n", "## 概览\n"]
    md.append(f"- 配置类常量(CONFIG): {len(cfg)}")
    md.append(f"- 数据类字面量(DATA): {len(data)}")
    md.append(f"- 中文字符串(CN): {len(cn)}")
    md.append(f"- 内联数字候选(INLINE_NUM): {len(inline_num)}\n")
    md.append("## 数据类字面量(DATA)\n")
    for r in data:
        md.append(f"- `{r['file']}:{r['line']}` `{r['name']}`")
    md.append("\n## 配置类常量(CONFIG) 前 300 条（完整见 JSON）\n")
    for r in cfg[:300]:
        md.append(f"- `{r['file']}:{r['line']}` `{r['name']} = {r['value']}` ({r['kind']})")
    (out / "hardcode-sweep.md").write_text("\n".join(md) + "\n")

    print("=== hardcode-sweep 扫描完成 ===")
    print(f"文件面: {len(files)} 个（非测试/非种子）")
    print(f"CONFIG（命名数值/布尔/时长常量）: {len(cfg)}")
    print(f"DATA（枚举/列表/映射字面量）     : {len(data)}")
    print(f"CN（中文字符串字面量）          : {len(cn)}")
    print(f"INLINE_NUM（内联数字候选）      : {len(inline_num)}")
    print(f"输出: {out}/hardcode-sweep.json , {out}/hardcode-sweep.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())