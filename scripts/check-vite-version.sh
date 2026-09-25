#!/usr/bin/env bash
# =============================================================================
# check-vite-version.sh —— 前端 vite「主版本不得漂移」守卫（OPT-FE-07 的落地护栏）
#
# 立项原因（2026-09-25）：docs/architecture/vite_version_policy.md §2.1 在 2026-08-16
# 就承诺了本文件（"CI 强制检查：添加 scripts/check-vite-version.sh 检测大版本差异、
# 阻止引入新的主版本"），但**该脚本从未创建**（find 全仓零命中）。结果是 OPT-FE-07
# 统一完版本后没有任何东西守着：谁在某个 package.json 里改回 5.x/6.x、或新前后端子项目
# 自带一个别的 majors 的 vite，都不会被拦。本文件补上这个缺口。
#
# 判据（口径来自 vite_version_policy.md §一）：
#   1) 面内每个 package.json 的 vite **依赖 pin**（键名字面为 "vite" 的那一行，
#      不是 scripts 里的 "dev": "vite"，也不是 vitest / @vitejs/plugin-vue /
#      vite-plugin-pwa / @storybook/vue3-vite 这些同前缀键）；
#   2) 每个 pin 的主版本必须等于 REQUIRED_MAJOR（默认 8 = 文档声明的现行主版本）；
#   3) pin 解析不出主版本（"latest" / "workspace:*" / "catalog:" / "file:.." 之类）
#      判**红**而不是放行 —— 判据分类不了的对象不能算通过；
#   4) 扫描结果为 0 个 package.json、或 0 条 vite pin ⇒ 判 rc=2 红。
#      「零对象」不等于「通过」：本仓已在 check-enum-consistency.sh 上吃过一次
#      「正则失效 → 全部落进数据不足分支 → 从不报错」的假绿亏（见 78 清单
#      「本轮新增发现」表第 1 行）。扫描面空了，只可能是面错了，不是版本对了。
#
# 扫描根的枚举口径（关键，别再踩 logs/ 那个坑）：
#   优先 `git -C 根 ls-files --cached --others --exclude-standard`，
#    —— 与姊妹闸 check-shell-cjk-expansion.sh 同一口径。理由是实测：本机工作区里
#      `git ls-files` 只给出 6 个（含一个不带 vite 的 packages/browser-core），
#      而朴素的 `find ! -path '*/node_modules/*'` 在同一棵树上给出 **80 个**，其中
#       hivemtk/logs/p703-*/clone/ 下的历史取证快照就占 35 个，另有 r45-*/、r22-hv/、
#      r48-shadow/、.tmp_files/ 等别泳道留下的影子克隆。拿 find 黑名单当面，
#      等于把守卫的读数绑在临时目录的清理习惯上 —— 别人刚丢下的 clone 会让它本地恒红、
#      CI 干净检出恒绿，两边同时失去意义。git 面天然免疫（这些路径都被 ignore 或根本不在仓内）。
#   仅当该根不在任何 git 工作树内时（自测用的 /tmp 假根）才退化成 find + 目录黑名单。
#
# 默认扫描面 = 主仓 + 同级 platform 仓（若存在）：
#   vite_version_policy.md §一 的表格本身跨两仓（列了 hivemtk/ 与 hivemtk-platform/ 两侧
#   的子项目），"主版本全体一致" 这个判据只在两仓同时在下才成立 —— 只扫主仓会漏掉
#   7 条 pin 里的 2 条。platform 仓不存在时（GitHub Actions 只 checkout 主仓）本闸
#   自动降级为只扫主仓，并在自证行里点名缺了哪个根，绝不静默把「没检出」读成「通过」。
#
# bash 3.2 兼容（macOS /bin/bash 至今 3.2.57），两条本仓已知假绿陷阱都绕开：
#   - 不用 declare -A（关联数组是 bash 4+）；累加一律走换行分隔的普通字符串。
#   - LC_CTYPE 为 UTF-8 时 `$VAR` 紧跟中文会连字节一起吃掉 ⇒ 全文变量一律写 ${VAR} 形式，
#     且回显文案里不放 < 、> （裸放在引号外会变成重定向）。
#   - 正则只用 POSIX ERE 字符类（[[:space:]]），不用 \s —— BSD grep 不认，
#     那正是 check-enum-consistency.sh 当年恒绿的直接原因。
#
# 用法：bash scripts/check-vite-version.sh [扫描根目录...] [--major N] [--help]
#   不给根 = 默认面（主仓 + 存在的同级 platform 仓）；给根 = 只扫这些根（自测用）。
#   环境变量 VITE_GUARD_MAJOR / VITE_GUARD_PLATFORM_ROOT 同样可覆盖默认口径。
#
# rc 语义：0 通过；1 确有 pin 不合口径（逐条点名 路径:行号:当前值）；
#          2 环境或判据本身有问题（扫描面为空、零 pin、参数不合法、根不存在）
#
# 反向测试（本仓规矩：新校验必须证明它真会开火）：
#   mkdir -p /tmp/hv-fake/proj && printf '{\n  "devDependencies": {\n    "vite": "^6.3.5"\n  }\n}\n' \
#     > /tmp/hv-fake/proj/package.json
#   bash scripts/check-vite-version.sh /tmp/hv-fake        # 必须 rc=1 且点名该文件
#   rm -rf /tmp/hv-fake && bash scripts/check-vite-version.sh   # 复跑必须 rc=0
# 改本闸（尤其改扫描面口径）后必须把「红一次 + 绿一次」两趟都真跑一遍。
# =============================================================================

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
WORKSPACE_ROOT="$(cd "${REPO_ROOT}/.." && pwd)"

