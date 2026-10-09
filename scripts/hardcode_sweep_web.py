#!/usr/bin/env python3
"""前端（user-web）硬编码扫描器：与 hardcode_sweep.py（Go 侧）同口径，补齐 Web 面。

五类（与需求「文字提示 / 门禁阈值 / 选项 / 地址 / 文案」对位）：
  CN      —— 未走 i18n（t()）的中文字面量：用户可见文案写死在模板/脚本里。
  CONFIG  —— 命名的数值/布尔常量：阈值、上限、超时、分页条数、批量大小。
  DATA    —— 硬编码的选项/枚举/标签映射字面量：应从字典表或接口取。
  URL     —— 写死的绝对地址（外链、ws、CDN、Webhook 回调）。
  TUNE    —— 内联调参形态：setTimeout/timeout:/limit:/size:/pageSize= 的裸数字。

边界（写清楚本扫器看见什么、看不见什么）：
  - 只看 user-web/src 下非测试的 .vue/.js，跳过 node_modules/dist/tests/stories。
  - t('...') / $t('...') / i18n.t('...') 调用内的字符串一律不算硬编码（已走 i18n）。
  - CN 会把 attributes(alt/title/placeholder) 与正文分开标记，便于判断「用户可见度」。
  - 模板里的 {{ }} 插值表达式、数字绑定、对象键不算文案。
  - 输出到 hardcode-sweep/（JSON + Markdown），供逐条迁移核对；不修改任何源码。
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from collections import Counter, defaultdict
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
WEB = REPO_ROOT / "user-web"
SRC = WEB / "src"

CN_RANGE = "\u4e00-\u9fa5"
CN_IN_STR = re.compile(r'([\'"`])((?:[^\\]|\\.)*?[' + CN_RANGE + r']+(?:[^\\]|\\.)*?)\1')
I18N_CALL = re.compile(r"(?:\$?t|i18n\.t|i18n\.te|te)\(\s*['\"`]")
# 命名常量: (const|let|var) IDENT = <number|bool|time>
CONST_ASSIGN = re.compile(
    r"\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*"
    r"(\d[\d_.e+]*(?:_ms|_s|_sec|_mb|_kb|px)?|true|false)"
)
# 选项/枚举数组或对象字面量
DATA_LIT = re.compile(
    r"\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*(?:\[|\{)"
)
URL_LIT = re.compile(r"(https?://|wss?://)[^\s'\"`)<>]{4,}")
TUNE_CTX = re.compile(
    r"(setTimeout|setInterval|timeout|interval|limit|pageSize|page_size|max|min|size|count|"
    r"retries|threshold|ttl|TTL|Timeout|Interval|Threshold|Threshold|expire|duration|weight|"
    r"duration|concurrency|batch|delays?|window|days|hours|minutes)"
)
NUM_ARG = re.compile(r"(?<![\w.])\b(\d{1,7})\b")

SKIP_DIR = ("node_modules", "dist", "tests", "stories", ".git", "coverage")
VISIBLE_ATTR = re.compile(
    r'(placeholder|title|alt|label|description|message|content|text|empty-text|confirm-button-text|cancel-button-text)='
)
SKIP_FILE_SUFFIX = (".spec.js", ".test.js", ".config.js")

# 这些模式里的中文不是「用户可见文案」
NOT_USER_FACING = re.compile(
    r"(console\.|logger\.|debugger|eslint|prettier|@ts-|TODO|FIXME|HACK|NOTE:|注意|说明|例如|示例|"
    r"https?://|data-testid|import |from ['\"])"
)


def strip_comments(text: str) -> str:
    """去块注释与行注释，保留行数（用等量空行占位）。"""
    text = re.sub(r"/\*.*?\*/", lambda m: "\n" * m.group(0).count("\n"), text, flags=re.S)
    text = re.sub(r"//[^\n]*", "", text)
    text = re.sub(r"(?m)^\s*\*.*$", "", text)  # jsdoc 行
    return text


def iter_files():
    for p in sorted(SRC.rglob("*")):
        if p.suffix not in (".vue", ".js"):
            continue
        sp = str(p)
        if any(t in sp for t in SKIP_DIR):
            continue
        if p.name.endswith(SKIP_FILE_SUFFIX):
            continue
        yield p


def mask_i18n(line: str) -> str:
    """把 t('...')/$t('...')/i18n.t('...') 内部的字符串挖空，保留列宽便于定位。"""
    out = []
    i = 0
    n = len(line)
    while i < n:
        m = I18N_CALL.search(line, i)
        if not m:
            out.append(line[i:])
            break
        out.append(line[i:m.start()])
        quote = line[m.end() - 1]
        j = m.end()
        while j < n:
            if line[j] == "\\":
                j += 2
                continue
            if line[j] == quote:
                break
            j += 1
        end = min(j + 1, n)
        out.append(" " * (end - m.start()))
        i = end
    return "".join(out)


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--out", default=str(REPO_ROOT / "hardcode-sweep"))
    args = ap.parse_args()
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)

    cn: list[dict] = []
    cfg: list[dict] = []
    data: list[dict] = []
    urls: list[dict] = []
    tune: list[dict] = []

    files = list(iter_files())
    for p in files:
        rel = str(p.relative_to(REPO_ROOT))
        try:
            raw_text = p.read_text(encoding="utf-8", errors="replace")
        except OSError:
            continue
        clean = strip_comments(raw_text)
        lines = clean.splitlines()
        raw_lines = raw_text.splitlines()
        for ln, code in enumerate(lines, 1):
            raw = raw_lines[ln - 1] if ln - 1 < len(raw_lines) else code
            if not code.strip():
                continue
            masked = mask_i18n(code)
            # CN：已走 i18n 的挖空
            for m in CN_IN_STR.finditer(masked):
                snippet = m.group(2)
                if not snippet.strip():
                    continue
                if NOT_USER_FACING.search(raw):
                    continue
                cn.append({
                    "file": rel,
                    "line": ln,
                    "text": snippet[:120],
                    "attr": bool(VISIBLE_ATTR.search(raw)),
                    "ctx": raw.strip()[:160],
                })
            for m in CONST_ASSIGN.finditer(code):
                cfg.append({"file": rel, "line": ln, "name": m.group(1),
                            "value": m.group(2), "ctx": raw.strip()[:120]})
            for m in DATA_LIT.finditer(code):
                data.append({"file": rel, "line": ln, "name": m.group(1),
                             "text": raw.strip()[:160]})
            for m in URL_LIT.finditer(code):
                urls.append({"file": rel, "line": ln, "url": m.group(0)[:160],
                             "ctx": raw.strip()[:120]})
            if TUNE_CTX.search(code):
                for m in NUM_ARG.finditer(code):
                    n = int(m.group(1))
                    if n in (0, 1, 2, 10, 100) and "size" not in code.lower():
                        continue
                    tune.append({"file": rel, "line": ln, "n": n, "ctx": raw.strip()[:160]})

    def dedup(rows: list[dict]) -> list[dict]:
        seen, res = set(), []
        for r in rows:
            k = (r["file"], r["line"], r.get("name") or r.get("text") or r.get("url") or r.get("n"))
            if k in seen:
                continue
            seen.add(k)
            res.append(r)
        return res

    cn, cfg, data, urls, tune = dedup(cn), dedup(cfg), dedup(data), dedup(urls), dedup(tune)
    attr_cn = [r for r in cn if r["attr"]]

    report = {
        "summary": {
            "files": len(files),
            "cn_strings": len(cn),
            "cn_visible_attr": len(attr_cn),
            "config_consts": len(cfg),
            "data_literals": len(data),
            "hardcoded_urls": len(urls),
            "tune_inline": len(tune),
        },
        "cn_strings": cn,
        "config_consts": cfg,
        "data_literals": data,
        "hardcoded_urls": urls,
        "tune_inline": tune,
    }
    (out / "hardcode-sweep-web.json").write_text(json.dumps(report, ensure_ascii=False, indent=2))

    md = ["# 前端（user-web）硬编码扫描报告\n", "## 概览\n",
          f"- 扫描文件: {len(files)}",
          f"- CN（未走 i18n 的中文字面量）: {len(cn)}（其中用户可见属性 {len(attr_cn)}）",
          f"- CONFIG（命名数值/布尔常量）: {len(cfg)}",
          f"- DATA（选项/枚举/标签映射字面量）: {len(data)}",
          f"- URL（写死绝对地址）: {len(urls)}",
          f"- TUNE（内联调参数字）: {len(tune)}\n",
          "## DATA 选项/枚举字面量\n"]
    for r in data:
        md.append(f"- `{r['file']}:{r['line']}` `{r['name']}`")
    md.append("\n## CONFIG 命名常量\n")
    for r in cfg:
        md.append(f"- `{r['file']}:{r['line']}` `{r['name']} = {r['value']}`")
    md.append("\n## URL 写死地址\n")
    for r in urls:
        md.append(f"- `{r['file']}:{r['line']}` `{r['url']}`")
    (out / "hardcode-sweep-web.md").write_text("\n".join(md) + "\n")

    print("=== hardcode-sweep-web 扫描完成 ===")
    print(f"文件面: {len(files)} 个")
    print(f"CN（中文字面量, 已挖空 t()）: {len(cn)}  其中用户可见属性: {len(attr_cn)}")
    print(f"CONFIG（命名常量）          : {len(cfg)}")
    print(f"DATA（选项/枚举字面量）      : {len(data)}")
    print(f"URL（写死绝对地址）          : {len(urls)}")
    print(f"TUNE（内联调参数字）        : {len(tune)}")
    print(f"输出: {out}/hardcode-sweep-web.json , {out}/hardcode-sweep-web.md")
    return 0


if __name__ == "__main__":
    sys.exit(main())