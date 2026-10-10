/**
 * 工作流依赖的写腿：在「编辑任务」这一页改的前置，必须真的改到服务端。
 *
 * 走查时测到的形状是：表单选了「工作流 (workflow)」并换了前置任务，保存后回列表再看进来，
 * 依赖还是原来那条。原因不在前端——PUT /tasks/:id 的请求结构体里根本没有这两个字段
 * （dto/task.go 的 UpdateBrowserTaskReq 逐字段拷，没拷的即丢弃），
 * 而写侧只有一扇门 PUT /tasks/:id/dependency，这一页从来没调过它。
 * 「界面显示改好了、执行照旧按旧前置拒」比直接报错更难发现，所以这里锁的是发出那一刀。
 *
 * 顺序也锁：那一扇门是「读整行—改两列—回写整行」，写在任务本体之前会把这次刚存的
 * 名字/步骤盖回去，两次保存互相吃字段。
 *
 * 腿分两类，读日志时别混：
 *   - 「换前置/清依赖/依赖接口失败/裸 id」四条是缺陷腿——改代码前它们必须红；
 *   - 「没碰就不发」「新建那一趟」是护栏腿——防的是实现顺手把每次保存都变成一次依赖写入。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'

const route = vi.hoisted(() => ({ value: { params: { id: '77' } } }))
const push = vi.hoisted(() => vi.fn())
const order = vi.hoisted(() => [])

const api = vi.hoisted(() => ({
  getBrowserTask: vi.fn(),
  createBrowserTask: vi.fn(),
  updateBrowserTask: vi.fn(),
  listBrowserTasks: vi.fn(),
  listPlatforms: vi.fn(),
  listBrowserCron: vi.fn(),
  createBrowserCron: vi.fn(),
  updateBrowserCron: vi.fn(),
  setBrowserTaskDependency: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => route.value,
  useRouter: () => ({ push }),
}))

import Editor from '@/views/browserAutomation/Editor.vue'

const WF_TASK = {
  id: 77, name: '下游发布', description: '', platform: 'xiaohongshu',
  url: 'https://example.com', task_type: 'workflow', brain_mode: false, brain_goal: '',
  steps: [], loop_count: 1, delay_ms: 1000, timeout_sec: 120,
  retry_on_fail: false, retry_delay_sec: 300, max_retry_times: 3,
  require_confirm: false, confirm_wait_sec: 600, status: 'ready',
  depends_on_task_id: 5, depends_on_mode: 'all_done',
}
const UP5 = { id: 5, name: '上游采集', status: 'ready' }
const UP8 = { id: 8, name: '上游采集B', status: 'ready' }

async function mountEditor() {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(Editor, { global: { plugins: [ElementPlus] }, attachTo: host })
  for (let i = 0; i < 4; i++) await flushPromises()
  return wrapper
}

// 走真实点击而不是直接改 form：下拉选项、以及「选了才出现模式那一项」的 v-if，
// 都是判据住的地方；直接改内部状态会把写了不用的模板也证成绿。
// nth 是给同一表单项里的第二只下拉用的（依赖与模式并排在「依赖前置任务」这一项里）。
async function pickIn(wrapper, labelText, optionText, nth = 0) {
  const item = Array.from(wrapper.element.querySelectorAll('.el-form-item'))
    .find((el) => el.textContent.includes(labelText))
  expect(item, `表单里没有「${labelText}」这一项`).toBeTruthy()
  const triggerEl = item.querySelectorAll('.el-select__wrapper')[nth]
  expect(triggerEl, `「${labelText}」那一项里没渲染出第 ${nth + 1} 只下拉`).toBeTruthy()
  triggerEl.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  const option = Array.from(document.querySelectorAll('.el-select-dropdown__item'))
    .find((o) => o.textContent.trim() === optionText)
  expect(option, `下拉里没有文案为 ${optionText} 的选项`).toBeTruthy()
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  return item
}

async function clickSave(wrapper) {
  const btn = wrapper.findAll('button').find((b) => b.text().includes('保存'))
  expect(btn, '页面上没有「保存」按钮').toBeTruthy()
  await btn.trigger('click')
  for (let i = 0; i < 4; i++) await flushPromises()
}

describe('编辑器里的依赖前置任务', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    order.length = 0
    route.value = { params: { id: '77' } }
    push.mockReset()
    api.getBrowserTask.mockImplementation((id) =>
      Promise.resolve(Number(id) === 77 ? { ...WF_TASK } : Promise.reject(new Error('没有这条任务'))))
    api.listBrowserTasks.mockResolvedValue({ list: [UP5, UP8] })
    api.listPlatforms.mockResolvedValue([])
    api.listBrowserCron.mockResolvedValue({ list: [] })
    api.updateBrowserTask.mockImplementation(async () => { order.push('task'); return { ...WF_TASK } })
    api.createBrowserTask.mockImplementation(async () => { order.push('task'); return { ...WF_TASK, id: 888 } })
    api.setBrowserTaskDependency.mockImplementation(async () => { order.push('dep'); return null })
  })
  afterEach(() => {
    vi.restoreAllMocks()
    // 某一格在断言处抛出时它自己的 unmount 就被跳过，Element Plus 传送到 body 的下拉留在原地；
    // pickIn 全局查选项，下一格会点中那份陈旧弹层，红成"取不到表单项"——红因根本不在它身上。
    document.body.replaceChildren()
  })

  it('换前置也换模式：保存把这一步发去依赖接口，而不是塞进 PUT 里丢掉', async () => {
    const wrapper = await mountEditor()
    await pickIn(wrapper, '依赖前置任务', '#8 上游采集B', 0)
    await pickIn(wrapper, '依赖前置任务', '前置曾成功 (any_success)', 1)
    await clickSave(wrapper)
    expect(api.setBrowserTaskDependency).toHaveBeenCalledTimes(1)
    expect(api.setBrowserTaskDependency.mock.calls[0]).toEqual([77, { depends_on_task_id: 8, depends_on_mode: 'any_success' }])
    // 依赖那一刀必须排在任务本体之后：它回写整行，先写会把刚存的名字/步骤盖回旧值
    expect(order).toEqual(['task', 'dep'])
    wrapper.unmount()
  })

  it('没碰依赖：一个依赖请求都不发（不许把每次保存都变成一次依赖写入）', async () => {
    const wrapper = await mountEditor()
    await clickSave(wrapper)
    expect(api.setBrowserTaskDependency).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('类型从 workflow 改走：把界面上已经看不见的旧依赖一起清掉', async () => {
    const wrapper = await mountEditor()
    await pickIn(wrapper, '任务类型', '单次 (one_shot)')
    await clickSave(wrapper)
    // 发的是显式 null：{} 到服务端是「不改」，那正是这个缺陷本身的形状
    expect(api.setBrowserTaskDependency).toHaveBeenCalledTimes(1)
    expect(api.setBrowserTaskDependency.mock.calls[0][1]).toEqual({ depends_on_task_id: null })
    wrapper.unmount()
  })

  it('依赖接口失败：说清「依赖没存上」，不能报「已保存」了事', async () => {
    const warn = vi.spyOn(ElMessage, 'warning')
    const ok = vi.spyOn(ElMessage, 'success')
    api.setBrowserTaskDependency.mockRejectedValue(Object.assign(new Error('前置任务不存在'), { bizCode: 'INVALID_1001', status: 400 }))
    const wrapper = await mountEditor()
    await pickIn(wrapper, '依赖前置任务', '#8 上游采集B')
    await clickSave(wrapper)
    expect(warn).toHaveBeenCalledWith(expect.stringContaining('依赖关系没存上'))
    expect(ok).not.toHaveBeenCalledWith('已保存')
    wrapper.unmount()
  })

  it('已存的前置不在「已发布」候选里：显示它本身，而不是一个裸 id', async () => {
    api.getBrowserTask.mockImplementation((id) => {
      const n = Number(id)
      if (n === 77) return Promise.resolve({ ...WF_TASK, depends_on_task_id: 9 })
      if (n === 9) return Promise.resolve({ id: 9, name: '退回草稿的上游', status: 'draft' })
      return Promise.reject(new Error('没有这条任务'))
    })
    const wrapper = await mountEditor()
    const item = Array.from(wrapper.element.querySelectorAll('.el-form-item'))
      .find((el) => el.textContent.includes('依赖前置任务'))
    // 候选列表只列 ready 的任务，而执行入口的旧前置已经退回 draft：
    // 没有这一条时下拉只回显 9，用户看不出这条工作流挂在谁身上、该不该改
    expect(item.textContent).toContain('#9 退回草稿的上游')
    expect(item.textContent).toContain('draft')
    wrapper.unmount()
  })

  it('新建 workflow 任务：依赖随创建请求一起走，不再补一次依赖写入', async () => {
    route.value = { params: {} }
    const wrapper = await mountEditor()
    await wrapper.find('input[maxlength="256"]').setValue('新建下游')
    await wrapper.find('input[placeholder="https://..."]').setValue('https://example.com')
    await pickIn(wrapper, '任务类型', '工作流 (workflow)')
    await pickIn(wrapper, '依赖前置任务', '#5 上游采集')
    await clickSave(wrapper)
    expect(api.createBrowserTask.mock.calls[0][0].depends_on_task_id).toBe(5)
    expect(api.setBrowserTaskDependency).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('新建时类型改走 workflow 之外：创建请求里不带那条选过的依赖', async () => {
    route.value = { params: {} }
    const wrapper = await mountEditor()
    await wrapper.find('input[maxlength="256"]').setValue('其实不是工作流')
    await wrapper.find('input[placeholder="https://..."]').setValue('https://example.com')
    await pickIn(wrapper, '任务类型', '工作流 (workflow)')
    await pickIn(wrapper, '依赖前置任务', '#5 上游采集')
    await pickIn(wrapper, '任务类型', '单次 (one_shot)')
    await clickSave(wrapper)
    // 带着它建单＝建出一条「页面上没有依赖栏、执行却按依赖被拒」的任务
    expect(api.createBrowserTask.mock.calls[0][0]).not.toHaveProperty('depends_on_task_id')
    wrapper.unmount()
  })
})
