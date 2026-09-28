import { describe, it, expect } from 'vitest'
import {
  STATUS_DISMISSED,
  STATUS_EXPORTED,
  STATUS_LABELED,
  STATUS_PENDING,
  canExport,
  canSubmitLabel,
  fixLayerOf,
  hasDismissReason,
  rowActions
} from '@/views/badCase/actions.js'

// Bad Case 队列的按钮判据用例（T-P8-03）。
//
// 这里钉住的是三句"多给一个按钮就有人吃到 409、少给一句约束就有脏样本进评测集"的判据：
// ① 终态（exported / dismissed）不给动作；② 判定依据必填；③ 导出只认"已打标"那一档的读数。
// 与后端 model/bad_case.go 的 BadCaseTransitions 同向：那边是权威，这里是界面副本，
// 所以用例里**不重新推一遍迁移表**，只逐点名单本卡被反复改错的那几格。

const row = (status, extra = {}) => ({ id: 'bc_1', status, ...extra })

describe('rowActions：一行的状态决定它能出现哪些按钮', () => {
  it('待判定的行：打标 + 撤销（顺序即从左到右）', () => {
    expect(rowActions(row(STATUS_PENDING), '7')).toEqual(['label', 'dismiss'])
  })

  it('已判定的行只剩撤销：重判会把 labeler_id 换成后一个人，结论的归属就假了', () => {
    expect(rowActions(row(STATUS_LABELED), '7')).toEqual(['dismiss'])
  })

  it('已导出/已撤销是终态，一个按钮都不给', () => {
    // exported 那一格是本卡最容易改错的：那行已经带着 eval_set_id 进了评测集，
    // 在队列里给它一个"撤销"等于让判案的人替一份**已经交出去的样本集**改历史。
    expect(rowActions(row(STATUS_EXPORTED), '7')).toEqual([])
    expect(rowActions(row(STATUS_DISMISSED), '7')).toEqual([])
  })

  it('值域外的状态一律不给按钮（与后端"迁移表里没有这条路就是不许"同一方向）', () => {
    for (const status of ['labeld', 'PENDING', '', 'pending_']) {
      expect(rowActions(row(status), '7')).toEqual([])
    }
  })

  it('没有身份时整页只读：动作端点全部要 operator，点了必回 401', () => {
    // 大小写/类型都要收敛成字符串比较：登录态里 id 可能是数字，
    // '7' 与 7 在 !me 这一判上必须是同一件事。
    expect(rowActions(row(STATUS_PENDING), '')).toEqual([])
    expect(rowActions(row(STATUS_PENDING))).toEqual([])
    expect(rowActions(null, '7')).toEqual([])
  })
})

describe('判定依据与撤销理由：必填就是必填', () => {
  it('类目给了但依据是空白 ⇒ 不能提交（后端对 note=="" 回 400）', () => {
    expect(canSubmitLabel('kb_missing', '库里有这条但写的是旧政策')).toBe(true)
    expect(canSubmitLabel('kb_missing', '')).toBe(false)
    expect(canSubmitLabel('kb_missing', '   ')).toBe(false)
    expect(canSubmitLabel('kb_missing', undefined)).toBe(false)
  })

  it('依据给了但类目没选 ⇒ 同样不能提交（后端对未知类目回 400）', () => {
    expect(canSubmitLabel('', '答非所问')).toBe(false)
    expect(canSubmitLabel('  ', '答非所问')).toBe(false)
    expect(canSubmitLabel(undefined, '答非所问')).toBe(false)
  })

  it('撤销理由：只认非空白字符串', () => {
    expect(hasDismissReason('这次答案其实是对的')).toBe(true)
    expect(hasDismissReason('')).toBe(false)
    expect(hasDismissReason('\n\t ')).toBe(false)
    expect(hasDismissReason(null)).toBe(false)
    // 数字 0 不是理由：`typeof note === 'string'` 这一句挡的就是这种"看起来有值"的输入
    expect(hasDismissReason(0)).toBe(false)
  })
})

describe('canExport：按钮点亮与否取自读数，不取自"队列看着非空"', () => {
  it('labeled>0 才能导', () => {
    expect(canExport({ by_status: { labeled: 3 } })).toBe(true)
  })

  it('labeled=0 时不能导，哪怕 pending 有一万条', () => {
    // 后端只取"已打标且未导出"的行。挂着一句"队列里有 10000 条"把按钮点亮，
    // 点下去必回 409，而那句 409 的真实含义是"一条都还没判" —— 界面本该先说这句话。
    expect(canExport({ by_status: { pending: 10000, labeled: 0 } })).toBe(false)
  })

  it('读数缺失/形状不对时按不可点处理，而不是按"大概能点"', () => {
    expect(canExport(null)).toBe(false)
    expect(canExport(undefined)).toBe(false)
    expect(canExport({})).toBe(false)
    expect(canExport({ by_status: null })).toBe(false)
    expect(canExport({ by_status: { labeled: '3' } })).toBe(true)
    expect(canExport({ by_status: { labeled: 'abc' } })).toBe(false)
  })
})

describe('fixLayerOf：责任层只读后端 taxonomy 回的那份表', () => {
  const taxonomy = { label_layer: { kb_missing: 'knowledge', intent_misjudge: 'intent' } }

  it('命中就回层', () => {
    expect(fixLayerOf('kb_missing', taxonomy)).toBe('knowledge')
    expect(fixLayerOf('intent_misjudge', taxonomy)).toBe('intent')
  })

  it('表里没有的类目回空串，不猜一个层', () => {
    // 猜的后果不是显示错一个词：这条链的下游是评测集，
    // "该找算法还是该找知识库"归错一层，整份统计里那一层就多算一条、另一层少算一条。
    expect(fixLayerOf('retrieve_miss', taxonomy)).toBe('')
    expect(fixLayerOf('', taxonomy)).toBe('')
  })

  it('taxonomy 缺失/形状不对回空串（前端不许有第二张表）', () => {
    expect(fixLayerOf('kb_missing', null)).toBe('')
    expect(fixLayerOf('kb_missing', {})).toBe('')
    expect(fixLayerOf('kb_missing', { label_layer: [] })).toBe('')
  })
})
