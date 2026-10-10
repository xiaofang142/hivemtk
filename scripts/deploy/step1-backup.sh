#!/usr/bin/env bash
# 生产机部署 step1：备份当前 user-server 二进制 + 前端产物（在服务器上执行，路径硬编码 /opt/hivemtk）
# 来源：根目录 .deploy-step1-backup.sh（2026-10-10 线上部署存档，2026-10-11 归位 scripts/deploy/）
set -e
TS=$(date +%Y%m%d_%H%M%S)
echo "TS=$TS"
cp -a /opt/hivemtk/user-server /opt/hivemtk/user-server.bak.$TS
cp -a /opt/hivemtk/user-server.bak.$TS /opt/hivemtk/user-server.bak.latest
tar czf /root/hivemtk-web-$TS.tgz -C /var/www/hivemtk .
cp -a /root/hivemtk-web-$TS.tgz /root/hivemtk-web-backup-latest.tgz
echo "--- backups ---"
ls -la /opt/hivemtk/user-server.bak.$TS /root/hivemtk-web-$TS.tgz
echo "--- disk after backup ---"
df -h / | tail -1
echo "--- current service ---"
systemctl is-active hivemtk
md5sum /opt/hivemtk/user-server
