import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'

// 组件渲染用例：证明 actions.js 那三条判据**穿过了模板**，而不只是活在判据文件里。
//
// 为什么有判据单测还要这一份：那里绿了只说明判据对，模板完全可以无视判据 ——
// 有人在 <el-table-column label="操作"> 里手写一个"打标"给已导出的行，actions.js 一个字都不用改。
// 所以这里数的是**渲染出来的按钮**：几行 pending 就给几个"打标"，多一个少一个都红。
//
// api/store 被 mock 掉是为了把用例钉在"渲染"这一件事上：查询串长什么样有
// api_badCase_query.test.js，端点归属有 api_badCase.test.js，这里再断一遍就是三份事实源。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  stats: vi.fn(),
  taxonomy: vi.fn(),
  get: vi.fn(),
  create: vi.fn(),
  label: vi.fn(),
  dismiss: vi.fn(),
  exportSet: vi.fn()
}))

// 身份可换：没有 user id 那一档要单独测（整页只读是一条产品判据，不是渲染巧合）。
const ctx = vi.hoisted(() => ({ me: { id: '7', role: 'manager' } }))

vi.mock('@/api/badCase.js', () => ({
  badCaseApi: {
    list: api.list,
    stats: api.stats,
    taxonomy: api.taxonomy,
    get: api.get,
    create: api.create,
    label: api.label,
    dismiss: api.dismiss,
    exportSet: api.exportSet
  }
}))
vi.mock('@/stores/user', () => ({ useUserStore: () => ({ userInfo: ctx.me }) }))

import List from '@/views/badCase/List.vue'

const TAXONOMY = {
  sources: ['low_confidence', 'zero_hit', 'manual'],
  statuses: ['pending', 'labeled', 'exported', 'dismissed'],
  labels: ['kb_missing', 'kb_stale', 'retrieve_miss', 'intent_misjudge'],
  fix_layers: ['knowledge', 'retrieval', 'generation', 'intent'],
  label_layer: {
    kb_missing: 'knowledge',
    kb_stale: 'knowledge',
    retrieve_miss: 'retrieval',
    intent_misjudge: 'intent'
  }
}

const ROWS = [
  {
    id: 'bc_1',
    source: 'low_confidence',
    status: 'pending',
    query_text: '这款还发货吗',
    confidence: 0.41,
    threshold: 0.62,
    retrieved_count: 2
  },
  {
    id: 'bc_2',
    source: 'zero_hit',
    status: 'labeled',
    query_text: '退货地址在哪',
    label: 'kb_missing',
    fix_layer: 'knowledge',
    label_note: '库里没有退货政策这一条',
    labeler_id: '9',
    confidence: 0.2,
    threshold: 0.62,
    retrieved_count: 0
  },
  {
    id: 'bc_3',
    source: 'manual',
    status: 'exported',
    query_text: '能不能改地址',
    label: 'kb_stale',
    fix_layer: 'knowledge',
    eval_set_id: 'evs_1',
    confidence: 0.3,
    threshold: 0.62,
    retrieved_count: 1
  }
]

const STATS = {
  by_status: { pending: 1, labeled: 1, exported: 1, dismissed: 0 },
  by_fix_layer: { knowledge: 2, retrieval: 0, generation: 0, intent: 0 },
  labeled_ratio: 0.6667
}

const countButtons = (scope, text) =>
  Array.from(scope.querySelectorAll('button')).filter((b) => b.textContent.trim() === text).length

