#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""五层架构分层静态门（Router→Controller→Service→Repository→Model，禁跨层）。

为什么需要这个门：
  AGENTS.md §2 / AI-EDITOR-CONTRACT.md C1 把「禁跨层」定为铁律，但长期只有
  「有人评审时看过」这一层保障——恒绿的规范与没人执行的规范长得一样
  （.github/workflows/seam-guard.yml 头注释记的是同一件事在异步全局上的另一例）。
  本门把分层规则里**可静态判定**的部分（import 边界）落成 CI 闸门：
    - Controller 层禁 import repository / gorm（不直接碰 DB，2026-11-14 实测基线 0）；
    - Service 层禁 import controller / router / gin（不碰 HTTP 细节）；
    - Repository 层禁 import controller / service / router / gin（不碰 HTTP / 不反向依赖）；
    - Model 层禁 import controller / service / repository / router / gin；
    - Router 层禁 import repository / model（路由装配不下沉取数）。
  注释/字符串里提到别的层不算违规：只解析真正的 import 块与单行 import。
  「Service 不写 SQL」这类语义规则静态判定不可靠（gorm 事务句柄在 service 传阅是既成架构），
  仍留给评审，本门不越权。

存量例外走 scripts/layer-boundary.baseline（格式：`规则|文件相对路径`，# 开头为注释）：
门只拦**新增**违规；基线条目对应的违规消失时会打印「过期条目」提示，便于逐步清零。

用法：
  python3 scripts/check-layer-boundary.py               # 全树扫描（默认 user-server/internal）
  python3 scripts/check-layer-boundary.py --root <dir>  # 指定扫描根（测试用）
  python3 scripts/check-layer-boundary.py --selftest    # 牙齿验证：内置夹具，必须全过
