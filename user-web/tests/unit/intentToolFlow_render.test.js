import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'

// 意图→工具调用流编排页（TB-02）的渲染与交互用例。
//
// 该页是「意图编排基座」的管理入口（用户唯一可感知面），钉五件事：
//   1. 词表三源（意图含家族归属/模板变量/工具名）来自 vocab 端点，页面不写死——
//      后端注册中心新增工具后，下拉自动可选；
//   2. 调用流预览按启用步渲染为 tool1 → tool2 链式形态（禁用步不出现）；
//   3. 参数模板用了白名单外变量的配置必须被客户端前置拦截（服务端 ValidateChainArgs 兜底）；
//   4. 未闭合占位符拦截；
//   5. 步骤晋升/上移下移的顺序语义：保存时 priority = 行号（顺序即配置）。
//
// api 全 mock（端点归属在 api 模块层），这里只钉"渲染 + 交互 + 校验"。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn(),
  vocab: vi.fn()
}))

vi.mock('@/api/intentToolChain', () => ({
  listIntentToolChains: api.list,
  createIntentToolChain: api.create,
  updateIntentToolChain: api.update,
  deleteIntentToolChain: api.remove,
  getIntentToolChainVocab: api.vocab
}))

import ToolFlow from '@/views/intentRecognition/ToolFlow.vue'
import { settle } from './settle-helper'

const mounted = []

function mountPage() {
  const w = mount(ToolFlow, { global: { plugins: [ElementPlus] } })
  mounted.push(w)
  return w
}

afterEach(() => {
  while (mounted.length) {
    try {
      mounted.pop().unmount()
    } catch {
      /* 重复卸载不抛即可 */
    }
  }
  document.body.innerHTML = ''
})

const VOCAB = {
  intents: ['ask_service', 'after_sale', 'order_status', 'greeting', 'complaint'],
  intent_families: { ask_service: 'service', after_sale: 'service', order_status: 'runtime', greeting: 'both', complaint: 'both' },
  template_vars: ['customer_id', 'session_id', 'content', 'channel_type', 'account_id'],
  tools: ['order.lookup', 'logistics.track', 'knowledge.search', 'card.show', 'follow_task.create']
}

const ROW = {
  id: 1,
  name: '快递/物流查询调用流（默认）',
  intent_code: 'ask_service',
  steps_json: JSON.stringify([
    { tool_name: 'order.lookup', args: { customer_id: '{{customer_id}}' }, priority: 1, enabled: true },
    { tool_name: 'logistics.track', args: {}, priority: 2, enabled: true },
    { tool_name: 'knowledge.search', args: {}, priority: 3, enabled: false }
  ]),
  reply_hint: '先查订单再查物流',
  enabled: true,
  updated_at: '2026-11-10 10:00:00'
}

beforeEach(() => {
  for (const fn of [api.list, api.create, api.update, api.remove, api.vocab]) {
    fn.mockReset()
  }
  api.vocab.mockResolvedValue({ data: VOCAB })
  api.list.mockResolvedValue({ data: { list: [ROW], total: 1 } })
  api.create.mockResolvedValue({ code: 0 })
  api.update.mockResolvedValue({ code: 0 })
  api.remove.mockResolvedValue({ code: 0 })
})

describe('意图→工具调用流编排页（TB-02）', () => {
  it('列表渲染：链式预览只含启用步，禁用步不出现；家族归属有标注', async () => {
    const w = mountPage()
    await settle(w)
    const text = w.text()
    expect(text).toContain('快递/物流查询调用流（默认）')
    expect(text).toContain('order.lookup → logistics.track')
    expect(text).not.toContain('knowledge.search →')
    // 家族标注：ask_service 属 service 家族（客服主链）
    expect(text).toContain('客服主链')
    // 变量白名单标签来自 vocab（el-tag 内容在 DOM 中）
    expect(w.html()).toContain('{{customer_id}}')
  })

  it('意图下拉按家族分组（共用/客服主链/推理闭环）', async () => {
    const w = mountPage()
    await settle(w)
    const groups = w.vm.intentGroups.map((g) => g.label)
    expect(groups).toEqual(['共用意图（两套识别体系）', '客服主链意图', '推理闭环意图'])
    expect(w.vm.intentGroups[0].items).toEqual(['complaint', 'greeting'])
    expect(w.vm.intentGroups[1].items).toEqual(['after_sale', 'ask_service'])
    expect(w.vm.intentGroups[2].items).toEqual(['order_status'])
  })

  it('上移下移改变链序，保存时 priority=行号', async () => {
    const w = mountPage()
    await settle(w)
    w.vm.openEdit({ ...ROW })
    await settle(w)
    expect(w.vm.form.steps.map((s) => s.tool_name)).toEqual(['order.lookup', 'logistics.track', 'knowledge.search'])
    w.vm.moveStep(0, 1)
    expect(w.vm.form.steps.map((s) => s.tool_name)).toEqual(['logistics.track', 'order.lookup', 'knowledge.search'])
    w.vm.form.intent_code = 'ask_service'
    await w.vm.save()
    expect(api.update).toHaveBeenCalledTimes(1)
    const payload = api.update.mock.calls[0][1]
    expect(payload.steps.map((s) => s.priority)).toEqual([1, 2, 3])
    expect(payload.steps[0].tool_name).toBe('logistics.track')
  })

  it('白名单外模板变量被客户端拦截（不发起保存请求）', async () => {
    const w = mountPage()
    await settle(w)
    w.vm.openCreate()
    w.vm.form.name = '坏配置'
    w.vm.form.intent_code = 'order_status'
    w.vm.form.steps = [{ tool_name: 'order.lookup', args: { customer_id: '{{evil_var}}' }, priority: 1, enabled: true }]
    await w.vm.save()
    expect(api.create).not.toHaveBeenCalled()
  })

  it('未闭合占位符被客户端拦截', async () => {
    const w = mountPage()
    await settle(w)
    w.vm.openCreate()
    w.vm.form.name = '坏配置2'
    w.vm.form.intent_code = 'order_status'
    w.vm.form.steps = [{ tool_name: 'order.lookup', args: { k: '{{open' }, priority: 1, enabled: true }]
    await w.vm.save()
    expect(api.create).not.toHaveBeenCalled()
  })

  it('空工具名被客户端拦截', async () => {
    const w = mountPage()
    await settle(w)
    w.vm.openCreate()
    w.vm.form.name = '空工具'
    w.vm.form.intent_code = 'order_status'
    w.vm.form.steps = [{ tool_name: '  ', args: {}, priority: 1, enabled: true }]
    await w.vm.save()
    expect(api.create).not.toHaveBeenCalled()
  })

  it('合法配置创建成功；编辑走 update；删除走 remove', async () => {
    const w = mountPage()
    await settle(w)
    w.vm.openCreate()
    w.vm.form.name = '合法链'
    w.vm.form.intent_code = 'order_status'
    w.vm.form.steps = [{ tool_name: 'order.lookup', args: { customer_id: '{{customer_id}}' }, priority: 1, enabled: true }]
    await w.vm.save()
    expect(api.create).toHaveBeenCalledTimes(1)
    expect(api.create.mock.calls[0][0].steps[0].args.customer_id).toBe('{{customer_id}}')

    w.vm.openEdit({ ...ROW })
    await settle(w)
    w.vm.form.name = '改名'
    await w.vm.save()
    expect(api.update).toHaveBeenCalledTimes(1)

    vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm')
    api.remove.mockClear()
    await w.vm.removeRow({ ...ROW })
    expect(api.remove).toHaveBeenCalledWith(1)
  })
})
