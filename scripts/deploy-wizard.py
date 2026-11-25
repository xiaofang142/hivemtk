#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""HiveMtk 部署向导（C1）。

用途：
  1) 交互模式（默认）：收集部署关键项（数据库/Redis/JWT/字段加密等密钥），
     对 .env-example 里仍是占位值（ChangeMe_* / your_*）的密钥自动替换为强随机值，
     写回项目根目录 .env。只改本脚本职责内的 .env，不触碰其它文件。
  2) --check 非交互只读自检（CI / 冒烟 / 巡检用）：
     只读探测，不写任何文件。退出码约定：
       0 = 无 FAIL（允许有 WARN；组件不可达、占位密钥、缺 .env、缺工具链都只是 WARN）
       1 = 存在 FAIL（硬配置错误：.env 存在但 JWT 密钥 / 字段加密密钥缺失或 <32 字符）
       2 = 自检本身无法执行（如 Python 版本过旧、项目根目录无法定位）

设计要点：
  - 纯 Python3 标准库（argparse/os/re/secrets/shutil/socket/subprocess/sys/urllib），
    不依赖 PyYAML，也不走 bash（规避 shellcheck / bash 3.2 / CJK 展开三类 shell 门槛）。
  - 端口与健康端点口径与 scripts/inference-host/_common.sh 的 wait_health 一致：
    组件健康 = TCP 通（DB/Redis）或 GET http://host:port/health 返回 2xx（LLM/Embedding/Rerank）。
  - 端口默认值单一源：user-server/internal/config/ports.go（8202/8203/8207/8208/8209）。
