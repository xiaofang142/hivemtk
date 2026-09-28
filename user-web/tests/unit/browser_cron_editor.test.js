/**
 * 定时任务的编辑器腿：cron 表达式与时区必须能在「新建/编辑任务」这一页配好并保存。
 *
 * 判据要锁的三件事，都是走查时在真实页面上实测到的：
 *   - 表单选了「定时 (cron)」却没有表达式输入，保存出来的是一条永远不会被唤起的 cron 任务；
 *     用户要再去详情页或触发器页补一刀才算配完，而这两处都没有提示「你还差一步」。
 *   - 已有触发器时再发一次新建 = 服务端按 task_id 唯一索引回 409「该任务已存在触发器」，
 *     于是「改一下表达式」这个动作永远保存不上。
 *   - 表达式留空 / 类型改走 cron 之外，都不该往触发器接口发任何请求。
 * 反向锁同批：留在 cron 且表达式非空时必须真发请求，否则这条腿恒绿、什么都不证明。
 *
 * 腿分两类，读日志时别混：
 *   - 「预填 / 更新已有 / 新建挂新 id」三条是缺陷腿——改代码前它们必须红，红在「表达式输入框不存在」；
 *   - 「类型改走不发 / 表达式留空不发」两条是护栏腿——它们断言的是「不许多发请求」，
 *     改动前后都该绿；它们防的是实现顺手把每次保存都变成一次触发器写入。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const route = vi.hoisted(() => ({ value: { params: { id: '77' } } }))
const push = vi.hoisted(() => vi.fn())

const api = vi.hoisted(() => ({
  getBrowserTask: vi.fn(),
  createBrowserTask: vi.fn(),
  updateBrowserTask: vi.fn(),
  listBrowserTasks: vi.fn(),
  listPlatforms: vi.fn(),
  listBrowserCron: vi.fn(),
  createBrowserCron: vi.fn(),
  updateBrowserCron: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => route.value,
  useRouter: () => ({ push }),
}))

import Editor from '@/views/browserAutomation/Editor.vue'

const CRON_TASK = {
  id: 77, name: '定时采集', description: '', platform: 'xiaohongshu',
  url: 'https://example.com', task_type: 'cron', brain_mode: false, brain_goal: '',
  steps: [], loop_count: 1, delay_ms: 1000, timeout_sec: 120,
  retry_on_fail: false, retry_delay_sec: 300, max_retry_times: 3,
  require_confirm: false, confirm_wait_sec: 600,
}
const TRIGGER = { id: 5, task_id: 77, cron_expr: '*/30 * * * *', time_zone: 'Asia/Shanghai', enabled: true }

// 表达式输入框用 placeholder 认（元素级 data-* 不属于这页既有风格，placeholder 已在页面上唯一）
const exprInput = (wrapper) => wrapper.find('input[placeholder*="* * * *"]')

async function mountEditor() {
  // attachTo：下拉的表单项要能在真实 DOM 里按 label 认出来；不挂载到 document 时
  // 整个组件在一段游离 DOM 上，querySelector 找不到「任务类型」那一项。
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(Editor, { global: { plugins: [ElementPlus] }, attachTo: host })
  for (let i = 0; i < 3; i++) await flushPromises()
  return wrapper
}

async function saveButton(wrapper) {
  const btn = wrapper.findAll('button').find((b) => b.text().includes('保存'))
  expect(btn, '页面上没有「保存」按钮').toBeTruthy()
  await btn.trigger('click')
  for (let i = 0; i < 3; i++) await flushPromises()
}

// 任务类型下拉：走真实点击而不是直接改 form——「选了 cron 才出现表达式框」这条判据
// 就住在模板的 v-if 上，直接改内部状态会把写了不用的模板也证成绿。
// 选项在 document 上按整串文案认（下拉是 teleport 到 body 的，且页面上有多只 el-select）。
async function pickTaskType(wrapper, text) {
  const item = Array.from(wrapper.element.querySelectorAll('.el-form-item')).find((el) => el.textContent.includes('任务类型'))
  expect(item, '表单里没有「任务类型」这一项').toBeTruthy()
  const triggerEl = item.querySelector('.el-select__wrapper')
  expect(triggerEl, '「任务类型」那一项里没渲染出下拉').toBeTruthy()
  triggerEl.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  const option = Array.from(document.querySelectorAll('.el-select-dropdown__item'))
    .find((o) => o.textContent.trim() === text)
  expect(option, `任务类型下拉里没有文案为 ${text} 的选项`).toBeTruthy()
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  // 这一句是「点中的确实是任务类型这只下拉」的证据：页面上还有平台/依赖等多只 el-select，
  // 抓错选项会把一次筛选当成类型切换证成绿。
  expect(item.textContent, `选择 ${text} 后「任务类型」没跟着变`).toContain(text.split(' ')[0])
}

