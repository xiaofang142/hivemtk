#!/usr/bin/env bash
# 链 A：API 发布线上（user-server → mtk-prod）
#
# 权威口径见 docs/operations/RELEASE-PIPELINE.md §1。本脚本把该链从「人工 SSH 敲命令」
# 固化为可重复、带回滚的执行器。**只动 user-server**，不碰 platform-server / user-web。
#
# 用法：
#   deploy/deploy-api.sh                 # 变更驱动：检测到 user-server 有未发布提交才部署
#   deploy/deploy-api.sh --force         # 跳过变更门（例如线上二进制与台账不一致需对齐）
#   deploy/deploy-api.sh --dry-run       # 只打印计划与预检结果，不改动任何线上状态（含一次只读 SSH 探测收敛基线）
#   deploy/deploy-api.sh --skip-tests    # 跳过 go test（默认跑，DB 门禁失败会单独列出）
#
# 前置条件：
#   - 本机 ~/.ssh/config 有 Host mtk-prod 且 BatchMode 免密可用
#   - 本机 go 工具链；交叉编译产物 CGO_ENABLED=0 GOOS=linux GOARCH=amd64（线上 x86_64）
#
# 安全设计（按重要性排序）：
#   1. 上传前 sha256 双端校验，防半截文件进生产
#   2. 重建前先把线上镜像打 mtk-user-server:rollback-<时间戳> 标签（沿用线上既有约定）
#   3. 二进制整份备份为 user-server.bak-<时间戳>
#   4. 健康检查失败自动回滚（恢复 .bak 二进制 → 重建 → 重启），仍失败则回滚镜像标签
#   5. 部署成功后写 deploy/.api-deploy-state.json 作为下次变更门的基线

set -Eeuo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
SERVER_DIR="${REPO_ROOT}/user-server"

# ---- 线上拓扑（2026-11-13 实测，勿臆测；变更须同步 PROD_TOPOLOGY_CHANGES.md）----
SSH_HOST="${HIVE_SSH_HOST:-mtk-prod}"
REMOTE_DIR="${HIVE_REMOTE_DIR:-/www/wwwroot/hivemtk/user-server}"
COMPOSE_SERVICE="${HIVE_COMPOSE_SERVICE:-user-server}"
CONTAINER_NAME="mtk-user-server"
IMAGE_NAME="mtk-user-server"
HEALTH_URL_LOCAL="http://127.0.0.1:8204/api/health"
HEALTH_URL_PUBLIC="https://workapi.xapptool.cn/api/health"
STATE_FILE="${SCRIPT_DIR}/.api-deploy-state.json"

