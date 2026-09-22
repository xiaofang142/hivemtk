#!/usr/bin/env bash
# =============================================================
# check-no-xapptool.sh —— 防回流闸：仓库内不得再出现已下线的 xapptool.cn 线上域
#
# 背景：hive / hiveuser / hiveuserapi / hivepaltform(api) / hivecontributor 所在服务器
# 已于 2026-09 到期且不续费（见 docs/superpowers/specs/2026-09-21-offline-deployment-design.md）。
# 本闸把"任何地方别再把它当默认值或示例"固化成可跑判据。
#
# 覆盖面口径（务必知道本闸摸不到什么，勿当全量保证）：
#   枚举源 = 两条并起来、**剥掉 find 的 ./ 前缀之后** sort -z -u 去重
#   （全程 NUL 分隔，见 candidates() 与其后的去重注释）：
#     ① `git ls-files --cached --others --exclude-standard -z`
#        —— 已追踪 + 未追踪但未被 .gitignore 排除的文件。
#        取"未追踪也扫"是为了本地新增文件在 git add 之前就能被拦下；
#        CI 里 checkout 后所有文件都已追踪，两种口径等价。
#        那个 `-z` 不是风格问题：不带它时 git 会按 core.quotePath=true 把非 ASCII
#        文件名转义成 "\347\247…" 并加引号，实测本仓 3 个中文名 md 因此从枚举里
#        变成不存在的路径 → [[ -f ]] 判假 → 静默跳过，闸对含 14 处旧域的
#        docs/architecture/FRP私域部署指南.md 印了 "OK 0 hits"（假绿）。
#     ② 本地配置文件（`.env` / `.env.*` / `*.env`），**即使被 gitignore 也扫**。
#        理由：`.gitignore` 的 `*.env` 把 `.env` 挡在外面，而运行期真正读的就是它——
#        实测 ./.env 与 ./.env.geo.local 共含 5 处旧域，只走①就是给最要命的那份留洞。
#        ②用 find 但严格限定文件名模式并 prune 掉 node_modules/.git/dist/build/vendor，
#        不是全量 find（全量会把产物残留算进账，数字失真）。
#   枚举自证：待扫文件数为 0 时本闸直接红（不是"仓库干净"），绿/红都印 scanned=N。
#   仍摸不到的：①②之外的构建产物；被 gitignore 且文件名不匹配②的本地文件；
#   grep -I 跳过的二进制文件（命中会印成 "Binary file ... matches" 污染行号，按不匹配处理）。
#   拆分字面量（"xapptool" + ".cn"）能躲过本闸的文本匹配，这一面由
#   user-server/internal/config/ports_test.go 的 NoRetiredOnlineDomain 用例看住
#   （它盯的是运行期默认常量，正是拆分写法唯一有意义的落点）。
#   每次只覆盖**调用所在的那一个仓**。工作区根级的 docs/ internal-docs/ artifacts/ cold-start/ scripts/
#   实测不在任何 git 仓内（git -C <dir> rev-parse 报"不是 git 仓库"），其中含旧域的
#   文件只能靠人工清单保证，本闸一律摸不到。
#   r22-hv/ 是 hivemtk 的影子克隆，靠 git 同步，不手改，也不在本闸范围内。
#   扫描范围是**调用时所在的这一个 git 仓**（hivemtk 或 hivemtk-platform，取决于你在哪个仓里跑它）：
#   两仓要各跑一次才都进账，跑一次只证明一个仓。白名单是仓内相对路径，
#   下面这组是 hivemtk 侧的例外，platform 侧当前实测 0 命中、无需例外条目。
#   website 迁入本仓后才会进本仓账（届时命中数会跳一批，那是覆盖面扩大，不是有人回退）。
#
# 例外白名单（只放"改写等于伪造取证"的历史事实，不放偷懒没改的文案）：
#   设计文档与实施计划自身必然指名旧域（要能核对才知道改了谁），故入白名单。
#   闸自身与它的反向测试必须在源码里写出被拦的模式与夹具内容，属自引用——
#   不放行则本闸永远拦自己（"撤掉夹具后仍红"，见 §7 实施实况），故二者一并入白名单。
#   白名单只豁免这几个具体路径，不豁免 scripts/ 或 docs/ 目录，新增违规照样拦。
#   GEO 站点归属用例（crawler_visit_selfsite_test.go）的断言对象就是"已下线域名不再算自家、
#   不再享有 A 级静态映射"，夹具必须指名那个域；换成任意别的死域等于把要防的那次回流改成防不住。
#   website/deploy.sh 同属"检测器自引用"：它的产物校验步要在 dist 里 grep 出那个域才能拦，
#   拦下后还要把域名印在报错文案里；改写等于拆掉这道产物门。
#
# 反向测试：scripts/check-no-xapptool.test.sh（注入违规必须让命中数 +1、撤掉必须回基线）
# =============================================================
# 注：不加 set -u。macOS 自带 bash 3.2 对跨函数变量共享会误报 unbound variable。
set -eo pipefail

# 扫描对象 = **调用时所在的 git 仓**，不是脚本所在仓。
# 曾用 `dirname BASH_SOURCE/..` 定根：那样从任何目录调用都只扫 hivemtk，
# 于是"同一份闸用在 platform 仓根跑一遍"这句记录其实是把 hivemtk 数了两遍（假绿）。
# 现在不在仓内时仍回落到脚本所在仓（保证改名影子克隆、仓外调用都有确定行为）。
ROOT="$(git rev-parse --show-toplevel 2>/dev/null || true)"
if [[ -z "$ROOT" ]]; then
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

