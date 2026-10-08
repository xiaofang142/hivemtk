#!/bin/bash
# 给 Go 构建缓存按**分配量**封顶：默认只报数字不动手，加 --apply 才删。
#
# 为什么要外接一个脚本：`go env -w GOCACHE` 只能换目录，Go 自己**没有**容量上限
# （构建缓存按内容寻址只增不减，唯一内建的收缩手段是 `go clean -cache` —— 整体清空，
# 代价是下一次全量构建从零开始）。实测默认缓存长到 55 GB、电池私有缓存长到 32 GB，
# 而共享盘只剩 14 GB 时，门禁会因写不下而报出与代码无关的红。
#
# 为什么按大小而不是按年龄：缓存里的大对象（编译产物、归档）与刚写入的小对象混在一起，
# 按 mtime 裁"一周前的"既放不掉当下最占地的那些，又会把马上要复用的条目删掉。
# 删任何一条都是安全的：条目按哈希寻址，命中不了就重编，不影响正确性。
#
# 用法：
#   bash scripts/trim-go-cache.sh                      # 只看不动（dry-run）
#   bash scripts/trim-go-cache.sh --cap 8              # 每个缓存封顶 8 GB，仍然只报
#   bash scripts/trim-go-cache.sh --cap 8 --apply      # 真删
#   bash scripts/trim-go-cache.sh --cache /tmp/gocache-xxx --apply   # 追加目标
#
# 有 go build/test/vet 在跑时**拒绝动手**（缓存正被写，裁剪只是白费）：
# 脚本会读那些进程自己的 GOCACHE 环境变量，指到本目标才算冲突，无关进程不拦。
set -u

CAP_GB=8
APPLY=0
EXTRA_CACHES=""
SELF="$(basename "$0")"

while [ $# -gt 0 ]; do
  case "$1" in
    --cap)
      if [ $# -lt 2 ]; then echo "❌ --cap 需要跟一个 GB 数"; exit 2; fi
      CAP_GB="$2"; shift 2 ;;
    --cap=*)  CAP_GB="${1#*=}"; shift ;;
    --cache)
      if [ $# -lt 2 ]; then echo "❌ --cache 需要跟一个目录"; exit 2; fi
      EXTRA_CACHES="$EXTRA_CACHES $2"; shift 2 ;;
    --apply)  APPLY=1; shift ;;
    -h|--help)
      sed -n '1,26p' "$0"; exit 0 ;;
    *)
      echo "❌ 未知参数：$1（本脚本只认 --cap / --cache / --apply）"; exit 2 ;;
  esac
done

case "$CAP_GB" in
  ''|*[!0-9]*) echo "❌ --cap 必须是整数 GB，实得：$CAP_GB"; exit 2 ;;
esac

GO_BIN="$(command -v go || true)"
if [ -z "$GO_BIN" ]; then
  echo "❌ 找不到 go，无法定位默认缓存（\$GOROOT/bin 不在 PATH？）"; exit 2
fi

DEFAULT_CACHE="$("$GO_BIN" env GOCACHE 2>/dev/null)"
if [ -z "$DEFAULT_CACHE" ]; then
  echo "❌ go env GOCACHE 回空，无法定位默认缓存"; exit 2
fi

CACHES="$DEFAULT_CACHE$EXTRA_CACHES"
CAP_KB=$((CAP_GB * 1024 * 1024))

