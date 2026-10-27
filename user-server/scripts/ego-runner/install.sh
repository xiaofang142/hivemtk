#!/usr/bin/env bash
# ego-runner 安装器（specs/015 C7，macOS 先行；设计文档 R2）
#
# 用法：
#   ./install.sh [--server-url ws://127.0.0.1:8204] [--token eg_...] [--ego-server-name <name>]
#   ./install.sh --check          # 只核对环境与配置，不写不装
#   ./uninstall.sh                # 卸载（同目录）
#
# 做的事（免 sudo，全部落在用户目录）：
#   1. 编译 runner（go build，需 Go 1.25+；或用 --bin 指定已编译二进制）
#   2. 落盘 ~/.hivemtk/bin/ego-runner + conf 模板（已存在的 conf 不覆盖）
#   3. 注册 LaunchAgent（cn.hivemtk.ego-runner，开机自启+崩溃重启）
#   4. --check 模式核对 server_url 可达 / ego-browser 存在 / conf 齐全
#
# 形态说明（调研 §6.5）：server_url 指向 loopback ⇒ 形态甲（conf 写入 loopback_only=true
# 仅为提示；服务端回环门由服务端按形态开关实现）。

set -euo pipefail

PREFIX="${HOME}/.hivemtk"
BIN_DIR="${PREFIX}/bin"
CONF="${PREFIX}/ego_runner.conf"
PLIST_LABEL="cn.hivemtk.ego-runner"
PLIST="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"
REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"   # user-server 根（本脚本在 scripts/ego-runner/ 下）

SERVER_URL="ws://127.0.0.1:8204"
TOKEN=""
EGO_SERVER_NAME=""
BIN_OVERRIDE=""
DO_CHECK=0

while [[ $# -gt 0 ]]; do
  case "$1" in
    --server-url) SERVER_URL="$2"; shift 2 ;;
    --token) TOKEN="$2"; shift 2 ;;
    --ego-server-name) EGO_SERVER_NAME="$2"; shift 2 ;;
    --bin) BIN_OVERRIDE="$2"; shift 2 ;;
    --check) DO_CHECK=1; shift ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数: $1" >&2; exit 1 ;;
  esac
done

say() { printf '\033[1;36m[ego-runner]\033[0m %s\n' "$*"; }
die() { printf '\033[1;31m[ego-runner] ❌ %s\033[0m\n' "$*" >&2; exit 1; }

# ---------- --check：只读核对 ----------
if [[ $DO_CHECK -eq 1 ]]; then
  say "server_url = ${SERVER_URL}"
  if command -v ego-browser >/dev/null 2>&1; then
    say "ego-browser = $(command -v ego-browser) ($("ego-browser" --version 2>/dev/null | head -1))"
  else
    say "⚠️ ego-browser 不在 PATH——请先安装并启动 ego lite（官网 DMG）"
  fi
  if [[ -f "$CONF" ]]; then
    say "conf = ${CONF}（已存在，不覆盖）"
    grep -E '^(server_url|workspace|ego_server_name|loopback_only)=' "$CONF" | sed 's/^/    /'
    grep -q '^token=eg_' "$CONF" && say "token = 已配置" || say "⚠️ conf 缺 token= 行"
  else
    say "⚠️ conf 不存在：${CONF}"
  fi
  [[ -x "${BIN_DIR}/ego-runner" ]] && say "runner 二进制 = ${BIN_DIR}/ego-runner" || say "⚠️ runner 未安装：${BIN_DIR}/ego-runner"
  launchctl list 2>/dev/null | grep -q "$PLIST_LABEL" && say "LaunchAgent = 已注册" || say "LaunchAgent = 未注册"
  exit 0
fi

# ---------- 1. 二进制 ----------
mkdir -p "$BIN_DIR"
if [[ -n "$BIN_OVERRIDE" ]]; then
  [[ -x "$BIN_OVERRIDE" ]] || die "--bin 指定的文件不可执行: $BIN_OVERRIDE"
  cp "$BIN_OVERRIDE" "${BIN_DIR}/ego-runner"
  say "使用指定二进制：$BIN_OVERRIDE"
else
  command -v go >/dev/null 2>&1 || die "未找到 go，请先安装 Go 1.25+ 或用 --bin 指定已编译二进制"
  say "编译 runner（go build ./cmd/ego-runner）…"
  (cd "$REPO_ROOT" && go build -o "${BIN_DIR}/ego-runner" ./cmd/ego-runner)
fi
say "已安装：${BIN_DIR}/ego-runner"

# ---------- 2. conf（不覆盖已有） ----------
if [[ -f "$CONF" ]]; then
  say "conf 已存在，保留：${CONF}"
else
  [[ -n "$TOKEN" ]] || die "conf 不存在且未提供 --token（在 user-web「浏览器自动化 → ego 资产中心 → Runner」页签发）"
  case "$SERVER_URL" in
    ws://127.0.0.1*|ws://localhost*|ws://[::1]*) LOOPBACK="true" ;;
    *) LOOPBACK="false" ;;
  esac
  mkdir -p "$PREFIX"
  {
    echo "# HiveMtk ego-runner 配置（key=value；详见 scripts/ego-runner/README.md）"
    echo "server_url=${SERVER_URL}"
    echo "token=${TOKEN}"
    echo "device_id=$(uuidgen | tr 'A-Z' 'a-z')"
    echo "loopback_only=${LOOPBACK}"
    [[ -n "$EGO_SERVER_NAME" ]] && echo "ego_server_name=${EGO_SERVER_NAME}"
  } > "$CONF"
  chmod 600 "$CONF"
  say "已生成 conf：${CONF}（loopback_only=${LOOPBACK}）"
fi

# ---------- 3. LaunchAgent ----------
mkdir -p "$(dirname "$PLIST")"
cat > "$PLIST" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>${PLIST_LABEL}</string>
  <key>ProgramArguments</key><array>
    <string>${BIN_DIR}/ego-runner</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>${PREFIX}/ego-runner.log</string>
  <key>StandardErrorPath</key><string>${PREFIX}/ego-runner.log</string>
</dict></plist>
EOF
launchctl unload "$PLIST" >/dev/null 2>&1 || true
launchctl load "$PLIST"
say "已注册 LaunchAgent：${PLIST_LABEL}（日志 ${PREFIX}/ego-runner.log）"

say "✅ 安装完成。核对：./install.sh --check"
say "   服务端确认：user-web → 浏览器自动化 → ego Runner 页应显示在线状态。"
