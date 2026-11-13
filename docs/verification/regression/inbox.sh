#!/usr/bin/env bash
# inbox 回归用例（台账 docs/verification/api-list.md）
set -uo pipefail
. "$(dirname "$0")/00-env.sh"

# 台账 #562-578｜inbox 全生命周期（读 + 状态翻转 + 标签 + 分配 + 统计 + 对账）
# 约定：操作 seed 会话 id=11；pin/star/mute/assign 用后复原
case_565_inbox_lifecycle() {
  CID=11
  echo "-- conversations list"; curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/conversations?page=1&page_size=3"; echo
  echo "-- detail";             curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/conversations/$CID"; echo
  echo "-- messages";           curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/conversations/$CID/messages?page=1&page_size=5"; echo
  echo "-- read";               curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/inbox/conversations/$CID/read"; echo
  echo "-- pin on/off";         curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"pinned":true}'  "$K4_BASE/api/inbox/conversations/$CID/pin"; echo
                                curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"pinned":false}' "$K4_BASE/api/inbox/conversations/$CID/pin"; echo
  echo "-- star on/off";        curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"starred":true}'  "$K4_BASE/api/inbox/conversations/$CID/star"; echo
                                curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"starred":false}' "$K4_BASE/api/inbox/conversations/$CID/star"; echo
  echo "-- mute on/off";        curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"muted":true}'  "$K4_BASE/api/inbox/conversations/$CID/mute"; echo
                                curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"muted":false}' "$K4_BASE/api/inbox/conversations/$CID/mute"; echo
  echo "-- tag add/remove";     curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' -d '{"tag":"k4-temp-tag"}' "$K4_BASE/api/inbox/conversations/$CID/tags"; echo
                                curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/inbox/conversations/$CID/tags/k4-temp-tag"; echo
  echo "-- msg delete 非法 source（应 400 无效的消息来源）"
  curl -sS -m 20 -X DELETE -H "$(auth)" "$K4_BASE/api/inbox/conversations/$CID/messages/999999?source=inbox"; echo
  echo "-- stats";              curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/stats"; echo
  echo "-- assignments";        curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/assignments?page=1&page_size=3"; echo
  echo "-- agents";             curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/agents"; echo
  echo "-- staff load";         curl -sS -m 20 -H "$(auth)" "$K4_BASE/api/inbox/staff/2/load"; echo
  echo "-- reconcile 非法 mode（应 400 列举合法值）"
  curl -sS -m 20 -X POST -H "$(auth)" "$K4_BASE/api/inbox/reconcile?mode=full"; echo
  echo "-- reconcile mode=unread"
  curl -sS -m 60 -X POST -H "$(auth)" "$K4_BASE/api/inbox/reconcile?mode=unread"; echo
}

# 台账 #572/#573｜assign human 语义（to_type 枚举门 + 分配 + 复原）
case_572_inbox_assign() {
  echo "-- to_type=user（应 400 invalid assign_to）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"conversation_id":11,"action":"assign","to_type":"user","to_user_id":"2"}' \
    "$K4_BASE/api/inbox/assign"; echo
  echo "-- assign to user 2"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"conversation_id":11,"action":"assign","to_type":"human","to_user_id":"2","remark":"k4 regression"}' \
    "$K4_BASE/api/inbox/assign"; echo
  echo "-- 复原回 user 3"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"conversation_id":11,"action":"assign","to_type":"human","to_user_id":"3","remark":"revert"}' \
    "$K4_BASE/api/inbox/assign"; echo
  echo "-- auto assign（candidates 必填）"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"conversation_id":11,"candidates":["2","3"],"mode":"round_robin"}' \
    "$K4_BASE/api/inbox/assign/auto"; echo
  echo "-- 复原回 user 3"
  curl -sS -m 20 -X POST -H "$(auth)" -H 'Content-Type: application/json' \
    -d '{"conversation_id":11,"action":"assign","to_type":"human","to_user_id":"3","remark":"revert"}' \
    "$K4_BASE/api/inbox/assign"; echo
}

"$@"