# 现行主版本：口径同 docs/architecture/vite_version_policy.md §一（2026-09-25 实测全仓
# 7 条 vite pin 均为 "^8.1.1"）。升主版本时改这里 + 改文档，二者必须同一批提交。
REQUIRED_MAJOR="${VITE_GUARD_MAJOR:-8}"
PLATFORM_ROOT_DEFAULT="${VITE_GUARD_PLATFORM_ROOT:-${WORKSPACE_ROOT}/hivemtk-platform}"

usage() {
  cat <<'HIVEMTK_EOF'
用法：bash scripts/check-vite-version.sh [扫描根目录...] [--major N] [--help]

  扫描根目录   零个或多个。给了就只扫这些目录，不给用默认面
               （主仓 + 存在的同级 hivemtk-platform）。自测时指到 /tmp 下的假根。
  --major N    声明的现行主版本，默认 8。等价于 VITE_GUARD_MAJOR=N。
  --help       本说明。

rc：0 通过 / 1 确有 vite pin 不合口径 / 2 环境或判据本身有问题（含扫描面为空）
HIVEMTK_EOF
}

# ---------------------------------------------------------------------------
# 参数解析
# ---------------------------------------------------------------------------
ROOTS=""
while [ $# -gt 0 ]; do
  case "$1" in
    --help|-h)
      usage
      exit 0
      ;;
    --major)
      if [ $# -lt 2 ]; then
        echo "::error::--major 后面缺少取值"
        exit 2
      fi
      REQUIRED_MAJOR="$2"
      shift 2
      ;;
    --major=*)
      REQUIRED_MAJOR="${1#--major=}"
      shift
      ;;
    --)
      shift
      while [ $# -gt 0 ]; do
        ROOTS="${ROOTS}${1}
"
        shift
      done
      ;;
    -*)
      echo "::error::未知参数：${1}（--help 看用法）"
      exit 2
      ;;
    *)
      ROOTS="${ROOTS}${1}
"
      shift
      ;;
  esac
done

case "${REQUIRED_MAJOR}" in
  ''|*[!0-9]*)
    echo "::error::主版本必须是不带符号的十进制数，拿到的是：「${REQUIRED_MAJOR}」"
    exit 2
    ;;
