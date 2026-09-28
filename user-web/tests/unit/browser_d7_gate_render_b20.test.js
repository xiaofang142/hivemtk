/**
 * 批20 D7 闸门 组件渲染用例（监控页）
 * 为什么在静态断言之还要这一份：静态串只证明源码里写过那句话，模板完全可以写了不用
 * —— 把 preview 插进一个 v-if 永不成立的节点、或者放行按钮照旧无条件可点，
 * 上一份文件照样全绿。这里数的是**渲染出来的 DOM 与真实点击发出的参数**。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({
  getBrowserSession: vi.fn(),
  getBrowserSessionSteps: vi.fn(),
  getBrowserSessionLogs: vi.fn(),
  getBrowserConfirmGate: vi.fn(),
  confirmBrowserSession: vi.fn(),
  stopBrowserSession: vi.fn(),
  exportBrowserSessionAudit: vi.fn(),
  interpretConfirmResult: vi.fn()
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '77' } }) }))

import Monitor from '@/views/browserAutomation/Monitor.vue'

const SESSION = {
  id: '77',
  status: 'active',
  confirm_pending: true,
  duration_ms: 1000,
  total_steps: 4,
  success_steps: 3
}
const GATE = {
  step_index: 3,
  payload_hash: 'cafe1234',
  preview: '这是一条将要真实发出去的评论正文',
  expires_at: '2026-09-21T10:00:00Z'
}

async function mountMonitor() {
  const wrapper = mount(Monitor, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  await flushPromises()
  return wrapper
}

describe('批20 闸门卡片：批之前看得见批的是什么', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.getBrowserSession.mockResolvedValue(SESSION)
    api.getBrowserSessionSteps.mockResolvedValue([])
    api.getBrowserSessionLogs.mockResolvedValue([])
    api.getBrowserConfirmGate.mockResolvedValue({ pending: true, gate: GATE })
    api.confirmBrowserSession.mockResolvedValue({ confirmed: true, status: 'granted' })
    api.interpretConfirmResult.mockReturnValue({ released: true, status: 'granted', gate: null })
  })
  afterEach(() => vi.restoreAllMocks())

  it('挂起中：正文预览与步骤序号渲染出来，且哈希取自闸门', async () => {
    const wrapper = await mountMonitor()
    expect(api.getBrowserConfirmGate).toHaveBeenCalledWith('77')
    expect(wrapper.text()).toContain(GATE.preview)
    expect(wrapper.text()).toContain('第 3 步')
    wrapper.unmount()
  })

  it('闸门读不到（pending=false / 接口报错）：不放行入口生效，按钮禁用', async () => {
    api.getBrowserConfirmGate.mockResolvedValue({ pending: false })
    const wrapper = await mountMonitor()
    expect(wrapper.text()).not.toContain(GATE.preview)
    const confirm = wrapper.findAll('button').find((b) => b.text().includes('确认放行'))
    expect(confirm.attributes('disabled')).toBeDefined()
    await confirm.trigger('click')
    expect(api.confirmBrowserSession).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('没有挂起：一个闸门请求都不发（confirm-gate 只服务 confirm_pending）', async () => {
    api.getBrowserSession.mockResolvedValue({ ...SESSION, confirm_pending: false })
    const wrapper = await mountMonitor()
    expect(api.getBrowserConfirmGate).not.toHaveBeenCalled()
    expect(wrapper.text()).not.toContain('待确认的写操作')
    wrapper.unmount()
  })

  it('点击放行：带着闸门那份哈希下发，而不是空 body', async () => {
    const wrapper = await mountMonitor()
    const confirm = wrapper.findAll('button').find((b) => b.text().includes('确认放行'))
    expect(confirm.attributes('disabled')).toBeUndefined()
    await confirm.trigger('click')
    await flushPromises()
    expect(api.confirmBrowserSession).toHaveBeenCalledWith('77', 'cafe1234')
    wrapper.unmount()
  })
})
