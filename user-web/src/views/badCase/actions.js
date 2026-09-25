// actions.js —— Bad Case 队列（T-P8-03）的按钮判据：一行的 status → 能出现哪些动作。
//
// 为什么单列一个文件而不是写在模板的 v-if 里：这几条判据里有两条是**产品红线**
// —— exported/dismissed 是终态，"撤销"只对还没导出的行开放。它们一旦以 v-if 的形式散在
// 模板里，就只能靠人眼 review，而下一次改模板的人不知道这条为什么在（看起来就是少写了一个按钮）。
// 写在这里它就有用例（tests/unit/badCase_actions.test.js）。
//
// 与后端 internal/model/bad_case.go 的 BadCaseTransitions 同口径：
// pending → labeled | dismissed，labeled → exported | dismissed，exported/dismissed → 无。
// 这里少给一个按钮是收敛，多给一个是让判案的人吃到 409。
// 未知 status 一律不给按钮，与后端"迁移表里没有这条路就是不许"同一方向。
//
// 五层归属：本文件只有判据，没有任何 HTTP 调用（那归 api/badCase.js）。

export const STATUS_PENDING = 'pending'
export const STATUS_LABELED = 'labeled'
export const STATUS_EXPORTED = 'exported'
export const STATUS_DISMISSED = 'dismissed'

// OPEN_STATUSES 还挂在队列里、还需要人给结论的两个态。
// 判完（labeled）没导出的行仍要看得见 —— 它是"这一层还没修"的证据，
// 从默认视图里抹掉它等于把待修的账本合进已结案的那一本。
export const OPEN_STATUSES = [STATUS_PENDING, STATUS_LABELED]

/**
 * 一行坏例可执行的动作。
 *
 * @param row 后端返回的坏例行（字段名沿用 JSON 蛇形）
 * @param me  当前登录者 id 的字符串形态；空串 = 没有身份
 * @returns {'label'|'dismiss'}[] 顺序即按钮从左到右
 */
export function rowActions(row, me = '') {
  // 打标与撤销都要 operator（labeler_id 是"这条结论谁下的"的唯一出处，
  // 后端没有身份回 401）。没有身份还给按钮，点下去就是一句"未登录"，而这个人确实是登录着的。
  if (!me) return []
  if (!row) return []
  if (row.status === STATUS_PENDING) return ['label', 'dismiss']
  if (row.status === STATUS_LABELED) return ['dismiss']
  // exported / dismissed / 未知值：终态或不认识，都不给按钮。
  // exported 尤其不能给"撤销"——那行已经带着 eval_set_id 进了评测集，
  // 在这儿把它判死等于悄悄改掉一份已经交出去的样本集。
  return []
}

/**
 * 类目现在能不能提交。
 *
 * 两条都得成立：类目在值域内 **且** 判定依据非空。依据必填是后端的 400，
 * 但把它做成"点了才知道"的表单错误，判案的人会在队列里逐条撞一遍；
 * 按钮直接禁用是把这句约束放进界面。
 */
export function canSubmitLabel(label, note) {
  if (typeof label !== 'string' || label.trim() === '') return false
  return typeof note === 'string' && note.trim().length > 0
}

/** 撤销理由给了没有（后端对空理由回 400，这一句是前端唯一的闸门）。 */
export function hasDismissReason(reason) {
  return typeof reason === 'string' && reason.trim().length > 0
}

/**
 * 导出按钮可点不可点，判据取自读数里的 labeled 数。
 *
 * 为什么看 labeled 而不是总数：后端只取"已打标且未导出"的行，
 * 挂着"队列里有 500 条"就把按钮点亮，点下去必回 409，
 * 而那一句 409 的真正含义是"这 500 条一条都还没判"—— 界面本该先说出这件事。
 * 读数缺失（还没轮询到）时按不可点处理，而不是按"大概能点"。
 */
export function canExport(stats) {
  const labeled = Number(stats?.by_status?.[STATUS_LABELED])
  return Number.isFinite(labeled) && labeled > 0
}

/**
 * 类目的责任层。只读后端 taxonomy 回的那份 label_layer，前端不抄第二张表。
 *
 * 抄一份的后果不是显示错一个词：库里加一个类目而前端没加，
 * 页面上就"选不到它"，而这条链的下游是评测集 —— 选不到等于统计里少一层，
 * 且少的那一层没人知道。取不到时回空串（调用方据此不显示，而不是显示猜的）。
 */
export function fixLayerOf(label, taxonomy) {
  const table = taxonomy?.label_layer
  if (!table || typeof table !== 'object') return ''
  return table[label] || ''
}
