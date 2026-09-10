#!/bin/bash
# Go NM Host 安装脚本：编译 → /usr/local/bin → 注册 Chrome Native Messaging manifest
# 用法: ./install.sh [EXTENSION_ID]
#   EXTENSION_ID 见扩展 dist 加载后的 ID（manifest.json 已写死 key，ID 固定不变）
# 前置: 已通过 admin 接口 POST /api/browser-automation/host/token/reset 生成 token

set -e
GO_HOST_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY_PATH="/usr/local/bin/hivemtk_browser_nm_host"
HOST_NAME="com.hivemtk.browser"

# 1. 编译（共享 user-server/go.mod，无需单独 go.mod）
echo "==> 编译 Go NM Host..."
cd "$GO_HOST_DIR/../.."   # → user-server 根
go build -o /tmp/hivemtk_browser_nm_host ./cmd/nm-host
sudo mv /tmp/hivemtk_browser_nm_host "$BINARY_PATH"

# 2. token 配置
if [ ! -f "$HOME/.hivemtk/nm_host.conf" ]; then
  mkdir -p "$HOME/.hivemtk"
  cat > "$HOME/.hivemtk/nm_host.conf" <<EOF
# HiveMTK NM Host 配置
# token 由 admin 接口生成: POST /api/browser-automation/host/token/reset
token=粘贴你的token
EOF
  echo "⚠️  请编辑 ~/.hivemtk/nm_host.conf 填入 token（admin 接口生成）"
fi

# 3. 注册 Native Messaging manifest（macOS 用户级）
EXT_ID="${1:-YOUR_EXT_ID_HERE}"
MANIFEST_DIR="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts"
mkdir -p "$MANIFEST_DIR"
cat > "$MANIFEST_DIR/$HOST_NAME.json" <<EOF
{
  "name": "$HOST_NAME",
  "description": "HiveMTK Browser Automation Native Messaging Host",
  "path": "$BINARY_PATH",
  "type": "stdio",
  "allowed_origins": ["chrome-extension://$EXT_ID/"]
}
EOF

echo "✅ 安装完成。"
echo "   1) 确认 ~/.hivemtk/nm_host.conf 已填 token"
echo "   2) chrome://extensions 加载 user-web/browser_automation/dist"
echo "   3) 验证: GET /api/browser-automation/host/status → hosts 非空"
