#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
检出「定义了 GORM 模型但未注册进 AutoMigrate」的情况。

背景（审计 DB-07 / 2026-09-16）：
  user-server 的 schema 完全由 GORM AutoMigrate 驱动（internal/pkg/db/migrate.go
  的 allModels() + 运行期 RegisterExtraModels）。**没进这张清单的模型，其数据表
  在全新部署上根本不会被创建** —— 而写入它的代码若又把 error 丢掉（`_ = ...`），
  整条链路会静默失效：功能永久为 0 且无任何报错。

  本脚本的原型发现：29 个 geo 模型里 `GeoCrawlerVisit` 是唯一没注册的，
  而 internal/router/router.go 正有一条 fire-and-forget 的写入路径，
  且错误被 `_ =` 丢弃。

判定口径（刻意保守，宁可多报不可漏报）：
  一个 struct 被视为"模型候选"，当且仅当它的字段里带 `gorm:"..."` tag
  **或** 它声明了 TableName() 方法。
  —— 只带 json tag 的 DTO/请求体不会被误报。

已知会命中但**不是**缺陷的情况，请写进 ALLOWLIST 并注明理由。

用法：
  python3 scripts/check_model_migration.py           # 报告（有未注册项则退出 1）
  python3 scripts/check_model_migration.py --all     # 连已注册的也一并列出

退出码：0 = 无未注册模型；1 = 存在未注册模型；2 = 无法定位 migrate.go
"""

import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
USER_SERVER = os.path.join(ROOT, 'user-server')
MIGRATE = os.path.join(USER_SERVER, 'internal', 'pkg', 'db', 'migrate.go')

# 命中但经核实并非缺陷的模型（写清理由，避免后人重复排查）
ALLOWLIST = {
    # 例：'SomeStruct': '视图/只读映射，由 XXX.sql 手工建',
}

SKIP_DIRS = {'vendor', 'node_modules', '.git', 'dist', 'build'}

TYPE_STRUCT = re.compile(r'^type\s+(\w+)\s+struct\s*\{', re.M)
TABLE_NAME = re.compile(r'func\s+\(\s*(?:\w+|\*?\w+)\s*\*?(\w+)\s*\)\s*TableName\s*\(')
REGISTERED_QUALIFIED = re.compile(r'&(\w+)\.(\w+)\{\}')
REGISTERED_BARE = re.compile(r'&(\w+)\{\}')


def iter_go_files(base):
    for dirpath, dirnames, filenames in os.walk(base):
        dirnames[:] = [d for d in dirnames if d not in SKIP_DIRS]
        for fn in sorted(filenames):
            if fn.endswith('.go'):
                yield os.path.join(dirpath, fn)


def struct_bodies(text):
    """用花括号配平切出每个 struct 的字段区，避免正则跨结构体误读。"""
    for m in TYPE_STRUCT.finditer(text):
        name = m.group(1)
        i = m.end() - 1          # 指向 '{'
        depth = 0
        for j in range(i, len(text)):
            if text[j] == '{':
                depth += 1
            elif text[j] == '}':
                depth -= 1
                if depth == 0:
                    yield name, text[i:j + 1]
                    break


def collect_models():
    """返回 {StructName: relpath}"""
    out = {}
    for path in iter_go_files(USER_SERVER):
        try:
            text = open(path, encoding='utf-8').read()
        except (OSError, UnicodeDecodeError):
            continue
        if 'gorm:"' not in text and 'TableName(' not in text:
            continue
        table_names = set(TABLE_NAME.findall(text))
        for name, body in struct_bodies(text):
            if 'gorm:"' not in body and name not in table_names:
                continue
            # 只把「像表」的结构体算作模型候选：必须有主键，或显式声明 TableName()。
            # 否则像 BounceByISPRow / CSATAggRow 这类**查询结果行**（带 gorm:"column:…"
            # 仅用于 Scan）会被大量误报 —— 它们从来不是表，理应不进 AutoMigrate。
            if 'primaryKey' not in body and name not in table_names:
                continue
            out.setdefault(name, os.path.relpath(path, ROOT))
    return out


def _names_in(text):
    out = set(m.group(2) for m in REGISTERED_QUALIFIED.finditer(text))
    out |= set(m.group(1) for m in REGISTERED_BARE.finditer(text))
    for m in re.finditer(r'RegisterExtraModels?\(\s*&(\w+)\.(\w+)\{\}', text):
        out.add(m.group(2))
    for m in re.finditer(r'RegisterExtraModels?\(\s*&(\w+)\{\}', text):
        out.add(m.group(1))
    return out


def collect_registered():
    """
    已注册模型 = 以下三处出现过的 &Xxx{}
      ① internal/pkg/db/migrate.go 的 allModels()（启动期 AutoMigrate 主体）
      ② internal/migration/migrations/*.go —— 独立的 Go 版本化迁移器，
         由 cmd/api/main.go 的 NewMigrationServiceDefault 在启动时执行
          （第一版脚本漏了这一处，导致 87 条几乎全是误报）
      ③ 各包 init() 里的 RegisterExtraModels(&Xxx{})
    """
    names = set()
    if os.path.isfile(MIGRATE):
        names |= _names_in(open(MIGRATE, encoding='utf-8').read())

    mig_dir = os.path.join(USER_SERVER, 'internal', 'migration', 'migrations')
    if os.path.isdir(mig_dir):
        for fn in sorted(os.listdir(mig_dir)):
            if fn.endswith('.go'):
                names |= _names_in(open(os.path.join(mig_dir, fn), encoding='utf-8').read())

    for path in iter_go_files(USER_SERVER):
        try:
            text = open(path, encoding='utf-8').read()
        except (OSError, UnicodeDecodeError):
            continue
        if 'RegisterExtraModels' in text:
            names |= _names_in(text)
    return names


def main():
    if not os.path.isfile(MIGRATE):
        print("❌ 找不到 %s" % MIGRATE)
        return 2

    models = collect_models()
    registered = collect_registered()
    missing = {k: v for k, v in models.items()
               if k not in registered and k not in ALLOWLIST}

    print("GORM 模型候选：%d    已注册 AutoMigrate：%d" % (len(models), len(registered)))

    if '--all' in sys.argv:
        print("\n已注册（前 10 个示例）：")
        for n in sorted(registered & set(models))[:10]:
            print("   ✅ %s" % n)
        print("   ... 共 %d 个" % len(registered & set(models)))

    if not missing:
        print("✅ 所有带 gorm tag / TableName 的模型均已注册 AutoMigrate")
        return 0

    print("\n❌ 以下 %d 个模型**未注册** AutoMigrate（全新部署不会建表）：" % len(missing))
    for name in sorted(missing):
        print("   %-28s %s" % (name, missing[name]))

    if ALLOWLIST:
        print("\n（已忽略 allowlist：%s）" % '、'.join(sorted(ALLOWLIST)))

    print("\n处理：")
    print("  ① 确属遗漏 → 加进 internal/pkg/db/migrate.go 的 allModels()")
    print("  ② 确属非表（视图/DTO/只读映射）→ 加进本脚本 ALLOWLIST 并注明理由")
    return 1


if __name__ == '__main__':
    sys.exit(main())