"""
from __future__ import annotations

import argparse
import os
import re
import secrets
import shutil
import socket
import sys
import urllib.error
import urllib.parse
import urllib.request

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from path_guard import safe_resolve  # noqa: E402  路径闸：.env 写入不得越出项目根
from url_guard import check_target_url  # noqa: E402  URL 闸：health 探测防 SSRF

# ---------------------------------------------------------------------------
# 基线常量（单一源：user-server/internal/config/ports.go / .env-example）
# ---------------------------------------------------------------------------
SCRIPT_PATH = os.path.abspath(__file__)
PROJECT_ROOT = os.path.dirname(os.path.dirname(SCRIPT_PATH))
ENV_EXAMPLE = os.path.join(PROJECT_ROOT, ".env-example")
ENV_FILE = os.path.join(PROJECT_ROOT, ".env")

DB_HOST_DEFAULT = "127.0.0.1"
DB_PORT_DEFAULT = "8202"
REDIS_HOST_DEFAULT = "127.0.0.1"
REDIS_PORT_DEFAULT = "8203"
LLM_BASE_URL_DEFAULT = "http://127.0.0.1:8207/v1"
EMBEDDING_BASE_URL_DEFAULT = "http://127.0.0.1:8208/v1"
RERANK_BASE_URL_DEFAULT = "http://127.0.0.1:8209"
HTTP_TIMEOUT = 3

# 必改密钥（占位或缺失 ⇒ 交互模式自动生成强随机值；--check 只读校验长度）。
# JWT 的读取优先级（internal/pkg/utils/jwt.go）：USER_JWT_SECRET > JWT_SECRET，
# 两者皆无或皆短于 32 字符时 user-server 启动 panic——所以自检按"有效 JWT 密钥"整体判定。
CRITICAL_KEYS = ("POSTGRES_PASSWORD", "REDIS_PASSWORD", "JWT_SECRET", "FIELD_ENCRYPTION_KEY")

# 可选密钥：交互时询问，不强制。
OPTIONAL_KEYS = ("MASTER_KEY", "SEED_PASSWORD")
PLATFORM_KEYS = ("MERCHANT_API_SECRET", "PLATFORM_ADMIN_PASSWORD")

MIN_SECRET_LEN = 32

# 占位值判定：.env-example 里的 ChangeMe_* 与 your_* 都是"没改过的占位"。
PLACEHOLDER_RE = re.compile(r"(changeme_|your_)", re.IGNORECASE)


def log(msg: str = "", level: str = "info") -> None:
    """统一输出。level 决定前缀，便于 CI 按 [PASS]/[WARN]/[FAIL] 抓取。"""
    if level == "pass":
        print("[PASS] " + msg)
    elif level == "warn":
        print("[WARN] " + msg)
    elif level == "fail":
        print("[FAIL] " + msg)
    else:
        print(msg)


# ---------------------------------------------------------------------------
# .env 读写（保留注释与既有键，只改目标键值）
# ---------------------------------------------------------------------------
def load_dotenv(path: str) -> dict[str, str]:
    """读 .env 风格文件为 dict（取首个 = 切分；忽略注释与空行；不做 shell 展开）。"""
    values: dict[str, str] = {}
    try:
        with open(path, "r", encoding="utf-8") as f:
            for line in f:
                line = line.strip()
                if not line or line.startswith("#") or "=" not in line:
                    continue
                key, _, val = line.partition("=")
                key = key.strip()
                val = val.strip()
                if val and val[0] in "\"'" and val[-1] == val[0]:
                    val = val[1:-1]
                values[key] = val
    except FileNotFoundError:
        pass
    return values


def update_dotenv(path: str, updates: dict[str, str]) -> None:
    """把 updates 写回 .env：已存在的行只改值，不存在的键追加到文件尾。

    注释与其它键逐字节保留（按行处理）。写入用临时文件 + os.replace 保证原子性。
    """
    updated_keys = set(updates)
    out_lines: list[str] = []
    try:
        with open(path, "r", encoding="utf-8") as f:
            lines = f.read().splitlines()
    except FileNotFoundError:
        lines = []

    for line in lines:
        stripped = line.strip()
        if stripped and not stripped.startswith("#") and "=" in stripped:
            key = stripped.split("=", 1)[0].strip()
            if key in updated_keys:
                line = f"{key}={updates[key]}"
                updated_keys.discard(key)
        out_lines.append(line)

    for key in updated_keys:  # 剩余：原文件里没有的键，追加
        out_lines.append(f"{key}={updates[key]}")

    # .env 路径过闸：写回目标必须 realpath 留在项目根内，拒绝 ../ / 符号链接逃逸。
    safe_path = safe_resolve(path, PROJECT_ROOT, allow_absolute=True)
    tmp = str(safe_path) + ".tmp"
    with open(tmp, "w", encoding="utf-8") as f:
        f.write("\n".join(out_lines) + ("\n" if out_lines else ""))
    os.replace(tmp, safe_path)


def strong_secret() -> str:
    """强随机值（64 hex 字符，远大于 32 字符下限）。"""
    return secrets.token_hex(32)


def is_placeholder(value: str) -> bool:
    return not value or bool(PLACEHOLDER_RE.search(value))


def mask(value: str) -> str:
    if not value:
        return "(空)"
    return f"{value[:4]}…(len={len(value)})"


# ---------------------------------------------------------------------------
# 交互模式
# ---------------------------------------------------------------------------
def prompt_yes_no(question: str, default: bool) -> bool:
    suffix = " [Y/n] " if default else " [y/N] "
    try:
        ans = input(question + suffix).strip().lower()
    except (EOFError, KeyboardInterrupt):
        print()
        return default
    if not ans:
        return default
    return ans in ("y", "yes", "1", "true")


def collect_value(key: str, current: str, force: bool) -> str:
    """收集单个密钥。force=True 表示占位/缺失，默认动作是自动生成。"""
    status = mask(current)
    if force:
        note = f"当前为占位值 {status}，回车自动生成强随机值（64 hex）"
    else:
        note = f"当前已设置 {status}，回车保持不变"
    try:
        ans = input(f"  {key}\n    {note}\n    输入新值或直接回车: ").strip()
    except (EOFError, KeyboardInterrupt):
        print()
        ans = ""
    if ans:
        return ans
    if force:
        return strong_secret()
    return current


def run_interactive(assume_yes: bool) -> int:
    """交互收集关键项并写回 .env。"""
    print("HiveMtk 部署向导（scripts/deploy-wizard.py）")
    print("=" * 60)
    print(f"项目根目录: {PROJECT_ROOT}")
    print(f"模板文件  : {ENV_EXAMPLE}")

    if not os.path.isfile(ENV_EXAMPLE):
        log("找不到 .env-example 模板，无法生成 .env", "fail")
        return 2

    # 目标 .env：不存在则从模板复制一份（保留全部注释与默认值）。
    if not os.path.isfile(ENV_FILE):
        shutil.copyfile(ENV_EXAMPLE, ENV_FILE)
        print(f"已从 .env-example 复制生成 .env（{ENV_FILE}）")
    current = load_dotenv(ENV_FILE)
    updates: dict[str, str] = {}

    print("\n① 必改密钥（生产必须强随机，占位值将自动替换）")
    for key in CRITICAL_KEYS:
        cur = current.get(key, "")
        if assume_yes:
            updates[key] = cur if not is_placeholder(cur) else strong_secret()
        else:
            updates[key] = collect_value(key, cur, force=is_placeholder(cur))

    print("\n② 可选密钥")
    for key in OPTIONAL_KEYS:
        cur = current.get(key, "")
        if key == "MASTER_KEY" and is_placeholder(cur):
            # 生产环境 MASTER_KEY 缺失会被拒绝启动，向导默认补齐。
            want = True if assume_yes else prompt_yes_no(
                f"  生成强随机 {key}？（生产环境缺失会拒绝启动）", True)
            updates[key] = strong_secret() if want else cur
        elif key == "SEED_PASSWORD" and is_placeholder(cur):
            want = False if assume_yes else prompt_yes_no(
                f"  设置 {key}？（不设则 seed 命令使用公开默认值 Seed@123456）", False)
            updates[key] = strong_secret() if want else cur
        else:
            updates[key] = cur

    # 平台集成：可选本地组件，只有开启时才需要下面两把密钥。
    cur_platform = current.get("PLATFORM_ENABLED", "false").strip().lower()
    platform_on = cur_platform in ("true", "1", "yes", "on")
    if assume_yes:
        want_platform = platform_on
    else:
        want_platform = prompt_yes_no(
            f"  启用平台集成 PLATFORM_ENABLED？(当前={cur_platform})", platform_on)
    updates["PLATFORM_ENABLED"] = "true" if want_platform else "false"
    if want_platform:
        print("  平台集成已开启，补齐平台侧密钥：")
        for key in PLATFORM_KEYS:
            cur = current.get(key, "")
            if assume_yes:
                updates[key] = cur if not is_placeholder(cur) else strong_secret()
            else:
                updates[key] = collect_value(key, cur, force=is_placeholder(cur))

    update_dotenv(ENV_FILE, updates)

    print("\n" + "=" * 60)
    print("已写入 .env：")
    for key in sorted(updates):
        print(f"  {key} = {mask(updates[key])}")
    print("\n后续步骤（见 docs/DEPLOYMENT_GUIDE.md）：")
    print("  make db-up && make inference-host-up && make user-build")
    print("  复核自检：python3 scripts/deploy-wizard.py --check（期望 rc=0）")
    return 0


# ---------------------------------------------------------------------------
# --check 只读自检
# ---------------------------------------------------------------------------
def _tcp_probe(host: str, port: int, timeout: float = 2.0) -> bool:
    try:
        with socket.create_connection((host, port), timeout=timeout):
            return True
    except OSError:
        return False


def _http_health(base_url: str) -> bool:
    """GET 服务根路径 /health，2xx 视为健康。

    与 Go 侧 httpInferenceProbe 同口径：base_url 带 /v1 前缀时也钉根路径 /health。
    """
    try:
        u = urllib.parse.urlparse(base_url.strip())
    except ValueError:
        return False
    if u.scheme not in ("http", "https") or not u.netloc:
        return False
    health = f"{u.scheme}://{u.netloc}/health"
    try:
        # health URL 过白名单闸：默认仅回环，禁 userinfo/私网/保留/链路本地。
        check_target_url(health)
    except ValueError:
        return False
    try:
        with urllib.request.urlopen(health, timeout=HTTP_TIMEOUT) as resp:
            return 200 <= resp.status < 300
    except (urllib.error.URLError, urllib.error.HTTPError, OSError):
        return False


def _effective(key: str, dotenv: dict[str, str]) -> str:
    """环境变量优先于 .env（进程启动前 source .env 注入，等价于 env 覆盖）。"""
    if key in os.environ and os.environ.get(key, "") != "":
        return os.environ[key]
    return dotenv.get(key, "")


def run_check() -> int:
    """只读自检。返回 0/1/2。"""
    fails = 0
    warns = 0

    log("HiveMtk 部署自检（--check 只读模式，不写任何文件）")
    log(f"项目根目录: {PROJECT_ROOT}")

    # 0) 自检可行性
    if sys.version_info < (3, 7):
        log("Python < 3.7，自检无法执行", "fail")
        return 2
    if not os.path.isdir(PROJECT_ROOT):
        log("项目根目录无法定位，自检无法执行", "fail")
        return 2
    log(f"Python {sys.version.split()[0]} 可用", "pass")

    # 1) 工具链 / 模板
    if os.path.isfile(ENV_EXAMPLE):
        log(".env-example 模板存在", "pass")
    else:
        log("找不到 .env-example（make install 的模板缺失）", "warn")
        warns += 1

    # 2) .env 与密钥
    dotenv = load_dotenv(ENV_FILE)
    env_exists = os.path.isfile(ENV_FILE)
    if env_exists:
        log(f".env 存在（{ENV_FILE}）", "pass")
    else:
        log("未找到 .env：按纯环境变量注入模式检查（K8s Secret / Vault 场景属正常）", "warn")
        warns += 1

    # JWT：USER_JWT_SECRET 优先，回落到 JWT_SECRET。
    jwt_user = _effective("USER_JWT_SECRET", dotenv)
    jwt_legacy = _effective("JWT_SECRET", dotenv)
    jwt = jwt_user or jwt_legacy
    if env_exists and not jwt:
        log("JWT 密钥缺失：USER_JWT_SECRET 与 JWT_SECRET 均未设置（user-server 启动会 panic）", "fail")
        fails += 1
    elif jwt and len(jwt) < MIN_SECRET_LEN:
        log(f"JWT 密钥长度 {len(jwt)} < {MIN_SECRET_LEN}（启动会 panic）", "fail")
        fails += 1
    elif jwt:
        if is_placeholder(jwt):
            log(f"JWT 密钥已设置但仍是占位值（{mask(jwt)}），建议换成强随机值", "warn")
            warns += 1
        else:
            log(f"JWT 密钥长度合规（len={len(jwt)}）", "pass")
    else:
        log("JWT 密钥未设置（无 .env 且环境变量也未注入）", "warn")
        warns += 1

    field_key = _effective("FIELD_ENCRYPTION_KEY", dotenv)
    if env_exists and not field_key:
        log("FIELD_ENCRYPTION_KEY 未设置（加密落库会 fail-closed）", "fail")
        fails += 1
    elif field_key and len(field_key) < MIN_SECRET_LEN:
        log(f"FIELD_ENCRYPTION_KEY 长度 {len(field_key)} < {MIN_SECRET_LEN}", "fail")
        fails += 1
    elif field_key:
        if is_placeholder(field_key):
            log(f"FIELD_ENCRYPTION_KEY 仍为占位值（{mask(field_key)}），建议替换", "warn")
            warns += 1
        else:
            log(f"FIELD_ENCRYPTION_KEY 长度合规（len={len(field_key)}）", "pass")
    else:
        log("FIELD_ENCRYPTION_KEY 未设置（无 .env 且环境变量也未注入）", "warn")
        warns += 1

    # 3) 数据层 TCP 探测（不可达只 WARN，不挡 rc：可能组件尚未拉起）
    db_host = _effective("DB_HOST", dotenv) or DB_HOST_DEFAULT
    db_port = int(_effective("DB_PORT", dotenv) or DB_PORT_DEFAULT)
    if _tcp_probe(db_host, db_port):
        log(f"PostgreSQL 可达（{db_host}:{db_port}）", "pass")
    else:
        log(f"PostgreSQL 不可达（{db_host}:{db_port}）——未启动属正常，不影响配置正确性", "warn")
        warns += 1

    redis_host = _effective("REDIS_HOST", dotenv) or REDIS_HOST_DEFAULT
    redis_port = int(_effective("REDIS_PORT", dotenv) or REDIS_PORT_DEFAULT)
    if _tcp_probe(redis_host, redis_port):
        log(f"Redis 可达（{redis_host}:{redis_port}）", "pass")
    else:
        log(f"Redis 不可达（{redis_host}:{redis_port}）——未启动属正常", "warn")
        warns += 1

    # 4) 推理组件 /health 探测（不可达只 WARN；本地 LLM 未就绪正好演示降级档场景）
    llm_base = _effective("LLM_BASE_URL", dotenv) or LLM_BASE_URL_DEFAULT
    if _http_health(llm_base):
        log(f"本地 LLM 健康（{llm_base} → /health 2xx）", "pass")
    else:
        log(f"本地 LLM 未就绪（{llm_base}）——启动后 user-server 会定档并打日志 [inference-tier]", "warn")
        warns += 1

    emb_base = _effective("EMBEDDING_BASE_URL", dotenv) or EMBEDDING_BASE_URL_DEFAULT
    if _http_health(emb_base):
        log(f"Embedding 健康（{emb_base} → /health 2xx）", "pass")
    else:
        log(f"Embedding 未就绪（{emb_base}）", "warn")
        warns += 1

    rerank_base = _effective("RERANK_BASE_URL", dotenv) or RERANK_BASE_URL_DEFAULT
    if _http_health(rerank_base):
        log(f"Rerank 健康（{rerank_base} → /health 2xx）", "pass")
    else:
        log(f"Rerank 未就绪（{rerank_base}）", "warn")
        warns += 1

    # 5) 汇总
    print()
    log(f"自检完成：FAIL={fails}，WARN={warns}", "fail" if fails else ("warn" if warns else "pass"))
    if fails:
        log("存在硬配置错误（密钥缺失/过短），修复后重跑本命令。退出码 1", "fail")
        return 1
    if warns:
        log("配置无硬错误；WARN 项多为组件未启动或占位密钥，不影响 rc。退出码 0", "warn")
    else:
        log("全部检查通过。退出码 0", "pass")
    return 0


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(
        prog="deploy-wizard.py",
        description="HiveMtk 部署向导：交互生成 .env；--check 只读自检（CI 用）。",
    )
    parser.add_argument(
        "--check", action="store_true",
        help="非交互只读自检，退出码 0/1/2（无 FAIL=0，有 FAIL=1，无法执行=2）",
    )
    parser.add_argument(
        "--yes", action="store_true",
        help="非交互生成：占位密钥自动强随机替换，其余保持现状（CI 初始化用）",
    )
    args = parser.parse_args(argv)

    if args.check:
        return run_check()
    return run_interactive(assume_yes=args.yes)


if __name__ == "__main__":
    sys.exit(main())
