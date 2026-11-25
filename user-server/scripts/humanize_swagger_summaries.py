#!/usr/bin/env python3
"""
humanize_swagger_summaries.py —— E-2 收尾（2026-10-24）：把 backfill_swagger_annotations.py
留下的「// [AUTO] FuncName」占位 Summary 人工化为真实中文描述。

口径（与规划 §4.2 收尾一致）：
  - 只处理形如 `^// \\[AUTO\\] (\\w+)$` 的占位行（backfill 批次生成的机器标记）；
  - 定位该占位所属的 handler 函数（占位行之后第一个非注释行必须是同名 FuncDecl），
    从函数体提取 Summary 文案，优先级：
      ① 函数体内 response.Success(ctx, _, "中文消息") 的中文消息（接口成功时说什么）；
      ② 注解块里既有的「// FuncName 中文说明」人工描述行（比裸错误消息更像 Summary；
         以 HTTP 动词开头的路由描述行不算）；
      ③ 函数体内第一处中文字符串字面量（错误消息等）；
      ④ 都没有则跳过，留待人工（脚本会打印清单）。
  - 替换形状：`// @Summary      文案`——与仓内既有手写注解同列对齐（@Summary + 6 空格），
    swag 标准标签；同块的 @Tags/@Router 不动，router 守卫按 @Router 计数，不受影响。
  - 中文文案超 120 字符截断（Summary 是一行）；转义（\\n、\\"）清洗为普通文本。

幂等：处理过的行不再是 [AUTO] 形状，重跑自然跳过。

用法：
  python3 scripts/humanize_swagger_summaries.py --dry-run   # 只打印计划
  python3 scripts/humanize_swagger_summaries.py             # 直接写回
"""
import argparse
import os
import re
import sys

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(_SCRIPT_DIR), "..", "scripts"))
from path_guard import safe_resolve  # noqa: E402  路径闸：写回目标不得越出 user-server

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))

# 与 internal/router/route_surface_consistency_test.go 的 annotationScanRoot/skipAnnotationDirs 同面：
# 守卫扫多大，人工化就扫多大，否则占位会藏在门外的目录里。
SKIP_DIRS = {".git", "docs", "node_modules", "dist", "vendor", "testdata"}

AUTO_RE = re.compile(r"^// \[AUTO\] (\w+)$")
FUNC_DECL_RE = re.compile(r"^func (?:\(\w+ \*?(\w+)\) )?(\w+)\(")
DOC_HINT_RE_TMPL = r"^// {fn} (\S.*)$"
HTTP_VERB_RE = re.compile(r"^(GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)\b")
CHINESE_RE = re.compile(r"[\u4e00-\u9fff]")
STR_LIT_RE = re.compile(r'"((?:[^"\\]|\\.)*)"')
SUCCESS_CALL_RE = re.compile(r"response\.Success\w*\s*\(")

MAX_SUMMARY_LEN = 120

# 提取出的文案若以这些后缀结尾（源码里为后面拼接 err.Error() 留的口），截掉：
# "查询失败: " 这类前缀式错误消息直译成 Summary 不成句。
TRAILING_TRIM = ("，", ",", "：", ":", " ", ";", "；")
# 文案中混入的行注释边界（doc-hint 行里夹着「// GET ...」路由备注）：截到该边界为止。
INLINE_COMMENT_RE = re.compile(r"\s*//\s*")

# 状态词直译不成 Summary（"获取成功"没有说出"获取了什么"），这类当无文案处理留人工。
GENERIC_RE = re.compile(
    r"(获取|查询|操作|更新|删除|创建|提交|保存|处理|执行|导入|同步|发送|拉取|加载)(成功)?"
    r"|(ok|success|done|error|failed|统计)"
)


def iter_go_files():
    for root, dirs, files in os.walk(REPO):
        dirs[:] = [d for d in dirs if d not in SKIP_DIRS]
        for f in sorted(files):
            if not f.endswith(".go") or f.endswith("_test.go"):
                continue
            yield os.path.join(root, f)


def func_body_lines(lines, decl_idx):
    """从 FuncDecl 行起做花括号配对，返回函数体行区间（含首尾）。"""
    depth = 0
    started = False
    for k in range(decl_idx, len(lines)):
        for ch in lines[k]:
            if ch == "{":
                depth += 1
                started = True
            elif ch == "}":
                depth -= 1
        if started and depth == 0:
            return lines[decl_idx : k + 1]
        if started and depth < 0:
            return None
    return None


def paren_span(text, open_idx):
    """从 text[open_idx] == '(' 起配对，返回右括号索引（找不到返回 -1）。"""
    depth = 0
    for k in range(open_idx, len(text)):
        c = text[k]
        if c == '"':  # 跳过字符串字面量里的括号
            k += 1
            while k < len(text):
                if text[k] == "\\":
                    k += 2
                    continue
                if text[k] == '"':
                    break
                k += 1
            continue
        if c == "(":
            depth += 1
        elif c == ")":
            depth -= 1
            if depth == 0:
                return k
    return -1


