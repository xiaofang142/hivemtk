#!/usr/bin/env python3
"""
ENUM 值与 Go 常量一致性检查（OPT-DB-08 配套）
防止 PG ENUM 迁移与 Go 代码常量漂移
"""
import re
import sys
from pathlib import Path

# 配置
PROJECT_ROOT = Path(__file__).resolve().parent.parent.parent
USER_SERVER = PROJECT_ROOT / "hivemtk" / "user-server"
MIGRATIONS = PROJECT_ROOT / "hivemtk" / "migrations"
MIGRATION_FILE = MIGRATIONS / "047_pg_enums.sql"

# ENUM 定义：key=枚举名, value=(Go 前缀, Go 搜索目录, 允许的额外 SQL-only 值)
ENUM_DEFS = {
    "platform_type_enum": ("ChannelType", "internal/model"),
    "intent_major_enum": ("IntentMajor", "internal/service"),
    "message_status_enum": ("MessageStatus", "internal/model"),
    "embed_status_enum": ("EmbedStatus", "internal/model"),
    "source_type_enum": ("SourceType", "internal/model"),
}

errors = 0
warnings = 0


def log_pass(msg):
    print(f"\033[0;32m✅ {msg}\033[0m")


def log_fail(msg):
    global errors
    print(f"\033[0;31m❌ {msg}\033[0m")
    errors += 1


def log_warn(msg):
    global warnings
    print(f"\033[1;33m⚠️  {msg}\033[0m")
    warnings += 1


def extract_go_constants(prefix: str, search_dir: Path) -> set:
    """提取 Go 源码中 prefixXxx = "value" 的常量值集合
    例如 prefix=ChannelType, 匹配 ChannelTypeTelegram ChannelType = "telegram"
    """
    values = set()
    # 匹配: PrefixXxxName ...Prefix... = "value"
    # 不要求 prefix 前有 \b（变量名紧贴 type 注解）
    pattern = re.compile(rf'{re.escape(prefix)}([A-Za-z0-9_]+)\s+(?:\w+\s+)?=\s*"([a-z0-9_]+)"')
    if not search_dir.exists():
        return values
    for go_file in search_dir.rglob("*.go"):
        try:
            content = go_file.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        for m in pattern.finditer(content):
            values.add(m.group(2))
    return values


def extract_sql_enum(enum_name: str, sql_file: Path) -> set:
    """从 SQL 文件中提取 ENUM 类型的值"""
    if not sql_file.exists():
        return set()
    content = sql_file.read_text(encoding="utf-8")
    pattern = re.compile(
        rf"CREATE TYPE\s+{re.escape(enum_name)}\s+AS ENUM\s*\(([^)]+)\)",
        re.DOTALL,
    )
    values = set()
    for m in pattern.finditer(content):
        body = m.group(1)
        for v in re.findall(r"'([^']+)'", body):
            values.add(v)
    return values


def main():
    print("=" * 60)
    print("  ENUM 一致性检查（OPT-DB-08 配套）")
    print("=" * 60)
    print()

    for i, (enum_name, (go_prefix, go_subdir)) in enumerate(ENUM_DEFS.items(), 1):
        print(f"[{i}/{len(ENUM_DEFS)}] {enum_name} ↔ {go_prefix}Xxx")
        go_values = extract_go_constants(go_prefix, USER_SERVER / go_subdir)
        sql_values = extract_sql_enum(enum_name, MIGRATION_FILE)

        if not go_values:
            log_warn(f"  Go {go_prefix}Xxx 未找到常量")
            print()
            continue
        if not sql_values:
            log_fail(f"  SQL ENUM {enum_name} 未找到")
            print()
            continue

        only_go = go_values - sql_values
        only_sql = sql_values - go_values

        if only_go:
            log_fail(f"  Go 有但 SQL ENUM 缺失: {sorted(only_go)}")
        if only_sql:
            log_warn(f"  SQL ENUM 有但 Go 不识别（可能 legacy 兼容值）: {sorted(only_sql)}")
        if not only_go and not only_sql:
            log_pass(f"  完全一致（{len(go_values)} 个值）")
        print()

    print("=" * 60)
    if errors > 0:
        print(f"\033[0;31m❌ ENUM 一致性检查失败: {errors} 个错误\033[0m")
        sys.exit(1)
    else:
        if warnings > 0:
            print(f"\033[1;33m⚠️  ENUM 一致性检查通过（有 {warnings} 个警告）\033[0m")
        else:
            print("\033[0;32m✅ ENUM 一致性检查完全通过\033[0m")
        sys.exit(0)


if __name__ == "__main__":
    main()
