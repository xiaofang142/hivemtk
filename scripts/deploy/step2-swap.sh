#!/usr/bin/env bash
# 生产机部署 step2：原子替换 /opt/hivemtk/user-server + 重启 + 健康检查，失败自动回滚到 bak.latest
# 前置：step1-backup.sh 已跑过（本脚本依赖 .bak.latest）；新二进制放 /opt/hivemtk/user-server.new
set -e
echo "=== 1) 原子替换二进制 ==="
chmod 0755 /opt/hivemtk/user-server.new
mv -f /opt/hivemtk/user-server.new /opt/hivemtk/user-server
ls -la /opt/hivemtk/user-server
md5sum /opt/hivemtk/user-server

echo "=== 2) 重启 systemd 服务 ==="
systemctl restart hivemtk
systemctl is-active hivemtk

echo "=== 3) 健康检查（最多 60s）==="
ok=0
for i in $(seq 1 30); do
  ct=$(curl -fsS -o /dev/null -w "%{content_type}" http://127.0.0.1:8204/api/health 2>/dev/null || true)
  case "$ct" in
    application/json*) ok=1; echo "  OK ${i}x2s content_type=$ct"; break ;;
  esac
  sleep 2
done

if [ "$ok" != "1" ]; then
  echo "  !! 健康检查失败 —— 回滚到上一版二进制"
  cp -a /opt/hivemtk/user-server.bak.latest /opt/hivemtk/user-server
  systemctl restart hivemtk
  sleep 6
  curl -fsS -o /dev/null -w "  rollback health content_type=%{content_type}\n" http://127.0.0.1:8204/api/health || true
  echo "--- 失败日志 ---"
  journalctl -u hivemtk -n 30 --no-pager
  exit 1
fi

echo "=== 4) 接口自证 ==="
curl -fsS http://127.0.0.1:8204/api/health; echo
curl -fsS -o /dev/null -w "  /api/public/config -> %{http_code} %{content_type}\n" http://127.0.0.1:8204/api/public/config || true

echo "=== 5) 启动日志（近 12 行，查 panic/error）==="
journalctl -u hivemtk --since "-90s" --no-pager | tail -12
echo "=== 6) 资源占用 ==="
systemctl status hivemtk --no-pager | head -6
