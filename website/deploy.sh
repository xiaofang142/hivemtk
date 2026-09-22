#!/usr/bin/env bash
# =============================================================
# HiveMTK 官网 (website) 本地构建 + 产物校验脚本
#
# 发布方式已换成 GitHub Pages：推送到 GitHub 远端（remote `upstream`）的 master 分支由
# .github/workflows/website-pages.yml 构建并发布 dist/。注意 Gitee 远端 `gitee-upstream`
# 不承载发布——只推 Gitee 不会更新官网。
# 本脚本不再 SSH、不再 rsync，只负责"推之前本地跑一遍"：
#   预检（依赖 / 源码结构 / i18n 词典完整性）→ 构建 → 产物校验
#
# 产物校验针对 Pages 特有的四类坑：
#   1) 子路径 base：index.html 里的资源引用必须带 /hivemtk/ 前缀
#   2) SPA 兜底：dist/404.html 必须存在（缺了未知路径就是 Pages 默认 404 页）
#   3) 深链 200：每条静态路由与每个 sitemap URL 都要有 <route>/index.html 壳
#   4) 旧域名回流：产物里不许再出现已到期下线的站点域名
#
# 用法:
#   ./deploy.sh                 # 预检 + 构建 + 产物校验
#   ./deploy.sh --preflight-only
#   ./deploy.sh --build-only    # 预检 + 构建，不校验产物
#   ./deploy.sh --verify-only   # 只校验已有 dist/
#   ./deploy.sh --skip-build    # 跳过构建，直接校验现有 dist/
#   ./deploy.sh --skip-install  # 不跑 npm ci（node_modules 已就绪时省时）
#   ./deploy.sh --skip-i18n-check   # 跳过 MISSING_UNIQ=0 强校验（不推荐）
#   ./deploy.sh -h|--help       # 帮助
#
# 环境变量:
#   NODE_BIN   指向可用 node（默认 PATH 第一个）
# =============================================================
set -eo pipefail
# 注：不加 -u。macOS 自带 bash 3.2 对 set -u 下函数间变量共享的实现过于激进，
#     即便变量已在脚本顶层赋值，跨函数读取仍可能被报 "unbound variable"。
#     改用下面的 ${VAR:-default} 默认值兜底，等价防护。

SCRIPT_PATH="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/$(basename "${BASH_SOURCE[0]}")"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

log()      { echo "[$(date +'%H:%M:%S')] $*"; }
log_warn() { echo "[$(date +'%H:%M:%S')] WARN: $*" >&2; }
log_err()  { echo "[$(date +'%H:%M:%S')] ERROR: $*" >&2; }
die()      { log_err "$*"; exit 1; }

NODE_BIN="${NODE_BIN:-node}"
# 单一源：vite.config.js 的 SITE_BASE；这里只用于产物校验，改 base 要同步改这里
SITE_BASE="${SITE_BASE:-/hivemtk/}"

SKIP_BUILD=""
SKIP_INSTALL=""
SKIP_I18N_CHECK=""
DRY_RUN=""
BUILD_ONLY=""
PREFLIGHT_ONLY=""
VERIFY_ONLY=""

while [[ $# -gt 0 ]]; do
  arg="$1"
  case "$arg" in
    --skip-build)         SKIP_BUILD=1; shift ;;
    --skip-install)       SKIP_INSTALL=1; shift ;;
    --build-only)         BUILD_ONLY=1; shift ;;
    --preflight-only)     PREFLIGHT_ONLY=1; shift ;;
    --verify-only)        VERIFY_ONLY=1; shift ;;
    --dry-run)            DRY_RUN=1; shift ;;
    --skip-i18n-check)    SKIP_I18N_CHECK=1; shift ;;
    -h|--help)            sed -n '2,/^# =\{20,\}$/p' "$SCRIPT_PATH"; exit 0 ;;
    *) die "未知参数: ${1}（试试 -h）" ;;
  esac
done

run() {
  if [[ -n "$DRY_RUN" ]]; then echo "[dry-run] $*"; return 0; fi
  bash -c "set -eo pipefail; $*"
}

