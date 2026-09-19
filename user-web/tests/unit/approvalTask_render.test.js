import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'

// 组件渲染用例：证明"审批类待办没有裸完成"这条红线**穿过了模板**，而不只是活在 actions.js 里。
//
// 为什么 actions.js 已经有单测还要这一份：那里绿了只说明判据对，模板完全可以无视判据
// —— 有人在 <el-table-column label="操作"> 里手写一个"完成"按钮，actions.js 一个字都不用改。
// 所以这里数的是**渲染出来的按钮**：完成 2 个、裁决 1 个，多一个少一个都红。
//
// 三个 api/store 依赖被 mock 掉是为了把用例钉在"渲染"这一件事上：
// 查询串长什么样有 api_humanTask_query.test.js，端点归属有 api_humanTask.test.js，
// 这里再断一遍就是三份事实源。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  counts: vi.fn(),
  claim: vi.fn(),
  release: vi.fn(),
  complete: vi.fn(),
  cancel: vi.fn(),
  getApproval: vi.fn(),
  decide: vi.fn()
}))

vi.mock('@/api/humanTask.js', () => ({
  humanTaskApi: {
    list: api.list,
    counts: api.counts,
    claim: api.claim,
    release: api.release,
    complete: api.complete,
    cancel: api.cancel
  }
}))
vi.mock('@/api/approval.js', () => ({
  approvalApi: { get: api.getApproval, decide: api.decide }
}))
// 坐席身份：id '7'。别人认领的行（assignee 8）因此不该出现"释放"。
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ userInfo: { id: '7', role: 'manager' } })
}))

import List from '@/views/approvalTask/List.vue'

const ROWS = [
  { id: 'ht_1', kind: 'conversation_handoff', status: 'pending', title: '转人工：报价咨询' },
  {
    id: 'ht_2',
    kind: 'approval',
    status: 'pending',
    title: '待审批：quote.send',
    subject_type: 'approval_request',
    subject_id: 'apr_1',
    sla_decide_at: '2026-09-20T08:00:00Z'
  },
  {
    id: 'ht_3',
    kind: 'collection_escalation',
    status: 'claimed',
    assignee_user_id: '8',
    title: '催收升级：D7 未回款'
  }
]

const APPROVAL_DETAIL = {
  id: 'apr_1',
  status: 'pending',
  policy_key: 'quote.send',
  subject_type: 'sop_node',
  subject_id: '4242/w1',
  expires_at: '2026-09-20T10:00:00Z',
  allowed_transitions: ['approved', 'rejected', 'expired']
}

const countButtons = (wrapper, text) =>
  wrapper.findAll('button').filter((b) => b.text().trim() === text).length

async function mountPage(rows = ROWS) {
  api.list.mockResolvedValue({ list: rows, total: rows.length })
  api.counts.mockResolvedValue({ at: '2026-09-20T09:00:00Z', total_open: rows.length, by_kind: [] })
  const wrapper = mount(List, { attachTo: document.body, global: { plugins: [createPinia(), ElementPlus] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('待办中心渲染：三类一起列，按钮按类分', () => {
  it('三类待办都在表格里（AC① 的"能列"）', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()
    for (const row of ROWS) expect(text).toContain(row.title)
    expect(text).toContain('会话转人工')
    expect(text).toContain('待审批')
    expect(text).toContain('催收升级')
  })

  it('"完成"只出现两次：会话类与催收类，审批类那一行没有（AC① 的"不许拆闸门"）', async () => {
    const wrapper = await mountPage()
    expect(countButtons(wrapper, '完成')).toBe(2)
    expect(countButtons(wrapper, '裁决')).toBe(1)
  })

  it('认领只给会话类的待处理行；释放只给挂在我名下的行', async () => {
    const wrapper = await mountPage()
    expect(countButtons(wrapper, '认领')).toBe(1)
    expect(countButtons(wrapper, '释放')).toBe(0)
    expect(countButtons(wrapper, '撤销')).toBe(3)
  })

  it('审批行按自己那一档 SLA（sla_decide_at）判逾期，不看别人的截止', async () => {
    // 审批的裁决截止 08:00Z 早于读数时刻 09:00Z ⇒ 恰好一行标红。
    // 判据写成"有几行标红"而不是"文案里有没有时间"：后者会把'未设截止'也算成通过。
    const wrapper = await mountPage()
    expect(wrapper.findAll('.overdue')).toHaveLength(1)

    const futureRows = ROWS.map((r) => (r.kind === 'approval' ? { ...r, sla_decide_at: '2026-09-20T23:00:00Z' } : r))
    const later = await mountPage(futureRows)
    expect(later.findAll('.overdue')).toHaveLength(0)
  })
})

describe('裁决抽屉：批准/驳回，且驳回没有理由就点不动', () => {
  it('点"裁决"读的是待办 subject_id 指向的那条审批', async () => {
    api.getApproval.mockResolvedValue(APPROVAL_DETAIL)
    const wrapper = await mountPage()
    const decideBtn = wrapper.findAll('button').find((b) => b.text().trim() === '裁决')
    await decideBtn.trigger('click')
    await flushPromises()
    expect(api.getApproval).toHaveBeenCalledWith('apr_1')
  })

  it('抽屉里只有 批准 / 驳回 两个结论按钮，没有 "已到期"', async () => {
    api.getApproval.mockResolvedValue(APPROVAL_DETAIL)
    const wrapper = await mountPage()
    wrapper.findAll('button').find((b) => b.text().trim() === '裁决').trigger('click')
    await flushPromises()
    const body = document.body.textContent
    expect(body).toContain('批准')
    expect(body).toContain('驳回')
    expect(countButtons(wrapper, '已到期')).toBe(0)
  })

  it('理由为空时驳回按钮是禁用的，填了才可点（后端不强制，这里是唯一闸门）', async () => {
    api.getApproval.mockResolvedValue(APPROVAL_DETAIL)
    const wrapper = await mountPage()
    wrapper.findAll('button').find((b) => b.text().trim() === '裁决').trigger('click')
    await flushPromises()

    const reject = () =>
      Array.from(document.querySelectorAll('button')).find((b) => b.textContent.trim() === '驳回')
    expect(reject().disabled).toBe(true)

    const textarea = document.querySelector('textarea')
    expect(textarea, '裁决说明输入框必须存在').toBeTruthy()
    textarea.value = '金额与报价单不符'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    expect(reject().disabled).toBe(false)
  })

  it('驳回按钮带着理由落进 decide()，verdict 是 rejected', async () => {
    api.getApproval.mockResolvedValue(APPROVAL_DETAIL)
    api.decide.mockResolvedValue({ ...APPROVAL_DETAIL, status: 'rejected', decided_by: '7' })
    const wrapper = await mountPage()
    wrapper.findAll('button').find((b) => b.text().trim() === '裁决').trigger('click')
    await flushPromises()
    const textarea = document.querySelector('textarea')
    textarea.value = '金额与报价单不符'
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
    Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent.trim() === '驳回')
      .click()
    await flushPromises()
    expect(api.decide).toHaveBeenCalledWith('apr_1', 'rejected', '金额与报价单不符')
  })
})
