import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'

// CH-37b 刀5：消息中心列表的 unknown（结果不明）行露出格。
//
// unknown 终态（「已提交但结果不明」，永不自动重发、待人工对账）在刀4 落地后，
// 列表页此前完全没有行级 status 的展示——运营在消息中心看不到哪条出站处于待对账，
// 只能去查库。本文件钉住：出站行 status='unknown' 时状态列出现「结果不明」warning
// 标签；delivered/failed 出站与入站行不出现（unknown 不冒充已送达/失败，也不误报）。
// 判据读的是渲染出来的标签文本与 class（warning 型），不是源码。

const api = vi.hoisted(() => ({
  getPlatforms: vi.fn(),
  getStats: vi.fn(),
  getMessages: vi.fn(),
  getMessageById: vi.fn(),
  markRead: vi.fn(),
  recallMessage: vi.fn(),
  pushMessage: vi.fn(),
}))
vi.mock('@/api/messageHub', () => ({ messageHubApi: api }))

const csApi = vi.hoisted(() => ({ getMyAgent: vi.fn() }))
vi.mock('@/api/customerService', () => csApi)

// AgentSocket 在 jsdom 下不起真连接，桩成空壳（onNewMessage 等回调永远不触发）。
const sock = vi.hoisted(() => ({ default: vi.fn(function () { return { connect: vi.fn(), close: vi.fn() } }) }))
vi.mock('@/utils/agentSocket', () => sock)

import List from '@/views/messageHub/List.vue'

const row = (over = {}) => ({
  id: over.id ?? 1,
  platform: 'douyin',
  account_id: 'acc-1',
  msg_id: 'm-' + (over.id ?? 1),
  msg_type: 'text',
  direction: 'outbound',
  is_read: true,
  is_group: false,
  is_ai_reply: false,
  recalled_at: null,
  recall_error: '',
  content: 'hello',
  sent_at: '2026-11-08T10:00:00Z',
  ...over,
})

let current = null

async function mountList(rows) {
  api.getPlatforms.mockResolvedValue({ platforms: ['douyin'], msg_types: ['text'] })
  api.getStats.mockResolvedValue({ total: rows.length, outbound_unknown: 1 })
  api.getMessages.mockResolvedValue({ list: rows, total: rows.length })
  csApi.getMyAgent.mockRejectedValue(new Error('no agent'))
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(List, {
    global: { plugins: [ElementPlus], mocks: { $t: (k) => k } },
    attachTo: host,
  })
  current = wrapper
  await settle(wrapper)
  await settle(wrapper)
  return wrapper
}

// 状态列标签的（文本, class）对：只看表格体第一行，避免分页/汇总区干扰。
const firstRowTags = (wrapper) => {
  const tr = wrapper.find('.el-table__body tr')
  expect(tr.exists(), '表格体应至少一行').toBeTruthy()
  return Array.from(tr.element.querySelectorAll('.el-tag')).map((t) => ({
    text: t.textContent.trim(),
    cls: t.className,
  }))
}

describe('消息中心列表的 unknown 行露出', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    csApi.getMyAgent.mockReset()
  })
  afterEach(() => {
    try {
      current?.unmount()
    } catch {
      /* 重复卸载不抛即可 */
    }
    current = null
    vi.restoreAllMocks()
    document.body.replaceChildren()
  })

  it('出站 unknown 行出现「结果不明」warning 标签', async () => {
    const wrapper = await mountList([row({ id: 7, status: 'unknown', push_error: 'ack_unknown' })])
    const tags = firstRowTags(wrapper)
    const unknownTag = tags.find((t) => t.text === '结果不明')
    expect(unknownTag, `状态列应含「结果不明」标签，实得 ${JSON.stringify(tags)}`).toBeTruthy()
    expect(unknownTag.cls).toContain('el-tag--warning')
  })

  it('delivered / failed / 入站行不出现「结果不明」', async () => {
    const wrapper = await mountList([
      row({ id: 1, status: 'delivered' }),
      row({ id: 2, status: 'failed', push_error: 'placeholder-account' }),
      row({ id: 3, status: 'unknown', direction: 'inbound' }),
      row({ id: 4, status: 'pending' }),
    ])
    // 四行都断言：逐行扫标签，任何一行出现「结果不明」都算误报。
    const trs = wrapper.findAll('.el-table__body tr')
    expect(trs.length).toBe(4)
    for (const tr of trs) {
      const texts = Array.from(tr.element.querySelectorAll('.el-tag')).map((t) => t.textContent.trim())
      expect(texts, `行不应露出「结果不明」: ${JSON.stringify(texts)}`).not.toContain('结果不明')
    }
  })

  it('撤回行优先保持既有展示（recalled 标签仍在，不受 unknown 分支影响）', async () => {
    const wrapper = await mountList([
      row({ id: 9, status: 'unknown', recalled_at: '2026-11-08T11:00:00Z' }),
    ])
    const tags = firstRowTags(wrapper)
    // 撤回是另一个维度（既有注释口径）：recalled 标签继续露出，unknown 标签不与之互斥也不重复。
    expect(tags.some((t) => t.text.includes('已撤回') || t.text.includes('recalled')), `撤回标签应仍在: ${JSON.stringify(tags)}`).toBeTruthy()
  })
})