# ---------- 预检 ----------
preflight() {
  log "########## 预检 ##########"

  if [[ -z "$SKIP_BUILD" ]]; then
    command -v node >/dev/null 2>&1 || die "本地命令缺失: node（构建需要 Node 20+）"
    command -v npm  >/dev/null 2>&1 || die "本地命令缺失: npm"
    local node_v node_major
    node_v="$(${NODE_BIN:-node} -v 2>/dev/null | sed 's/^v//' || echo unknown)"
    node_major="${node_v%%.*}"
    if [[ "$node_major" =~ ^[0-9]+$ ]]; then
      [[ "$node_major" -ge 20 ]] || die "Node 版本过低: v$node_v（package.json engines 要求 >=20）"
    else
      log_warn "无法解析 node 版本: '$node_v'，跳过版本校验"
    fi
    log "  node: v${node_v}"
  else
    log "  跳过 node 检查（--skip-build）"
  fi

  [[ -f "$ROOT/package.json"   ]] || die "缺少 package.json（$ROOT）"
  [[ -f "$ROOT/check_i18n.mjs" ]] || die "缺少 check_i18n.mjs（$ROOT）"
  [[ -f "$ROOT/scripts/postbuild.mjs" ]] || die "缺少 scripts/postbuild.mjs（$ROOT）"
  log "  本地源码: ok"

  # i18n 词典完整性：MISSING_UNIQ 必须为 0，否则 $t('中文') 在英/日/阿界面会裸奔
  if [[ -z "$SKIP_I18N_CHECK" ]]; then
    log "==> 跑 i18n 词典预检（check_i18n.mjs）"
    local out
    if ! out="$($NODE_BIN "$ROOT/check_i18n.mjs" 2>&1)"; then
      echo "$out" | tail -50
      die "check_i18n.mjs 失败，请修复后重试（--skip-i18n-check 可跳过但不推荐）"
    fi
    if echo "$out" | grep -q '^IMPORT_FAIL'; then
      echo "$out" | grep '^IMPORT_FAIL' | head -5
      die "有 i18n 词典模块 import 失败，其键全部视为缺失"
    fi
    local uniq
    uniq="$(echo "$out" | grep -E '^TOTAL_LITERAL_KEYS=' | head -1 | sed 's/.*MISSING_UNIQ=\([0-9]*\).*/\1/')"
    [[ -n "$uniq" ]] || die "未抓到 MISSING_UNIQ 行，check_i18n.mjs 输出异常"
    if [[ "$uniq" != "0" ]]; then
      echo "$out" | tail -30
      die "i18n 词典仍有 $uniq 个未翻译键；先补齐再发布（详见 .i18n-missing.jsonl）"
    fi
    log "  i18n 词典: ok（MISSING_UNIQ=0）"
  else
    log_warn "跳过 i18n 词典预检（--skip-i18n-check）"
  fi
}

# ---------- 构建 ----------
build_website() {
  log "########## 构建官网 (website) ##########"
  local install_cmd="npm ci --no-audit --no-fund"
  [[ -n "$SKIP_INSTALL" ]] && install_cmd="true"
  log "==> ${install_cmd} + vite build + postbuild"
  run "( export PATH=\"$(dirname "$NODE_BIN"):\$PATH\" \
         && ${install_cmd} \
         && npm run build )"
  [[ -d "$ROOT/dist" ]] || die "构建失败：未产出 dist/ 目录"
  local fsize
  fsize="$(du -sh "$ROOT/dist" | awk '{print $1}')"
  log "  构建完成：$ROOT/dist ($fsize)"
}

