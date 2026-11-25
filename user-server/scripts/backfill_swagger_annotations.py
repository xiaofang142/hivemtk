#!/usr/bin/env python3
"""
backfill_swagger_annotations.py —— E-2 注解机械补齐（2026-10-24）。

输入：
  --routes /tmp/routes_clean.json   dump_routes 产出的 [{method,path,handler}]
  --dry-run                         只打印计划，不写文件

口径（与规划 §4.2 一致）：
  - 只给「真实注册路由的服务 handler」补注解；未接线函数不碰。
  - 每条路由若 handler 已有任何 @Router 注解（同 handler 多路由别名时），跳过。
  - 注解块插在 FuncDecl 的 doc 注释末尾（无 doc 则新建），形如：
      // Summary 占位（按 handler 名生成）
      // @Tags      <Controller 去掉 Controller 后缀，小写连字符>
      // @Router    /api/xxx/{id} [get]
  - gin :param 一律转 OpenAPI {param}（守卫 badForm 检查）。
  - Summary 用「<Tag> <Handler>」占位——本脚本管形状对齐，文案润色留给各域 owner；
    占位文案前缀「[AUTO]」标明机器生成，swag 生成器照常收录。

写回策略：AST 定位 FuncDecl 的 doc 注释块在源文件中的行区间，直接文本插入，
保留其余字节不动（不重排 import/格式）。
"""
import argparse
import json
import os
import re
import sys

_SCRIPT_DIR = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(_SCRIPT_DIR), "..", "scripts"))
from path_guard import safe_resolve  # noqa: E402  路径闸：源文件写回不得越出 user-server

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))  # user-server/
TOKEN = "@" + "Router"


def handler_key(handler: str):
    # gin Routes() 的 Handler 形如：
    #   "hivemtk-user/internal/controller.(*CustomerSessionController).GetSessions-fm"
    # 去掉包路径前缀、-fm/-fmr 后缀。
    h = handler
    h = re.sub(r'-fm[rd]?$', '', h)
    m = re.search(r'\(\*?(\w+)\)\.(\w+)$', h)
    if m:
        return m.group(1), m.group(2)
    m = re.search(r'([\w.]+)\.(\w+)$', h)
    if m:
        return None, m.group(2)
    return None, None


def param_to_brace(path: str) -> str:
    return re.sub(r':([A-Za-z_][A-Za-z0-9_]*)', r'{\1}', path)


def tag_for(ctrl_type: str) -> str:
    t = ctrl_type
    if t.endswith('Controller'):
        t = t[:-len('Controller')]
    # 驼峰 → 连字符小写
    return re.sub(r'(?<!^)(?=[A-Z])', '-', t).lower()


def parse_go_func_positions(src: str):
    """返回 {(recv_type, func_name): (doc_start_line, decl_line)}，行号 1-based。
    doc_start_line 为 doc 注释首行（无 doc 则等于 decl 行）。"""
    out = {}
    lines = src.split('\n')
    i = 0
    n = len(lines)
    while i < n:
        m = re.match(r'^func \((\w+) \*?(\w+)\) (\w+)\(', lines[i])
        if m:
            typ, fn = m.group(2), m.group(3)
            # 向上找连续注释块
            j = i - 1
            while j >= 0 and lines[j].startswith('//'):
                j -= 1
            doc_start = j + 1 if j + 1 < i else i  # 无注释时 doc_start = i（在 decl 行前插入）
            out[(typ, fn)] = (doc_start, i)
        i += 1
    return out


def has_router_ann(src_lines, doc_start, decl_line):
    for k in range(doc_start, decl_line):
        if TOKEN in src_lines[k]:
            return True
    return False


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--routes', default='/tmp/routes_clean.json')
    ap.add_argument('--dry-run', action='store_true')
    args = ap.parse_args()

    # routes 是只读输入（命令行给，默认 /tmp/routes_clean.json）：realpath 解析符号链接后确认是普通文件。
    routes_path = os.path.realpath(args.routes)
    if not os.path.isfile(routes_path):
        raise SystemExit(f"routes 文件不存在或不是普通文件: {args.routes}")
    routes = json.load(open(routes_path))

    # 1. 按 (Type, Func) 聚合路由（同 handler 多别名：取第一条做注解，跳过其余）
    by_handler = {}
    for r in routes:
        typ, fn = handler_key(r['handler'])
        if not fn:
            continue
        by_handler.setdefault((typ, fn), []).append((r['method'], r['path']))

    # 2. 找每个 handler 的定义文件（internal/controller、internal/geo/controller）
    ctrl_dirs = ['internal/controller', 'internal/geo/controller', 'internal/bridge']
    file_index = {}  # (typ, fn) -> path
    for d in ctrl_dirs:
        full = os.path.join(REPO, d)
        if not os.path.isdir(full):
            continue
        for f in os.listdir(full):
            if not f.endswith('.go') or f.endswith('_test.go'):
                continue
            src = open(os.path.join(full, f)).read()
            for (typ, fn) in parse_go_func_positions(src):
                file_index.setdefault((typ, fn), os.path.join(full, f))

    # 3. 逐文件插入
    edits = {}  # path -> list of (insert_line, lines_to_insert)
    plan = []
    skipped_ann = 0
    for (typ, fn), rts in sorted(by_handler.items(), key=lambda kv: (kv[0][0] or '', kv[0][1])):
        fp = file_index.get((typ, fn))
        if not fp:
            continue
        src = open(fp).read()
        positions = parse_go_func_positions(src)
        if (typ, fn) not in positions:
            continue
        src_lines = src.split('\n')
        doc_start, decl_line = positions[(typ, fn)]
        if has_router_ann(src_lines, doc_start, decl_line):
            skipped_ann += 1
            continue
        method, path = rts[0]
        ann_path = param_to_brace(path)
        tag = tag_for(typ or fn)
        summary = f'[AUTO] {fn}'
        block = [
            f'// {summary}',
            f'// @Tags      {tag}',
            f'// {TOKEN}    {ann_path} [{method.lower()}]',
        ]
        edits.setdefault(fp, []).append((doc_start, block))
        plan.append(f'{fp}: {typ}.{fn} -> {method} {ann_path}')

    print(f'计划补注解 handler 数: {len(plan)}（已有注解跳过 {skipped_ann}）')
    if args.dry_run:
        for p in plan[:20]:
            print(' ', p)
        if len(plan) > 20:
            print(f'  ... 共 {len(plan)}')
        return

    # 4. 写回（每文件按插入行倒序，避免行号漂移）
    changed = 0
    for fp, inserts in edits.items():
        # 写回目标过闸：realpath+commonpath 必须留在 user-server 根内。
        fp = safe_resolve(fp, REPO)
        src = open(fp).read()
        lines = src.split('\n')
        for insert_line, block in sorted(inserts, key=lambda x: -x[0]):
            # 若函数上一行不是注释结尾且不是空行，补一个空行分隔
            pre = lines[insert_line - 1] if insert_line > 0 else ''
            new = []
            if pre and not pre.startswith('//') and pre.strip() != '':
                new.append('//')
            new.extend(block)
            lines[insert_line:insert_line] = new
        with open(fp, 'w') as fh:
            fh.write('\n'.join(lines))
        changed += 1
    print(f'写回文件数: {changed}')


if __name__ == '__main__':
    main()
