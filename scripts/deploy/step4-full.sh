#!/usr/bin/env bash
# 生产机部署 step4：完整一键部署（备份+原子换二进制+重启+msg_hourly_summary 建表验证+前端+接口自证）
# 等价于 step1+step2+step3 合并，另加 table AutoMigrate 验证；唯一依赖是两端产物已上传
set -e
TS=$(date +%Y%m%d_%H%M%S)
echo "=== 1) 备份当前二进制 + 前端 ==="
cp -a /opt/hivemtk/user-server /opt/hivemtk/user-server.bak.$TS
cp -a /opt/hivemtk/user-server /opt/hivemtk/user-server.bak.latest
tar czf /root/hivemtk-web-$TS.tgz -C /var/www/hivemtk .
cp -a /root/hivemtk-web-$TS.tgz /root/hivemtk-web-backup-latest.tgz
ls -la /opt/hivemtk/user-server.bak.$TS /root/hivemtk-web-$TS.tgz

echo "=== 2) 记录部署前 msg_hourly_summary 是否存在 ==="
PW=$(grep -m1 '^POSTGRES_PASSWORD=' /opt/hivemtk/.env | cut -d= -f2-)
before=$(docker exec -e PGPASSWORD="$PW" mtk-postgres psql -U admin -p 8202 -d user_db -tAc \
  "select case when exists(select 1 from information_schema.tables where table_name='msg_hourly_summary') then 'EXISTS' else 'MISSING' end;")
echo "  before = $before"

echo "=== 3) 原子替换 + 重启 ==="
chmod 0755 /opt/hivemtk/user-server.new
mv -f /opt/hivemtk/user-server.new /opt/hivemtk/user-server
md5sum /opt/hivemtk/user-server
systemctl restart hivemtk
ok=0
for i in $(seq 1 40); do
  ct=$(curl -fsS -o /dev/null -w "%{content_type}" http://127.0.0.1:8204/api/health 2>/dev/null || true)
  case "$ct" in application/json*) ok=1; echo "  health OK ${i}x2s"; break ;; esac
  sleep 2
done
if [ "$ok" != "1" ]; then
  echo "  !! 健康检查失败，回滚"
  cp -a /opt/hivemtk/user-server.bak.latest /opt/hivemtk/user-server
  systemctl restart hivemtk
  journalctl -u hivemtk -n 25 --no-pager
  exit 1
fi

echo "=== 4) 部署前端 ==="
tar xzf /root/user-web-dist.tgz -C /var/www/hivemtk
rm -f /root/user-web-dist.tgz
cat /var/www/hivemtk/version.json; echo

echo "=== 5) 验证建表（AutoMigrate 应已创建 msg_hourly_summary）==="
for i in $(seq 1 10); do
  after=$(docker exec -e PGPASSWORD="$PW" mtk-postgres psql -U admin -p 8202 -d user_db -tAc \
    "select case when exists(select 1 from information_schema.tables where table_name='msg_hourly_summary') then 'EXISTS' else 'MISSING' end;")
  echo "  after(${i}) = $after"
  [ "$after" = "EXISTS" ] && break
  sleep 3
done
echo "--- 表结构 ---"
docker exec -e PGPASSWORD="$PW" mtk-postgres psql -U admin -p 8202 -d user_db -tAc \
  "select column_name||' '||data_type from information_schema.columns where table_name='msg_hourly_summary' order by ordinal_position;"
echo "--- 唯一索引（ON CONFLICT 目标）---"
docker exec -e PGPASSWORD="$PW" mtk-postgres psql -U admin -p 8202 -d user_db -tAc \
  "select indexname from pg_indexes where tablename='msg_hourly_summary';"

echo "=== 6) 接口自证 ==="
curl -fsS http://127.0.0.1:8204/api/health; echo
for p in /api/capabilities /api/kb-connectors /api/mcp/credentials; do
  code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8204$p")
  echo "  $p -> $code (401=路由在且需鉴权)"
done
