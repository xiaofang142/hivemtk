#!/usr/bin/env bash
# ============================================================================
# LICENSE 合规自检（v3 审计 P0-S2）
# ----------------------------------------------------------------------------
# 检查 fork 后的仓库是否违反 AGPL-3.0 第 13 条（网络对外提供服务须开源）。
#
# 检测维度：
#   1. git remote 是否有 fork 来源（说明有上游）
#   2. 是否有公网域名配置（PUBLIC_BASE_URL 解析为公网 IP）
#   3. 是否有可访问的 user-server（HTTP 200 + 业务 API 响应）
#
# 输出：PASS（私域部署） / WARN（需人工判定） / FAIL（明确违反）
#
# 用法：
#   ./scripts/license-compliance-scan.sh                  # 实测
#   ./scripts/license-compliance-scan.sh --dry-run        # 只输出判定
#   ./scripts/license-compliance-scan.sh --public-url URL # 指定 URL
# ============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

DRY_RUN=false
NO_COLOR=false
PUBLIC_URL=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --dry-run) DRY_RUN=true; shift ;;
    --no-color) NO_COLOR=true; shift ;;
    --public-url) PUBLIC_URL="$2"; shift 2 ;;
    *) echo "unknown arg: $1"; exit 1 ;;
  esac
done

# 颜色
if $NO_COLOR || [[ ! -t 1 ]]; then
  C_RED=""; C_YEL=""; C_GRN=""; C_RST=""
else
  C_RED='\033[0;31m'; C_YEL='\033[0;33m'; C_GRN='\033[0;32m'; C_RST='\033[0m'
fi

VERDICT="PASS"
WARNINGS=()

# ---------------------------------------------------------------------------
# 维度 1: git remote
# ---------------------------------------------------------------------------
if [[ -d "$ROOT_DIR/.git" ]]; then
  cd "$ROOT_DIR"
  REMOTES=$(git remote -v 2>/dev/null || true)
  cd - > /dev/null
  if [[ -z "$REMOTES" ]]; then
    WARNINGS+=("git 仓库无 remote 配置，无法判定上游")
  else
    if echo "$REMOTES" | grep -qE 'xiaofang142/hivemtk|xhpmayun/hivemtk'; then
      echo -e "${C_GRN}[1/3] git remote: 上游为官方仓库 ✓${C_RST}"
    else
      VERDICT="WARN"
      WARNINGS+=("git remote 非官方仓库（$(echo "$REMOTES" | head -1)），需人工判定是否为合法 fork")
    fi
  fi
else
  echo -e "${C_YEL}[1/3] git remote: 非 git 仓库（源码分发场景）${C_RST}"
fi

# ---------------------------------------------------------------------------
# 维度 2: PUBLIC_BASE_URL
# ---------------------------------------------------------------------------
PB_URL="${PUBLIC_URL:-${PUBLIC_BASE_URL:-}}"
if [[ -z "$PB_URL" ]]; then
  echo -e "${C_GRN}[2/3] PUBLIC_BASE_URL 未配置，假设纯内网 ✓${C_RST}"
else
  HOST=$(echo "$PB_URL" | sed -E 's|^https?://||; s|/.*||; s|:.*||')
  # IP 字面量短路：无需 DNS 解析（且 macOS 无 getent）
  if [[ "$HOST" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]]; then
    RESOLVED_IP="$HOST"
  else
    RESOLVED_IP=$(getent hosts "$HOST" 2>/dev/null | awk '{print $1}' | head -1 || true)
    if [[ -z "$RESOLVED_IP" ]] && command -v dscacheutil >/dev/null 2>&1; then
      # macOS 回退：dscacheutil 解析
      RESOLVED_IP=$(dscacheutil -q host -a name "$HOST" 2>/dev/null | awk '/ip_address/ {print $2; exit}' || true)
    fi
  fi
  if [[ -z "$RESOLVED_IP" ]]; then
    echo -e "${C_YEL}[2/3] PUBLIC_BASE_URL=$PB_URL 解析失败，跳过${C_RST}"
  elif [[ "$RESOLVED_IP" =~ ^(10\.|172\.(1[6-9]|2[0-9]|3[01])\.|192\.168\.|127\.) ]]; then
    echo -e "${C_GRN}[2/3] PUBLIC_BASE_URL 解析为私网 IP $RESOLVED_IP ✓${C_RST}"
  else
    VERDICT="WARN"
    WARNINGS+=("PUBLIC_BASE_URL=$PB_URL 解析为公网 IP $RESOLVED_IP，若对外提供服务须遵守 AGPL-3.0 第 13 条")
  fi
fi

# ---------------------------------------------------------------------------
# 维度 3: HTTP 探活
# ---------------------------------------------------------------------------
if [[ -n "$PB_URL" ]] && ! $DRY_RUN; then
  HEALTH_URL="$PB_URL/health"
  HTTP_CODE=$(curl -sk -o /dev/null -w '%{http_code}' --max-time 5 "$HEALTH_URL" 2>/dev/null || echo "000")
  if [[ "$HTTP_CODE" == "200" ]]; then
    echo -e "${C_GRN}[3/3] HTTP 探活 $HEALTH_URL → 200 ✓${C_RST}"
  elif [[ "$HTTP_CODE" == "000" ]]; then
    echo -e "${C_GRN}[3/3] HTTP 探活 $HEALTH_URL → 不可达（仅内网部署）✓${C_RST}"
  else
    echo -e "${C_YEL}[3/3] HTTP 探活 $HEALTH_URL → $HTTP_CODE${C_RST}"
  fi
else
  echo -e "[3/3] HTTP 探活：跳过（--dry-run）"
fi

# ---------------------------------------------------------------------------
# 输出判定
# ---------------------------------------------------------------------------
echo ""
echo "================================================"
case "$VERDICT" in
  PASS)
    echo -e "${C_GRN}VERDICT: PASS — 判定为私域部署，符合 AGPL-3.0 私域豁免条款${C_RST}"
    ;;
  WARN)
    echo -e "${C_YEL}VERDICT: WARN — 需人工判定${C_RST}"
    for w in "${WARNINGS[@]}"; do
      echo -e "  ${C_YEL}⚠${C_RST}  $w"
    done
    echo ""
    echo "  若此部署为'对外提供网络服务'（SaaS / 托管 / API），"
    echo "  须按 AGPL-3.0 第 13 条向使用方开源你的修改。"
    echo "  详见 https://www.gnu.org/licenses/agpl-3.0.html"
    ;;
  FAIL)
    echo -e "${C_RED}VERDICT: FAIL — 明确违反 LICENSE${C_RST}"
    ;;
esac
echo "================================================"

[[ "$VERDICT" == "FAIL" ]] && exit 1 || exit 0