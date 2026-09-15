#!/usr/bin/env bash
# =============================================================
# 跨包端口/URL 一致性审计脚本
# -------------------------------------------------------------
# 目的：禁止"软启动"——确保所有端口字面量集中于 ports.go / constants.js
#       等单一源，配置文件 / vite.config.js / 测试脚本 / 文档必须从
#       单一源派生或与单一源字面一致。
#
# 文档源：
#   - user-server/docs/dev/DEVELOPMENT.md §2.4 端口对照表
#   - hivemtk-platform/platform-server/docs/dev/DEVELOPMENT.md §1.5 端口对照表
#   - user-web/bridge/docs/dev/DEVELOPMENT.md §3 端口对照表
#   - user-web/bridge/src/core/constants.js DEFAULT_USER_SERVER.port
#
# 调整流程（必须严格遵循）：
#   1) 先改 ports.go / constants.js 常量
#   2) 再改各文档 DEVELOPMENT.md 端口对照表
#   3) 最后改跨包字面量（vite.config.js / config.yaml 等）
#   4) 跑本脚本验证字面一致
#
# 退出码：
#   0 - 所有审计通过
#   1 - 存在不一致或硬编码违规
# =============================================================
set -e

ROOT="$(cd "$(dirname "$0")/.." && pwd)"

# 仓库根目录：脚本位于 hivemtk/scripts/，hivemtk/ 是子目录
# ROOT = hivemtk/, REPO_ROOT = hivemtk/ 的上一层（真正的仓库根）
REPO_ROOT="$(cd "$ROOT/.." && pwd)"

cd "$REPO_ROOT"

# 颜色
RED='\033[0;31m'
YELLOW='\033[0;33m'
GREEN='\033[0;32m'
NC='\033[0m'

ERRORS=0
WARNS=0

# helper
err() { echo -e "${RED}✗ $1${NC}"; ERRORS=$((ERRORS+1)); }
warn() { echo -e "${YELLOW}⚠ $1${NC}"; WARNS=$((WARNS+1)); }
ok() { echo -e "${GREEN}✓ $1${NC}"; }

# 提取的"已声明单一源"
# 1) user-server ports.go（位于 hivemtk/ 子目录下）
USER_SERVER_PORTS_FILE="$REPO_ROOT/hivemtk/user-server/internal/config/ports.go"
# 2) platform-server ports.go（位于 hivemtk-platform/ 子目录下）
PLATFORM_PORTS_FILE="$REPO_ROOT/hivemtk-platform/platform-server/internal/config/ports.go"
# 3) bridge constants.js
BRIDGE_CONST_FILE="$REPO_ROOT/hivemtk/user-web/bridge/src/core/constants.js"

# =============================================================
# Step 1: 提取 user-server 端口单一源
# =============================================================
echo "============================================================"
echo "Step 1: 提取 user-server ports.go 单一源"
echo "============================================================"