esac

# ---------------------------------------------------------------------------
# 辅助函数
# ---------------------------------------------------------------------------

# display_path：把绝对路径缩写成相对工作区根的形态，读日志时对齐、可比对。
display_path() {
  _p="$1"
  case "${_p}" in
    "${WORKSPACE_ROOT}"/*) printf '%s\n' "${_p#${WORKSPACE_ROOT}/}" ;;
    "${REPO_ROOT}"/*) printf '%s\n' "${_p#${REPO_ROOT}/}" ;;
    *) printf '%s\n' "${_p}" ;;
  esac
}

# enum_pkgjson：列出一个根里的 package.json（每行一个绝对路径）。
# git 面优先；根不在 git 工作树内时退化成 find + 目录黑名单。
enum_pkgjson() {
  _root="$1"
  if git -C "${_root}" rev-parse --show-toplevel >/dev/null 2>&1; then
    {
      git -C "${_root}" ls-files --cached --others --exclude-standard 2>/dev/null |
        grep -E '(^|/)package\.json$' |
        while IFS= read -r _rel; do
          printf '%s/%s\n' "${_root}" "${_rel}"
        done
    } || true
  else
    find "${_root}" \
      \( -name node_modules -o -name .git -o -name dist -o -name build \
         -o -name coverage -o -name logs -o -name snapshots -o -name .cache \
         -o -name .tmp_files -o -name 'r45-*' -o -name 'r22-hv' -o -name 'r48-shadow' \) -prune \
      -o -name package.json -print 2>/dev/null || true
  fi
}

# lock_resolved：informational only —— 同目录 package-lock.json 解析到的 vite 版本。
# 解析不出来一律回 "-"，绝不影响判定，也不会把本闸变红。
lock_resolved() {
  _dir="$1"
  _lk="${_dir}/package-lock.json"
  _v="-"
  if [ -f "${_lk}" ]; then
    _raw="$(grep -A2 '"node_modules/vite": {' "${_lk}" 2>/dev/null |
      sed -n -E 's/.*"version":[[:space:]]*"([^"]*)".*/\1/p' | head -1 || true)"
    if [ -n "${_raw}" ]; then
      _v="${_raw}"
    else
      _v="?"
    fi
  fi
  printf '%s\n' "${_v}"
}

# ---------------------------------------------------------------------------
# 组装扫描根
# ---------------------------------------------------------------------------
if [ -z "${ROOTS}" ]; then
  ROOTS="${REPO_ROOT}
"
  if [ -d "${PLATFORM_ROOT_DEFAULT}" ]; then
    ROOTS="${ROOTS}${PLATFORM_ROOT_DEFAULT}
"
    ROOTS_NOTE="主仓 + 同级 platform 仓"
  else
    ROOTS_NOTE="仅主仓（同级 platform 仓不存在：${PLATFORM_ROOT_DEFAULT}）"
  fi
else
  ROOTS_NOTE="显式传入的根"
fi

MISSING_ROOT=""
while IFS= read -r _r; do
  [ -n "${_r}" ] || continue
  if [ ! -d "${_r}" ]; then
    MISSING_ROOT="${MISSING_ROOT}${_r}
"
  fi
done <<ROOTEOf
${ROOTS}
ROOTEOf

if [ -n "${MISSING_ROOT}" ]; then
  echo "::error::下列扫描根不存在，扫描面不成立，本闸拒绝按「通过」处理："
  printf '%s' "${MISSING_ROOT}" | while IFS= read -r _m; do
    [ -n "${_m}" ] || continue
    echo "  ${_m}"
  done
  exit 2
fi

# ---------------------------------------------------------------------------
# 主扫描
# ---------------------------------------------------------------------------
FILES_SEEN=0
PKG_WITH_PIN=0
PIN_RECORDS=0
MAJORS_SEEN=""
BAD_LIST=""      # 每行：显示路径:行号:当前值 （主版本=N）
ROSTER=""        # 自证用的逐条台账

