#!/usr/bin/env bash
# =============================================================================
# check_browser_host.sh
# 浏览器自动化链路健康检查(I3: BROWSER_AUTOMATION_TECH_DECISION.md §4.3)
# 覆盖整链: 扩展 manifest → NM Host 二进制/配置/Chrome 注册 → 服务端 host/status
#
# 用法:
#   bash scripts/check_browser_host.sh [--offline] [--base-url URL] [--token JWT]
#   --offline        仅做本机静态检查,不请求服务端
#   --base-url URL   服务端基址(默认 http://127.0.0.1:8204,见 internal/config/ports.go)
#   --token JWT      登录用户 JWT(无则跳过服务端在线检查,提示而非失败)
#   HIVE_MTK_JWT 环境变量亦可提供 JWT
#
# 退出码: 0 全通过(允许 WARN); 1 存在 FAIL
# =============================================================================

set -u

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
HIVEMTK_DIR="$PROJECT_ROOT/hivemtk"

BASE_URL="${1:-}"
OFFLINE=0
JWT="${HIVE_MTK_JWT:-}"

while [ $# -gt 0 ]; do
  case "$1" in
    --offline) OFFLINE=1; shift ;;
    --base-url) BASE_URL="$2"; shift 2 ;;
    --token) JWT="$2"; shift 2 ;;
    *) BASE_URL="$1"; shift ;;
  esac
done
BASE_URL="${BASE_URL:-http://127.0.0.1:8204}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

FAIL=0
log_pass() { echo -e "${GREEN}✅ $1${NC}"; }
log_fail() { echo -e "${RED}❌ $1${NC}"; FAIL=$((FAIL+1)); }
log_warn() { echo -e "${YELLOW}⚠️  $1${NC}"; }
log_info() { echo "   $1"; }

EXT_DIR="$HIVEMTK_DIR/user-web/browser_automation"
SRC_MANIFEST="$EXT_DIR/manifest.json"
DIST_MANIFEST="$EXT_DIR/dist/manifest.json"
HOST_MAIN="$HIVEMTK_DIR/user-server/cmd/nm-host/main.go"
HOST_BIN="/usr/local/bin/hivemtk_browser_nm_host"
HOST_CONF="$HOME/.hivemtk/nm_host.conf"
HOST_NAME="com.hivemtk.browser"

echo "============================================================"
echo "  浏览器自动化链路健康检查 (I3)"
echo "  决策: docs/architecture/BROWSER_AUTOMATION_TECH_DECISION.md §4.3"
echo "============================================================"
echo ""

# -----------------------------------------------------------------------------
# [1/5] 扩展 manifest(源码 + 构建产物)
# -----------------------------------------------------------------------------
echo "[1/5] 扩展 manifest 检查..."
if [ -f "$SRC_MANIFEST" ]; then
  SRC_VER=$(grep -o '"version": *"[^"]*"' "$SRC_MANIFEST" | head -1 | cut -d'"' -f4)
  log_pass "源码 manifest 存在 (version=$SRC_VER)"
else
  log_fail "源码 manifest 缺失: user-web/browser_automation/manifest.json"
  SRC_VER=""
fi
if [ -f "$DIST_MANIFEST" ]; then
  DIST_VER=$(grep -o '"version": *"[^"]*"' "$DIST_MANIFEST" | head -1 | cut -d'"' -f4)
  if [ -n "${SRC_VER:-}" ] && [ "$SRC_VER" != "$DIST_VER" ]; then
    log_fail "dist manifest 版本($DIST_VER)与源码($SRC_VER)不一致——需重新构建扩展"
  else
    log_pass "dist manifest 存在 (version=$DIST_VER,与源码一致)"
  fi
else
  log_warn "dist manifest 缺失——扩展尚未构建(chrome://extensions 需加载 dist/)"
fi

# -----------------------------------------------------------------------------
# [2/5] NM Host 版本锚点(main.go hostVersion vs manifest)
# -----------------------------------------------------------------------------
echo "[2/5] NM Host 版本锚点检查..."
if [ -f "$HOST_MAIN" ]; then
  HOST_VER=$(grep -o 'hostVersion = envOr("HIVE_MTK_HOST_VERSION", "[^"]*")' "$HOST_MAIN" | grep -o '"[0-9.]*")' | tr -d '")')
  if [ -n "${SRC_VER:-}" ] && [ -n "${HOST_VER:-}" ] && [ "$SRC_VER" != "$HOST_VER" ]; then
    log_fail "hostVersion($HOST_VER)与扩展 manifest($SRC_VER)不同步(Chrome SW ScriptCache 缓存排查锚点)"
  elif [ -n "${HOST_VER:-}" ]; then
    log_pass "hostVersion=$HOST_VER 与扩展版本一致"
  else
    log_warn "未能解析 hostVersion"
  fi