# 正在写缓存的 go 进程：mac 上 ps eww 能读到自己用户的进程环境，
# 只有它的 GOCACHE 指进本次目标目录时才判冲突（别的项目在编译不该拦住本脚本）。
busy_for() {
  target="$1"
  count=0
  for pid in $(pgrep -f 'go (test|build|vet)' 2>/dev/null); do
    envline="$(ps eww -p "$pid" -o command= 2>/dev/null | tr ' ' '\n' | grep '^GOCACHE=' | sed 's/^GOCACHE=//')"
    case "$envline" in
      "$target"/*|"$target") count=$((count + 1)) ;;
    esac
  done
  echo "$count"
}

TOTAL_FREED_KB=0
FAILED=0

echo "══════ Go 构建缓存封顶（每目标 ${CAP_GB} GB；$([ $APPLY -eq 1 ] && echo 'APPLY=1 真删' || echo 'dry-run 只报数')）══════"
for cache in $CACHES; do
  if [ ! -d "$cache" ]; then
    echo "  ↷ 跳过不存在的目录：$cache"
    continue
  fi
  size_kb="$(du -sk "$cache" 2>/dev/null | awk '{print $1}')"
  if [ -z "$size_kb" ]; then
    echo "  ❌ 读不到大小：$cache"
    FAILED=1
    continue
  fi
  files="$(find "$cache" -type f 2>/dev/null | wc -l | tr -d ' ')"
  printf "  目标 %s\n    分配量=%s MB 文件数=%s\n" "$cache" "$((size_kb / 1024))" "$files"
  if [ "$size_kb" -le "$CAP_KB" ]; then
    echo "    ✓ 未超上限，不动"
    continue
  fi
  need_kb=$((size_kb - CAP_KB))

  busy="$(busy_for "$cache")"
  if [ "$busy" -gt 0 ] && [ $APPLY -eq 1 ]; then
    echo "    ❌ 有 $busy 个 go 进程的 GOCACHE 指向这里，拒绝边写边裁（等它跑完，或加 --cap 提高上限）"
    FAILED=1
    continue
  fi

  # 从最大的一条开始删，删够 need_kb 就停。阈值不写死"只删 8 MB 以上的"：
  # 实测默认缓存里 8 MB 以上的对象只够释放 41.8 GB，而达 8 GB 上限要放掉 47.6 GB
  # —— 卡在半路会让"封顶"这句承诺落空，所以按大小把全部文件排一遍、够数即停。
  victim_kb=0
  victim_n=0
  # 一次 ls -l 把大小与路径一起带出来（删完再 ls 就读不到大小了），
  # 所以判"删了多少"用这份清单，收尾的真实回收量另用 du 复测。
  while read -r kb f; do
    [ -z "${f:-}" ] && continue
    if [ $APPLY -eq 1 ]; then
      rm -f "$f" || { echo "    ❌ 删除失败：$f"; FAILED=1; }
    fi
    victim_kb=$((victim_kb + kb))
    victim_n=$((victim_n + 1))
    [ "$victim_kb" -ge "$need_kb" ] && break
  done <<EOF
$(find "$cache" -type f -exec ls -l {} + 2>/dev/null | sort -k5 -n -r | awk '{ printf "%d %s\n", int($5 / 1024), $NF }')
EOF

  if [ $APPLY -eq 1 ]; then
    after_kb="$(du -sk "$cache" 2>/dev/null | awk '{print $1}')"
    freed_kb=$((size_kb - after_kb))
    TOTAL_FREED_KB=$((TOTAL_FREED_KB + freed_kb))
    printf "    删 %s 条 ⇒ 分配量 %s MB → %s MB（回收 %s MB）\n" \
      "$victim_n" "$((size_kb / 1024))" "$((after_kb / 1024))" "$((freed_kb / 1024))"
  else
    printf "    （dry-run）达上限需释放 %s MB，按大小排序可释放约 %s MB / %s 条\n" \
      "$((need_kb / 1024))" "$((victim_kb / 1024))" "$victim_n"
  fi
done

echo "════════════════════════════════════════"
if [ $APPLY -eq 0 ]; then
  echo "ℹ️ 本轮没有删除任何东西（加 --apply 才动手）"
  [ $FAILED -eq 1 ] && exit 2
  exit 0
fi
printf "✅ 共回收 %s MB\n" "$((TOTAL_FREED_KB / 1024))"
if [ $FAILED -eq 1 ]; then
  echo "❌ 过程中有目标没处理成功，见上面 ❌ 行"
  exit 2
fi
