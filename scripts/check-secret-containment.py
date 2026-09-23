#!/usr/bin/env python3
"""真值包含闸：不问"形状像不像口令"，直接问"这台机器上那几枚私有口令/密钥字节，有没有出现在要推的字节里"。

候选集 = hub `.env` 里键名命中 PASS|SECRET|TOKEN|KEY|DSN|CRED|SALT 且值长≥6 的取值
        ∪ 容器 `mtk-postgres` 的 env 里同类键的取值（同名同值并枚）
面 = 树内取证产物 *.log ／ 本笔提交全量差异 ／ 审计文档 ／ 探针脚本 ／ 整棵 tip 树（git grep）
一枚值若已在排除取证档后的已跟踪源码里出现（弱口令表、测试常量），单列为"公开常量"，不计入口令面。
反向自证：把候选里第一枚临时拼进一行假日志，出现数必须 +1，否则本闸没牙。
值本身任何形态都不打印：RAW 命中只印 键名／值长／各面计数。
候选取不到 ⇒ 退 2（ENV-BROKEN：没跑过，不是绿）。任一私有值出现 ⇒ 退 1。全清 ⇒ 退 0。
"""
import re
import subprocess
import sys
from pathlib import Path

SECRET_KEY_RE = re.compile(r"(PASS|SECRET|TOKEN|KEY|DSN|CRED|SALT)", re.I)
HUB_ENV = Path("/Users/xiaofang/Documents/www/go/hivemtk/hivemtk/.env")
LOG_GLOB = "docs/superpowers/specs/ledger/logs/**/*.log"
DOC = "docs/architecture/CHANNEL_INTEGRATION_AUDIT_2026-09.md"
PROBE = "scripts/cap-ab-53300-probe.py"


def env_values(path):
    out = []
    for line in path.read_text(errors="replace").splitlines():
        m = re.match(r"\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(.*)$", line)
        if not m or not SECRET_KEY_RE.search(m.group(1)):
            continue
        v = m.group(2).strip().strip('"').strip("'")
        if len(v) >= 6:
            out.append((m.group(1), v))
    return out


def container_values():
    try:
        raw = subprocess.run(["docker", "inspect", "mtk-postgres", "--format",
                              "{{range .Config.Env}}{{println .}}{{end}}"],
                             capture_output=True, text=True, timeout=30).stdout
    except Exception as e:
        print(f"[容器 env] 读取失败：{type(e).__name__}（本面缺席，候选只剩 .env）")
        return []
    out = []
    for line in raw.splitlines():
        if "=" not in line:
            continue
        k, v = line.split("=", 1)
        if SECRET_KEY_RE.search(k) and len(v) >= 6:
            out.append((k, v))
    return out


def count(haystacks, needle):
    return sum(h.count(needle) for h in haystacks)


def grep_count(args, value):
    r = subprocess.run(["git", "grep", "-c", "-F", "-e", value] + args,
                       capture_output=True, text=True)
    return sum(int(ln.rsplit(":", 1)[-1]) for ln in r.stdout.splitlines()
               if ln.rsplit(":", 1)[-1].isdigit())


def main() -> int:
    by_value = {}
    for k, v in env_values(HUB_ENV) + container_values():
        by_value.setdefault(v, k)
    # 一律按 (键名, 值) 迭代：键名可打印，值绝不入档、不入屏
    vals = [(k, v) for v, k in by_value.items()]
    print(f"候选真值枚数={len(vals)}（hub .env ∪ 容器 mtk-postgres env 的口令类值，按值并枚）")
    if not vals:
        print("⇒ ENV-BROKEN：一枚都没取到，本闸没有可比对象，不许判绿")
        return 2

    logs = [p.read_text(errors="replace") for p in sorted(Path(".").glob(LOG_GLOB))]
    diff = subprocess.run(["git", "show", "HEAD", "--no-color"],
                          capture_output=True, text=True, check=True).stdout
    doc = Path(DOC).read_text()
    probe = Path(PROBE).read_text()
    print(f"面：树内产物 {len(logs)} 份／提交差异 {len(diff)} 字节／文档＋探针 2 份／整棵 tip 树")

    pool = logs + [diff, doc, probe]
    base = count(pool, vals[0][1])
    planted = count(pool + [f"noise password={vals[0][1]} noise"], vals[0][1])
    print(f"反向自证：未种={base} 种入={planted}（须 planted=base+1）⇒ {'有牙' if planted == base + 1 else '失效'}")
    if planted != base + 1:
        return 1

    total = public_total = 0
    for k, v in vals:
        # 反向自证过的一类故障：值与键名在字典里对调之后，屏上印的就是值。
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", k):
            print(f"⇒ ABORT：待打印的身份字段不是 env 键名形状（长度 {len(k)}），疑似键/值对调，禁止输出")
            return 2
        tracked = grep_count(["HEAD", "--", ":!docs/superpowers"], v)
        hits = {"树内产物": count(logs, v), "提交差异": diff.count(v), "文档": doc.count(v),
                "探针脚本": probe.count(v), "整棵tip树": grep_count(["HEAD"], v)}
        s = sum(hits.values())
        if tracked > 0:
            public_total += s
            print(f"  公开常量 >> 键 {k}（值长={len(v)}，已跟踪源码 {tracked} 处命中）：{hits}")
            continue
        total += s
        if s:
            print(f"  命中 >> 键 {k}（值长={len(v)}，已跟踪源码 0 处）：{hits}")
    print(f"判定：{len(vals)} 枚候选 × 5 个面 ⇒ 私有真值出现合计={total}（须 0）；公开常量出现合计={public_total}（另判）")
    print(f"⇒ {'绿：无一枚非公开口令字节入档' if total == 0 else '红：立即摘字节，禁止推送'}")
    return 0 if total == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
