/**
 * 任务详情页点「执行」的三条结局：引导、排队提示、跳进会话。
 *
 * 走查时列表页与详情页是两套代码：列表页 8001 会开安装引导、执行成功会跳进那条会话，
 * 详情页却只把 err.message 弹一句就走——用户在详情页点「执行」拿到「Host 未连接」，
 * 页面留在原地，既不知道要装什么，也不知道去哪儿装（同一个动作在两个入口有两种结局）。
 * 这里锁的是详情页必须与列表页同判据：分流只看业务码，不看文案也不只看 HTTP 状态。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'

const route = vi.hoisted(() => ({ value: { params: { id: '77' } } }))
const push = vi.hoisted(() => vi.fn())

const api = vi.hoisted(() => ({
  getBrowserTask: vi.fn(),
  publishBrowserTask: vi.fn(),
  runBrowserTask: vi.fn(),
  listBrowserTaskSessions: vi.fn(),
  createBrowserCron: vi.fn(),
  listBrowserCron: vi.fn(),
  enableBrowserCron: vi.fn(),
  disableBrowserCron: vi.fn(),
  deleteBrowserCron: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => route.value,
  useRouter: () => ({ push }),
}))

import Detail from '@/views/browserAutomation/Detail.vue'

const TASK = {
  id: 77, name: '定时采集', platform: 'xiaohongshu', url: 'https://example.com',
  task_type: 'cron', status: 'ready', brain_mode: false, brain_goal: '', steps: [],
  loop_count: 1, delay_ms: 1000, timeout_sec: 120,
  retry_on_fail: false, require_confirm: false,
}
const TRIGGER = { id: 5, task_id: 77, cron_expr: '*/30 * * * *', time_zone: 'Asia/Shanghai', enabled: true }

const bizErr = (message, bizCode, status) => Object.assign(new Error(message), { bizCode, status })

async function mountDetail() {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(Detail, { global: { plugins: [ElementPlus] }, attachTo: host })
  for (let i = 0; i < 3; i++) await flushPromises()
  return wrapper
}

const clickRun = async (wrapper) => {
  const btn = wrapper.findAll('button').find((b) => b.text().includes('执行'))
  expect(btn, '页面上没有「执行」按钮').toBeTruthy()
  await btn.trigger('click')
  for (let i = 0; i < 3; i++) await flushPromises()
}

describe('详情页的执行入口', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    route.value = { params: { id: '77' } }
    push.mockReset()
    api.getBrowserTask.mockResolvedValue(TASK)
    api.listBrowserTaskSessions.mockResolvedValue({ list: [] })
    api.listBrowserCron.mockResolvedValue({ list: [] })
  })
  afterEach(() => vi.restoreAllMocks())

  it('Host 未连接（8001）：把安装引导开在原地，而不是只弹一句错误', async () => {
    api.runBrowserTask.mockRejectedValue(bizErr('本机 Chrome 未连接 HiveMTK 扩展/Host', 'BROWSER_HOST_OFFLINE_8001', 409))
    const wrapper = await mountDetail()
    await clickRun(wrapper)
    expect(document.body.textContent, '8001 之后页面上找不到安装引导').toContain('chrome://extensions')
    expect(push).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('同 Host 串行闸占用（8002）：只提示排队，不开引导', async () => {
    const warn = vi.spyOn(ElMessage, 'warning')
    api.runBrowserTask.mockRejectedValue(bizErr('已有浏览器任务执行中（同一 Host 串行）', 'BROWSER_TASK_BUSY_8002', 409))
    const wrapper = await mountDetail()
    await clickRun(wrapper)
    expect(document.body.textContent).not.toContain('chrome://extensions')
    // 「不开引导」不够：排队是可自愈的临时态，得让用户看见原因，而不是被静默吞掉
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('串行'))
    expect(push).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('执行成功：跳进返回的那条会话，点了执行就要看得见执行', async () => {
    api.runBrowserTask.mockResolvedValue({ session_id: 91 })
    const wrapper = await mountDetail()
    await clickRun(wrapper)
    expect(push).toHaveBeenCalledWith('/browser-automation/sessions/91')
    wrapper.unmount()
  })

  it('草稿任务的触发器：把「到点也不会执行」写在页面上', async () => {
    api.getBrowserTask.mockResolvedValue({ ...TASK, status: 'draft' })
    api.listBrowserCron.mockResolvedValue({ list: [TRIGGER] })
    const wrapper = await mountDetail()
    // 页头本来就有「发布」按钮，光看「发布」二字证明不了这条提示存在
    expect(wrapper.text(), '挂了启用中的触发器却没有提示任务还没发布').toContain('不可执行')
    wrapper.unmount()
  })

  it('触发器启停失败：重读后端真相，而不是留着一个没生效的开关', async () => {
    api.listBrowserCron.mockResolvedValue({ list: [{ ...TRIGGER, enabled: true }] })
    api.disableBrowserCron.mockRejectedValue(bizErr('触发器不存在', 'NOT_FOUND_1002', 404))
    const wrapper = await mountDetail()
    const before = api.getBrowserTask.mock.calls.length
    const toggle = wrapper.findAll('button').find((b) => b.text().includes('停用'))
    expect(toggle, '触发器卡片里没有「停用」按钮').toBeTruthy()
    await toggle.trigger('click')
    for (let i = 0; i < 4; i++) await flushPromises()
    expect(api.disableBrowserCron).toHaveBeenCalledWith(TRIGGER.id)
    expect(api.getBrowserTask.mock.calls.length, '失败后没有重读任务与触发器').toBeGreaterThan(before)
    wrapper.unmount()
  })
})