describe('编辑器里的定时配置', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    route.value = { params: { id: '77' } }
    api.getBrowserTask.mockResolvedValue(CRON_TASK)
    api.listBrowserTasks.mockResolvedValue({ list: [] })
    api.listPlatforms.mockResolvedValue([])
    api.listBrowserCron.mockResolvedValue({ list: [TRIGGER] })
    api.updateBrowserTask.mockResolvedValue({ ...CRON_TASK })
    api.createBrowserTask.mockResolvedValue({ ...CRON_TASK, id: 888 })
    api.createBrowserCron.mockResolvedValue({ ...TRIGGER, id: 9 })
    api.updateBrowserCron.mockResolvedValue({ ...TRIGGER, cron_expr: '*/10 * * * *' })
  })
  afterEach(() => vi.restoreAllMocks())

  it('编辑 cron 任务：表达式与时区从触发器读回来预填，而不是空着', async () => {
    const wrapper = await mountEditor()
    const input = exprInput(wrapper)
    expect(input.exists(), '选了「定时 (cron)」却没有表达式输入框').toBe(true)
    expect(input.element.value).toBe(TRIGGER.cron_expr)
    wrapper.unmount()
  })

  it('改表达式后保存：更新已有触发器，绝不新建（新建撞 task_id 唯一索引 = 409）', async () => {
    const wrapper = await mountEditor()
    await exprInput(wrapper).setValue('*/10 * * * *')
    await saveButton(wrapper)
    expect(api.updateBrowserCron).toHaveBeenCalledTimes(1)
    expect(api.updateBrowserCron.mock.calls[0][0]).toBe(TRIGGER.id)
    expect(api.updateBrowserCron.mock.calls[0][1].cron_expr).toBe('*/10 * * * *')
    expect(api.createBrowserCron).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('cron 任务还没有触发器：保存后按表单值新建一个', async () => {
    api.listBrowserCron.mockResolvedValue({ list: [] })
    const wrapper = await mountEditor()
    await exprInput(wrapper).setValue('0 9 * * *')
    await saveButton(wrapper)
    expect(api.createBrowserCron).toHaveBeenCalledTimes(1)
    const sent = api.createBrowserCron.mock.calls[0][0]
    expect(sent.task_id).toBe(77)
    expect(sent.cron_expr).toBe('0 9 * * *')
    expect(api.updateBrowserCron).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('类型改走 cron 之外：保存不新建也不更新触发器（回收由服务端负责）', async () => {
    const wrapper = await mountEditor()
    await pickTaskType(wrapper, '单次 (one_shot)')
    await saveButton(wrapper)
    expect(api.createBrowserCron).not.toHaveBeenCalled()
    expect(api.updateBrowserCron).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('表达式留空：一个触发器请求都不发', async () => {
    api.listBrowserCron.mockResolvedValue({ list: [] })
    const wrapper = await mountEditor()
    await exprInput(wrapper).setValue('')
    await saveButton(wrapper)
    expect(api.createBrowserCron).not.toHaveBeenCalled()
    expect(api.updateBrowserCron).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('新建 cron 任务：触发器挂在创建接口返回的那个 id 上', async () => {
    route.value = { params: {} }
    api.getBrowserTask.mockResolvedValue(null)
    const wrapper = await mountEditor()
    await wrapper.find('input[maxlength="256"]').setValue('新建定时')
    await wrapper.find('input[placeholder="https://..."]').setValue('https://example.com')
    // 默认类型是 one_shot：先切到 cron，表达式输入框才会出现
    await pickTaskType(wrapper, '定时 (cron)')
    await exprInput(wrapper).setValue('*/5 * * * *')
    await saveButton(wrapper)
    expect(api.createBrowserTask).toHaveBeenCalledTimes(1)
    expect(api.createBrowserCron).toHaveBeenCalledTimes(1)
    expect(api.createBrowserCron.mock.calls[0][0].task_id).toBe(888)
    wrapper.unmount()
  })
})
