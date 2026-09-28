// actions.js 待办中心（N-9）的按钮判据：一行的 (kind, status, 认领人) → 能出现哪些动作。
//
// 为什么单列一个文件而不是写在模板的 v-if 里：这三条判据里有一条是**产品红线**
// —— 审批类待办不许有"完成"也不许有"撤销"（服务端同一判据，见 internal/model/human_task.go
// 的 humanTaskActionKinds）。它一旦以 v-if 的形式散在模板里，就只能靠人眼 review，
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
    // 只有 decide：审批类待办不许有"完成"也不许有"撤销"。
    // 把还在等的审批待办从池子里关掉（无论哪一颗），approval_requests 那一行仍是 pending ——
    // 流程还挂着，而没人看得见它在等。这就是拆闸门。
    // 这条红线现在**服务端也认**：humanTaskActionKinds 里 complete/cancel 都不再开放给
    // approval 类，待办侧点下去是 409 且提示会指回 /api/approvals/:id/decide。
    // 前端因此少给按钮是收敛：不再有人先吃到那一句 409。
    // 裁决（批准/驳回）不属于这里：它走 approval_requests，落定后由系统收口这条待办。
    return ['decide']
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
 * 两份后端读数一起用，各管一件事：
 *  - allowed_transitions：这一行在状态机上去得了哪些态（"pending 能变成 approved"）；
 *  - decidable_by_human：人现在还有没有机会去落其中一个（那扇已经关上的门）。
 * 只读第一份会画出"按钮亮着、点下去永远 409"的形状：TTL 到期到清扫器跑上来之间
 * 最多有一整个轮次（实测 5 分钟），那段时间里状态照旧是 pending、三个目标照单全列，
 * 而服务端已经不收这次裁决了 —— 流程早就按超时被推走了。
 *
 * 取 true 才给按钮（不是"取 false 才收起"）：字段缺失/后端没升级时，宁可少给两颗按钮
 * （坐席看见的是"这条批不动"，会去查），也不能给一颗点了会写出假审计账的按钮。
 * 判据本身仍取自后端回显，前端不写死跃迁表：跃迁归 approval_requests 所有，
 * 抄一份就是第二个事实源（它会在后端加第四个状态的那天准时失真）。
 */
export function verdictButtons(approval) {
  if (!approval || approval.status !== 'pending') return []
  if (approval.decidable_by_human !== true) return []
  const allowed = Array.isArray(approval.allowed_transitions) ? approval.allowed_transitions : []
  return HUMAN_VERDICTS.filter((v) => allowed.includes(v))
}

/**
 * 按钮为什么不在了 —— 给操作者的一句实话。
 *
 * 少了这一句，"没有按钮"读起来像"我没有权限"，而真原因是"这扇门的时间已经用完了"。
 * 两者的处置方向完全相反（前者去找管理员开权限，后者去修流程的 TTL 或看清扫轮次）。
 *
 * 三种情况分三句，其中第三种不许并进第二种：读数**缺失**时断言"时限已过"，
 * 就是把"没读到"说成"读到了一个坏消息"（同后端"读故障不许伪装成空态"那条口径）。
 */
export function verdictBlockReason(approval) {
  if (!approval || approval.status !== 'pending') return ''
  const flag = approval.decidable_by_human
  if (flag === true) return ''
  if (flag === false) return '挂起时限已过：这条已经按超时收口，本次不会有人工裁决入口。'
  return '审批详情里没有「现在还能不能人工裁决」这一格读数，本次不提供裁决入口（前后端版本不一致）。'
}