PATTERN='xapptool\.cn'

# NUL 分隔的待扫清单落盘一次：既要喂给 xargs，又要先数出扫描文件数做枚举自证，
# 用进程替换两遍读会漏（管道只能消费一次）。
# 两份清单：LIST_RAW 是循环剥完 ./ 的结果，LIST 是它 sort -z -u 之后的最终口径。
TMP_LIST_RAW="$(mktemp "${TMPDIR:-/tmp}/no-xapptool-list.XXXXXX")"
TMP_LIST="$(mktemp "${TMPDIR:-/tmp}/no-xapptool-list.XXXXXX")"
trap 'rm -f "$TMP_LIST_RAW" "$TMP_LIST"' EXIT

WHITELIST=(
  "docs/superpowers/specs/2026-09-21-offline-deployment-design.md"
  "docs/superpowers/plans/2026-09-21-offline-deployment.md"
  "scripts/check-no-xapptool.sh"
  "scripts/check-no-xapptool.test.sh"
  "user-server/internal/geo/repository/crawler_visit_selfsite_test.go"
  "website/deploy.sh"
)

is_whitelisted() {
  local f="$1" w
  for w in "${WHITELIST[@]}"; do
    [[ "$f" == "$w" ]] && return 0
  done
  return 1
}

# NUL 分隔贯穿全程。曾经按行分隔：`git ls-files` 默认 core.quotePath=true，
# 非 ASCII 文件名会被转义并加引号输出（实测本仓 3 个中文名的 md 全中招），
# 于是 [[ -f "$f" ]] 判假、静默 continue —— docs/architecture/FRP私域部署指南.md
# 的 14 处旧域就这么从闸眼下溜过去，闸还印了 "OK 0 hits"。
# -z 让 git 输出原始字节且以 \0 分隔，名字里带空格/中文/引号都不再需要转义。
#
# 这里**不**先去重：① 给的是 `user-web/.env.example`，② 给的是
# `./user-web/.env.example`，同一文件两副面孔，在剥前缀之前 sort -u 去不掉。
# 去重放到下面循环之后（见 TMP_LIST 那一步）。
candidates() {
  git ls-files --cached --others --exclude-standard -z
  find . \( -name node_modules -o -name .git -o -name dist -o -name build -o -name vendor \) -prune \
    -o -type f \( -name '.env' -o -name '.env.*' -o -name '*.env' \) -print0
}

while IFS= read -r -d '' f; do
  f="${f#./}"
  is_whitelisted "$f" && continue
  [[ -f "$f" ]] || continue
  printf '%s\0' "$f"
done < <(candidates) > "$TMP_LIST_RAW"

# 归一化之后再 sort -z -u，才是"实际要扫的文件数"。
# 老写法在 candidates() 里去重，实测有 5 个同时被两条源枚举的 .env 形状文件
# （scripts/inference-host/models.env、user-server/.env.example、
# user-web/.env.example、user-web/.env.development、user-web/.env.production）
# 各进清单两次：scanned 比真值多 5（同一刻实测印 4331 / 真值 4326），
# 更要紧的是这些文件里的一处命中会被 grep 两遍 ⇒ 印两行 file:line、hit 计数翻倍，
# 而 .env.example 正是"示例基址"最可能回落到旧域的那类文件。
sort -z -u < "$TMP_LIST_RAW" > "$TMP_LIST"
SCAN_FILES="$(tr -dc '\0' < "$TMP_LIST" | wc -c | tr -d ' ')"

# 枚举自证：扫到 0 个文件只可能是管道断了或不在仓根，此时"0 命中"是假绿，必须红。
if [[ "$SCAN_FILES" -eq 0 ]]; then
  echo "FAIL no-xapptool: 枚举到 0 个待扫文件，闸没跑起来（这不是『仓库是干净的』）" >&2
  exit 1
fi

# 批量喂给 grep：逐文件 spawn 一次实测 39s，xargs 批扫 <2s。
# 输出即 `file:line:content`，天然就是待办清单格式，直接透传。
MATCHES="$(xargs -0 grep -HnIE "$PATTERN" < "$TMP_LIST" 2>/dev/null || true)"

# scanned 单独一行、且刻意不含数字以外的歧义：反向测试用 `tr -dc '0-9'` 从
# FAIL 摘要行取命中数，把文件数并进同一行会把它读成 144209 这种拼接数。
echo "scanned=$SCAN_FILES files (whitelist=${#WHITELIST[@]} paths)"

if [[ -z "$MATCHES" ]]; then
  echo "OK no-xapptool: 0 hits"
  exit 0
fi

hits="$(printf '%s\n' "$MATCHES" | wc -l | tr -d ' ')"
# 只印 file:line，丢掉命中行正文——②会把 .env 这类含口令的文件扫进来，
# 原文透传等于把口令打进 CI 日志。代价：路径含冒号时截错，本仓无此类文件名。
printf '%s\n' "$MATCHES" | cut -d: -f1,2
echo "FAIL no-xapptool: $hits hit(s) 仍指向已下线线上域" >&2
exit 1
