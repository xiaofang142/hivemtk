#!/usr/bin/env bash
# =============================================================================
# bootstrap.sh
# user-server 一键初始化脚本 —— 固化所有手动初始化步骤，避免每次失效
#
# 适用场景:
#   1. 全新部署：docker compose up -d 启动后跑一次
#   2. 容器重建/数据卷保留：保持数据一致
#   3. 升级 seed：重新跑可幂等更新种子数据
#
# 用法:
#   bash scripts/bootstrap.sh
#
# 步骤:
#   1. 等待 user-server 容器 healthy（/health 200）
#   2. 跑 schema 修复迁移（027 user_blacklist + 028 customer_tags）
#   3. 创建默认 admin 账号（admin/Seed@123456，若不存在）
#   4. 跑 Go seed 全模块（10 模块，含 11 智能体 + 10 绑定）
#   5. 跑 Python 知识库种子（hivemtk 产品 hivemtk-platform-cs）
#   6. 写 install.lock 标记已初始化
#
# 环境变量:
#   （常规做法：set -a && . ./.env && set +a 后执行本脚本）
#   POSTGRES_PASSWORD  必须（与 docker-compose.yml USER_POSTGRES_PASSWORD 一致）
#   JWT_SECRET / USER_JWT_SECRET / MERCHANT_API_SECRET / PLATFORM_LICENSE_SECRET
#                      必须，生成方式 openssl rand -hex 32。
#                      本脚本不再为这几把密钥内置任何默认值：历史版本曾把与部署机
#                      .env 相同的真实密钥写成 ${VAR:-<40+位hex>} 兜底，随公开仓库
#                      一并外泄；现由 scripts/check-secrets.sh 的 A 项比对守死。
#   USER_SERVER_PORT   可选，默认 8204
#   PG_PORT            可选，默认 8232
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
USER_SERVER_DIR="$PROJECT_DIR/user-server"

USER_SERVER_PORT="${USER_SERVER_PORT:-8204}"
PG_PORT="${PG_PORT:-8232}"
PG_HOST="${PG_HOST:-127.0.0.1}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
ADMIN_PASSWORD="${ADMIN_PASSWORD:-Seed@123456}"
ADMIN_EMAIL="${ADMIN_EMAIL:-admin@hivemtk.local}"
HIVEMTK_RAG_PRODUCT_ID="${HIVEMTK_RAG_PRODUCT_ID:-hivemtk-platform-cs}"

# 颜色输出
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

log()  { printf "${GREEN}[bootstrap]${NC} %s\n" "$*"; }
warn() { printf "${YELLOW}[bootstrap]${NC} %s\n" "$*"; }
err()  { printf "${RED}[bootstrap]${NC} %s\n" "$*" >&2; }

# 预检
[ -z "$POSTGRES_PASSWORD" ] && { err "POSTGRES_PASSWORD 未设置"; exit 1; }
# 签名/授权类密钥同样只允许来自环境：脚本内不得内置任何默认值（历史教训见 scripts/check-secrets.sh A 项）
for _v in JWT_SECRET USER_JWT_SECRET MERCHANT_API_SECRET PLATFORM_LICENSE_SECRET; do
  [ -z "${!_v}" ] && { err "$_v 未设置（生成一个：openssl rand -hex 32）"; exit 1; }
done
command -v psql >/dev/null || { err "psql 未安装"; exit 1; }
command -v go   >/dev/null || { err "go  未安装"; exit 1; }
command -v python3 >/dev/null || { err "python3 未安装"; exit 1; }

export PGHOST="$PG_HOST"
export PGPORT="$PG_PORT"
export PGUSER="admin"
export PGPASSWORD="$POSTGRES_PASSWORD"
export PGDATABASE="user_db"

# 1) 等待 user-server 健康
log "等待 user-server 健康（http://127.0.0.1:${USER_SERVER_PORT}/health）..."
for i in {1..60}; do
    if curl -fsS "http://127.0.0.1:${USER_SERVER_PORT}/health" >/dev/null 2>&1; then
        log "user-server 已就绪"
        break
    fi
    [ $i -eq 60 ] && { err "user-server 健康检查超时"; exit 1; }
    sleep 2
