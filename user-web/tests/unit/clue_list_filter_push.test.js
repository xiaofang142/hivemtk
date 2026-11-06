/**
 * 线索列表页：筛选条件必须交给服务端，而不是在已加载的那一页里再挑一遍。
 *
 * 走查实测到的形状：类型选「Telegram」（库里真有 29 条）后表格写作「暂无线索数据」，
 * 而分页条仍是「共 214 条」——一句假空态配一句真总数，读起来像数据坏了。
 * 根因是前端把 list(page, limit) 的返回值用 computed 又过滤了一遍，
 * 第三个参数从未存在过，服务端那套筛选层（repository/clue_query.go）整块空转。
 *
 * 这里钉两件事：
 * 1) 每次查询都把条件送到 api 那一层（旧写法根本没有第三个参数，任何一条都过不了）；
 * 2) 表格渲染的就是服务端给的那批行，前端不再二次过滤——
 *    第 5 格故意让服务端返回与所选类型不同的行（真实服务端不会这样），
 *    只要前端还按本地类型再筛一次，表格就会空，这一格就会红。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  statistics: vi.fn(),
  delete: vi.fn(),
  import: vi.fn(),
}))
vi.mock('@/api/clue', () => ({ clueApi: api }))

import List from '@/views/clue/List.vue'

// 服务端视角的一页数据：type 4 = Telegram（权威表见 constants/cardPlatform.js）
const serverRows = (n, type) => Array.from({ length: n }, (_, i) => ({
  id: `clue-${type}-${i}`,
  name: `线索${i}`,
  account: `@acct${i}`,
  type,
  is_verify: 0,
  city: '',
  address: '',
  source_id: '',
  owner_account: '',
  create_time: 1791600000 + i,
  updated_at: 1791600000 + i,
}))

// 本文件所有挂载登记到 current，afterEach 统一卸载：旧版每条用例结尾手动
// wrapper.unmount()，断言一红就跳过，组件（及其 watcher）活到文件结束；
// 下一条用例挂载时上一个还没卸，pickOption 里"展开的下拉面板应有 1 个"
// 这类对 body 全量枚举的断言就会数到上一只的下拉（teleport 面板同样在 body 上）。
// 释放点唯一化到 afterEach：绿路径同样执行，断言失败也不漏。
let current = null

async function mountList() {
  const host = document.createElement('div')
  document.body.appendChild(host)
  const wrapper = mount(List, {
    global: { plugins: [ElementPlus], mocks: { $t: (k) => k } },
    attachTo: host,
  })
  current = wrapper
  await settle(wrapper)
  return wrapper
}

// 统计卡的「今日新增」也借道 list(1, 1, {start})，所以「最后一次调用」可能是那条计数请求。
// 页面这一批数据只认 limit ≠ 1 的调用。
const pageQueries = () => api.list.mock.calls.filter((c) => c[1] !== 1)
const lastFilters = () => pageQueries().at(-1)?.[2] ?? '(没有带筛选的列表请求)'
const rowTypeCells = (wrapper) => wrapper.findAll('.el-table__body tr td:nth-child(4)').map((td) => td.text())

async function pickOption(wrapper, label, text) {
  const item = wrapper.findAll('.el-form-item').find((f) => f.element.querySelector('label')?.textContent.trim() === label)
  expect(item, `筛选区里没有「${label}」`).toBeTruthy()
  const trigger = item.element.querySelector('.el-select__wrapper')
  expect(trigger, `「${label}」没有下拉`).toBeTruthy()
  trigger.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle(wrapper)
  // Element Plus 会把三个下拉都预先渲染进 body，开合状态在各自的 .el-popper 父节点上；
  // 只在这唯一展开的面板里找项，才能证明点的是「${label}」这一只而不是别只的下拉。
  const open = Array.from(document.querySelectorAll('.el-select-dropdown'))
    .filter((d) => d.parentElement?.style.display !== 'none')
  expect(open, `展开的下拉面板应有 1 个，实际 ${open.length} 个`).toHaveLength(1)
  const option = Array.from(open[0].querySelectorAll('.el-select-dropdown__item'))
    .find((o) => o.textContent.trim() === text)
  expect(option, `「${label}」里没有 ${text} 这一项`).toBeTruthy()
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle(wrapper)
}

async function clickButton(wrapper, text) {
  const btn = wrapper.findAll('button').find((b) => b.text().includes(text))
  expect(btn, `页面上没有「${text}」按钮`).toBeTruthy()
  await btn.trigger('click')
  await settle(wrapper)
}

describe('线索列表页把筛选下推到服务端', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.list.mockResolvedValue({ list: serverRows(10, 4), total: 29 })
    api.statistics.mockResolvedValue([{ type: 4, total: 29, verify_total: 0 }])
  })
  afterEach(() => {
    try {
      current?.unmount()
    } catch {
      /* 重复卸载不抛即可，不掩盖用例本身的失败 */
    }
    current = null
    vi.restoreAllMocks()
    document.body.replaceChildren()
  })

  it('首屏：第 1 页、每页 10、不带任何筛选条件', async () => {
    const wrapper = await mountList()
    expect(pageQueries()).toHaveLength(1)
    expect(pageQueries()[0]).toEqual([1, 10, {}])
  })

  it('选类型 + 搜索：type 进条件、页码回到第 1 页', async () => {
    const wrapper = await mountList()
    await pickOption(wrapper, '线索类型', 'Telegram')
    await clickButton(wrapper, '搜索')
    expect(lastFilters()).toMatchObject({ type: '4' })
  })

  it('选「未验证」：0 是真条件，不能被当成没传', async () => {
    api.list.mockResolvedValue({ list: serverRows(10, 9), total: 214 })
    const wrapper = await mountList()
    await pickOption(wrapper, '验证状态', '未验证')
    await clickButton(wrapper, '搜索')
    expect(lastFilters()).toMatchObject({ is_verify: 0 })
  })

  it('关键字原样进条件（不截断、不本地化）', async () => {
    const wrapper = await mountList()
    const kwInput = wrapper.findAll('.el-form-item')
      .find((f) => f.element.querySelector('label')?.textContent.trim() === '关键字')
      .find('input')
    await kwInput.setValue('a&b 测试')
    await clickButton(wrapper, '搜索')
    expect(lastFilters()).toMatchObject({ keyword: 'a&b 测试' })
  })

  it('表格就是服务端给的那批行：前端不再按类型二次过滤', async () => {
    const wrapper = await mountList()
    // 让服务端返回 type 9（抖音）的行，而筛选器停在 Telegram：
    // 旧写法会在本地把这 10 行全部滤掉 ⇒ 空表 + 「共 29 条」
    api.list.mockResolvedValue({ list: serverRows(10, 9), total: 29 })
    await pickOption(wrapper, '线索类型', 'Telegram')
    await clickButton(wrapper, '搜索')
    expect(wrapper.findAll('.el-table__body tr')).toHaveLength(10)
    expect(rowTypeCells(wrapper)[0]).toBe('抖音')
    expect(wrapper.find('.el-table__empty-text').exists()).toBe(false)
  })

  it('总数取自服务端，不是一页的行数', async () => {
    const wrapper = await mountList()
    expect(wrapper.find('.el-pagination__total').text()).toContain('29')
  })

  it('统计卡走全局口径：另发一次全量计数，不用当前页凑数', async () => {
    api.list.mockResolvedValue({ list: serverRows(3, 4), total: 3 })
    api.statistics.mockResolvedValue([
      { type: 4, total: 29, verify_total: 7 },
      { type: 9, total: 161, verify_total: 0 },
    ])
    const wrapper = await mountList()
    const nums = wrapper.findAll('.stat-card .stat-value').map((n) => Number(n.text()))
    expect(nums[0], '线索总数应是服务端全量 190，而不是本页 3 行').toBe(190)
    expect(nums[1]).toBe(7)
    expect(nums[2]).toBe(183)
    // 今日新增走 start 条件的计数请求：limit=1 只要总数
    const todayCall = api.list.mock.calls.find((c) => c[1] === 1 && c[2]?.start > 0)
    expect(todayCall, '今日新增没有发出带 start 的计数请求').toBeTruthy()
  })

  it('第 2 页上换筛选：回到第 1 页重查，不停在筛完的第 2 页空表', async () => {
    api.list.mockResolvedValue({ list: serverRows(10, 4), total: 29 })
    const wrapper = await mountList()
    await wrapper.find('.btn-next').trigger('click')
    await settle(wrapper)
    expect(pageQueries().at(-1)[0]).toBe(2)
    await pickOption(wrapper, '线索类型', '抖音')
    await clickButton(wrapper, '搜索')
    const q = pageQueries().at(-1)
    expect(q[0], `换筛选后仍停在第 ${q[0]} 页（19 条命中全在第 1 页，读起来像筛出 0 条）`).toBe(1)
    expect(q[2]).toMatchObject({ type: '9' })
  })

  it('重置：条件清空后重新查询，而不是在旧的那一页上恢复', async () => {
    const wrapper = await mountList()
    await pickOption(wrapper, '线索类型', 'Telegram')
    await clickButton(wrapper, '搜索')
    const before = api.list.mock.calls.length
    await clickButton(wrapper, '重置')
    expect(api.list.mock.calls.length).toBeGreaterThan(before)
    expect(lastFilters()).toEqual({})
  })
})
