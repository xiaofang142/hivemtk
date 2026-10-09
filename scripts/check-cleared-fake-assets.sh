#!/usr/bin/env bash
# 已清零的"假性未完成"资产不得原样长回来。
#
# 为什么单独一个脚本而不是往 scripts/check-unwired-assets.sh 里加行：那个脚本此刻正被另一条
# 泳道改（git status 里是 M），共享 git 索引下同一文件不能叠两次改动。本脚本登记的条目
# 全部来自 docs/architecture/PSEUDO_INCOMPLETE_INVENTORY_2026-10.md 的 A 类，判据形状与那条目
# 一样只有两种：某个说谎的符号/文件必须不再存在，或某段真接线必须还在。
#
# 三种退出码：0 通过；1 有条目不符合预期；2 连被检查的文件都不在（"扫不到"不等于"没问题"）。
# 用法：scripts/check-cleared-fake-assets.sh [--repo DIR]
set -uo pipefail

REPO="."
while [ $# -gt 0 ]; do
  case "$1" in
    --repo) REPO="$2"; shift 2 ;;
    *) echo "未知参数: $1" >&2; exit 2 ;;
  esac
done

SERVER="${REPO}/user-server/internal"
pass=0
fail=0
missing=0

# gone <文件> <模式> <说明>
gone() {
  local f="${SERVER}/$1" pat="$2" why="$3"
  if [ ! -f "$f" ]; then
    printf 'BROKEN  缺文件 %s（本条要断言 %s 不在里面）\n' "$f" "$why"
    missing=$((missing + 1))
    return
  fi
  if grep -qF -- "$pat" "$f"; then
    printf 'FAIL    %s 里又出现了 %s\n' "$f" "$pat"
    fail=$((fail + 1))
  else
    printf 'ok      不存在: %s\n' "$why"
    pass=$((pass + 1))
  fi
}

# here <文件> <模式> <说明>
here() {
  local f="${SERVER}/$1" pat="$2" why="$3"
  if [ ! -f "$f" ]; then
    printf 'BROKEN  缺文件 %s（本条要断言 %s 在里面）\n' "$f" "$why"
    missing=$((missing + 1))
    return
  fi
  if grep -qF -- "$pat" "$f"; then
    printf 'ok      在场: %s\n' "$why"
    pass=$((pass + 1))
  else
    printf 'FAIL    %s 里找不到 %s\n' "$f" "$pat"
    fail=$((fail + 1))
  fi
}

# absent <相对仓库根的路径> <说明>
absent() {
  local f="${REPO}/$1" why="$2"
  if [ -e "$f" ]; then
    printf 'FAIL    %s 又回来了（它曾是一个零装配点的假资产）\n' "$f"
    fail=$((fail + 1))
  else
    printf 'ok      文件仍不存在: %s\n' "$why"
    pass=$((pass + 1))
  fi
}

echo "── 说谎的形状不得长回来 ──"
gone "service/smart_cs_orchestrator.go" "func SetGlobalFAQAnswerCache" \
  "包级注入口（全仓零调用方 ⇒ 编排器恒拿 nil 缓存）"
gone "service/smart_cs_orchestrator.go" "globalFAQCache" \
  "只写不读的答案缓存全局"
gone "bridge/reach_adapter.go" "GlobalBridgeReachAdapter" \
  "只写不读的 bridge 触达适配器全局"
gone "router/router.go" "GlobalBridgeReachAdapter" \
  "装配点往没人读的格子里写"
gone "app/sales_engine_factory.go" "func RegisterAgentPrivateMessageTools" \
  "注释自称调用方是 router.Setup() 的重复装配口"
gone "aiagent/agent/tooluse/private_message_tools.go" "func RegisterPrivateMessageTools" \
  "只喂上面那个死装配口的注册包装"
gone "service/reach_pipeline.go" "func InitReachPipelineService" \
  "触达流水线全局单例的 Init（Get 恒回 nil）"
gone "service/reach_pipeline.go" "func GetReachPipelineService" \
  "触达流水线全局单例的 Get"
gone "repository/faq_entry.go" "ListByKB" \
  "吃掉 kbID 参数返回全表的 FAQ 读法"
gone "repository/sop_template.go" "ListByKB" \
  "吃掉 kbID 参数返回全表的 SOP 读法"
gone "aiagent/knowledge/repository/knowledge_document.go" "ListByKB" \
  "吃掉 kbID 参数返回全表的文档读法"
absent "user-server/internal/aiagent/agent/tooluse/production_reach_adapter.go" \
  "ProductionReachAdapter（零构造点）"
absent "user-server/internal/dto/knowledge_base.go" \
  "KB DTO（含从未被读过的 MemberCount/DocCount）"

echo "── 真接线的形状不得退回去 ──"
here "service/reach_pipeline_dispatch.go" "没有装配真实发送器" \
  "未装配发送器时必须报错，而不是编一个假消息号"
here "router/service_routes.go" "SetReachSender(app.NewPipelineReachSender(db))" \
  "调度器的发送器装配点（没有 if sender != nil 那一档）"
here "service/workflow_node_executor.go" "NewSubflowNodeExecutor(orch)" \
  "子流程节点必须拿到编排器，否则它把没跑的子流程记成已完成"
here "app/sales_engine_factory.go" "attachFAQAnswerCache(o, gormDB)" \
  "语义答案缓存的装配点"
here "app/faq_cache_wiring.go" "faq_answer_enabled" \
  "缓存开关与参数中心条目的对应关系"
here "model/knowledge_base.go" "没有任何写入路径维护" \
  "冗余统计列的实测口径（别再写回“实际从内容表 COUNT”）"

printf '\n读数: 通过 %d / 违规 %d / 检查对象缺失 %d\n' "$pass" "$fail" "$missing"
if [ "$missing" -gt 0 ]; then
  echo "❌ 检查对象本身缺失，本门没有判定资格（不是通过）"
  exit 2
fi
if [ "$fail" -gt 0 ]; then
  echo "❌ 已清零的假性未完成资产出现回归"
  exit 1
fi
echo "✅ ${pass} 条判据全部成立"