def success_message(body_text):
    """优先级①：response.Success(ctx, x, "中文消息") 里的中文消息。"""
    for m in SUCCESS_CALL_RE.finditer(body_text):
        close = paren_span(body_text, m.end() - 1)
        if close < 0:
            continue
        call = body_text[m.end() : close]
        for sm in STR_LIT_RE.finditer(call):
            if CHINESE_RE.search(sm.group(1)):
                return clean(sm.group(1))
    return None


def first_chinese_literal(body_text):
    """优先级③：函数体内第一处中文字符串字面量。"""
    for sm in STR_LIT_RE.finditer(body_text):
        if CHINESE_RE.search(sm.group(1)):
            return clean(sm.group(1))
    return None


def clean(raw):
    """清洗 Go 字符串字面量的源码形态：反转义、压平换行、截断。"""
    s = raw.replace('\\"', '"').replace("\\n", " ").replace("\\t", " ").replace("\\\\", "\\")
    s = re.sub(r"\s+", " ", s).strip()
    s = INLINE_COMMENT_RE.split(s, maxsplit=1)[0].strip()  # 去掉「// GET ...」路由备注尾巴
    s = s.rstrip("".join(TRAILING_TRIM))  # 去掉为 err.Error() 预留的尾标点
    if len(s) > MAX_SUMMARY_LEN:
        s = s[:MAX_SUMMARY_LEN].rstrip("".join(TRAILING_TRIM))
    return s


def doc_hint(lines, auto_idx, decl_idx, fn):
    """优先级②：注解块里既有的「// FuncName 中文说明」行。"""
    hint_re = re.compile(DOC_HINT_RE_TMPL.replace("{fn}", re.escape(fn)))
    for j in range(auto_idx + 1, decl_idx):
        m = hint_re.match(lines[j])
        if not m:
            continue
        text = m.group(1).strip()
        if not CHINESE_RE.search(text):
            continue
        # 「// List GET /api/backup/list → ...」这类是路由描述不是 Summary，跳过
        if HTTP_VERB_RE.match(text) or text.startswith("/"):
            continue
        return clean(text)
    return None


def process_file(path, dry_run):
    """返回 (替换数, 人工遗留数, 计划行列表)。"""
    src = open(path).read()
    lines = src.split("\n")
    replaced = 0
    manual = 0
    plan = []
    changed = False
    for i, line in enumerate(lines):
        m = AUTO_RE.match(line)
        if not m:
            continue
        fn = m.group(1)
        # 占位行之后应是注解块剩余行 + FuncDecl：第一个非注释行必须匹配同名函数
        decl_idx = None
        for j in range(i + 1, len(lines)):
            if lines[j].startswith("//"):
                continue
            decl_idx = j
            break
        dm = FUNC_DECL_RE.match(lines[decl_idx]) if decl_idx is not None else None
        if dm is None or dm.group(2) != fn:
            manual += 1
            plan.append((i, fn, None, "占位行找不到同名 handler 函数"))
            continue
        body = func_body_lines(lines, decl_idx)
        if body is None:
            manual += 1
            plan.append((i, fn, None, "函数体花括号配对失败"))
            continue
        body_text = "\n".join(body)
        summary = (
            success_message(body_text)
            or doc_hint(lines, i, decl_idx, fn)
            or first_chinese_literal(body_text)
        )
        # 文案过短不成句的（"ok"、"获取成功"等状态词直译不成 Summary）当无文案处理，留人工
        if not summary or GENERIC_RE.fullmatch(summary):
            manual += 1
            plan.append((i, fn, None, "提取文案过短不成句（%r），需人工" % summary))
            continue
        new_line = "// @Summary      " + summary
        plan.append((i, fn, new_line, None))
        lines[i] = new_line
        replaced += 1
        changed = True
    if changed and not dry_run:
        # 写回目标过闸：realpath+commonpath 必须留在 user-server 根内。
        with open(safe_resolve(path, REPO), "w") as fh:
            fh.write("\n".join(lines))
    return replaced, manual, plan


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()

    total_replaced = 0
    total_manual = 0
    files_touched = 0
    leftovers = []
    for path in iter_go_files():
        replaced, manual, plan = process_file(path, args.dry_run)
        if replaced:
            files_touched += 1
        total_replaced += replaced
        total_manual += manual
        for i, fn, new_line, why in plan:
            if new_line:
                print(f"  {rel(path)}:{i + 1}  {fn} -> {new_line[len('// @Summary      '):]}")
            else:
                leftovers.append(f"{rel(path)}:{i + 1}  {fn}  （{why}）")

    mode = "DRY-RUN" if args.dry_run else "写回"
    print(f"[{mode}] 人工化 {total_replaced} 条，涉及文件 {files_touched} 个，人工遗留 {total_manual} 条")
    if leftovers:
        print("人工遗留清单（读 handler 代码逐条补真实中文描述）：")
        for l in leftovers:
            print("  " + l)


def rel(path):
    return os.path.relpath(path, REPO)


if __name__ == "__main__":
    main()
