/**
 * 监控页的轮询在「读不到」面前该怎么做。
 *
 * 走查时实测到的形状：会话 id 同样来自 URL，指着一条已被清理/别的实例上的会话进来，
 * 这一页每 2s 发两次请求、每次都 404，页面空白，控制台每轮多一条未捕获 rejection。
 * 根因是轮询的终止条件写在 session 上（终态才停），而 session 正因为读不到才是空的——
 * 于是「终止条件」永远不会成立。这一页看起来在监控，其实什么都没监控。
 *
 * 收口的判据分两档，两档都得锁：404 是不会自己变回来的（立刻收），
 * 网络抖动是会自己变回来的（连错几轮才收）。只写后一档＝坏链接照样空转到天荒地老；
 * 只写前一档＝一次抖动就把这一页判死，而用户只剩刷新整页这一条路。
 *
 * 顺带锁停止接口那半边：服务端对「没有在跑的执行协程」回的是 200 + stopped:false。
 * 把它播报成「停止请求已发送」，用户就会等一次永远不会来的收口，
 * 而那条残留会话仍按 created/active 占着并发闸，下一次下发被判「已有任务执行中」。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'

const api = vi.hoisted(() => ({
  getBrowserSession: vi.fn(),
  getBrowserSessionSteps: vi.fn(),
  getBrowserSessionLogs: vi.fn(),
  getBrowserConfirmGate: vi.fn(),
  confirmBrowserSession: vi.fn(),
  stopBrowserSession: vi.fn(),
  exportBrowserSessionAudit: vi.fn(),
  interpretConfirmResult: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '900' } }),
  useRouter: () => ({ push: vi.fn() }),
}))

import Monitor from '@/views/browserAutomation/Monitor.vue'

const ACTIVE = { id: '900', status: 'active', confirm_pending: false, duration_ms: 1000, total_steps: 2, success_steps: 1 }
const bizErr = (message, bizCode, status) => Object.assign(new Error(message), { bizCode, status })
const NOT_FOUND = () => bizErr('会话不存在', 'NOT_FOUND_1002', 404)

// 轮询是这一页的判据本身，所以必须用假时钟：真等 2s×N 轮在负载机器上会漂成超时，
// 而「漂没漂」和「停没停」在日志里长得一模一样。
async function mountMonitor() {
  vi.useFakeTimers()
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(Monitor, { global: { plugins: [ElementPlus] }, attachTo: host })
  await flushPromises()
  await flushPromises()
  return wrapper
}

const tick = async (ms) => { await vi.advanceTimersByTimeAsync(ms) }
const clickWhere = async (wrapper, label) => {
  const btn = wrapper.findAll('button').find((b) => b.text().includes(label))
  expect(btn, `页面上没有「${label}」按钮`).toBeTruthy()
  await btn.trigger('click')
  await flushPromises()
  return btn
}

describe('监控页读不到会话时', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.getBrowserSessionSteps.mockResolvedValue([])
    api.getBrowserSessionLogs.mockResolvedValue([])
    api.getBrowserConfirmGate.mockResolvedValue({ pending: false })
    api.stopBrowserSession.mockResolvedValue({ stopped: true })
  })
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('404：把原因写在页面上，并立刻收掉轮询（终态条件挂在读不到的 session 上）', async () => {
    api.getBrowserSession.mockRejectedValue(NOT_FOUND())
    const wrapper = await mountMonitor()
    expect(api.getBrowserSession).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('会话不存在')
    // 没有 session 就不该留两张空表：告警说这一页没在监控任何东西，下面却摆着表头和 No Data
    expect(wrapper.text()).not.toContain('步骤执行')
    expect(wrapper.text()).not.toContain('命令流')
    await tick(20000)
    // 旧写法到这一句还是 11 次：每 2s 两次 404，永远等不来终止条件
    expect(api.getBrowserSession).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })

  it('非 404 的读失败：先再等两轮才收（一次抖动不该把这一页判死）', async () => {
    api.getBrowserSession.mockRejectedValue(bizErr('网关超时', 'UPSTREAM_500', 500))
    const wrapper = await mountMonitor()
    await tick(2000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(2)
    await tick(2000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(3)
    await tick(60000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('收掉之后还给用户一个重试：点它要真的再读一次，并重新排上轮询', async () => {
    api.getBrowserSession.mockRejectedValue(NOT_FOUND())
    const wrapper = await mountMonitor()
    await tick(20000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(1)
    api.getBrowserSession.mockResolvedValue(ACTIVE)
    await clickWhere(wrapper, '重试')
    expect(api.getBrowserSession).toHaveBeenCalledTimes(2)
    // 会话卡片那一段只在 session 存在时渲染：页头本来就印着 #900，看它证明不了读通了
    expect(wrapper.text()).toContain('成功率')
    expect(wrapper.text()).not.toContain('会话不存在')
    await tick(2000)
    // 「重试」只发一次请求的话，这一页其实并没有回到监控状态
    expect(api.getBrowserSession).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('执行中的会话读通了就照常在轮询，且终态自己收口', async () => {
    api.getBrowserSession.mockResolvedValue(ACTIVE)
    const wrapper = await mountMonitor()
    await tick(2000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(2)
    api.getBrowserSession.mockResolvedValue({ ...ACTIVE, status: 'completed' })
    await tick(2000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(3)
    await tick(20000)
    expect(api.getBrowserSession).toHaveBeenCalledTimes(3)
    wrapper.unmount()
  })

  it('停止接口回 stopped:false：不许报成「请求已发送」', async () => {
    const warn = vi.spyOn(ElMessage, 'warning')
    const ok = vi.spyOn(ElMessage, 'success')
    api.getBrowserSession.mockResolvedValue(ACTIVE)
    api.stopBrowserSession.mockResolvedValue({ stopped: false })
    const wrapper = await mountMonitor()
    await clickWhere(wrapper, '停止')
    expect(api.stopBrowserSession).toHaveBeenCalledTimes(1)
    expect(ok).not.toHaveBeenCalled()
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('已不在执行中'))
    wrapper.unmount()
  })

  it('停止接口回 stopped:true：这才可以说「已发送」', async () => {
    const ok = vi.spyOn(ElMessage, 'success')
    const warn = vi.spyOn(ElMessage, 'warning')
    api.getBrowserSession.mockResolvedValue(ACTIVE)
    api.stopBrowserSession.mockResolvedValue({ stopped: true })
    const wrapper = await mountMonitor()
    await clickWhere(wrapper, '停止')
    expect(ok).toHaveBeenCalledWith(expect.stringContaining('停止请求已发送'))
    expect(warn).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
