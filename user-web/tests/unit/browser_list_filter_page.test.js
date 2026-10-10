/**
 * 列表页：换筛选条件是一次新的查询，不是同一批数据的下一页。
 *
 * 走查时实测到的形状：翻到第 2 页再看状态筛选，选「草稿」之后表格空了，
 * 分页条仍停在第 2 页——读起来像「草稿状态下没有任务」，而第 1 页全是。
 * 分页控件只在总页数变小往下夹那一档会自己纠正；条件变严而总页数没变的那一档它不管，
 * 于是一半的情况自愈、另一半留着一张空表，这种「时灵时不灵」最难被当成缺陷报上来。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({
  listBrowserTasks: vi.fn(),
  publishBrowserTask: vi.fn(),
  runBrowserTask: vi.fn(),
  pauseBrowserTask: vi.fn(),
  deleteBrowserTask: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))

import List from '@/views/browserAutomation/List.vue'

// 41 条 × 每页 20 → 3 页；行状态用 ready，免得列表自己挂上 5s 轮询定时器把计数带偏
const rows = (n) => Array.from({ length: n }, (_, i) => ({
  id: i + 1, name: `任务${i + 1}`, task_type: 'one_shot', status: 'ready', brain_mode: false, last_run_at: null,
}))

async function mountList() {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(List, { global: { plugins: [ElementPlus] }, attachTo: host })
  for (let i = 0; i < 3; i++) await flushPromises()
  return wrapper
}

const lastQuery = () => api.listBrowserTasks.mock.calls.at(-1)[0]

async function goNextPage(wrapper) {
  const next = wrapper.find('.btn-next')
  expect(next.exists(), '分页条上没有下一页按钮').toBeTruthy()
  await next.trigger('click')
  await flushPromises()
}

// 走真实点击而不是直接改 query：判据住在「筛选变化时该发什么参数」这条链上，
// 直接改内部状态会把「模板里那句 @change 根本没接到重置」也证成绿。
async function filterBy(wrapper, statusText) {
  const triggerEl = wrapper.element.querySelector('.toolbar .el-select__wrapper')
  expect(triggerEl, '工具条里没有状态下拉').toBeTruthy()
  triggerEl.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
  const option = Array.from(document.querySelectorAll('.el-select-dropdown__item'))
    .find((o) => o.textContent.trim() === statusText)
  expect(option, `状态下拉里没有 ${statusText} 这一项`).toBeTruthy()
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await flushPromises()
}

describe('列表页的筛选与分页', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.listBrowserTasks.mockResolvedValue({ list: rows(20), total: 41 })
  })
  afterEach(() => {
    vi.restoreAllMocks()
    // 同 browser_editor_dependency：断言抛出会跳过本格 unmount，传到 body 的下拉留给下一格点。
    document.body.replaceChildren()
  })

  it('第 2 页上换筛选：回到第 1 页去查新条件', async () => {
    const wrapper = await mountList()
    await goNextPage(wrapper)
    expect(lastQuery().page).toBe(2)
    await filterBy(wrapper, '草稿')
    // 旧写法带着 page=2 一起发：筛完停在第 2 页的空表上，而草稿全在第 1 页
    expect(lastQuery()).toMatchObject({ status: 'draft', page: 1 })
    wrapper.unmount()
  })

  it('翻页这条路照旧走：点下一页就是第 2 页（别把重置做成只会回第 1 页）', async () => {
    const wrapper = await mountList()
    await goNextPage(wrapper)
    expect(lastQuery()).toMatchObject({ status: '', page: 2 })
    wrapper.unmount()
  })

  it('首屏默认第 1 页、不带状态（空串不是「筛到空」）', async () => {
    const wrapper = await mountList()
    expect(api.listBrowserTasks).toHaveBeenCalledTimes(1)
    expect(lastQuery()).toMatchObject({ status: '', page: 1, limit: 20 })
    wrapper.unmount()
  })
})