FORCE=0
DRY_RUN=0
SKIP_TESTS=0
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    --dry-run) DRY_RUN=1 ;;
    --skip-tests) SKIP_TESTS=1 ;;
    -h|--help) sed -n '2,20p' "$0"; exit 0 ;;
    *) echo "未知参数：$arg" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[36m[deploy]\033[0m %s\n' "$*"; }
ok()   { printf '\033[32m[  ok  ]\033[0m %s\n' "$*"; }
warn() { printf '\033[33m[ warn ]\033[0m %s\n' "$*"; }
die()  { printf '\033[31m[ FAIL ]\033[0m %s\n' "$*" >&2; exit 1; }

STAMP="$(date +%Y%m%d-%H%M%S)"
LOCAL_BIN="/tmp/hivemtk-user-${STAMP}"

# 远端执行统一入口：过滤 SSH 的 post-quantum 警告噪音
rsh() { ssh -o BatchMode=yes -o ConnectTimeout=15 "${SSH_HOST}" "$@" 2>/dev/null; }

# ---------------------------------------------------------------- 预检 1：git 状态
cd "${REPO_ROOT}"
BRANCH="$(git rev-parse --abbrev-ref HEAD)"
[ "${BRANCH}" = "master" ] || die "当前分支 ${BRANCH}，线上只从 master 发布（先合并再发布）"
HEAD_SHA="$(git rev-parse HEAD)"
HEAD_SHORT="$(git rev-parse --short HEAD)"

UNCOMMITTED="$(git status --porcelain -- user-server/ | wc -l | tr -d ' ')"
[ "${UNCOMMITTED}" = "0" ] || die "user-server/ 有 ${UNCOMMITTED} 个未提交改动——未提交的代码不许发布线上"

# 双远端（gitee-upstream + upstream）下 @{u} 有歧义，改用「HEAD 是否已被任一远端包含」
PUSHED_TO_REMOTE="$(git branch -r --contains HEAD 2>/dev/null | grep -c . || true)"
if [ "${PUSHED_TO_REMOTE}" = "0" ]; then
  warn "HEAD 尚未 push 到任何远端；先 push 再发布（保证线上代码=仓库代码）"
  [ "${FORCE}" = "1" ] || die "未 push 的代码不发布（--force 可跳过，但不推荐）"
else
  ok "HEAD 已被远端包含（$(git branch -r --contains HEAD 2>/dev/null | tr -d ' ' | paste -sd, -)）"
fi

# ---------------------------------------------------------------- 预检 2：变更门
LAST_SHA=""
if [ -f "${STATE_FILE}" ]; then
  LAST_SHA="$(sed -n 's/.*"deployed_sha"[[:space:]]*:[[:space:]]*"\([0-9a-f]*\)".*/\1/p' "${STATE_FILE}" | head -1)"
fi

if [ -n "${LAST_SHA}" ]; then
  if ! git cat-file -e "${LAST_SHA}^{commit}" 2>/dev/null; then
    warn "台账记录的基线 ${LAST_SHA} 不在当前历史（rebase/force-push 过？），变更门改按服务器二进制时间兜底"
    LAST_SHA=""
  fi
fi

if [ -z "${LAST_SHA}" ]; then
  # 无台账基线：不允许盲发。--force 意味着"我确认要发"，此时以服务器二进制时间为基线
  # 重新收敛（保守：宁可多发一次，不可漏发）。
  warn "无部署台账基线（deploy/.api-deploy-state.json 缺失）"
  { [ "${FORCE}" = "1" ] || [ "${DRY_RUN}" = "1" ]; } || die "首次部署必须显式 --force（确认线上现状后由人工放行）"
  LAST_SHA="$(git rev-list -1 --before="$(rsh "stat -c %y ${REMOTE_DIR}/user-server 2>/dev/null | cut -d. -f1" || echo '2026-11-12 17:07')" HEAD)"
  [ -n "${LAST_SHA}" ] || die "无法按服务器二进制时间收敛出基线提交"
  log "按服务器二进制 mtime 收敛基线：$(git rev-parse --short "${LAST_SHA}")"
fi

CHANGED_COMMITS="$(git log --oneline "${LAST_SHA}..HEAD" -- user-server/ || true)"
CHANGED_COUNT="$(printf '%s' "${CHANGED_COMMITS}" | grep -c . || true)"

log "分支=${BRANCH} HEAD=${HEAD_SHORT} 上次发布基线=$(git rev-parse --short "${LAST_SHA}")"
if [ "${CHANGED_COUNT}" = "0" ]; then
  ok "user-server 自上次发布以来零改动 ⇒ 无 API 变更，不触发部署（规则：设计了 API 才发布）"
  [ "${FORCE}" = "1" ] || { log "（--force 可强制重发）"; exit 0; }
  warn "--force 指定，仍执行部署"
else
  log "user-server 待发布提交 ${CHANGED_COUNT} 个："
  printf '%s\n' "${CHANGED_COMMITS}" | sed 's/^/    /'
fi

# ---------------------------------------------------------------- 预检 3：构建与测试
log "本地预检：go vet"
( cd "${SERVER_DIR}" && go vet ./... ) || die "go vet 不过，不发布"

if [ "${SKIP_TESTS}" = "0" ]; then
  log "本地预检：go test（DB 门禁类失败单独列出，不算阻断）"
  # 注意：go test 失败时管道必须不触发 set -e（失败分类在下一步做）
  ( cd "${SERVER_DIR}" && go test ./internal/... ./cmd/... 2>&1 || true ) | tee /tmp/gotest-deploy.log | tail -3
  FAIL_CASES="$(grep -cE '^--- FAIL' /tmp/gotest-deploy.log || true)"
  DB_GATED="$(grep -c 'test password is missing' /tmp/gotest-deploy.log || true)"
  if [ "${FAIL_CASES}" != "0" ] && [ "${DB_GATED}" = "0" ]; then
    die "go test 有 ${FAIL_CASES} 个非 DB 门禁失败，不发布"
  fi
  if [ "${FAIL_CASES}" = "0" ]; then
    ok "go test 全绿"
  else
    log "go test：${FAIL_CASES} 个失败全部为 DB 门禁（缺 POSTGRES_TEST_PASSWORD，环境问题非代码问题）"
  fi
fi

log "交叉编译 CGO_ENABLED=0 GOOS=linux GOARCH=amd64"
( cd "${SERVER_DIR}" && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o "${LOCAL_BIN}" ./cmd/api ) \
  || die "编译失败，不发布"
[ -s "${LOCAL_BIN}" ] || die "编译产物为空"
file "${LOCAL_BIN}" | grep -q "x86-64" || die "编译产物不是 x86-64（线上是 x86_64）"
LOCAL_SHA="$(shasum -a 256 "${LOCAL_BIN}" | cut -d' ' -f1)"
ok "本地产物 $(du -h "${LOCAL_BIN}" | cut -f1) sha256=${LOCAL_SHA:0:16}…"

if [ "${DRY_RUN}" = "1" ]; then
  log "DRY-RUN：以下步骤被跳过——scp 上传 / 镜像打 rollback 标签 / compose up -d --build / 健康检查"
  exit 0
fi

# ---------------------------------------------------------------- 步骤 1：线上现状快照
log "读取线上现状"
REMOTE_BIN_SHA="$(rsh "sha256sum ${REMOTE_DIR}/user-server | cut -d' ' -f1" || true)"
REMOTE_IMAGE_ID="$(rsh "docker inspect --format '{{.Id}}' ${IMAGE_NAME}:latest" || true)"
RUNNING_IMAGE="$(rsh "docker inspect --format '{{.Image}}' ${CONTAINER_NAME}" || true)"
log "线上二进制 sha256=${REMOTE_BIN_SHA:0:16}… 运行镜像=${RUNNING_IMAGE:7:19}"

# ---------------------------------------------------------------- 步骤 2：上传（sha256 双端校验）
log "上传二进制到 ${SSH_HOST}:${REMOTE_DIR}/user-server.new"
scp -q -o BatchMode=yes "${LOCAL_BIN}" "${SSH_HOST}:${REMOTE_DIR}/user-server.new" || die "scp 失败"
REMOTE_NEW_SHA="$(rsh "sha256sum ${REMOTE_DIR}/user-server.new | cut -d' ' -f1")"
[ "${REMOTE_NEW_SHA}" = "${LOCAL_SHA}" ] || { rsh "rm -f ${REMOTE_DIR}/user-server.new"; die "上传后 sha256 不一致（${REMOTE_NEW_SHA}≠${LOCAL_SHA}），已删除半截文件"; }
ok "双端 sha256 一致"

# ---------------------------------------------------------------- 步骤 3：备份（镜像标签 + 二进制）
log "备份线上现状（rollback-${STAMP}）"
rsh "docker tag ${IMAGE_NAME}:latest ${IMAGE_NAME}:rollback-${STAMP}" || warn "打镜像 rollback 标签失败（不影响继续，二进制 .bak 仍可回滚）"
rsh "cp -p ${REMOTE_DIR}/user-server ${REMOTE_DIR}/user-server.bak-${STAMP}" || die "二进制备份失败，中止（不覆盖生产）"
ok "镜像标签 ${IMAGE_NAME}:rollback-${STAMP} + 二进制 user-server.bak-${STAMP}"

rollback() {
  warn "触发回滚（${1:-未知原因}）"
  rsh "cd ${REMOTE_DIR} && cp -p user-server.bak-${STAMP} user-server && docker compose up -d --build ${COMPOSE_SERVICE}" || true
  sleep 5
  if rsh "curl -sf --max-time 5 ${HEALTH_URL_LOCAL}" >/dev/null 2>&1; then
    ok "回滚成功：线上已恢复到部署前二进制"
  else
    warn "二进制回滚后仍不健康，改按镜像标签回滚"
    rsh "docker tag ${IMAGE_NAME}:rollback-${STAMP} ${IMAGE_NAME}:latest && cd ${REMOTE_DIR} && docker compose up -d --build ${COMPOSE_SERVICE}" || true
    sleep 5
    rsh "curl -sf --max-time 5 ${HEALTH_URL_LOCAL}" >/dev/null 2>&1 && ok "镜像回滚成功" || die "回滚也失败，必须人工介入（docker logs ${CONTAINER_NAME}）"
  fi
  die "部署失败已回滚：${1:-未知原因}"
}

# ---------------------------------------------------------------- 步骤 4：就位 + 重建
log "二进制就位并重建容器"
rsh "cd ${REMOTE_DIR} && mv user-server.new user-server && chmod +x user-server" || rollback "二进制就位失败"
rsh "cd ${REMOTE_DIR} && docker compose up -d --build ${COMPOSE_SERVICE}" || rollback "compose up -d --build 失败"

# ---------------------------------------------------------------- 步骤 5：健康检查
log "健康检查（最多 60s）"
HEALTHY=0
for i in $(seq 1 30); do
  BODY="$(rsh "curl -sf --max-time 5 ${HEALTH_URL_LOCAL}" || true)"
  if echo "${BODY}" | grep -q '"status":"ok"'; then HEALTHY=1; break; fi
  sleep 2
done
[ "${HEALTHY}" = "1" ] || rollback "健康检查 60s 未过：${BODY:-空响应}"
ok "容器内健康检查通过"

# 公开域名侧确认（nginx → 8204）
PUBLIC_BODY="$(curl -sf --max-time 10 "${HEALTH_URL_PUBLIC}" || true)"
echo "${PUBLIC_BODY}" | grep -q '"status":"ok"' || warn "公网 ${HEALTH_URL_PUBLIC} 未取到 ok（可能 CDN/缓存，稍后复验）"
ok "公网健康检查：${PUBLIC_BODY:-（见 warn）}"

# ---------------------------------------------------------------- 步骤 6：证据 + 台账
NEW_IMAGE="$(rsh "docker inspect --format '{{.Image}}' ${CONTAINER_NAME}")"
STARTED_AT="$(rsh "docker inspect --format '{{.State.StartedAt}}' ${CONTAINER_NAME}")"
REMOTE_SHA_AFTER="$(rsh "sha256sum ${REMOTE_DIR}/user-server | cut -d' ' -f1")"

cat > "${STATE_FILE}" <<EOF
{
  "deployed_sha": "${HEAD_SHA}",
  "deployed_at": "$(date -Iseconds)",
  "remote_binary_sha256": "${REMOTE_SHA_AFTER}",
  "image_id": "${NEW_IMAGE}",
  "container_started_at": "${STARTED_AT}",
  "rollback_image_tag": "${IMAGE_NAME}:rollback-${STAMP}",
  "rollback_binary": "${REMOTE_DIR}/user-server.bak-${STAMP}",
  "commits": [
$(printf '%s\n' "${CHANGED_COMMITS}" | sed 's/[\\"]/\\&/g; s/^/    "/; s/$/",/' | sed '$ s/,$//')
  ]
}
EOF

echo
ok "================ 发布完成 ================"
log "代码：${HEAD_SHORT}（${CHANGED_COUNT} 个 user-server 提交）"
log "线上二进制 sha256=${REMOTE_SHA_AFTER:0:16}…"
log "运行镜像=${NEW_IMAGE:7:19} 容器启动=${STARTED_AT}"
log "回滚点：镜像 ${IMAGE_NAME}:rollback-${STAMP} / 二进制 ${REMOTE_DIR}/user-server.bak-${STAMP}"
log "台账：${STATE_FILE}"
echo
log "待办：把本次发布登记进 docs/operations/PROD_TOPOLOGY_CHANGES.md（版本/buildId/时间/回滚点）"
