#!/usr/bin/env bash
# =============================================================
# HiveMtk 用户端 发布脚本
#
# 部署形态（2026-09 起）：默认纯本地。
#   - 本地（宿主机）：user-server 二进制 + PG + Redis + LLM 推理栈
#   - 远端：只有显式给出 DEPLOY_HOST 才会执行前端推送那一段。
#     历史上那台云端主机（含反向代理层与 frps）已随域名一起停用，
#     所以这里刻意不留任何默认目标机 —— 留一个就是让发布脚本往黑洞里 rsync。
#   - API 路径（本地）：浏览器 → http://127.0.0.1:8204/api/…
#
# 部署内容：
#   - user-server → 本地二进制（go build → nohup 重启，127.0.0.1:8204）
#   - user-web 前端 → $DEPLOY_HOST:$WEBROOT_USERWEB/dist/（需显式 DEPLOY_HOST）
#   - embed-sdk     → $DEPLOY_HOST:$WEBROOT_USERWEB/embed-sdk-dist/（同上）
#
# 用法:
#   ./deploy-user.sh --api-only                          # 纯本地：只重启本地 user-server
#   DEPLOY_HOST=<你自己的主机> ./scripts/deploy-user.sh   # 全量：本地 + 推前端到该主机
#   ./deploy-user.sh --web-only                          # 只推前端（同样必须给 DEPLOY_HOST）
#   ./deploy-user.sh --skip-build                        # 跳过本地前端构建
#   ./deploy-user.sh --反向代理层-only                    # 只更新云端 同源托管配置
#   ./deploy-user.sh --dry-run                           # 仅打印命令
# =============================================================
set -euo pipefail

# ---------- 路径 ----------
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

# ---------- 日志 ----------
log()     { echo "[$(date +'%H:%M:%S')] $*"; }
log_warn(){ echo "[$(date +'%H:%M:%S')] WARN: $*" >&2; }
log_err() { echo "[$(date +'%H:%M:%S')] ERROR: $*" >&2; }
die()     { log_err "$*"; exit 1; }

# ---------- 可覆盖变量 ----------
DEPLOY_USER="${DEPLOY_USER:-root}"
# 刻意无默认值：远端步骤必须显式指定目标机（见 preflight 里的 need_deploy_host）
DEPLOY_HOST="${DEPLOY_HOST:-}"
REMOTE="ssh $DEPLOY_USER@$DEPLOY_HOST"

WEBROOT_USERWEB="${WEBROOT_USERWEB:-/www/wwwroot/hivemtk/user-web}"
USER_SERVER_PORT="${USER_SERVER_PORT:-8204}"

REMOTE_DEPLOY_DIR="${REMOTE_DEPLOY_DIR:-/www/wwwroot/hivemtk}"
REMOTE_GIT_REPO="${REMOTE_GIT_REPO:-git@gitee.com:xhpmayun/hivemtk.git}"
REMOTE_GIT_BRANCH="${REMOTE_GIT_BRANCH:-master}"

# 发布后的健康检查打本地端口：域名停用后，"发布是否成功"这件事只有本地这一处能自证
HEALTH_URL="${HEALTH_URL:-http://127.0.0.1:${USER_SERVER_PORT}/api/health}"

SKIP_HEALTHCHECK="${SKIP_HEALTHCHECK:-}"
SKIP_BUILD="${SKIP_BUILD:-}"
DRY_RUN="${DRY_RUN:-}"
MODE="${MODE:-all}"

# ---------- 参数解析 ----------
while [[ $# -gt 0 ]]; do
  case "$1" in
    --web-only)     MODE="web"; shift ;;
    --api-only)     MODE="api"; shift ;;
    --skip-build)   SKIP_BUILD=1; shift ;;
    --反向代理层-only)   MODE="反向代理层"; shift ;;
    --dry-run)      DRY_RUN=1; shift ;;
    -h|--help)
      # 按标记取"用法"段，不写死行号（改注释就不会让 --help 悄悄截半）
      sed -n '/^# 用法:/,/^# =\{10,\}/p' "$0"
      exit 0
      ;;
    *) die "未知参数: $1" ;;
  esac
done

# ---------- 本地命令 ----------
run() {
  if [[ -n "$DRY_RUN" ]]; then echo "[dry-run] $*"; return 0; fi
  bash -c "set -euo pipefail; $*"
}

# ---------- 远端命令 ----------
run_remote() {
  local desc="$1"; shift
  if [[ -n "$DRY_RUN" ]]; then
    echo "[dry-run|remote] $desc -> $REMOTE $*"
    return 0
  fi
  $REMOTE "$@"
}

# ---------- 远端步骤判定 ----------
# web / all 两种模式含"推前端到远端"，必须显式给 DEPLOY_HOST；api 模式纯本地，不需要。
mode_needs_remote() {
  case "$MODE" in
    web|all) return 0 ;;
    *) return 1 ;;
  esac
}

