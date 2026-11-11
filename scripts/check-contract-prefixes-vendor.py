#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""L-13 跨仓契约闸前缀对账：实现清单 vs vendor 快照。

背景：用户侧 contract_header.go contractGuardedPrefixes 与平台侧
check-merchant-scope.py CONTRACT_GUARDED 语义一一对应但需手工同步，漂移均静默。
本脚本双仓同文（cp 同步规程，同 check-md-links-offline.py）：按仓型自动选提取器
（找到 user-server/internal/platform/contract_header.go → Go 提取器；否则找
platform-server/scripts/check-merchant-scope.py → Python 提取器），归一化（strip
尾斜杠）后与 scripts/contract_guarded_prefixes.vendor.txt 集合比对，不等即红。

--self-test：注入假 vendor 红绿用例自证有牙。退出码：0 绿 / 1 漂移 / 2 环境错。
"""
import os
import re
import sys

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
VENDOR = os.path.join(ROOT, "scripts", "contract_guarded_prefixes.vendor.txt")
GO_IMPL = os.path.join(ROOT, "user-server", "internal", "platform", "contract_header.go")
PY_IMPL = os.path.join(ROOT, "platform-server", "scripts", "check-merchant-scope.py")

GO_RE = re.compile(r"contractGuardedPrefixes\s*=\s*\[\]string\{(.*?)\}", re.S)
PY_RE = re.compile(r"^CONTRACT_GUARDED\s*=\s*\[(.*?)\]", re.S | re.M)
LIT_RE = re.compile(r'"(/[A-Za-z0-9_\-./]+)"')


def norm(prefix):
    return prefix.rstrip("/")


def extract_impl():
    if os.path.isfile(GO_IMPL):
        src = open(GO_IMPL, encoding="utf-8").read()
        m = GO_RE.search(src)
        if not m:
            raise SystemExit("contract_header.go 里找不到 contractGuardedPrefixes 块")
        return [norm(x) for x in LIT_RE.findall(m.group(1))]
    if os.path.isfile(PY_IMPL):
        lines = []
        in_block = False
        for line in open(PY_IMPL, encoding="utf-8"):
            if line.startswith("CONTRACT_GUARDED"):
                in_block = True
                continue
            if in_block:
                if line.strip().startswith("]"):
                    break
                line = line.split("#", 1)[0]
                lines += LIT_RE.findall(line)
        return [norm(x) for x in lines]
    raise SystemExit("两仓实现文件都找不到（脚本须放在双仓 scripts/ 下）")


def read_vendor():
    out = []
    for line in open(VENDOR, encoding="utf-8"):
        line = line.split("#", 1)[0].strip()
        if line:
            out.append(norm(line))
    return out


def check():
    impl = extract_impl()
    vendor = read_vendor()
    if sorted(set(impl)) != sorted(set(vendor)):
        print("L-13 对账红：实现与 vendor 不一致（归一化后）")
        print("  实现: %s" % sorted(set(impl)))
        print("  vendor: %s" % sorted(set(vendor)))
        print("  规程：清单变更双仓同改（vendor 文件头注释）")
        return 1
    print("L-13 对账绿：实现 %d 前缀 == vendor %d 前缀（归一化集合相等）" % (len(set(impl)), len(set(vendor))))
    return 0


def self_test():
    global VENDOR
    saved = VENDOR
    try:
        impl_set = set(extract_impl())
        # 绿用例：vendor=实现
        VENDOR = _write_tmp(sorted(impl_set))
        if check() != 0:
            print("self-test FAIL: 一致 vendor 误报红")
            return False
        # 红用例：vendor 少一项
        VENDOR = _write_tmp(sorted(impl_set - {sorted(impl_set)[0]}))
        if check() != 1:
            print("self-test FAIL: 缺项 vendor 误报绿")
            return False
        print("self-test OK：一致绿 ✓ 缺项红 ✓")
        return True
    finally:
        VENDOR = saved


def _write_tmp(prefixes):
    import tempfile
    fd, path = tempfile.mkstemp(suffix=".txt")
    with os.fdopen(fd, "w", encoding="utf-8") as f:
        f.write("\n".join(prefixes) + "\n")
    return path


def main():
    if "--self-test" in sys.argv:
        sys.exit(0 if self_test() else 2)
    sys.exit(check())


if __name__ == "__main__":
    main()