# ---------- 产物校验 ----------
verify_dist() {
  log "########## 产物校验（Pages 发布前置） ##########"
  [[ -d "$ROOT/dist" ]] || die "本地 dist 不存在，请先构建（去掉 --skip-build）"

  local idx_count
  idx_count="$(find "$ROOT/dist" -maxdepth 1 -name '*.html' | wc -l | tr -d ' ')"
  [[ -f "$ROOT/dist/index.html" ]] || die "dist/index.html 缺失"

  if [[ ! -f "$ROOT/dist/404.html" ]]; then
    die "dist/404.html 缺失：Pages 无服务端 rewrite，深链只能靠它兜底（检查 scripts/postbuild.mjs 是否跑过）"
  fi
  log "  SPA 兜底: ok（index.html + 404.html，html 文件数 ${idx_count}）"

  # 深链真 200：Pages 无 rewrite，未铺设的深链返回 404 —— 页面能渲染，但搜索引擎
  # 会把 sitemap.xml 公布的子页面判为「已删除」。这里交叉核对两个独立来源：
  # 路由清单（src/router/index.js）和 sitemap 的 <loc>，两边都必须有目录壳。
  # 刻意不复用 postbuild 自己写的清单：那样只能自证，抓不到它漏铺的路由。
  local routes shell_missing loc route
  routes="$(grep -oE "^ +path: '/[a-z-]+'," "$ROOT/src/router/index.js" \
    | tr -d " ,'" | sed -E 's#^path:/##' | sort -u)"
  [[ -n "$routes" ]] || die "未能从 src/router/index.js 解析出静态路由，深链门失效"
  shell_missing=""
  while IFS= read -r route; do
    [[ -f "$ROOT/dist/$route/index.html" ]] || shell_missing="$shell_missing $route"
  done <<< "$routes"
  while IFS= read -r loc; do
    route="${loc#*"$SITE_BASE"}"
    route="${route%%\?*}"
    route="${route%/}"
    [[ -z "$route" ]] && continue
    [[ -f "$ROOT/dist/$route/index.html" ]] || shell_missing="$shell_missing sitemap:$route"
  done <<< "$(grep -oE '<loc>[^<]+</loc>' "$ROOT/public/sitemap.xml" | sed -E 's#</?loc>##g')"
  [[ -z "$shell_missing" ]] || die "以下深链缺 200 壳（postbuild 漏铺或 sitemap 与路由漂移）:$shell_missing"
  log "  深链 200: ok（路由壳与 sitemap URL 全覆盖）"

  # base 前缀：入口 html 引用的 js/css 必须挂在 SITE_BASE 下
  if ! grep -q "src=\"${SITE_BASE}" "$ROOT/dist/index.html"; then
    die "dist/index.html 的脚本引用未带 base 前缀 ${SITE_BASE}：Pages 子路径下会白屏（检查 vite.config.js 的 base）"
  fi
  log "  资源前缀: ok（index.html 引用带 ${SITE_BASE}）"

  # 站内根绝对路径：Vite 只给 public/ 里真实存在的资源加前缀，
  # 写了个 public 里没有的 /foo.png，产物里就是裸 "/foo.png" —— Pages 上必然 404。
  local rootrefs
  rootrefs="$(grep -o '\(href\|src\)="/[^"]*"' "$ROOT/dist/index.html" | grep -v "=\"${SITE_BASE}" || true)"
  if [[ -n "$rootrefs" ]]; then
    echo "$rootrefs" | sed 's/^/    /'
    die "dist/index.html 有站内绝对路径未带 base（上面列出）：要么把资源放进 public/，要么删掉这条引用"
  fi
  log "  站内绝对路径: ok（无裸 / 引用）"

  # 旧域名回流：产物里出现即说明有硬编码没清干净
  local stale
  stale="$(grep -rl 'xapptool\.cn' "$ROOT/dist" 2>/dev/null || true)"
  if [[ -n "$stale" ]]; then
    echo "$stale" | sed 's/^/    /'
    die "产物里仍有已到期域名 xapptool.cn（见上）"
  fi
  log "  旧域名: ok（产物无 xapptool.cn）"

  # 封面二维码等 publicDir 资产必须随产物发布，否则页面上是碎图
  [[ -f "$ROOT/dist/wechat.jpg" ]] || die "dist/wechat.jpg 缺失：public/ 资产未进入产物"
  log "  publicDir 资产: ok（wechat.jpg 已发布）"
}

# ===================== 主流程 =====================
if [[ -n "$VERIFY_ONLY" ]]; then
  verify_dist
  log "产物校验通过 ✅"
  exit 0
fi

preflight
[[ -n "$PREFLIGHT_ONLY" ]] && { log "预检通过（--preflight-only，未构建）"; exit 0; }

if [[ -z "$SKIP_BUILD" ]]; then
  build_website
else
  log "跳过本地构建（--skip-build）"
fi

if [[ -n "$BUILD_ONLY" ]]; then
  log "仅构建完成（--build-only，未校验产物）"
  exit 0
fi

verify_dist

if [[ -n "$DRY_RUN" ]]; then
  log "完成（--dry-run 只跳过了构建命令）"
else
  log "产物就绪 ✅  下一步：git push 由 website-pages.yml 发布到 GitHub Pages"
fi