else
  log_fail "NM Host 源码缺失: user-server/cmd/nm-host/main.go"
fi

# -----------------------------------------------------------------------------
# [3/5] NM Host 二进制 + token 配置
# -----------------------------------------------------------------------------
echo "[3/5] NM Host 二进制与配置检查..."
if [ -x "$HOST_BIN" ]; then
  log_pass "Host 二进制可执行: $HOST_BIN"
else
  log_warn "Host 二进制缺失: $HOST_BIN —— 运行 user-server/cmd/nm-host/install.sh 安装"
fi
if [ -f "$HOST_CONF" ]; then
  if grep -q '^token=.\+' "$HOST_CONF" && ! grep -q '^token=粘贴你的token' "$HOST_CONF"; then
    log_pass "Host token 已配置 (~/.hivemtk/nm_host.conf)"
  else
    log_fail "Host token 未填写: 请先调 POST /api/browser-automation/host/token/reset 生成再填入"
  fi
else
  log_fail "Host 配置缺失: ~/.hivemtk/nm_host.conf (install.sh 会生成模板)"
fi

# -----------------------------------------------------------------------------
# [4/5] Chrome Native Messaging 注册表
# -----------------------------------------------------------------------------
echo "[4/5] Chrome Native Messaging 注册检查..."
case "$(uname -s)" in
  Darwin) NM_DIR="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts" ;;
  Linux)  NM_DIR="$HOME/.config/google-chrome/NativeMessagingHosts" ;;
  *)      NM_DIR="" ;;
esac
if [ -z "${NM_DIR:-}" ]; then
  log_warn "未知操作系统,跳过浏览器注册表检查 ($(uname -s))"
elif [ -f "$NM_DIR/$HOST_NAME.json" ]; then
  REG_PATH=$(grep -o '"path": *"[^"]*"' "$NM_DIR/$HOST_NAME.json" | head -1 | cut -d'"' -f4)
  if [ -x "${REG_PATH:-/nonexistent}" ]; then
    log_pass "NM manifest 已注册且 path 可执行 ($REG_PATH)"
  else
    log_fail "NM manifest 已注册但 path 不可执行: ${REG_PATH:-<空>} —— 重跑 install.sh"
  fi
else
  log_fail "NM manifest 未注册: $NM_DIR/$HOST_NAME.json 缺失 —— 运行 install.sh [EXTENSION_ID]"
fi

# -----------------------------------------------------------------------------
# [5/5] 服务端在线检查(可跳过)
# -----------------------------------------------------------------------------
echo "[5/5] 服务端 host/status 在线检查..."
if [ "$OFFLINE" -eq 1 ]; then
  log_warn "--offline 模式:跳过服务端检查"
elif [ -z "$JWT" ]; then
  log_warn "未提供 JWT(--token 或 HIVE_MTK_JWT):跳过在线检查,仅完成本机静态检查"
  log_info "在线验证: curl -H \"Authorization: Bearer <JWT>\" $BASE_URL/api/browser-automation/host/status"
else
  RESP=$(curl -s -m 10 -H "Authorization: Bearer $JWT" "$BASE_URL/api/browser-automation/host/status" 2>/dev/null || true)
  if [ -z "$RESP" ]; then
    log_fail "服务端不可达: $BASE_URL (确认 user-server 已启动,端口见 internal/config/ports.go DefaultListenPort=8204)"
  elif echo "$RESP" | grep -q '"code":0\|"code": 0'; then
    log_pass "host/status 返回 code=0"
    log_info "$(echo "$RESP" | head -c 500)"
  else
    log_fail "host/status 返回异常: $(echo "$RESP" | head -c 300)"
  fi
fi

echo ""
echo "============================================================"
if [ "$FAIL" -eq 0 ]; then
  echo -e "${GREEN}  健康检查通过 (0 FAIL,允许 WARN)${NC}"
else
  echo -e "${RED}  健康检查发现 $FAIL 项 FAIL${NC}"
fi
echo "============================================================"
exit $FAIL
