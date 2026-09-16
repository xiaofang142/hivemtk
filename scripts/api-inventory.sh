#!/usr/bin/env bash
# 审计 M9：API 清单报告
#
# 提取后端路由(user-server)与前端 API 调用(user-web)，**并列清单**供人工复核。
#
# ⚠️ 2026-09-16 更正（RISK-06）：本脚本原名「一致性比对」，但它**从不比对** ——
# 只是把两侧各列一遍，没有 diff、没有基线、也不因不匹配而失败。
# 真正的阻断式门禁是 `scripts/audit_api_contract.py --strict`
# （方法敏感、含 .vue、UNMATCHED 必须为 0），已接在 .github/workflows/api-contract.yml。
# 本脚本定位为**非阻塞的清单快照**，名字与注释已按此更正，避免让人误以为这里有门禁。
#
# 本轮同时修掉一个盲区：前端扫描原先只看 user-web/src/api 目录，
# 漏掉写在 .vue 单文件组件里的调用（与 TOOL-06 同源，实测 81 处）。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUT="${REPO_ROOT}/api-inventory.md"

# 2026-09-16 补：原正则只认双引号与反引号，而前端大量写成单引号
# （`http.get('/api/xxx')`），导致清单长期**漏掉绝大多数前端调用**。
# 现统一三种引号：' " `
API_PATH_RE="['\"\`]/api/[^'\"\`]+['\"\`]"

: > "$OUT"
{
  echo "# API 调用 / 路由一致性报告"
  echo "_生成时间: $(date -u +%Y-%m-%dT%H:%M:%SZ)_"
  echo ""

  echo "## 后端路由 (user-server/internal/router)"
  if [ -d "${REPO_ROOT}/user-server/internal/router" ]; then
    grep -rhoE '\.(GET|POST|PUT|DELETE|PATCH)\("[^"]+"' "${REPO_ROOT}/user-server/internal/router" \
      | sed -E 's/\.(GET|POST|PUT|DELETE|PATCH)\("([^"]+)"/\1 \2/' | sort -u
  fi

  echo ""
  echo "## 前端 API 调用 (user-web/src 下的 .js 与 .vue)"
  # 扫描范围与 audit_api_contract.py 对齐：整个 src，含 .vue，跳过依赖/产物目录。
  if [ -d "${REPO_ROOT}/user-web/src" ]; then
    grep -rhoE "$API_PATH_RE" \
      --include='*.js' --include='*.vue' \
      --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=build \
      "${REPO_ROOT}/user-web/src" | tr -d "\"'\`" | sort -u
  fi

  echo ""
  echo "## 前端 API 路径（动态参数归一化为 :param）"
  # 与上一节同源正则，额外把模板字面量 `${id}` 折叠成 :param，便于与后端路由形状比对。
  # 注：sed ERE 在 BSD 下 `{` 会被当成区间量词，故统一写成字符类 [}]
  if [ -d "${REPO_ROOT}/user-web/src" ]; then
    grep -rhoE "$API_PATH_RE" \
      --include='*.js' --include='*.vue' \
      --exclude-dir=node_modules --exclude-dir=dist --exclude-dir=build \
      "${REPO_ROOT}/user-web/src" \
      | tr -d "\"'\`" \
      | sed -E 's/\$[{][^}]+[}]/:param/g; s/[?&].*$//' | sort -u
  fi
} >> "$OUT"

echo "API inventory written to ${OUT}"
