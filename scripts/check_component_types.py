#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
校验 user-web/src/types/components.d.ts 里没有指向**已删除组件**的声明。

背景（审计 TOOL-06 / 2026-09-16）：
  components.d.ts 是 unplugin-vue-components 自动生成的。它只会在**新增**组件时
  追加声明，**不会**在组件被删除时移除对应行 —— 于是每次删组件都会留下一行
  `Xxx: typeof import('./../components/Xxx.vue')['default']` 指向不存在的文件。

  这类悬空声明不会让 vite dev 报错（dev 不做类型检查），但会让
  `vue-tsc` / 构建期的类型检查直接失败；而本仓库 CI 若未跑 vue-tsc，
  它就一直潜伏着 —— 属于典型的「写了但没人执行」的隐性断链。

  实测：删除 KbCitation.vue / PlaygroundAdvanced.vue 后，
  本脚本能立即报出 2 处悬空声明（已清理）。

用法：
  python3 scripts/check_component_types.py          # 退出码非 0 表示存在悬空声明
  python3 scripts/check_component_types.py --fix    # 直接删除悬空行（改前请确认）

退出码：
  0 = 无悬空声明；1 = 存在悬空声明；2 = 文件缺失/无法解析
"""

import os
import re
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：DTS/相对 import 不得越出仓库

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
DTS = os.path.join(ROOT, 'user-web', 'src', 'types', 'components.d.ts')

# 只关心相对路径（本地组件）；element-plus 等三方包路径不动
LOCAL_IMPORT = re.compile(r"^(\s*)(\w+):\s*typeof import\('(\./\.\./[^']+)'\)\['default'\]\s*$")


def main():
    if not os.path.isfile(DTS):
        print("❌ 找不到 %s" % DTS)
        return 2

    with open(DTS, encoding='utf-8') as f:
        lines = f.readlines()

    # DTS 本身与其解析出的相对 import 都过闸：realpath+commonpath 必须留在仓库内。
    dts_path = safe_resolve(DTS, ROOT, allow_absolute=True)
    base = os.path.dirname(dts_path)
    dangling = []   # (行号, 组件名, 相对路径)
    for i, line in enumerate(lines):
        m = LOCAL_IMPORT.match(line.rstrip('\n'))
        if not m:
            continue
        rel = m.group(3)
        # 组件声明里的 `./../x.vue` 是本仓合法的相对写法：realpath 后仅要求
        # commonpath 仍在仓库根内（拒绝 ../ 逃到仓外与符号链接逃逸），不按字面拦 ..。
        full = os.path.realpath(os.path.join(base, rel))
        if os.path.commonpath([ROOT, full]) != ROOT:
            raise ValueError(f"组件声明路径越出仓库: {rel!r}")
        if not os.path.exists(full):
            dangling.append((i, m.group(2), rel))

    if not dangling:
        print("✅ components.d.ts 无悬空声明（共 %d 条本地组件声明全部命中文件）"
              % len([l for l in lines if LOCAL_IMPORT.match(l.rstrip('\n'))]))
        return 0

    print("❌ components.d.ts 有 %d 条声明指向已删除的组件：" % len(dangling))
    for i, name, rel in dangling:
        print("   行 %d: %s -> %s" % (i + 1, name, rel))

    if '--fix' in sys.argv:
        keep = [l for i, l in enumerate(lines) if i not in {d[0] for d in dangling}]
        with open(safe_resolve(DTS, ROOT, allow_absolute=True), 'w', encoding='utf-8') as f:
            f.writelines(keep)
        print("✅ 已删除 %d 行悬空声明（建议随后执行 npm run 类型生成以确认）" % len(dangling))
        return 0

    print("\n修复方式：")
    print("  ① 重新生成：npm run build 后由 unplugin-vue-components 重写（视项目脚本而定）")
    print("  ② 直接删行：python3 scripts/check_component_types.py --fix")
    return 1


if __name__ == '__main__':
    sys.exit(main())