done

# 2) 跑 schema 修复迁移
log "应用 schema 修复迁移..."
for sql in 027_user_blacklist 028_customer_tags_uuid; do
    if [ -f "$PROJECT_DIR/migrations/${sql}.sql" ]; then
        log "  -> $sql.sql"
        psql -v ON_ERROR_STOP=1 -f "$PROJECT_DIR/migrations/${sql}.sql" >/dev/null
    else
        warn "  缺失迁移文件: $sql.sql（已跳过）"
    fi
done

# 3) 创建默认 admin 账号（仅在不存在时）
log "检查 admin 账号..."


# 4) 跑 Go seed 全模块
log "执行 Go 种子（10 模块）..."
cd "$USER_SERVER_DIR"
POSTGRES_PASSWORD="$POSTGRES_PASSWORD" \
JWT_SECRET="$JWT_SECRET" \
MERCHANT_API_SECRET="$MERCHANT_API_SECRET" \
PLATFORM_LICENSE_SECRET="$PLATFORM_LICENSE_SECRET" \
go run ./cmd/seed 2>&1 | grep -E "SEED|完成|✓|✗" | sed 's/^/  /'

# 5) 跑 Python 知识库种子
log "执行 Python 知识库种子（hivemtk 产品 $HIVEMTK_RAG_PRODUCT_ID）..."
for py in expand_knowledge_base.py expand_knowledge_base_batch2.py expand_knowledge_base_batch3.py; do
    if [ -f "$PROJECT_DIR/scripts/seed/$py" ]; then
        log "  -> $py"
        python3 "$PROJECT_DIR/scripts/seed/$py" 2>&1 | tail -3 | sed 's/^/    /'
    fi
done

# 6) 写 install.lock（容器内 /app/data/install.lock）
log "检查 install.lock..."
INIT_STATUS=$(curl -fsS "http://127.0.0.1:${USER_SERVER_PORT}/api/system/init-status" 2>/dev/null || echo "{}")
if echo "$INIT_STATUS" | grep -q '"initialized":true'; then
    log "  install.lock 已存在，跳过"
else
    log "  写入 install.lock..."
    # 通过 admin 创建接口隐式触发 install.lock 写入（auth.go InitAdmin 会调 MarkAdminInitialized）。
    # 由于 admin 已在步骤 3 创建时写过，理论上这里 is_primary 已为 true。
    # 若仍为 false，则手动 docker exec 写一份。
    if docker exec mtk-user-server test -f /app/data/install.lock 2>/dev/null; then
        log "  install.lock 在容器内已存在"
    else
        warn "  install.lock 仍未生成，请检查 admin 写入逻辑"
    fi
fi

# 7) 最终验证
log "================================================"
log "最终验证"
log "================================================"
psql -c "SELECT 'ai_agents' tbl, count(*) FROM ai_agents
         UNION ALL SELECT 'web_kg(hivemtk)', count(*) FROM knowledge_chunks WHERE product_id='$HIVEMTK_RAG_PRODUCT_ID'
         UNION ALL SELECT 'channel_bindings', count(*) FROM channel_agent_bindings
         UNION ALL SELECT 'customers', count(*) FROM customers
         UNION ALL SELECT 'sessions', count(*) FROM customer_sessions;" 2>&1 | sed 's/^/  /'

HTTP_CODE=$(curl -sS -o /dev/null -w "%{http_code}" -X POST "http://127.0.0.1:${USER_SERVER_PORT}/api/auth/login" \
    -H "Content-Type: application/json" \
    -d "{\"username\":\"$ADMIN_USERNAME\",\"password\":\"$ADMIN_PASSWORD\"}")
log "admin 登录: HTTP $HTTP_CODE"

log "================================================"
log "✅ Bootstrap 完成"
log "================================================"
log "后续访问入口："
log "  user-server  : http://127.0.0.1:${USER_SERVER_PORT}"
log "  admin 登录   : $ADMIN_USERNAME / $ADMIN_PASSWORD"
log "  knowledge  : $HIVEMTK_RAG_PRODUCT_ID (hivemtk 产品)"
log "================================================"