need_deploy_host() {
  if mode_needs_remote && [[ -z "$DRY_RUN" && -z "$DEPLOY_HOST" ]]; then
    die "缺少 DEPLOY_HOST：本次模式（${MODE}）要把前端推到远端，而本脚本不再内置默认目标机。纯本地发布请用 --api-only；确实要推远端请显式指定，例如 DEPLOY_HOST=10.0.0.5 ./scripts/deploy-user.sh"
  fi
}

# ---------- 预检 ----------
preflight() {
  log "########## 预检 ##########"

  for cmd in ssh scp rsync git go; do
    command -v "$cmd" >/dev/null 2>&1 || die "本地命令缺失: $cmd"
  done
  log "  本地命令: ok"

  need_deploy_host

  if [[ -n "$DRY_RUN" ]]; then
    log "  跳过 SSH/本地预检（dry-run）"
    return 0
  fi

  if mode_needs_remote; then
    $REMOTE "echo ok" >/dev/null 2>&1 || die "无法 SSH 到 $DEPLOY_USER@$DEPLOY_HOST"
    log "  SSH 连通: ok"
  else
    log "  本次无远端步骤，跳过 SSH 检查"
  fi

  # 本地 docker（PG + Redis 在本地宿主机运行）
  command -v docker >/dev/null 2>&1 || die "本地命令缺失: docker（数据层 PG+Redis 需要）"
  log "  本地 docker: ok"

  # 本地 go（user-server 二进制编译需要）
  command -v go >/dev/null 2>&1 || die "本地命令缺失: go（user-server 编译需要）"
  log "  本地 go: ok"

  # 本地 .env（user-server 与 docker compose 均依赖）
  if [[ -f "$ROOT/.env" ]]; then
    log "  本地 .env: ok"
  else
    log_warn "本地 $ROOT/.env 不存在，user-server 启动可能失败（DB 密码等缺失）"
  fi
}

# ---------- 本地构建前端 ----------
build_web() {
  [[ "$SKIP_BUILD" == "1" ]] && { log "跳过前端构建（--skip-build）"; return 0; }

  log "########## 构建 user-web 前端 ##########"
  run "cd user-web && npm ci --no-audit --no-fund && npm run build"
  [[ -d user-web/dist ]] || die "user-web 构建失败：未产出 dist/ 目录"
  log "  user-web 构建完成"

  log "########## 构建 embed-sdk ##########"
  run "cd embed-sdk && npm ci --no-audit --no-fund && npm run build"
  [[ -d embed-sdk/dist ]] || die "embed-sdk 构建失败：未产出 dist/ 目录"
  log "  embed-sdk 构建完成"
}

# ---------- 推 dist 到远端 ----------
push_web() {
  log "########## 推送前端资源 ##########"
  run_remote "创建远端目录" "mkdir -p $WEBROOT_USERWEB/dist $WEBROOT_USERWEB/embed-sdk-dist"

  log "==> 推送 user-web/dist -> $WEBROOT_USERWEB/dist/"
  if command -v rsync >/dev/null 2>&1; then
    run "rsync -az --delete --exclude='.user.ini' user-web/dist/ $DEPLOY_USER@$DEPLOY_HOST:$WEBROOT_USERWEB/dist/"
  else
    run_remote "清空远端 dist" "find $WEBROOT_USERWEB/dist -mindepth 1 -name '.user.ini' -prune -o -mindepth 1 -delete"
    run "scp -r user-web/dist/. $DEPLOY_USER@$DEPLOY_HOST:$WEBROOT_USERWEB/dist/"
  fi

  log "==> 推送 embed-sdk/dist -> $WEBROOT_USERWEB/embed-sdk-dist/"
  if command -v rsync >/dev/null 2>&1; then
    run "rsync -az --delete embed-sdk/dist/ $DEPLOY_USER@$DEPLOY_HOST:$WEBROOT_USERWEB/embed-sdk-dist/"
  else
    run_remote "清空远端 embed-sdk-dist" "find $WEBROOT_USERWEB/embed-sdk-dist -mindepth 1 -delete"
    run "scp -r embed-sdk/dist/. $DEPLOY_USER@$DEPLOY_HOST:$WEBROOT_USERWEB/embed-sdk-dist/"
  fi

  log "  前端资源推送完成"
}

