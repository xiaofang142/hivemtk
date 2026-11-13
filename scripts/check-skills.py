#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""skills/ 权威源规范校验器（agentskills.io 开放规范的仓库内落法）。

为什么需要这个门：
  skills/*/SKILL.md 是「部署副本的权威源」，靠 description 决定模型是否加载——
  name 写错（大写/超长）或 description 缺失/超限，部署后就是静默失效：
  skill 列表里看不到或永不触发，与「没有这个 skill」无异（恒绿的坏门同一类问题）。
  本门把 agentskills.io 规范的可判定约束落成 CI 闸门：
    - name：必填、与目录名一致、^[a-z0-9]+(-[a-z0-9]+)*$、≤64 字符；
    - description：必填、≤1024 字符（规范上限）、必须含「何时触发」语义（含触发词启发检查）；
    - 正文非空；文件以单个换行结尾（markdownlint MD047 同口径）。

用法：
  python3 scripts/check-skills.py               # 扫描 skills/（默认脚本同级上级目录）
  python3 scripts/check-skills.py --root <dir>  # 指定 skills 根（测试用）
  python3 scripts/check-skills.py --selftest    # 牙齿验证：内置夹具必须全过
"""

import argparse
import os
import re
import sys
import tempfile

NAME_RE = re.compile(r"^[a-z0-9]+(-[a-z0-9]+)*$")
DESC_LIMIT = 1024
NAME_LIMIT = 64
# 「何时触发」启发检查：description 至少命中一个触发语义词（中英皆可），否则视为只写了"是什么"
TRIGGER_HINTS = ("当用户", "使用", "invoke", "when", "trigger", "use ", "问", "要求",
                 "后", "调用", "任务", "之前", "之前必须")


def parse_frontmatter(src):
    """返回 (meta dict, body) 或 None（无 frontmatter）。不引第三方 YAML 依赖，只取顶层标量键。"""
    if not src.startswith("---"):
        return None
    parts = src.split("---", 2)
    if len(parts) < 3:
        return None
    meta, body = {}, parts[2]
    for line in parts[1].strip("\n").splitlines():
        m = re.match(r"^([A-Za-z_-]+)\s*:\s*(.*)$", line)
        if m:
            meta[m.group(1)] = m.group(2).strip().strip('"')
    return meta, body


def check_skill(rel, src):
    """返回问题列表（空=合规）。rel 形如 hivemtk-release/SKILL.md。"""
    issues = []
    dirname = rel.split("/")[0]
    fm = parse_frontmatter(src)
    if fm is None:
        return [f"[{rel}] 缺 YAML frontmatter（--- name/description ---）"]
    meta, body = fm
    name, desc = meta.get("name", ""), meta.get("description", "")
    if not name:
        issues.append(f"[{rel}] name 必填")
    else:
        if name != dirname:
            issues.append(f"[{rel}] name={name} 与目录名 {dirname} 不一致（部署器按目录放置）")
        if not NAME_RE.match(name):
            issues.append(f"[{rel}] name={name} 不符 ^[a-z0-9-]+$（agentskills.io 规范）")
        if len(name) > NAME_LIMIT:
            issues.append(f"[{rel}] name 超 {NAME_LIMIT} 字符")
    if not desc:
        issues.append(f"[{rel}] description 必填（模型靠它决定是否加载，缺失=永不触发）")
    else:
        if len(desc) > DESC_LIMIT:
            issues.append(f"[{rel}] description {len(desc)} 字符超规范上限 {DESC_LIMIT}")
        if not any(h in desc.lower() for h in TRIGGER_HINTS):
            issues.append(f"[{rel}] description 只写了「是什么」缺「何时触发」语义（加载率主因）")
    if len(body.strip()) < 10:
        issues.append(f"[{rel}] 正文为空/过短")
    if src and not src.endswith("\n"):
        issues.append(f"[{rel}] 文件未以单个换行结尾（MD047）")
    return issues


def run(root):
    if not os.path.isdir(root):
        print(f"❌ skills 根不存在：{root}")
        return 2
    total, all_issues = 0, []
    for entry in sorted(os.listdir(root)):
        path = os.path.join(root, entry, "SKILL.md")
        if not os.path.isfile(path):
            continue  # 非 skill 目录（README 等）跳过
        total += 1
        with open(path, encoding="utf-8", errors="replace") as fh:
            src = fh.read()
        all_issues.extend(check_skill(f"{entry}/SKILL.md", src))
    print(f"扫描 {total} 个 skill（根：{root}）")
    if all_issues:
        for i in all_issues:
            print("❌ " + i)
        return 1
    print("✅ 全部符合 agentskills.io 可判定约束")
    return 0


SELFTEST_CASES = [
    # (目录/SKILL.md 相对路径, 内容, 期望问题数)
    ("good-skill/SKILL.md",
     '---\nname: good-skill\ndescription: "做 X。当用户要求 X 或提到 Y 时使用。"\n---\n\n# 标题\n\n正文内容超过十个字符。\n',
     0),
    ("Bad_Name/SKILL.md",
     '---\nname: BadName\ndescription: "当用户要求 X 时使用。"\n---\n\n正文内容超过十个字符。\n',
     2),  # name 不合规范（大写）+ 与目录名 Bad_Name 不一致
    ("no-desc/SKILL.md",
     '---\nname: no-desc\n---\n\n正文内容超过十个字符。\n',
     1),  # description 缺失
    ("no-fm/SKILL.md",
     '# 没有 frontmatter\n\n正文。\n',
     1),  # 缺 frontmatter
]


def selftest():
    failures = []
    with tempfile.TemporaryDirectory() as tmp:
        for rel, src, want in SELFTEST_CASES:
            path = os.path.join(tmp, rel)
            os.makedirs(os.path.dirname(path), exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                fh.write(src)
        all_issues = []
        for entry in sorted(os.listdir(tmp)):
            p = os.path.join(tmp, entry, "SKILL.md")
            if os.path.isfile(p):
                with open(p, encoding="utf-8") as fh:
                    all_issues.extend(check_skill(f"{entry}/SKILL.md", fh.read()))
        if len(all_issues) != sum(w for _r, _s, w in SELFTEST_CASES):
            failures.append(f"问题数不符：want={sum(w for _r, _s, w in SELFTEST_CASES)} got={len(all_issues)}: {all_issues}")
    if failures:
        print("❌ selftest 失败：")
        for f in failures:
            print(f"   {f}")
        return 2
    print(f"✅ selftest 通过：{len(SELFTEST_CASES)} 夹具（合规/坏name/缺desc/缺fm）")
    return 0


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    ap = argparse.ArgumentParser(description="skills/ agentskills.io 规范校验")
    ap.add_argument("--root", default=os.path.join(here, "..", "skills"))
    ap.add_argument("--selftest", action="store_true")
    args = ap.parse_args()
    sys.exit(selftest() if args.selftest else run(os.path.abspath(args.root)))


if __name__ == "__main__":
    main()
