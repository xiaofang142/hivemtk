#!/usr/bin/env python3
"""服务面禁 stdout 直出门（AUD-07 防回潮）。

口径：user-server/internal/ 下除 _test.go 与 internal/migration/** 外，
生产代码不得出现 fmt.Print/Printf/Println —— 可观测面必须走 zerolog
（绕过 logger = 丢租户上下文/脱敏链/级别采样，事故时不可对账）。

排除项（均设计内，有论证）：
  - internal/migration/**：Migration 接口签名 Up(ctx) 无 logger 入参，
    进度输出走 stdout 是该框架既有通道（任务态落库另有 logger）。
  - cmd/** CLI 工具：人机交互输出（进度/结果表），非服务进程可观测面。
  - _test.go：测试输出自由。
"""
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1] / "user-server" / "internal"
EXCLUDE_PARTS = {"migration"}

hits = []
scanned = 0
for p in ROOT.rglob("*.go"):
    if p.name.endswith("_test.go"):
        continue
    if EXCLUDE_PARTS.intersection(p.parts):
        continue
    scanned += 1
    for i, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
        if "fmt.Print" in line:
            hits.append(f"{p.relative_to(ROOT)}:{i}: {line.strip()}")

if hits:
    print(f"check-stdout-print: {len(hits)} 处 fmt.Print 直出（应改 logger.*）：", file=sys.stderr)
    print("\n".join(hits), file=sys.stderr)
    sys.exit(1)
print(f"check-stdout-print: 0 hit（扫描 {scanned} 文件；migration/cmd/_test 按口径排除）")
