#!/usr/bin/env bash
# =============================================================================
# rollback-tenant-p0.sh — 多租户 P0 回滚演练（2026-10-11）
#
# 回滚分三层，从"无损秒级"到"彻底还原"：
#   L1 开关旁路（秒级，不回滚代码）：TENANT_ISOLATION=off ⇒ scope.TeamScoped
#      全部直通，系统行为回到 2026-10-11 之前（全共享）。生产改 .env 后
#      `systemctl restart hivemtk` 生效。
#   L2 数据还原（把归属抹平）：回填把存量行归入默认团队；本脚本可把
#      system_users/业务表的 team_id 全部清回 0（回到"平台侧全见"语义）。
#   L3 代码回滚：回退二进制（/opt/hivemtk/user-server.bak.latest，step4 部署
#      自带备份）——teams 表与 team_id 列对旧二进制不可见，无需删列。
#
# 用法：
#   bash scripts/rollback-tenant-p0.sh --dry-run            # 演练：只打印将执行的 SQL/步骤
#   bash scripts/rollback-tenant-p0.sh --level 1            # L1（本机 .env 提示 + 服务器命令打印）
#   bash scripts/rollback-tenant-p0.sh --level 2 --execute  # L2 数据抹平（需 --execute 确认）
# =============================================================================
set -e

LEVEL=1
EXECUTE=0
case "${1:-}" in
  --dry-run) LEVEL=1 ;;
  --level) LEVEL="${2:-1}"; [ "${3:-}" = "--execute" ] && EXECUTE=1 ;;
  *) ;;
esac

echo "== 多租户 P0 回滚演练（level=$LEVEL execute=$EXECUTE）=="

echo "
[L1] 开关旁路（推荐首选，秒级生效、无数据变更）：
  1) 编辑生产机 /opt/hivemtk/.env：TENANT_ISOLATION=off
  2) systemctl restart hivemtk
  3) 验证：curl -s http://127.0.0.1:8204/api/health（应 ok）
  语义：scope.TeamScoped / Create 回调全部直通；teams 表保留但不再参与过滤。"

SQL_L2=$(cat <<'EOF'
-- L2 数据抹平（谨慎：抹平后默认团队归属丢失，重新启用隔离需重跑回填）
UPDATE system_users SET team_id = 0 WHERE role <> 'admin';
UPDATE customers            SET team_id = 0 WHERE team_id <> 0;
UPDATE inbox_conversations  SET team_id = 0 WHERE team_id <> 0;
UPDATE message_hub          SET team_id = 0 WHERE team_id <> 0;
UPDATE clues                SET team_id = 0 WHERE team_id <> 0;
UPDATE opportunities        SET team_id = 0 WHERE team_id <> 0;
UPDATE rag_products         SET team_id = 0 WHERE team_id <> 0;
UPDATE kb_documents         SET team_id = 0 WHERE team_id <> 0;
EOF
)

echo "
[L2] 数据抹平 SQL（默认团队保留在 teams 表，不动）：$SQL_L2"

if [ "$LEVEL" = "2" ]; then
  if [ "$EXECUTE" != "1" ]; then
    echo "
（dry-run：以上 SQL 未执行。加 --execute 才会真的跑。）"
  else
    PW=$(grep -m1 '^POSTGRES_PASSWORD=' /opt/hivemtk/.env | cut -d= -f2-)
    docker exec -e PGPASSWORD="$PW" mtk-postgres psql -U admin -p 8202 -d user_db -c "$SQL_L2"
    echo "L2 完成。"
  fi
fi

echo "
[L3] 代码回滚（仅当需要回退功能本身）：
  cp -a /opt/hivemtk/user-server.bak.latest /opt/hivemtk/user-server
  systemctl restart hivemtk
  说明：teams 表 / 各业务表 team_id 列 / mcp_credential_keys 对旧二进制不可见，
  无需 DROP 列（AutoMigrate 只加不删的另一半：旧版本忽略新列）。"
echo "
演练结论口径：L1 秒级止血 → L2 数据抹平（可选）→ L3 回退二进制（最后手段）。"
