import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

// 转化漏斗看板的摘要区渲染用例（T-P4-06）。
//
// 这张卡在末尾加了"商机"这一段。后端五段是测试钉住的，但**前端的三块 KPI 原先按数组下标
// 取数**（`stages[0]` / `stages[stages.length - 1]`）：加一段之后"最后一段"从会话变成商机，
// 于是标题写着「转化量(会话)」的那块卡片会开始显示商机数，「端到端转化率」也顺手换了口径。
// 标签没动、数字动了，而且后端加段不会让前端任何一处变红 —— 这类"绿着的错"只能靠
// **数渲染出来的数字**来守，所以这里断言的是 DOM 文本，不是组件内部状态。
//
// echarts 被 mock 掉是因为 jsdom 里没有 canvas；漏斗图怎么画不归本用例管。

const api = vi.hoisted(() => ({
  getFunnel: vi.fn(),
  getStageDetails: vi.fn()
}))

vi.mock('@/api/conversionFunnel', () => ({ default: api }))
vi.mock('@/utils/echarts', () => ({
  safeInit: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() }))
}))
vi.mock('echarts', () => ({ init: vi.fn(() => ({ setOption: vi.fn(), resize: vi.fn(), dispose: vi.fn() })) }))

import List from '@/views/conversionFunnel/List.vue'

// 一段一条、数字各不相同：任何"按位置取段"的错法都会落成一个对不上的数字。
const STAGES = [
  { stage: 'visit', name: '访问', count: 10, rate: 100, drop_rate: 0 },
  { stage: 'clue', name: '线索', count: 6, rate: 60, drop_rate: 40 },
  { stage: 'intent', name: '意向', count: 3, rate: 50, drop_rate: 50 },
  { stage: 'session', name: '会话', count: 2, rate: 66.666, drop_rate: 33.334 },
  { stage: 'opportunity', name: '商机', count: 1, rate: 50, drop_rate: 50 }
]

async function mountPage(stages = STAGES) {
  api.getFunnel.mockResolvedValue({
    start_time: '2026-09-01T00:00:00Z',
    end_time: '2026-09-30T23:59:59Z',
    stages,
    total: stages[0] ? stages[0].count : 0,
    conversion: 20
  })
  api.getStageDetails.mockResolvedValue({ stage: 'clue', name: '线索', count: 6, top_sources: [] })
  const wrapper = mount(List, { attachTo: document.body, global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

// statistics 返回 {标题: 值文本}，顺序即模板顺序（el-statistic 的 head/content 成对出现）。
function statistics(wrapper) {
  const out = {}
  for (const el of wrapper.findAll('.el-statistic')) {
    const title = el.find('.el-statistic__head').text().trim()
    out[title] = el.find('.el-statistic__content').text().trim()
  }
  return out
}

beforeEach(() => {
  vi.clearAllMocks()
})

describe('转化漏斗 KPI：五段之后仍按阶段名取数', () => {
  it('「转化量(会话)」显示的是会话段的 2，不是末段商机的 1', async () => {
    const wrapper = await mountPage()
    const s = statistics(wrapper)
    expect(Object.keys(s)).toContain('转化量(会话)')
    expect(s['总进入量']).toContain('10')
    expect(s['转化量(会话)']).toContain('2')
    expect(s['转化量(会话)']).not.toContain('1')
  })

  it('「端到端转化率」仍是 访问→会话 的 20%，没跟着末段一起改成 10%', async () => {
    const wrapper = await mountPage()
    const s = statistics(wrapper)
    expect(s['端到端转化率']).toContain('20')
    expect(s['端到端转化率']).not.toContain('10.0')
  })

  it('阶段明细把第五段也列出来（商机这一格必须在表里）', async () => {
    const wrapper = await mountPage()
    const rows = wrapper.findAll('.el-table__body-wrapper tbody tr')
    expect(rows).toHaveLength(5)
    const text = rows.map((r) => r.text()).join('|')
    expect(text).toContain('商机')
    expect(text).toContain('会话')
  })

  it('副标题写出的链路包含商机（口径写在页面上，不能只写在代码里）', async () => {
    const wrapper = await mountPage()
    expect(wrapper.find('.page-sub').text()).toContain('访问 → 线索 → 意向 → 会话 → 商机')
  })

  it('缺段时不炸：只回四段（老后端）时 KPI 仍取会话、取不到就 0', async () => {
    // 这条防的是"按名字取"改完之后对缺段的处理：宁可显示 0，也不能拿别的段顶上去。
    const wrapper = await mountPage(STAGES.filter((s) => s.stage !== 'session'))
    const s = statistics(wrapper)
    expect(s['转化量(会话)']).toContain('0')
    expect(s['总进入量']).toContain('10')
  })
})
