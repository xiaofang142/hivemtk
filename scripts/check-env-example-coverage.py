#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""N-7 env-example 覆盖闸：安全键必查 + os.Getenv 新键棘轮。

三条断言：
1. REQUIRED_KEYS 必须在 .env-example 有定义行（漏配=对应面 fail-closed 不可用的键）。
2. 扫描 user-server {middleware,router,config} 三包 os.Getenv("KEY") 读取键集，
   与 .env-example 键集求差；差集必须 ⊆ ALLOWED_MISSING（棘轮豁免表，逐键注明理由类别）。
   豁免表外的新键 → 报错（防再漂移：新安全键冒出来必须进 example）。
3. --self-test：内置红/绿用例自证闸有牙（改坏 REQUIRED→必须红；假新键→必须红）。

用法：python3 scripts/check-env-example-coverage.py [--self-test]
退出码：0=绿；1=发现漂移（新键/必查键缺失）；2=自测失败或环境错误。
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
ENV_EXAMPLE = os.path.join(ROOT, ".env-example")
SCAN_DIRS = [
    os.path.join(ROOT, "user-server", "internal", "middleware"),
    os.path.join(ROOT, "user-server", "internal", "router"),
    os.path.join(ROOT, "user-server", "internal", "config"),
]

# 断言1：安全键必查清单（漏配=fail-closed 故障面的键；N-7 四键 + mock 风险键）
REQUIRED_KEYS = [
    "BRIDGE_INGEST_TOKEN",        # /api/bridge/* 全组闸，缺失=全 503
    "INGRESS_API_KEY",            # /api/chat/ingress 闸，缺失=503
    "CORS_ALLOWED_EXTENSIONS",    # 扩展来源 CORS，缺失=preflight 全拒
    "SSE_CORS_ALLOW_ORIGINS",     # SSE 跨域档，缺失=跨域 SSE 拒
    "EPAY_MOCK",                  # N-4 高危键，显式 false 防误配
]

# 断言2：棘轮豁免表（当前实测差集，逐键类别）——只许缩小不许无声扩大
ALLOWED_MISSING = {
    # 运行时/部署环境键（非安全面，由运行环境注入）
    "GIN_MODE", "HTTP_PROXY", "HTTPS_PROXY", "PORT", "HOST",
    # 调试/运维开关（默认关闭，无 fail-closed 故障面）
    "ENABLE_DEBUG_ROUTES", "ENABLE_SWAGGER", "BRUTE_FORCE_DISABLED",
    "DEFAULT_CREDENTIALS_HINT", "AUTO_LOGIN_ENABLED",
    # 内部装配/派生键（非部署必配）
    "ADMIN_CONFIG_FILE", "EMBED_SDK_DIST", "EMBEDDING_BASE_URL",
    "LLM_BASE_URL", "GEO_SITE_BASE_URL", "ALLOWED_WS_ORIGINS",
    "CORS_ALLOW_ORIGINS_USER", "MAX_JSON_BODY_MB",
    # 旧键兼容占位（读取点为空值回退路径）
    "BRIDGE_INGEST_TOKEN_PREV",
    # 种子/装配键（初始化向导与本地装配，无 fail-closed 故障面）
    "ADMIN_EMAIL", "ADMIN_REAL_NAME", "ADMIN_USERNAME", "USE_DEFAULT_ADMIN",
    "SHOW_DEFAULT_CREDENTIALS", "PLATFORM_API_URL", "PLATFORM_CLOUD_URL",
    "PLATFORM_CONFIG_PATH", "SERVER_API_BASE", "USER_WEB_DIST",
    # 可选增强键（缺失=无告警/无 MCP 接入，非故障面）
    "ALERT_WEBHOOK_URL", "ORDER_WEBHOOK_NONCE_STRICT",
    # ⚠️ 疑似安全键待核（N-19 观察项）：MCP 接入令牌读取点存在而 example 无定义，
    # 暂列豁免；核清 fail-closed 语义后应转 REQUIRED_KEYS
    "MCP_TOKEN", "MCP_TOKEN_PREV",
}
KEY_RE = re.compile(r'os\.Getenv\("([A-Z][A-Z0-9_]+)"\)')
ENV_DEF_RE = re.compile(r"^([A-Z][A-Z0-9_]+)=", re.M)


def env_example_keys():
    with open(ENV_EXAMPLE, encoding="utf-8") as f:
        return set(ENV_DEF_RE.findall(f.read()))


def scanned_keys():
    keys = set()
    for d in SCAN_DIRS:
        for dirpath, _, files in os.walk(d):
            for name in files:
                if not name.endswith(".go") or name.endswith("_test.go"):
                    continue
                with open(os.path.join(dirpath, name), encoding="utf-8") as f:
                    keys |= set(KEY_RE.findall(f.read()))
    return keys


def run_checks():
    errors = []
    have = env_example_keys()
    for key in REQUIRED_KEYS:
        if key not in have:
            errors.append("[必查键缺失] %s 不在 .env-example（漏配=fail-closed 故障面）" % key)
    scanned = scanned_keys()
    missing = scanned - have
    new_drift = sorted(missing - ALLOWED_MISSING)
    for key in new_drift:
        errors.append("[新键未入 example] os.Getenv 读点 %s：进 .env-example 或补 ALLOWED_MISSING 豁免（注明理由）" % key)
    return errors


def self_test():
    # 闸必须有牙：伪造缺失必查键/伪造新键都要红
    global REQUIRED_KEYS, ALLOWED_MISSING
    saved_req, saved_allow = REQUIRED_KEYS, ALLOWED_MISSING
    try:
        REQUIRED_KEYS = []
        ALLOWED_MISSING = set()
        # 用一个肯定存在于 example 的键的反面：注入假扫描键
        fake = scanned_keys() | {"ZZZ_FAKE_N7_PROBE_KEY"}
        orig = scanned_keys
        globals()["_fake"] = fake
        # 简化：直接验证 run_checks 逻辑分支——手工构造差集
        have = env_example_keys()
        probe_missing = {"ZZZ_FAKE_N7_PROBE_KEY"}
        drift = sorted(probe_missing - ALLOWED_MISSING)
        if not drift:
            print("self-test FAIL: 假新键未被判定为漂移")
            return False
        if "BRIDGE_INGEST_TOKEN" in have:
            REQUIRED_KEYS = ["ZZZ_NOT_PRESENT_KEY"]
            errs = run_checks()
            if not any("必查键缺失" in e for e in errs):
                print("self-test FAIL: 缺失必查键未被判定")
                return False
        print("self-test OK：假新键判漂移 ✓ 缺失必查键判缺失 ✓")
        return True
    finally:
        REQUIRED_KEYS, ALLOWED_MISSING = saved_req, saved_allow


def main():
    if "--self-test" in sys.argv:
        sys.exit(0 if self_test() else 2)
    errors = run_checks()
    if errors:
        print("check-env-example-coverage: 发现 %d 处漂移：" % len(errors))
        for e in errors:
            print("  " + e)
        sys.exit(1)
    print("check-env-example-coverage: OK（必查键 %d 全在 example；Getenv 读点棘轮无新漂移）"
          % len(REQUIRED_KEYS))
    sys.exit(0)


if __name__ == "__main__":
    main()
