#!/usr/bin/env bash
# =============================================================================
# bootstrap.sh
# user-server 一键初始化脚本 —— 固化所有手动初始化步骤，避免每次失效
#
# 适用场景:
#   1. 全新部署：docker compose up -d 起数据层（PG/Redis）+ 宿主机起 user-server 后跑一次
#   2. 容器重建/数据卷保留：保持数据一致
#   3. 升级 seed：重新跑可幂等更新种子数据
#
# 用法:
#   bash scripts/bootstrap.sh
#
# 步骤:
#   1. 等待 user-server 健康（/health 200）
#   2. 跑 schema 修复迁移（027 user_blacklist + 028 customer_tags）
#   3. 创建默认 admin 账号与演示坐席（实际由步骤 4 的 Go seed 写入，口令取 SEED_PASSWORD）
#   4. 跑 Go seed 全模块（10 模块，含 11 智能体 + 10 绑定）
#   5. 跑 Python 知识库种子（hivemtk 产品 hivemtk-platform-cs）
#   6. 核对安装态：install.lock 已 initialized 才算装完，否则本脚本失败退出
#
# 环境变量:
#   （常规做法：set -a && . ./.env && set +a 后执行本脚本）
#   POSTGRES_PASSWORD  必须（与 docker-compose.yml USER_POSTGRES_PASSWORD 一致）
#   JWT_SECRET 或 USER_JWT_SECRET
#                      必须且只需一把，生成方式 openssl rand -hex 32。
#                      代码先读 USER_JWT_SECRET、为空才回落到 JWT_SECRET
#                      （internal/pkg/utils/jwt.go），历史上这里却要求两把同时非空。
#                      本脚本不再为这几把密钥内置任何默认值：历史版本曾把与部署机
#                      .env 相同的真实密钥写成 ${VAR:-<40+位hex>} 兜底，随公开仓库
#                      一并外泄；现由 scripts/check-secrets.sh 的 A 项比对守死。
#                      注：历史版本还强制要求 PLATFORM_LICENSE_SECRET（商户授权签名密钥）。
#                      2026-09 授权链路下线后两侧 Go 代码均已无任何读取点，故不再要求设置；
#                      旧 .env 里残留该键不影响运行，可自行删除。
#   MERCHANT_API_SECRET
#                      仅 PLATFORM_ENABLED=true 时必须：它只被 config/platform.yaml 的
#                      出站 HMAC 签名消费，平台端关态（离线部署默认）下整条链路不装配，
#                      没有它 seed 与 user-server 都照常跑，故不再拦安装。
#   USER_SERVER_PORT   可选，默认 8204
#   PG_PORT            可选，默认 8232
#   ADMIN_USERNAME     可选，默认 admin
#   ADMIN_PASSWORD / SEED_PASSWORD
#                      可选，默认 Seed@123456 —— 这个默认值随开源仓库公开，
#                      保留它只是为了 e2e/幂等重跑不破；对外暴露的安装应显式设置。
#                      两者取其一即可：SEED_PASSWORD 优先，未设时继承 ADMIN_PASSWORD，
#                      Go seed 与步骤 7 的登录校验用同一个值（历史上 ADMIN_PASSWORD
#                      只影响登录校验、不影响 seed 实际写入口令，设了也白设）。
#   INSTALL_LOCK_PATH  可选，本脚本不设置它，但它决定步骤 6 核对的是哪个文件：
#                      未设时 user-server 把 install.lock 写在**自己的进程 CWD**（./install.lock），
#                      从哪个目录启动就写在哪个目录，换目录重启＝读不到旧锁、铸一枚新 install_id，
#                      平台侧（PLATFORM_ENABLED=true）会把它记成一个新装商户。
#                      生产请设成绝对路径并随实例持久化，详见 .env-example 与
#                      docs/operations/MERCHANT_INITIALIZATION_FLOW.md §四「落盘位置」。
# =============================================================================

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
USER_SERVER_DIR="$PROJECT_DIR/user-server"

USER_SERVER_PORT="${USER_SERVER_PORT:-8204}"
PG_PORT="${PG_PORT:-8232}"
PG_HOST="${PG_HOST:-127.0.0.1}"
ADMIN_USERNAME="${ADMIN_USERNAME:-admin}"
# 口令只有一个事实源：SEED_PASSWORD（未设时取 ADMIN_PASSWORD，再未设时是公开默认值）。
# 两者历史上各走各的——ADMIN_PASSWORD 只用于步骤 7 登录校验，Go seed 另用自己写死的常量，
# 于是"设了 ADMIN_PASSWORD 却仍被种成默认口令"。这里合一后，seed 写入与登录校验必然同值。
SEED_PASSWORD="${SEED_PASSWORD:-${ADMIN_PASSWORD:-Seed@123456}}"
ADMIN_PASSWORD="$SEED_PASSWORD"
export SEED_PASSWORD
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
# JWT 两把取其一即可（代码先读 USER_JWT_SECRET，为空才回落到 JWT_SECRET）
if [ -z "${JWT_SECRET:-}" ] && [ -z "${USER_JWT_SECRET:-}" ]; then
  err "JWT_SECRET 与 USER_JWT_SECRET 至少设置一把（生成一个：openssl rand -hex 32）"
  exit 1