# ---------- 本地部署 user-server（宿主机二进制） ----------
deploy_api() {
  log "########## 部署 user-server（本地宿主机二进制）##########"

  # 0) 确保数据层（PG + Redis）在本地运行
  log "==> 检查本地数据层（PG + Redis）"
  run "cd \"$ROOT\" && docker compose up -d"
  log "  本地 PG(127.0.0.1:8202) + Redis(127.0.0.1:8203) 已就绪"

  # 1) 编译 user-server 二进制（本地宿主机原生）
  log "==> 编译 user-server 二进制"
  run "cd \"$ROOT/user-server\" && mkdir -p bin && CGO_ENABLED=0 go build -o bin/user-server ./cmd/api"
  [[ -f "$ROOT/user-server/bin/user-server" ]] || die "user-server 编译失败"
  log "  二进制已生成：$ROOT/user-server/bin/user-server"

  # 2) 停止旧的 user-server 进程
  # 兼容三种历史启动方式：../user-server/bin/user-server、./bin/user-server、
  # 旧版 /tmp/hivemtk-user-server。模式首字符加 [] 是防止匹配到包裹 pkill 的
  # bash -c 命令行本身（自匹配会把部署脚本误杀）；末尾再按端口兜底清理残留监听。
  log "==> 停止旧 user-server 进程"
  run "pkill -f 'user-server/bin/[u]ser-server|[h]ivemtk-user-server|[b]in/user-server' 2>/dev/null || true"
  run "lsof -ti tcp:${USER_SERVER_PORT} 2>/dev/null | xargs kill 2>/dev/null || true"
  run "sleep 1"

  # 3) 启动新 user-server 进程（nohup，加载本地 .env）
  # cwd 必须是 user-server/ —— config.yaml 用相对路径 os.ReadFile("config.yaml")，
  # 否则读不到配置文件会回落到 docker 服务名 postgres-user:8202 导致 DB 连接失败。
  log "==> 启动 user-server（nohup，端口 8204）"
  run "mkdir -p \"$ROOT/user-server/logs\""
  run "cd \"$ROOT\" && set -a && [ -f .env ] && source .env; set +a && cd user-server && nohup ../user-server/bin/user-server > logs/user-server.log 2>&1 &"
  run "sleep 2"

  # 4) 本地健康检查
  log "==> 本地健康检查 127.0.0.1:8204"
  local i=0
  local ct=''
  while (( i < 30 )); do
    # 用 /api/health（非 /health）并校验 Content-Type: application/json，
    # 避免 user-server NoRoute 或 反向代理层 SPA 兜底返回 200+HTML 导致假通过
    # 注意：set -euo pipefail 下避免把 ct=$(...) 与 && 短路链合并，
    # 否则 curl 失败会让整个命令链非零退出导致脚本中断；改用独立 if 包裹。
    ct=$(curl -fsS -o /dev/null -w "%{content_type}" "http://127.0.0.1:8204/api/health" 2>/dev/null || true)
    if [[ "$ct" == application/json* ]]; then
      # 注意：变量后紧跟全角括号‘）’ 时，Bash 会把 ct） 当成变量名，
      # 在 set -u 下报 unbound variable；务必用 ${ct} 显式定界。
      log "  ✅ user-server 已启动（127.0.0.1:8204，${i}s，Content-Type: ${ct}）"
      return 0
    fi
    sleep 1
    i=$((i+1))
  done
  log_warn "user-server 本地健康检查超时，请查看 user-server/logs/user-server.log"

  log "  user-server 部署完成"
}

# ---------- 更新静态托管配置（反向代理层同源托管）----------
# 2026-09-03 注：函数体暂缺，--反向代理层-only 模式不生效。
# 静态资源已通过 push_web() rsync 到云端 /www/wwwroot/hivemtk/user-web/dist/
deploy_reverse_proxy() {
  log_warn 'deploy_reverse_proxy 函数体暂缺，跳过'
}

healthcheck() {
  [[ -n "$SKIP_HEALTHCHECK" ]] && { log "跳过健康检查"; return 0; }
  log "########## 健康检查 ##########"

  local max_wait=60
  local i=0
  local ct=''
  while (( i < max_wait )); do
    # 用 /api/health（非 /health）并校验 Content-Type: application/json，
    # 避免 反向代理层 SPA 兜底返回 200+HTML（前端 index.html）导致假通过
    # 注意：在 set -euo pipefail 下，避免将 ct=$(...) 与 && 短路链合并，
    # 否则 curl 失败会让整个命令链非零退出导致脚本中断；改用独立 if 包裹。
    ct=$(curl -fsS -o /dev/null -w "%{content_type}" "$HEALTH_URL" 2>/dev/null || true)
    if [[ "$ct" == application/json* ]]; then
      # 注意：变量后紧跟全角括号‘）’ 时，Bash 会把 ct） 当成变量名，
      # 在 set -u 下报 unbound variable；务必用 ${ct} 显式定界。
      log "  ✅ ${HEALTH_URL} 通过（${i}s，Content-Type: ${ct}）"
      return 0
    fi
    sleep 3
    i=$((i+3))
  done

  log_warn "健康检查超时（${max_wait}s），请手动检查"
  return 0
}

# ===================== 主流程 =====================
preflight

case "$MODE" in
  web)
    build_web
    push_web
    ;;
  api)
    deploy_api
    ;;
  反向代理层)
    ;;
  all)
    build_web
    push_web
    deploy_api
    ;;
  *) die "未知模式: $MODE" ;;
esac

healthcheck

if [[ -n "$DRY_RUN" ]]; then
  log "发布完成（--dry-run 仅预览，未实际执行）。"
else
  log "=========================================="
  log "✅ 发布完成！"
  log "  本地 API: ${HEALTH_URL%/api/health}"
  if mode_needs_remote; then
    log "  前端已推送到: $DEPLOY_USER@$DEPLOY_HOST:$WEBROOT_USERWEB/dist/"
  else
    log "  本次无远端推送（纯本地模式）"
  fi
  log "=========================================="
fi