PKGLIST=""
while IFS= read -r _r; do
  [ -n "${_r}" ] || continue
  PKGLIST="${PKGLIST}$(enum_pkgjson "${_r}")
"
done <<ROOTS2EOf
${ROOTS}
ROOTS2EOf

# 去重（多根可能嵌套）+ 丢掉空行；不用 declare -A，靠 sort -u。
PKGLIST_UNIQ="$(printf '%s\n' "${PKGLIST}" | sed -e '/^[[:space:]]*$/d' | sort -u || true)"

set -f  # 关 glob：路径里出现 * 或空格时也不参与字段展开
OLD_IFS="$IFS"
IFS='
'
for _pkg in ${PKGLIST_UNIQ}; do
  [ -n "${_pkg}" ] || continue
  [ -f "${_pkg}" ] || continue
  FILES_SEEN=$((FILES_SEEN + 1))

  # 只认键名字面为 "vite" 的依赖行；^[[:space:]]*"vite" 把同前缀键（vitest、
  # vite-plugin-pwa、@storybook/vue3-vite）和 scripts 段的 "dev": "vite" 全部排除在外。
  MATCHES="$(grep -nE '^[[:space:]]*"vite"[[:space:]]*:[[:space:]]*"[^"]*"' "${_pkg}" 2>/dev/null || true)"
  if [ -z "${MATCHES}" ]; then
    continue
  fi

  PKG_WITH_PIN=$((PKG_WITH_PIN + 1))
  _disp="$(display_path "${_pkg}")"
  _lock="$(lock_resolved "$(dirname "${_pkg}")")"

  while IFS= read -r _ln; do
    [ -n "${_ln}" ] || continue
    _line="${_ln%%:*}"
    _val="$(printf '%s\n' "${_ln}" | sed -n -E 's/^[0-9]+:[[:space:]]*"vite"[[:space:]]*:[[:space:]]*"([^"]*)".*/\1/p')"
    _major="$(printf '%s\n' "${_val}" | sed -n -E 's/^[^0-9]*([0-9]+).*/\1/p')"

    PIN_RECORDS=$((PIN_RECORDS + 1))

    if [ -z "${_major}" ]; then
      # 解析不出主版本：判红，不静默放行。
      BAD_LIST="${BAD_LIST}  ${_disp}:${_line}:${_val} （解析不出主版本）
"
      ROSTER="${ROSTER}  [RED] ${_disp}:${_line} pin=\"${_val}\" major=无法解析 lock=${_lock}
"
      continue
    fi

    MAJORS_SEEN="${MAJORS_SEEN}${_major}
"
    if [ "${_major}" != "${REQUIRED_MAJOR}" ]; then
      BAD_LIST="${BAD_LIST}  ${_disp}:${_line}:${_val} （主版本=${_major}，应为 ${REQUIRED_MAJOR}）
"
      ROSTER="${ROSTER}  [RED] ${_disp}:${_line} pin=\"${_val}\" major=${_major} lock=${_lock}
"
    else
      ROSTER="${ROSTER}  [ok ] ${_disp}:${_line} pin=\"${_val}\" major=${_major} lock=${_lock}
"
    fi
  done <<INNEREOF
${MATCHES}
INNEREOF
done
IFS="$OLD_IFS"
set +f

# 观测到的主版本集合（去重后逐个列出，供「并存」判词用）
MAJORS_UNIQ="$(printf '%s\n' "${MAJORS_SEEN}" | sed -e '/^[[:space:]]*$/d' | sort -u -n || true)"
MAJORS_COUNT="$(printf '%s\n' "${MAJORS_UNIQ}" | sed -e '/^[[:space:]]*$/d' | grep -c '' || true)"
[ -n "${MAJORS_COUNT}" ] || MAJORS_COUNT=0
MAJORS_TEXT="$(printf '%s\n' "${MAJORS_UNIQ}" | sed -e '/^[[:space:]]*$/d' | tr '\n' ' ' | sed -e 's/[[:space:]]*$//' || true)"
[ -n "${MAJORS_TEXT}" ] || MAJORS_TEXT="（空）"
# lock 分布只做参考（不影响判定）：把 "8.3.0 x6" 这样的计数摘要压成一行，去掉 uniq -c 的前导空格。
LOCK_DIST="$(printf '%s\n' "${ROSTER}" | sed -n -E 's/.*lock=([^ ]*).*/\1/p' | sort | uniq -c |
  sed -E -e 's/^[[:space:]]*//' -e 's/^([0-9]+)[[:space:]]+(.*)$/\2 x\1/' | tr '\n' ';' || true)"
[ -n "${LOCK_DIST}" ] || LOCK_DIST="（无）"

echo "── vite 主版本守卫 ──"
echo "扫描面口径：${ROOTS_NOTE}"
echo "扫描根："
printf '%s\n' "${ROOTS}" | while IFS= read -r _r; do
  [ -n "${_r}" ] || continue
  echo "  $(display_path "${_r}")"
done
echo "vite pin 台账（现行主版本应为 ${REQUIRED_MAJOR}）："
if [ -n "${ROSTER}" ]; then
  printf '%s' "${ROSTER}"
else
  echo "  （无）"
fi

# ---------------------------------------------------------------------------
# 零对象自检：面空 / 零 pin 都属判据失效，按 rc=2 红
# ---------------------------------------------------------------------------
if [ "${FILES_SEEN}" -eq 0 ]; then
  echo "::error::扫描到 0 个 package.json —— 这是扫描根/枚举口径坏了，不是版本对了。拒绝判绿。"
  echo "自证|扫描package.json=0 含vite的pin=0 声明主版本=${REQUIRED_MAJOR} 观测主版本集合=（空） 判定=RED(rc=2 零对象)"
  exit 2
fi

if [ "${PIN_RECORDS}" -eq 0 ]; then
  echo "::error::扫到 ${FILES_SEEN} 个 package.json，但其中 0 条 vite pin —— 判据（键名字面为 vite 的依赖行）没命中任何东西，等于零覆盖。拒绝判绿。"
  echo "自证|扫描package.json=${FILES_SEEN} 含vite的pin=0 声明主版本=${REQUIRED_MAJOR} 观测主版本集合=（空） 判定=RED(rc=2 零对象)"
  exit 2
fi

# ---------------------------------------------------------------------------
# 判定
# ---------------------------------------------------------------------------
if [ "${MAJORS_COUNT}" -gt 1 ]; then
  echo "观测到 ${MAJORS_COUNT} 个主版本并存：${MAJORS_TEXT}（口径只允许 1 个：${REQUIRED_MAJOR}）"
fi

if [ -n "${BAD_LIST}" ]; then
  echo "::error::vite 主版本漂移 —— 以下每条落点不合口径（路径:行号:当前值）："
  printf '%s' "${BAD_LIST}"
  echo "处置：把上述 pin 统一到 ${REQUIRED_MAJOR}.x，并同步 docs/architecture/vite_version_policy.md §一 的版本策略口径。"
  echo "自证|扫描package.json=${FILES_SEEN} 含vite的pin=${PIN_RECORDS}(来自${PKG_WITH_PIN}个文件) 声明主版本=${REQUIRED_MAJOR} 观测主版本集合=${MAJORS_TEXT} lock分布=${LOCK_DIST} 判定=RED(rc=1)"
  exit 1
fi

echo "自证|扫描package.json=${FILES_SEEN} 含vite的pin=${PIN_RECORDS}(来自${PKG_WITH_PIN}个文件) 声明主版本=${REQUIRED_MAJOR} 观测主版本集合=${MAJORS_TEXT} lock分布=${LOCK_DIST} 判定=GREEN"
exit 0