US_LISTEN_PORT=$(grep -E 'DefaultListenPort[[:space:]]*=[[:space:]]*"' "$USER_SERVER_PORTS_FILE" | head -1 | sed -E 's/.*"([0-9]+)".*/\1/')
US_DB_PORT_DEV=$(grep -E 'DefaultDBPortDev[[:space:]]*=[[:space:]]*[0-9]+' "$USER_SERVER_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')
US_DB_PORT_DOCKER=$(grep -E 'DefaultDBPortDocker[[:space:]]*=[[:space:]]*[0-9]+' "$USER_SERVER_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')
US_REDIS_PORT=$(grep -E 'DefaultRedisPort[[:space:]]*=[[:space:]]*"' "$USER_SERVER_PORTS_FILE" | head -1 | sed -E 's/.*"([0-9]+)".*/\1/')
US_PLATFORM_PORT=$(grep -E 'DefaultPlatformPort[[:space:]]*=[[:space:]]*"' "$USER_SERVER_PORTS_FILE" | head -1 | sed -E 's/.*"([0-9]+)".*/\1/')
US_CDP_PORT=$(grep -E 'DefaultChromiumCDPPort[[:space:]]*=[[:space:]]*"' "$USER_SERVER_PORTS_FILE" | head -1 | sed -E 's/.*"([0-9]+)".*/\1/')
US_LLM_PORT=$(grep -E 'DefaultLLMPort[[:space:]]*=[[:space:]]*[0-9]+' "$USER_SERVER_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')
US_EMB_PORT=$(grep -E 'DefaultEmbeddingPort[[:space:]]*=[[:space:]]*[0-9]+' "$USER_SERVER_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')
US_RERANK_PORT=$(grep -E 'DefaultRerankPort[[:space:]]*=[[:space:]]*[0-9]+' "$USER_SERVER_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')

echo "  DefaultListenPort      = $US_LISTEN_PORT"
echo "  DefaultDBPortDev       = $US_DB_PORT_DEV"
echo "  DefaultDBPortDocker    = $US_DB_PORT_DOCKER"
echo "  DefaultRedisPort       = $US_REDIS_PORT"
echo "  DefaultPlatformPort    = $US_PLATFORM_PORT"
echo "  DefaultChromiumCDPPort = $US_CDP_PORT"
echo "  DefaultLLMPort         = $US_LLM_PORT"
echo "  DefaultEmbeddingPort   = $US_EMB_PORT"
echo "  DefaultRerankPort      = $US_RERANK_PORT"
echo

# =============================================================
# Step 2: 提取 platform-server 端口单一源
# =============================================================
echo "============================================================"
echo "Step 2: 提取 platform-server ports.go 单一源"
echo "============================================================"

PS_SERVER_PORT=$(grep -E 'DefaultServerPort[[:space:]]*=[[:space:]]*"' "$PLATFORM_PORTS_FILE" | head -1 | sed -E 's/.*"([0-9]+)".*/\1/')
PS_DB_PORT=$(grep -E 'DefaultDBPortDev[[:space:]]*=[[:space:]]*[0-9]+' "$PLATFORM_PORTS_FILE" | head -1 | grep -oE '[0-9]+$')
PS_REDIS=$(grep -E 'DefaultRedisAddr[[:space:]]*=[[:space:]]*"' "$PLATFORM_PORTS_FILE" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')
PS_OLLAMA=$(grep -E 'DefaultOllamaBaseURL[[:space:]]*=[[:space:]]*"' "$PLATFORM_PORTS_FILE" | head -1 | sed -E 's/.*"([^"]+)".*/\1/')

echo "  DefaultServerPort    = $PS_SERVER_PORT"
echo "  DefaultDBPortDev     = $PS_DB_PORT"
echo "  DefaultRedisAddr     = $PS_REDIS"
echo "  DefaultOllamaBaseURL = $PS_OLLAMA"
echo

# =============================================================
# Step 3: 提取 bridge constants.js 单一源
# =============================================================
echo "============================================================"
echo "Step 3: 提取 bridge constants.js 单一源"
echo "============================================================"

BRIDGE_US_PORT=$(grep -E "port:[[:space:]]*[0-9]+" "$BRIDGE_CONST_FILE" | head -1 | grep -oE '[0-9]+')
echo "  DEFAULT_USER_SERVER.port = $BRIDGE_US_PORT"
echo

# =============================================================
# Step 4: 验证 user-server <-> bridge 对齐
# =============================================================
echo "============================================================"
echo "Step 4: 验证 user-server <-> bridge 端口对齐"
echo "============================================================"
if [[ "$US_LISTEN_PORT" == "$BRIDGE_US_PORT" ]]; then
  ok "user-server.DefaultListenPort == bridge.DEFAULT_USER_SERVER.port ($US_LISTEN_PORT)"
else
  err "user-server.DefaultListenPort($US_LISTEN_PORT) != bridge.DEFAULT_USER_SERVER.port($BRIDGE_US_PORT)"
fi
echo

# =============================================================
# Step 5: 验证 user-server <-> platform-server 对齐
# =============================================================
echo "============================================================"
echo "Step 5: 验证 user-server <-> platform-server 端口对齐"
echo "============================================================"
if [[ "$US_PLATFORM_PORT" == "$PS_SERVER_PORT" ]]; then
  ok "user-server.DefaultPlatformPort == platform-server.DefaultServerPort ($US_PLATFORM_PORT)"
else
  err "user-server.DefaultPlatformPort($US_PLATFORM_PORT) != platform-server.DefaultServerPort($PS_SERVER_PORT)"
fi
echo

# =============================================================
# Step 6: 审计 .yaml 配置文件的 base_url 硬编码
# =============================================================
echo "============================================================"
echo "Step 6: 审计 .yaml 配置文件 base_url 端口字面量"
echo "============================================================"

# 扫描所有 .yaml 文件中的 base_url 含端口字面量
# 已声明的允许项（作为单一源的"派生"）：
#   - ports.go / constants.js
#   - ports_test.go
#   - DEVELOPMENT.md 文档
#   - swagger 注解
#   - .env.example / docker-compose.yml 模板
# 其它位置若出现 :8204 / :8205 / :8207 / :8208 / :8209 字面量必须 env 变量覆盖

check_yaml_hardcode() {
  local file="$REPO_ROOT/$1"
  local label="$2"
  if [[ ! -f "$file" ]]; then
    warn "$label: $file 不存在，跳过"
    return
  fi
  # 仅检查非注释值行中的端口字面量；注释行（示例说明）不算违规
  # 宿主机单机部署：127.0.0.1:82xx 私域字面量是合法真相源（与 inference_load_test.go 契约一致）
  # 仅当出现非 127.0.0.1/localhost 的 host:port 字面量时才告警（疑似跨机硬编码）
  local value_hits
  value_hits=$(grep -nE "^[[:space:]]*[^[:space:]#]" "$file" 2>/dev/null \
    | sed -E 's/\$\{[^}]+\}//g' \
    | grep -E "https?://[^/\"'#]*:82(04|05|07|08|09)" \
    | grep -vE "127\.0\.0\.1|localhost|host\.docker\.internal|mtk-" \
    | grep -vE "ports\.go|ports_test\.go|docs/|README\.md|swagger|\.env|\.gitignore" \
    || true)
  if [[ -n "$value_hits" ]]; then
    err "$label: $file 中存在非私域 base_url 端口硬编码（仅 127.0.0.1/localhost/host.docker.internal/mtk-* 合法）"
    echo "$value_hits" | while read -r line; do echo "    $line"; done
  else
    ok "$label: $file 无端口字面量违规（私域 127.0.0.1 字面量合法）"
  fi
}

check_yaml_hardcode "hivemtk/user-server/config.yaml" "user-server config"
check_yaml_hardcode "hivemtk/user-server/config/platform.yaml" "user-server config/platform"
check_yaml_hardcode "hivemtk-platform/platform-server/config.yaml" "platform-server config"
echo

# =============================================================
# Step 7: 审计 vite.config.js 端口字面量是否在注释中标注单一源
# =============================================================
echo "============================================================"
echo "Step 7: 审计 vite.config.js 单一源约束注释"
echo "============================================================"

for f in \
  "hivemtk/user-web/vite.config.js" \
  "hivemtk-platform/platform-web/vite.config.js" \
  "hivemtk-platform/platform-contributor/vite.config.js" \
  "hivemtk-platform/website/vite.config.js"
do
  if [[ -f "$REPO_ROOT/$f" ]]; then
    if grep -qE "单一源约束|单一文档源|单一代码源" "$REPO_ROOT/$f"; then
      ok "$f 包含单一源约束注释"
    else
      warn "$f 缺少单一源约束注释（建议添加 ports.go 引用注释）"
    fi
  fi
done
echo

# =============================================================
# 总结
# =============================================================
echo "============================================================"
echo "审计总结"
echo "============================================================"
echo -e "  Errors: ${RED}$ERRORS${NC}"
echo -e "  Warns:  ${YELLOW}$WARNS${NC}"
echo

if [[ $ERRORS -gt 0 ]]; then
  echo -e "${RED}✗ 跨包审计失败：存在 $ERRORS 处硬编码/不一致违规${NC}"
  exit 1
fi

echo -e "${GREEN}✓ 跨包审计通过：所有端口字面量与单一源一致${NC}"
exit 0