fi
# 平台端关态（离线部署默认）下不拦安装：MERCHANT_API_SECRET 只服务平台出站签名链路
if [ "${PLATFORM_ENABLED:-false}" = "true" ] && [ -z "${MERCHANT_API_SECRET:-}" ]; then
  err "PLATFORM_ENABLED=true 需要 MERCHANT_API_SECRET（生成一个：openssl rand -hex 32）"
  exit 1
fi
command -v psql >/dev/null || { err "psql 未安装"; exit 1; }
command -v go   >/dev/null || { err "go  未安装"; exit 1; }
command -v python3 >/dev/null || { err "python3 未安装"; exit 1; }

# 演示口令告警：默认值 Seed@123456 随开源仓库公开（config.yaml / docs / e2e 里都写着），
# 保留它是为了幂等重跑与 e2e，不代表它可以出现在对外可达的安装上。
# 按小写形匹配（bash 3.2 没有 ${VAR,,}，用 tr）：否则 Admin123 这类大小写变体绕过告警。
_seed_pw_lc=$(printf '%s' "$SEED_PASSWORD" | tr '[:upper:]' '[:lower:]')
case "$_seed_pw_lc" in
  seed@123456)
    warn "演示/admin 口令用的是仓库公开的默认值 Seed@123456；"
    warn "  对外可达的安装请显式设置：SEED_PASSWORD=\"<自定口令>\" bash scripts/bootstrap.sh"
    warn "  （或只设 ADMIN_PASSWORD，两者在本脚本里同值）"
    warn "  注意：这条只对「新装 / 库里还没有 admin 行」的实例生效。存量实例上 admin(id=1) 的口令被"
    warn "  system_users 上的 v3_36.0 守卫触发器锁死（改/删/停用一律拒），重跑本脚本换不掉它——"
    warn "  见 docs/DEPLOYMENT_GUIDE.md §6.2 末「换掉已经装好的那台」注记里的两条可行路径。"
    ;;
  admin123|admin888|123456|654321|admin|root|password|abc123|iloveyou)
    # 这一档不是仓库公开值，而是字典攻击第一轮就命中的常见口令。单独提醒的理由是绑定的地址：
    # 2026-09-22 本机 `lsof -nP -iTCP -sTCP:LISTEN` 实测 user-server 听 *:8204、platform-server 听 *:8205
    # （`*` 而非 127.0.0.1 ⇒ 同一局域网内任何设备都能直接打到登录页，弱口令在这里不是"不够好"而是"已开门"）。
    warn "admin 口令落在常见弱口令名单里（首轮字典即命中）；"
    warn "  本机 dev 栈的服务端口监听的是 *（局域网可达），不只是 127.0.0.1；"
    warn "  要么换成强口令（SEED_PASSWORD=\"<自定强口令>\" bash scripts/bootstrap.sh；存量实例重跑换不掉 id=1，"
    warn "  见本脚本上一档的注意与 docs/DEPLOYMENT_GUIDE.md §6.2 末注记），"
    warn "  要么把服务收回到 127.0.0.1 再跑。"
    ;;
esac

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
SEED_PASSWORD="$SEED_PASSWORD" \
go run ./cmd/seed 2>&1 | grep -E "SEED|完成|✓|✗" | sed 's/^/  /'

# 5) 跑 Python 知识库种子
log "执行 Python 知识库种子（hivemtk 产品 ${HIVEMTK_RAG_PRODUCT_ID}）..."
for py in expand_knowledge_base.py expand_knowledge_base_batch2.py expand_knowledge_base_batch3.py; do
    if [ -f "$PROJECT_DIR/scripts/seed/$py" ]; then
        log "  -> $py"
        python3 "$PROJECT_DIR/scripts/seed/$py" 2>&1 | tail -3 | sed 's/^/    /'
    fi
done

# 6) 核对安装态（install.lock 是否已 initialized）
# 唯一真相源是 user-server 自己的 init-status：它读的就是这个进程实际生效的那份 install.lock，
# 并把"库里有超管但锁不在"的情况就地回填（见 MERCHANT_INITIALIZATION_FLOW.md §三）。
# 这里过去还查过 `docker exec mtk-user-server test -f /app/data/install.lock`——那个容器从来不存在
# （user-server/Dockerfile 已随 94415060 删除，compose 里只有 PG/Redis），所以那条分支恒为假、
# 只会打一句 warn 让脚本继续报"✅ 完成"。现在按安装态本身判，没装成就是失败。
log "检查 install.lock..."
INIT_STATUS=$(curl -fsS "http://127.0.0.1:${USER_SERVER_PORT}/api/system/init-status" 2>/dev/null || echo "{}")
if echo "$INIT_STATUS" | grep -q '"initialized":true'; then
    log "  install.lock 已就绪（initialized=true）"
else
    err "  安装态未就绪：/api/system/init-status 返回 ${INIT_STATUS}"
    err "  可能原因：① 步骤 4 的 seed 没建出 role=admin 的超管；② user-server 进程写不了它的 install.lock。"
    err "  install.lock 默认写在 user-server 的进程 CWD（./install.lock）；生产请显式设 INSTALL_LOCK_PATH"
    err "  为绝对路径并随实例持久化，否则换目录启动会铸出新 install_id、平台侧按新装实例记账。"
    exit 1
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
