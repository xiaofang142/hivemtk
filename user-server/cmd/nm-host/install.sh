#!/bin/bash
# Go NM Host 安装脚本：编译 → 落到可执行目录 → 注册 Chrome Native Messaging manifest
#
# 扩展 ID 不需要手抄：扩展 manifest.json 里写死了 key（Chrome 据此算出固定 ID），
# 脚本自己推导。让人抄 ID 参数是假绿入口——抄错一位，Chrome 只会回一句
# "Specified native messaging host not found"，看不出是 allowed_origins 不匹配。
#
# 用法:
#   ./install.sh            完整安装（编译 + 落盘 + 注册 manifest + 建配置模板）
#   ./install.sh --check    只核对：打印将要使用的 ID 与各路径，不写任何文件、不编译
set -e

GO_HOST_DIR="$(cd "$(dirname "$0")" && pwd)"
HOST_NAME="com.hivemtk.browser"
EXT_DIR="$(cd "$GO_HOST_DIR/../../../user-web/browser_automation" && pwd)"
EXT_MANIFEST="$EXT_DIR/manifest.json"
CONF_DIR="$HOME/.hivemtk"
CONF_FILE="$CONF_DIR/nm_host.conf"
MANIFEST_DIR="$HOME/Library/Application Support/Google/Chrome/NativeMessagingHosts"
DEFAULT_WS_URL="ws://127.0.0.1:8204/api/browser/host-ws"

# 二进制落点：/usr/local/bin 可写就沿用既有安装位置，否则退到用户目录。
# 全程不用 sudo —— 需要 sudo 的脚本在无 tty 的 CI/远程执行里会直接卡住。
if [ -w /usr/local/bin ]; then
  BINARY_PATH="/usr/local/bin/hivemtk_browser_nm_host"
else
  BINARY_PATH="$CONF_DIR/bin/hivemtk_browser_nm_host"
fi

# 扩展 ID = sha256(manifest key 的 SPKI DER) 前 32 个十六进制位，逐位 0-f → a-p
derive_ext_id() {
  local key hex
  if [ ! -f "$EXT_MANIFEST" ]; then
    echo "❌ 找不到扩展 manifest：${EXT_MANIFEST}" >&2
    exit 1
  fi
  key=$(sed -n 's/.*"key": "\([^"]*\)".*/\1/p' "$EXT_MANIFEST")
  if [ -z "$key" ]; then
    echo "❌ ${EXT_MANIFEST} 里没有 key 字段：ID 会随每次加载变化，注册不了固定的 allowed_origins" >&2
    exit 1
  fi
  hex=$(printf '%s' "$key" | openssl base64 -d -A | openssl dgst -sha256 | awk '{print $NF}')
  printf '%s' "$hex" | cut -c1-32 | tr '0123456789abcdef' 'abcdefghijklmnop'
}

EXT_ID="$(derive_ext_id)"

case "${1:-}" in
  --check)
    echo "ext_id=${EXT_ID}"
    echo "binary_path=${BINARY_PATH}"
    echo "manifest_dir=${MANIFEST_DIR}"
    echo "conf_file=${CONF_FILE}"
    if [ -f "$EXT_DIR/dist/manifest.json" ]; then
      echo "extension_dist=built"
    else
      echo "extension_dist=missing（先在 ${EXT_DIR} 跑 npm install && npm run build）"
    fi
    exit 0
    ;;
  "") ;;
  *)
    echo "❌ 未知参数：${1}（扩展 ID 现在由 manifest 的 key 自动推导，不再接受参数）" >&2
    exit 1
    ;;
esac

# 1. 编译（共享 user-server/go.mod，无需单独 go.mod）
echo "==> 编译 Go NM Host..."
TMP_BIN="$(mktemp)"
( cd "$GO_HOST_DIR/../.." && go build -o "$TMP_BIN" ./cmd/nm-host )

# 2. 落盘 + 赋可执行位（Chrome fork 子进程需要执行位）
if [ "$BINARY_PATH" != "$TMP_BIN" ]; then
  mkdir -p "$(dirname "$BINARY_PATH")"
  mv "$TMP_BIN" "$BINARY_PATH"
fi
chmod 755 "$BINARY_PATH"
echo "==> 二进制：${BINARY_PATH}"

# 3. 注册 Native Messaging manifest（macOS 用户级，无需管理员权限）
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
echo "==> 已注册扩展 ID：${EXT_ID}"

# 4. token / 服务地址配置
mkdir -p "$CONF_DIR"
if [ ! -f "$CONF_FILE" ]; then
  cat > "$CONF_FILE" <<EOF
# HiveMTK NM Host 配置
# token 由服务端「浏览器自动化 → Host 状态」页的「重置 Host Token」生成
token=粘贴你的token
# 服务端地址：user-server 不在本机 8204 时改成实际地址（环境变量 HIVE_MTK_WS_URL 优先）
server_url=${DEFAULT_WS_URL}
EOF
  chmod 600 "$CONF_FILE"
  echo "⚠️  请编辑 ${CONF_FILE} 填入 token"
elif ! grep -q '^server_url=' "$CONF_FILE"; then
  # 已有配置不静默改写（里面是用户的 token）：只把缺的那一行报出来
  echo "ℹ️  ${CONF_FILE} 没有 server_url= 行；服务端口不是 8204 时加上，否则 Host 只会连默认地址"
fi

echo "✅ 安装完成。接下来："
echo "   1) 构建扩展（dist 是构建产物、不在版本控制里）：cd user-web/browser_automation && npm install && npm run build"
echo "   2) chrome://extensions → 开发者模式 → 加载已解压的扩展程序 → 选 user-web/browser_automation/dist"
echo "   3) 确认 ${CONF_FILE} 已填 token；改过 token 或 server_url 后要重启 Host 进程（扩展断开不会自动重连）"
echo "   4) 验证：浏览器自动化 → Host 状态，该台应显示「可服务」"
