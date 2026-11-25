"""脚本侧出站 URL 闸（Mimosa SSRF 修复共用）。

判据：
  1. 只能是绝对 http/https URL，禁 userinfo、禁非 http(s) scheme；
  2. host 必须命中白名单（默认回环），精确匹配或同 IP 的不同字面写法；
  3. 域名只按精确白名单放行，不做 DNS 解析（防 DNS rebinding）；
  4. IP 字面量若非回环，无论私网/保留/链路本地/组播/未指定一律拒绝。

用法：
    from url_guard import check_target_url
    check_target_url(url)                       # 默认仅回环
    check_target_url(url, allowed_hosts={"pay.example.com"})  # 额外放行受控域名
"""

from __future__ import annotations

import ipaddress
import os
from urllib.parse import urlsplit

DEFAULT_ALLOWED_HOSTS = ("localhost", "127.0.0.1", "::1")
ALLOWED_HOSTS_ENV = "HIVEMTK_SCRIPT_ALLOWED_HOSTS"


def allowed_target_hosts(extra_env: str | None = None) -> set[str]:
    """白名单 = 默认回环 + 进程环境显式追加（逗号分隔）+ 调用方显式集合。"""
    raw = os.environ.get(ALLOWED_HOSTS_ENV, "") if extra_env is None else extra_env
    return set(DEFAULT_ALLOWED_HOSTS) | {
        h.strip().lower().rstrip(".") for h in raw.split(",") if h.strip()
    }


def _host_in_allowlist(hostname: str, allowed: set[str]) -> bool:
    hostname = hostname.lower().rstrip(".")
    if hostname in allowed:
        return True
    try:
        addr = ipaddress.ip_address(hostname)
    except ValueError:
        return False
    for candidate in allowed:
        try:
            if addr == ipaddress.ip_address(candidate):
                return True
        except ValueError:
            continue
    return False


def check_target_url(url: str, *, allowed_hosts: set[str] | None = None,
                     allow_nonloopback_ip: bool = False) -> str:
    """校验 URL 合法且 host 在白名单内，原样返回 url；违规抛 ValueError。"""
    parts = urlsplit(url)
    if parts.scheme not in ("http", "https") or not parts.netloc \
            or parts.username or parts.password or parts.fragment:
        raise ValueError(f"非法目标 URL（仅 http(s)、禁 userinfo/fragment）: {url!r}")
    hostname = parts.hostname
    allowed = allowed_hosts if allowed_hosts is not None else allowed_target_hosts()
    if not hostname or not _host_in_allowlist(hostname, allowed):
        raise ValueError(f"目标 host 不在白名单: {hostname!r}")
    if allow_nonloopback_ip:
        return url
    try:
        addr = ipaddress.ip_address(hostname.lower().rstrip("."))
    except ValueError:
        return url  # 域名靠精确白名单放行，不解析 DNS
    if not addr.is_loopback:
        raise ValueError(f"禁止请求非回环 IP（私网/保留/链路本地一律拒绝）: {hostname!r}")
    return url