async function mountPage(rows = ROWS, stats = STATS) {
  api.list.mockResolvedValue({ list: rows, total: rows.length })
  api.stats.mockResolvedValue(stats)
  api.taxonomy.mockResolvedValue(TAXONOMY)
  const wrapper = mount(List, {
    attachTo: document.body,
    global: { plugins: [createPinia(), ElementPlus] }
  })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  ctx.me = { id: '7', role: 'manager' }
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('队列渲染：三类状态一起列，按钮按状态分', () => {
  it('三条坏例的现场都在表格里，来源与状态都译成人话', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()
    for (const row of ROWS) expect(text).toContain(row.query_text)
    expect(text).toContain('置信度偏低')
    expect(text).toContain('零命中')
    expect(text).toContain('坐席补录')
    expect(text).toContain('待判定')
    expect(text).toContain('已判定')
    expect(text).toContain('已导出')
  })

  it('"打标"只给待判定的那一行；已判定的只剩"撤销"；已导出的一个都不给', async () => {
    // 已导出那一格是本卡最贵的红线：那行已经带着 eval_set_id 交出去了，
    // 队列里还能给它打标 = 事后偷换一份已经用于算分的样本。
    const wrapper = await mountPage()
    expect(countButtons(document.body, '打标')).toBe(1)
    expect(countButtons(document.body, '撤销')).toBe(2)
    wrapper.unmount()
  })

  it('没有 user id 时整页只读，并挂着那句"只能看不能点"', async () => {
    ctx.me = null
    const wrapper = await mountPage()
    expect(countButtons(document.body, '打标')).toBe(0)
    expect(countButtons(document.body, '撤销')).toBe(0)
    expect(wrapper.text()).toContain('登录态里没有 user id')
    wrapper.unmount()
  })

  it('值域外的状态自己站出来，不藏进任何一档读数', async () => {
    const dirty = {
      by_status: { pending: 1, labeled: 1, exported: 1, dismissed: 0, pending_: 3 },
      by_fix_layer: {},
      labeled_ratio: 0.5
    }
    const wrapper = await mountPage(ROWS, dirty)
    expect(wrapper.text()).toContain('值域外状态')
    expect(wrapper.text()).toContain('pending_ 3')
    wrapper.unmount()
  })

  it('labeled=0 时导出按钮是禁用的，读数里有已判定样本才可点', async () => {
    const findExport = () =>
      Array.from(document.querySelectorAll('button')).find((b) =>
        b.textContent.trim().includes('导出评测集')
      )
    const empty = await mountPage(ROWS, { ...STATS, by_status: { pending: 5, labeled: 0 } })
    expect(findExport().disabled).toBe(true)
    empty.unmount()

    const filled = await mountPage()
    expect(findExport().disabled).toBe(false)
    filled.unmount()
  })
})

describe('503 与读数：底座不可用不等于队列清完了', () => {
  it('列表回 503 时页面长期挂着"一次都没读到"，而不是显示一张空表', async () => {
    api.list.mockRejectedValue(Object.assign(new Error('unavailable'), { status: 503 }))
    api.stats.mockRejectedValue(Object.assign(new Error('unavailable'), { status: 503 }))
    api.taxonomy.mockResolvedValue(TAXONOMY)
    const wrapper = mount(List, {
      attachTo: document.body,
      global: { plugins: [createPinia(), ElementPlus] }
    })
    await flushPromises()
    expect(wrapper.text()).toContain('Bad Case 底座不可用')
    expect(wrapper.text()).toContain('一次都没读到')
    wrapper.unmount()
  })

  it('已判率读数印成百分数（它是本卡北极星，不该要人自己心算 0.6667）', async () => {
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('66.7%')
    wrapper.unmount()
  })
})

