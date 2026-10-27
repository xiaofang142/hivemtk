#!/usr/bin/env bash
# ego-runner 卸载器（specs/015 C7）：停 LaunchAgent、删二进制与 plist；
# conf 与日志保留（重装即复用；要彻底清除请手动删 ~/.hivemtk/）。
set -euo pipefail

PREFIX="${HOME}/.hivemtk"
PLIST_LABEL="cn.hivemtk.ego-runner"
PLIST="${HOME}/Library/LaunchAgents/${PLIST_LABEL}.plist"

launchctl unload "$PLIST" 2>/dev/null || true
rm -f "$PLIST" "${PREFIX}/bin/ego-runner"
echo "[ego-runner] 已卸载（conf 保留：${PREFIX}/ego_runner.conf）"
