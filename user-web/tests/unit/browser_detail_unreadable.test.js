/**
 * 任务详情页的「读不到」这一面：读失败必须留下一句能看见的话。
 *
 * 走查时实测到的形状：带着一个已删任务的 id 进来（列表页删掉一条、再按浏览器后退/收藏夹
 * 回到它的详情），页面停在一片空白上——既没说读不到，也没给退路，控制台里一条未捕获的
 * rejection。根因是这段代码把主干读取裸裸地 await 着，而整页模板挂在 v-if="task" 上：
 * task 为空时那棵子树根本不渲染，异常也没有 catch 接。
 *
 * 另一半同样要紧：主干拿到之后，旁支（执行历史/回执/触发器）任何一条失败都不该再把
 * 已经显示出来的内容拖回去。旧写法里 listBrowserTaskSessions 抛穿＝后面的触发器读取
 * 整个不发，于是「会话读失败」在页面上表现成「这条 cron 任务没有触发器」——
 * 一个假答案，比空着更贵。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const route = vi.hoisted(() => ({ value: { params: { id: '77' } } }))
const push = vi.hoisted(() => vi.fn())

const api = vi.hoisted(() => ({
  getBrowserTask: vi.fn(),
  publishBrowserTask: vi.fn(),
  runBrowserTask: vi.fn(),
  listBrowserTaskSessions: vi.fn(),
  listBrowserTaskReceipts: vi.fn(),
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
  for (let i = 0; i < 4; i++) await flushPromises()
  return wrapper
}

describe('详情页读不到时的样子', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    route.value = { params: { id: '77' } }
    push.mockReset()
    api.getBrowserTask.mockResolvedValue(TASK)
    api.listBrowserTaskSessions.mockResolvedValue({ list: [] })
    api.listBrowserTaskReceipts.mockResolvedValue({ list: [] })
    api.listBrowserCron.mockResolvedValue({ list: [] })
  })
  afterEach(() => vi.restoreAllMocks())

  it('任务本体 404：把服务端那句原因写在页面上，而不是留一张白屏', async () => {
    api.getBrowserTask.mockRejectedValue(bizErr('任务不存在', 'NOT_FOUND_1002', 404))
    const wrapper = await mountDetail()
    const text = wrapper.text()
    // 白屏的代价是用户不知道是「没有内容」还是「页面坏了」，只能反复刷同一个坏链接
    expect(text).toContain('任务不存在')
    expect(text).toContain('返回任务列表')
    wrapper.unmount()
  })

  it('读不到之后点重试：再发一次真实读取，读通了就把详情渲染出来', async () => {
    api.getBrowserTask.mockRejectedValueOnce(bizErr('任务不存在', 'NOT_FOUND_1002', 404))
    const wrapper = await mountDetail()
    expect(api.getBrowserTask).toHaveBeenCalledTimes(1)
    api.getBrowserTask.mockResolvedValue(TASK)
    const retry = wrapper.findAll('button').find((b) => b.text().includes('重试'))
    expect(retry, '读不到的那一屏上没有「重试」入口').toBeTruthy()
    await retry.trigger('click')
    for (let i = 0; i < 4; i++) await flushPromises()
    expect(api.getBrowserTask).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).toContain(TASK.name)
    // 错误态读通之后要退场：留着那条红幅会让用户以为这一页仍不可信
    expect(wrapper.text()).not.toContain('任务读不到')
    wrapper.unmount()
  })

  it('执行历史读失败：不带走触发器那一步（旧写法抛穿后它整个不发）', async () => {
    api.listBrowserTaskSessions.mockRejectedValue(bizErr('会话列表读不到', 'INTERNAL_500', 500))
    api.listBrowserCron.mockResolvedValue({ list: [TRIGGER] })
    const wrapper = await mountDetail()
    expect(wrapper.text()).toContain(TASK.name)
    expect(api.listBrowserCron, '会话读失败之后再没有读过触发器').toHaveBeenCalledTimes(1)
    // 触发器卡片渲染出来＝页面上看得见它此刻的表达式，而不是「这条任务没有触发器」
    expect(wrapper.text()).toContain(TRIGGER.cron_expr)
    wrapper.unmount()
  })

  it('触发器读失败：任务详情与执行历史照旧在，卡片走「添加触发器」空态', async () => {
    api.listBrowserCron.mockRejectedValue(bizErr('触发器读不到', 'INTERNAL_500', 500))
    const wrapper = await mountDetail()
    expect(wrapper.text()).toContain(TASK.name)
    expect(api.listBrowserTaskSessions).toHaveBeenCalledTimes(1)
    expect(wrapper.text()).toContain('添加触发器')
    wrapper.unmount()
  })

  it('回执读失败：主干不受影响（这条腿在旧代码里已经是对的，锁住别退回去）', async () => {
    api.listBrowserTaskReceipts.mockRejectedValue(bizErr('回执读不到', 'INTERNAL_500', 500))
    const wrapper = await mountDetail()
    expect(wrapper.text()).toContain(TASK.name)
    expect(wrapper.text()).toContain('暂无回执')
    expect(api.listBrowserCron).toHaveBeenCalledTimes(1)
    wrapper.unmount()
  })
})
