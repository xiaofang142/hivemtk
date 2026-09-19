// actions.js 待办中心（N-9）的按钮判据：一行的 (kind, status, 认领人) → 能出现哪些动作。
//
// 为什么单列一个文件而不是写在模板的 v-if 里：这三条判据里有一条是**产品红线**
// —— 审批类待办不许有"完成"按钮。它一旦以 v-if 的形式散在模板里，就只能靠人眼 review，
// 而下一次改模板的人不知道这条为什么在（看起来就是"少写了一个按钮"）。
// 写在这里它就有用例（tests/unit/approvalTask_actions.test.js）。
//
// 与后端 internal/model/human_task.go 的两张表（humanTaskActionKinds /
// humanTaskActionTargets）同口径：**这里少给一个按钮是收敛，多给一个是让坐席吃到 409**。
// 未知 kind / 未知 status 一律不给按钮，与后端"判定表里没有这条路就是不许"同一方向。
//
// 五层归属：本文件只有判据，没有任何 HTTP 调用（那归 api/humanTask.js）。

export const KIND_HANDOFF = 'conversation_handoff'
export const KIND_APPROVAL = 'approval'
export const KIND_COLLECTION = 'collection_escalation'

// OPEN_STATUSES 还占着池子的两个态。
const OPEN_STATUSES = ['pending', 'claimed']
const STATUS_PENDING = 'pending'
const STATUS_CLAIMED = 'claimed'

// KIND_SLA_FIELD 每类读自己那一列 SLA。
// 写死三列名而不是"取第一个非空的 sla_*"：后者会让一条审批待办显示成它的会话首响时刻，
// 而值班照着那个时间去催的是另一件事的截止。与后端 HumanTaskSLAField 一一对应。
const KIND_SLA_FIELD = {
  [KIND_HANDOFF]: 'sla_first_response_at',
  [KIND_APPROVAL]: 'sla_decide_at',
  [KIND_COLLECTION]: 'sla_escalate_at'
}

/** 这条待办的 SLA 列名；未知类回空串（调用方据此不显示截止，而不是显示别人的截止）。 */
export function slaField(kind) {
  return KIND_SLA_FIELD[kind] || ''
}

/**
 * 一行待办可执行的动作。
 *
 * @param task 后端返回的待办行（字段名沿用 JSON 蛇形）
 * @param me   当前登录者 id 的字符串形态；空串 = 没有身份
 * @returns {'decide'|'claim'|'release'|'complete'|'cancel'}[] 顺序即按钮从左到右的顺序
 */
export function rowActions(task, me = '') {
  // 动作类端点全部要 operator（后端没有身份回 401）。没有身份还给按钮，
  // 点下去就是一句"未登录或会话里没有 user id"，而这个人确实是登录着的 —— 更难查。
  if (!me) return []
  if (!task || !OPEN_STATUSES.includes(task.status)) return []
  if (task.kind === KIND_APPROVAL) {
    // 没有 complete：把审批待办"完成"掉，池子里那条消失了而 approval_requests
    // 仍是 pending —— 流程还挂着，而没人看得见它在等。这就是拆闸门。
    // 注意后端**允许**对 approval 类调 Complete（humanTaskActionKinds 里 complete 三类通用），
    // 所以这是一条只在前端的红线；要收到服务端得给那张表加 kind 判据（已进本卡交接清单）。
    return ['decide', 'cancel']
  }
  if (task.kind === KIND_COLLECTION) return ['complete', 'cancel']
  if (task.kind !== KIND_HANDOFF) return []
  const acts = []
  if (task.status === STATUS_PENDING) acts.push('claim')
  // 别人认领的不给 release：旁人替放等于悄悄把 A 正在处理的会话转回池子而 A 不知道
  // （后端同一判据回 403）。complete 照给 —— "这件事做完了"是一句关于世界的事实，谁点的不影响真假。
  else if (task.status === STATUS_CLAIMED && String(task.assignee_user_id || '') === String(me)) {
    acts.push('release')
  }
  acts.push('complete', 'cancel')
  return acts
}

/**
 * 驳回理由给了没有。
 *
 * 后端对驳回理由**不强制**（裁决与驳回走同一个 Decide，note 可为空），
 * 所以这一句是驳回理由的唯一闸门。少了它，"驳回了"和"为什么驳回"就会在复盘时分成两件事。
 */
export function hasRejectReason(note) {
  return typeof note === 'string' && note.trim().length > 0
}

// HUMAN_VERDICTS 人给得出的两个结论，顺序即按钮顺序。
// 表里没有 'expired'：到期是清扫器按 TTL 落的状态，不是人的结论。
// 把它做成按钮等于让人替时间签字 —— 而 approval_requests.decided_by 会记下那个人的 id，
// 事后复盘"这批为什么到期没人批"时，那条记录就在误导下一个读它的人。
const HUMAN_VERDICTS = ['approved', 'rejected']

/**
 * 一条审批详情上现在能点哪两个结论。
 *
 * 判据取自后端回显的 allowed_transitions，而不是前端自己再写一张跃迁表：
 * 跃迁归 approval_requests 所有，前端抄一份就是第二个事实源（它会在后端加第四个
 * 状态的那天准时失真）。这里只在这份清单上**做一次交集**，把 expired 筛掉。
 */
export function verdictButtons(approval) {
  if (!approval || approval.status !== 'pending') return []
  const allowed = Array.isArray(approval.allowed_transitions) ? approval.allowed_transitions : []
  return HUMAN_VERDICTS.filter((v) => allowed.includes(v))
}