describe('打标抽屉：类目与依据两条腿都齐了才点得动', () => {
  const submitButton = () =>
    Array.from(document.querySelectorAll('button')).find(
      (b) => b.textContent.trim() === '提交判定'
    )

  // 抽屉是 teleport 到 body 的，四只抽屉（详情/打标/补录/导出结果）同时在 DOM 里，
  // 所以必须按**标题**认抽屉：querySelector('.el-drawer') 拿到的是排最前那只（详情），
  // 在它里面找输入框只会拿到 null。
  const markDrawer = () =>
    Array.from(document.querySelectorAll('.el-drawer')).find((d) =>
      d.textContent.includes('归因打标')
    )

  const openMarkDrawer = async () => {
    await mountPage()
    Array.from(document.querySelectorAll('button'))
      .find((b) => b.textContent.trim() === '打标')
      .click()
    await flushPromises()
    expect(markDrawer(), '点打标后抽屉必须打开').toBeTruthy()
  }

  const typeNote = async (text) => {
    const textarea = markDrawer().querySelector('textarea')
    expect(textarea, '打标抽屉里必须有判定依据输入框').toBeTruthy()
    textarea.value = text
    textarea.dispatchEvent(new Event('input', { bubbles: true }))
    await flushPromises()
  }

  // 走真实点击而不是直接改组件内部状态：这条用例要证的正是"界面能走到那条判据"。
  // 选项必须按**整串文案**认（抽屉里那一份带责任层后缀「知识库缺内容（知识库）」，
  // 筛选器里还有一份同名的"知识库缺内容"）—— 上一版就是抓到筛选器那只，点了个筛选，
  // mark.label 压根没动，看起来像判据失效。
  const pickCategory = async (text) => {
    const wrapperEl = markDrawer().querySelector('.el-select__wrapper')
    expect(wrapperEl, '打标抽屉里必须有类目下拉框').toBeTruthy()
    wrapperEl.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    const option = Array.from(document.querySelectorAll('.el-select-dropdown__item')).find(
      (o) => o.textContent.trim() === text
    )
    expect(option, `类目下拉框里没有文案为 ${text} 的选项`).toBeTruthy()
    option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await flushPromises()
    // 这一句是"选择真的落在了抽屉那只下拉框上"的证据。少了它，上一版那种
    // "抓到筛选器的同名选项、mark.label 压根没动"的失效会一路伪装成"判据生效"（禁用态照样对）。
    expect(markDrawer().textContent).toContain(text)
  }

  it('抽屉刚打开时提交是禁用的（类目没选、依据没写）', async () => {
    await openMarkDrawer()
    expect(submitButton().disabled).toBe(true)
  })

  it('只写依据仍然禁着：缺类目就是缺类目', async () => {
    await openMarkDrawer()
    await typeNote('库里没有这一条')
    expect(submitButton().disabled).toBe(true)
  })

  it('只选类目也一样禁着：依据必填是后端的 400，界面不该让人点下去才知道', async () => {
    await openMarkDrawer()
    await pickCategory('知识库缺内容（知识库）')
    expect(submitButton().disabled).toBe(true)
  })

  it('选类目 + 写依据 ⇒ 可提交，且提交带的是 (id, 类目, 依据)', async () => {
    api.label.mockResolvedValue({ ...ROWS[0], status: 'labeled', label: 'kb_missing' })
    await openMarkDrawer()
    await pickCategory('知识库缺内容（知识库）')
    await typeNote('库里没有退货政策这一条')

    expect(submitButton().disabled).toBe(false)
    submitButton().click()
    await flushPromises()
    expect(api.label).toHaveBeenCalledWith('bc_1', 'kb_missing', '库里没有退货政策这一条')
  })

  it('后端回 409（这条已被别人判定）时那句结论读得到，不许静默', async () => {
    api.label.mockRejectedValue(
      Object.assign(new Error('bad_case: 当前状态不允许该操作'), { status: 409 })
    )
    await openMarkDrawer()
    await pickCategory('知识库缺内容（知识库）')
    await typeNote('两条腿都补齐了')
    submitButton().click()
    await flushPromises()
    // 409 走的是"关掉抽屉 + 刷队列 + 说一句话"这一路（别人的结论该出现在列表里），
    // 所以断言打在 toast 上而不是抽屉里 —— 抽屉此刻本来就应该是关着的。
    expect(document.body.textContent).toContain('当前状态不允许该操作')
  })

  it('底座 5xx 时抽屉不关、错留在原地：那不该表现成"没判上，刷新看看"', async () => {
    api.label.mockRejectedValue(Object.assign(new Error('坏例操作失败'), { status: 500 }))
    await openMarkDrawer()
    await pickCategory('知识库缺内容（知识库）')
    await typeNote('两条腿都补齐了')
    submitButton().click()
    await flushPromises()
    expect(markDrawer().textContent).toContain('坏例操作失败')
  })
})
