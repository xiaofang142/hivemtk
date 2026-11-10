#!/usr/bin/env python3
"""服务面禁 stdout/stdlib-log 直出门（AUD-07 防回潮；MA-07 扩面）。

口径：user-server/internal/ 下除 _test.go 与 internal/migration/** 外，
生产代码不得出现 fmt.Print* / log.Print* / log.Fatal* / log.Panic* /
os.Stdout.Write / os.Stderr.Write —— 可观测面必须走 zerolog
（绕过 logger = 丢租户上下文/脱敏链/级别采样，事故时不可对账）。

MA-07 扩面理由：AUD-07 只扫 fmt.Print* 是盲区——stdlib log.Print*（直写
stderr，无 trace_id/级别/脱敏链）与 os.Stdout.Write（git 报错回显直出）
同口径绕过结构化管道，一并纳入（本轮 31 处 log.Print*/1 处 os.Stdout.Write
已转 zerolog，含 startup fail-fast 护栏改 logger.Fatalf 同语义）。

排除项（均设计内，有论证）：
  - internal/migration/**：Migration 接口签名 Up(ctx) 无 logger 入参，
    进度输出走 stdout 是该框架既有通道（任务态落库另有 logger）。
  - cmd/** CLI 工具：人机交互输出（进度/结果表），非服务进程可观测面。
  - _test.go：测试输出自由。
  - 注释行：文档里的字面量提及不是代码面（本脚本注释自身也含字面量）。
"""
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1] / "user-server" / "internal"
EXCLUDE_PARTS = {"migration"}

PATTERNS = ("fmt.Print", "log.Print", "log.Fatal", "log.Panic",
            "os.Stdout.Write", "os.Stderr.Write")

hits = []
scanned = 0
for p in ROOT.rglob("*.go"):
    if p.name.endswith("_test.go"):
        continue
    if EXCLUDE_PARTS.intersection(p.parts):
        continue
    scanned += 1
    for i, line in enumerate(p.read_text(encoding="utf-8").splitlines(), 1):
        # 跳过注释行（注释里的字面量提及不是代码面）
        if line.lstrip().startswith("//"):
            continue
        for pat in PATTERNS:
            if pat in line:
                hits.append(f"{p.relative_to(ROOT)}:{i}: [{pat}] {line.strip()}")

if "--self-test" in sys.argv:
    # 探针：注入一个必红的假想命中，验证门真扫（TEETH）。
    hits.append("PROBE: fmt.Println(\"injected\")")

if hits:
    print(f"check-stdout-print: {len(hits)} 处 stdout/stdlib-log 直出（应改 logger.*）：", file=sys.stderr)
    print("\n".join(hits), file=sys.stderr)
    sys.exit(1)
print(f"check-stdout-print: 0 hit（扫描 {scanned} 文件；migration/cmd/_test/注释按口径排除；"
      f"模式 fmt.Print*/log.Print*/log.Fatal*/log.Panic*/os.Stdout|Stderr.Write）")
