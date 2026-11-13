#!/usr/bin/env bash
# bootstrap-agent-env.sh — 新机器/新 clone 的第一步（幂等，可反复重跑）
#
# 为什么存在（2026-11-14 初心审核实测）：全新 clone 后约束体系有三个静默断点——
#   1. git 钩子未配置（core.hooksPath 是每 clone 手动一次性配置，GIT_RULES.md:40 有记载
#      但 AGENTS.md 主路径从不提）→ commit-msg 白名单静默失效；
#   2. skills 权威源随仓走，但生效要装进编辑器目录（~/.agents/skills 等）→ 新成员的
#      agent 身上 browser-debug-rule / hivemtk-release / hivemtk-coding 全部不存在；
#   3. 编码动作序列没有强制入口 → 由 skill hivemtk-coding 补（装完即生效）。
# 本脚本一次补平 1、2；3 随安装到位。
#
# 用法（内层仓根）：
#   bash scripts/bootstrap-agent-env.sh          # 配置钩子 + 安装 skills + 自检
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

echo "── [1/3] git 钩子（commit-msg 白名单 / pre-push 密钥闸）──"
git config core.hooksPath .githooks
for h in .githooks/*; do [ -x "$h" ] || chmod +x "$h"; done
echo "   core.hooksPath = $(git config core.hooksPath) ✓"

echo "── [2/3] 安装 skills 到本机编辑器目录（复数 skills，OpenCode 官方口径）──"
for d in "$HOME/.config/opencode/skills" "$HOME/.agents/skills"; do
  mkdir -p "$d"
  cp -R "$ROOT/skills/." "$d/"
done

echo "── [3/3] 自检（权威源 = 部署副本）──"
fail=0
while IFS= read -r f; do
  rel="${f#"$ROOT"/skills/}"
  for d in "$HOME/.config/opencode/skills" "$HOME/.agents/skills"; do
    if ! diff -q "$f" "$d/$rel" > /dev/null 2>&1; then
      echo "   ❌ 不一致：$d/$rel"
      fail=1
    fi
  done
done < <(find "$ROOT/skills" -name SKILL.md)
n=$(find "$ROOT/skills" -name SKILL.md | wc -l | tr -d ' ')
if [ "$fail" = "0" ]; then
  echo "   ✓ $n 个 skill 权威源与两处部署副本完全一致"
  echo "bootstrap 完成：钩子已生效，skills 已就位。编码动作序列见 skill hivemtk-coding。"
else
  echo "bootstrap 存在不一致，见上方 ❌；重跑本脚本可修复。"
  exit 1
fi