"""

import argparse
import os
import re
import sys
import tempfile

# 各层禁止 import 的前缀。`internal/xxx` 会匹配模块前缀 hivemtk-user/internal/xxx 及其子包。
FORBIDDEN = {
    "controller": ("internal/repository", "gorm.io/"),
    "service": ("internal/controller", "internal/router", "github.com/gin-gonic/gin"),
    "repository": ("internal/controller", "internal/service", "internal/router",
                   "github.com/gin-gonic/gin"),
    "model": ("internal/controller", "internal/service", "internal/repository",
              "internal/router", "github.com/gin-gonic/gin"),
    "router": ("internal/repository", "internal/model"),
}
LAYERS = ("controller", "service", "repository", "model", "router")

# 单行 import 三形态都吃：import "p" / import _ "p" / import 别名 "p"（2026-11-14 牙齿测试实证
# 首版漏掉 `import _ "..."`，注入违规没红——恒绿的门与坏门一样危险，故有此条与对应夹具）。
_SINGLE_IMPORT_RE = re.compile(r'^[ \t]*import[ \t]+(?:(?:[_\w]+|\.)[ \t]+)?"([^"]+)"', re.M)
_BLOCK_IMPORT_RE = re.compile(r'^[ \t]*import[ \t]*\((.*?)^\)', re.S | re.M)
_QUOTED_RE = re.compile(r'"([^"]+)"')


def go_imports(src):
    """只取真正的 import 声明（块 + 单行），不碰注释/struct tag 里的引号。"""
    found = set(_SINGLE_IMPORT_RE.findall(src))
    for block in _BLOCK_IMPORT_RE.findall(src):
        found.update(_QUOTED_RE.findall(block))
    return found


def layer_of(rel_parts):
    """按路径部件判层；一个文件只属一层，优先取最深（更具体）的命中。"""
    hit = [i for i, p in enumerate(rel_parts) if p in LAYERS]
    return rel_parts[hit[-1]] if hit else None


def scan(root):
    """返回违规列表 [(layer, token, relpath, import_path)] 与扫描文件数。"""
    violations, scanned = [], 0
    for dirpath, _dirnames, filenames in os.walk(root):
        for fn in sorted(filenames):
            if not fn.endswith(".go") or fn.endswith("_test.go"):
                continue  # 测试文件合法地跨层装配夹具，不属生产分层
            path = os.path.join(dirpath, fn)
            rel = os.path.relpath(path, root)
            parts = rel.split(os.sep)
            layer = layer_of(parts[:-1] + [fn])  # 目录部件判层
            if layer is None:
                continue
            scanned += 1
            try:
                with open(path, encoding="utf-8", errors="replace") as fh:
                    src = fh.read()
            except OSError:
                continue
            for imp in go_imports(src):
                for tok in FORBIDDEN[layer]:
                    if tok in imp:
                        violations.append((layer, tok, rel.replace(os.sep, "/"), imp))
    return violations, scanned


def load_baseline(path):
    if not os.path.isfile(path):
        return set()
    entries = set()
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            entries.add(tuple(line.split("|", 1)))
    return entries


def run(root, baseline_path):
    violations, scanned = scan(root)
    baseline = load_baseline(baseline_path)
    here = set()  # 本次真实命中的 (layer, relpath)，用于识别过期基线条目
    fresh, pardoned = [], 0
    for layer, tok, rel, imp in violations:
        if (layer, rel) in baseline:
            pardoned += 1
        else:
            fresh.append((layer, tok, rel, imp))
        here.add((layer, rel))
    stale = sorted(e for e in baseline if e not in here)

    print(f"扫描 {scanned} 个非测试 .go（根：{root}）")
    print(f"跨层违规：命中 {len(violations)} 处；基线豁免 {pardoned}；新增 {len(fresh)}")
    if stale:
        print(f"⚠️ 基线过期条目（违规已消失，可清理 {len(stale)} 行）：")
        for layer, rel in stale:
            print(f"   {layer}|{rel}")
    if fresh:
        print("\n❌ 新增跨层 import（违反 AGENTS.md §2 / 契约 C1），先改设计再上库：")
        for layer, tok, rel, imp in fresh:
            print(f"   [{layer}] {rel}: import {imp}（禁 {tok}）")
        return 1
    print("✅ 无新增跨层 import")
    return 0


SELFTEST_CASES = [
    # (文件相对路径, 内容, 期望违规 (layer, token) 集合)
    ("controller/bad.go",
     'package controller\n\nimport (\n\t"hivemtk-user/internal/repository"\n\t"gorm.io/gorm"\n)\n',
     {("controller", "internal/repository"), ("controller", "gorm.io/")}),
    ("controller/clean.go",
     '// 注释里提到 internal/repository 与 gorm.io/gorm 不算违规\npackage controller\n\n'
     'import "hivemtk-user/internal/service"\n',
     set()),
    ("service/bad.go",
     'package service\n\nimport (\n\t"github.com/gin-gonic/gin"\n\t"hivemtk-user/internal/controller"\n)\n',
     {("service", "github.com/gin-gonic/gin"), ("service", "internal/controller")}),
    ("repository/bad.go",
     'package repository\n\nimport (\n\t"hivemtk-user/internal/service"\n\t"github.com/gin-gonic/gin"\n)\n',
     {("repository", "internal/service"), ("repository", "github.com/gin-gonic/gin")}),
    ("model/bad.go",
     'package model\n\nimport "github.com/gin-gonic/gin"\n',
     {("model", "github.com/gin-gonic/gin")}),
    ("model/ok_gorm.go",
     'package model\n\nimport "gorm.io/gorm"\n', set()),  # model 用 gorm 是本职
    ("router/bad.go",
     'package router\n\nimport "hivemtk-user/internal/model"\n',
     {("router", "internal/model")}),
    ("controller/alias_import.go",
     'package controller\n\nimport _ "hivemtk-user/internal/repository"\n',
     {("controller", "internal/repository")}),
    ("helper/ignored.go",
     'package helper\n\nimport "hivemtk-user/internal/repository"\n', set()),  # 非五层目录不管
    ("service/skip_test.go",
     'package service\n\nimport "github.com/gin-gonic/gin"\n', set()),  # 测试文件豁免
]


def selftest():
    failures = []
    with tempfile.TemporaryDirectory() as tmp:
        for rel, src, expected in SELFTEST_CASES:
            path = os.path.join(tmp, rel)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(src)
        got_all, scanned = scan(tmp)
        got = {(l, t) for l, t, _r, _i in got_all}
        want = set().union(*[e for _f, _s, e in SELFTEST_CASES])
        if got != want:
            failures.append(f"检测集不符：want={sorted(want)} got={sorted(got)}")
        if scanned != 8:  # 10 夹具 = 8 个属层的非测试 .go + 1 非层目录 + 1 _test.go 豁免
            failures.append(f"扫描数不符：want=8 got={scanned}")
        # 基线豁免与新增判定的牙齿：豁免一条后它必须从 fresh 里消失
        with tempfile.NamedTemporaryFile("w", suffix=".baseline", delete=False) as bf:
            bf.write("controller|controller/bad.go\n")  # 格式：规则|相对 root 的路径
            bpath = bf.name
        violations, _ = scan(tmp)
        baseline = load_baseline(bpath)
        fresh = [v for v in violations if (v[0], v[2]) not in baseline]
        if len(fresh) != len(violations) - 2:  # bad.go 的两条(controller)应被豁免
            failures.append(f"基线豁免失效：总 {len(violations)} 豁免后余 {len(fresh)}")
        os.unlink(bpath)
    if failures:
        print("❌ selftest 失败：")
        for f in failures:
            print(f"   {f}")
        return 2
    print(f"✅ selftest 通过：{len(SELFTEST_CASES)} 夹具、检测/豁免/注释豁免/测试豁免全对")
    return 0


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser(description="五层架构 import 分层门")
    ap.add_argument("--root", default=os.path.join(here, "..", "user-server", "internal"))
    ap.add_argument("--baseline", default=os.path.join(here, "layer-boundary.baseline"))
    ap.add_argument("--selftest", action="store_true", help="内置夹具牙齿验证")
    args = ap.parse_args()
    if args.selftest:
        sys.exit(selftest())
    sys.exit(run(os.path.abspath(args.root), args.baseline))


if __name__ == "__main__":
    main()
