import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

// 动态阈值参数页的渲染用例。
//
// 这一页是「配置治理」在用户侧唯一的落地界面，所以它的每一条友好性承诺都要有钉子：
//   1. 已下架的旋钮必须显式可见且不可编辑 —— 页面挂着一个改了不生效的旋钮，
//      比没有这个旋钮更糟（用户改完去验证，行为一定不变，原因却藏在代码里）。
//   2. 分组中文名必须来自后端 —— 原来这里是前端写死的 18 项 GROUP_LABELS，
//      后端实际有 29 组，新增的组在页面上静默回落成英文 key。
//   3. duration 不能一律显示「N 秒」 —— 86400 秒没人能一眼读成一天。
//   4. 高危参数保存前必须二次确认；需重启/敏感参数必须有提示。
//   5. 已下架参数不得计入「重置整组」的条数（后端 BulkResetGroup 会跳过它们）。
//
// api 被 mock：端点归属与查询串有各自的用例，这里只钉"渲染 + 交互"。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  listGroups: vi.fn(),
  auditLogs: vi.fn(),
  update: vi.fn(),
  reset: vi.fn(),
  bulkReset: vi.fn()
}))

vi.mock('@/api/configParams', () => ({
  list: api.list,
  listGroups: api.listGroups,
  auditLogs: api.auditLogs,
  update: api.update,
  reset: api.reset,
  bulkReset: api.bulkReset
}))

import ConfigParams from '@/views/system/ConfigParams.vue'
import { settle } from './settle-helper'

// 本文件原来没有 afterEach：每条用例 mount 出的组件连同它的 watcheffect 一直活到
// 文件结束（不 attachTo body 看不见，但实例、watcher、teleport 归属都在）。
// 登记到 mounted，统一在 afterEach 卸载——释放点唯一化，断言失败也不漏。
const mounted = []

function mountPage() {
  const w = mount(ConfigParams, { global: { plugins: [ElementPlus] } })
  mounted.push(w)
  return w
}

afterEach(() => {
  while (mounted.length) {
    try {
      mounted.pop().unmount()
    } catch {
      /* 重复卸载不抛即可，不掩盖用例本身的失败 */
    }
  }
  document.body.innerHTML = ''
})

const GROUPS = [
  { group: 'lock', label: '分布式锁', description: '锁与幂等的存活时间', sort_order: 10 },
  { group: 'agent_llm', label: 'AI 大模型', description: '模型调用与熔断', sort_order: 20 },
  // 这一组是「标题真的来自后端」的活证：watchdog_probe 不在任何前端硬编码表里，
  // 页面要是哪天改回本地 GROUP_LABELS 兜底，这里只能落回 key 本身或「-」，
  // 断「分布式锁/AI 大模型」这种老组名永远抓不住那类回归——它们的硬编码文案与
  // 后端 mock 恰好同文。后端新增组静默回落英文 key 正是这一卡修掉的 bug。
  { group: 'watchdog_probe', label: '后端新增看门狗组', description: '探针与自愈参数', sort_order: 30 }
]

function param(over) {
  return {
    group: 'lock',
    key: 'inbox_human_ttl',
    name: '收件箱人工锁存活时间',
    description: '超过后其他坐席可以抢接管',
    value_type: 'duration',
    value: '86400',
    default_value: '86400',
    min: '60',
    max: '604800',
    step: '60',
    read_only: false,
    restart: false,
    category: '',
    updated_by: 0,
    deprecated: false,
    ...over
  }
}

describe('ConfigParams.vue 渲染与友好性承诺', () => {
  beforeEach(() => {
    api.list.mockReset()
    api.listGroups.mockReset()
    api.update.mockReset().mockResolvedValue({})
    api.reset.mockReset().mockResolvedValue({})
    api.bulkReset.mockReset().mockResolvedValue({})
    api.listGroups.mockResolvedValue([...GROUPS])
    api.list.mockResolvedValue([
      param({}),
      param({
        key: 'ai_processing_ttl',
        name: 'AI 处理中标记存活时间（已下架）',
        value: '120',
        default_value: '120',
        deprecated: true,
        deprecated_at: '2026-10-12T03:04:05Z',
        deprecated_reason: 'tryAcquireAILock 全树只有测试调用，生产并发闸由 ai_processing_ttl 承担'
      }),
      param({
        group: 'agent_llm',
        key: 'temperature',
        name: '采样温度',
        value_type: 'float',
        value: '0.7',
        default_value: '0.7',
        min: '0',
        max: '2',
        risk: 'danger',
        restart: true,
        unit: ''
      })
    ])
  })

  it('分组中文名来自后端，不再是前端硬编码表', async () => {
    const w = mountPage()
    await settle(w)
    expect(api.listGroups).toHaveBeenCalled()
    const text = w.text()
    expect(text).toContain('分布式锁')
    expect(text).toContain('AI 大模型')
    // watchdog_probe 是后端 mock 独有的组名：旧版断言只认「分布式锁/AI 大模型」，
    // 可这两组的硬编码文案与后端 mock 恰好同文——就算整页改回写死的 GROUP_LABELS
    // 这两条也照样绿。后端新增的组在硬编码表里查无此项，回落 key 即红，这才是真证。
    expect(text).toContain('后端新增看门狗组')
  })

  it('默认不显示已下架项，打开开关后才列出', async () => {
    const w = mountPage()
    await settle(w)
    // 后端即使把已下架项一起返回（过滤回归 / 老后端配新前端），页面也不渲染它。
    // 判据用表格行数而不是全文搜索「已下架」三个字 —— 开关的标签本身就叫
    // 「显示已下架」，全文搜索会把开关当成一条已下架记录，判红得毫无道理。
    expect(api.list).toHaveBeenLastCalledWith(undefined, false)
    expect(w.findAll('.el-table__row')).toHaveLength(1)

    await w.find('.el-switch').trigger('click')
    await settle(w)
    expect(api.list).toHaveBeenLastCalledWith(undefined, true)
    expect(w.findAll('.el-table__row')).toHaveLength(2)
  })

  it('duration 显示成人类可读单位而不是一串秒数', async () => {
    const w = mountPage()
    await settle(w)
    const text = w.text()
    expect(text).toContain('1 天')
    expect(text).not.toContain('86400 秒')
  })

  it('已下架项的编辑按钮禁用，且行内给出下架原因', async () => {
    const w = mountPage()
    await settle(w)
    await w.find('.el-switch').trigger('click')
    await settle(w)
    // 下架原因必须出现在行内（不是 tooltip 里悬停才看得到）。
    expect(w.text()).toContain('tryAcquireAILock')
    // 两行各有各的编辑按钮，所以这里数的是「禁用的那一个有几个」而不是
    // 「编辑按钮有几个」—— 早先写成后者，红得有理有据：数的是总数，
    // 而我想验的是「下架那个被禁了」，两件事被一个断言混成了一件。
    const editButtons = w.findAll('button').filter((b) => b.text().includes('编辑'))
    expect(editButtons).toHaveLength(2)
    const disabled = editButtons.filter((b) => b.attributes('disabled') !== undefined)
    expect(disabled).toHaveLength(1)
    // 只看 disabled 属性不够：Element Plus 会同时给 aria-disabled 与
    // is-disabled class，三者一致才算真禁用（历史上出现过只加了 class 的情况）。
    expect(disabled[0].attributes('aria-disabled')).toBe('true')
    expect(disabled[0].classes()).toContain('is-disabled')
  })

  it('高危 + 需重启参数带徽标', async () => {
    const w = mountPage()
    await settle(w)
    // temperature 在 agent_llm 组，当前选中的是第一个组 lock —— 必须先切过去，
    // 否则「找不到徽标」会被误读成「徽标没渲染」。
    await w.findAll('.group-item')[1].trigger('click')
    await settle(w)
    const text = w.text()
    expect(text).toContain('高危')
    expect(text).toContain('需重启')
  })

  it('种子里的状态标记（「（未接线）」等）不出现在给用户看的表格里', async () => {
    const w = mountPage()
    await settle(w)
    expect(w.text()).not.toContain('（未接线）')
  })

  it('已下架项不计入「重置整组」条数', async () => {
    const w = mountPage()
    await settle(w)
    await w.find('.el-switch').trigger('click')
    await settle(w)
    // 只有 ai_processing_ttl 有「已修改」标记（value 120 vs default 120 → 实为未改），
    // 这里再显式造一条已修改且已下架的行，确认它不进计数。
    api.list.mockResolvedValue([
      param({ key: 'modified_live', value: '3600', default_value: '86400' }),
      param({ key: 'modified_dead', value: '3600', default_value: '86400', deprecated: true, deprecated_reason: '无消费点' })
    ])
    await w.vm.loadAll()
    await settle(w)
    expect(w.text()).toContain('重置整组')
    // 两条都「已修改」，但 modified_dead 已下架 ⇒ 只有 1 条进计数。
    // 后端 BulkResetGroup 遇 deprecated 是「跳过」而不是「整组失败」，
    // 前端报 2 就是骗用户。
    expect(w.find('.reset-badge').text()).toBe('1')
  })

  it('搜索跨组时列出分组列并给出提示', async () => {
    const w = mountPage()
    await settle(w)
    const input = w.find('.header-actions input')
    await input.setValue('temperature')
    await settle(w)
    expect(w.text()).toContain('正在跨全部')
    expect(w.text()).toContain('AI 大模型')
  })

  it('只读项的编辑与重置按钮都禁用', async () => {
    // 服务端对 read_only 的 UpdateValue/ResetToDefault 一律回 ErrParamReadOnly，
    // 表格这一列曾经只禁了「编辑」没禁「重置」：用户点得动，然后收到一条红字。
    api.list.mockResolvedValue([
      param({ key: 'embedding_dimension', name: 'Embedding 维度', value: '512', default_value: '1024', read_only: true })
    ])
    const w = mountPage()
    await settle(w)
    const buttons = w.findAll('button')
    const edit = buttons.filter((b) => b.text().includes('编辑'))
    const reset = buttons.filter((b) => b.text() === '重置')
    expect(edit).toHaveLength(1)
    expect(reset).toHaveLength(1)
    expect(edit[0].attributes('disabled')).toBeDefined()
    expect(reset[0].attributes('disabled')).toBeDefined()
    // 正控制：同一列的写侧入口对可写项必须是**能点**的，否则上面两条断言
    // 只是「整列都渲染成禁用」的假绿。
    api.list.mockResolvedValue([param({ key: 'embedding_dimension', value: '512', default_value: '1024' })])
    await w.vm.loadAll()
    await settle(w)
    const writable = w.findAll('button')
    expect(writable.filter((b) => b.text() === '重置')[0].attributes('disabled')).toBeUndefined()
  })

  it('已修改的只读项不计入「重置整组」条数', async () => {
    // 后端 BulkResetGroup 遇 read_only 是「跳过」，整组请求不会失败，
    // 所以前端多报的那一条既没被改、也不会有任何错误提示——纯粹的谎。
    api.list.mockResolvedValue([
      param({ key: 'embedding_dimension', value: '512', default_value: '1024', read_only: true })
    ])
    const w = mountPage()
    await settle(w)
    expect(w.find('.reset-badge').exists()).toBe(false)
    api.list.mockResolvedValue([
      param({ key: 'embedding_dimension', value: '512', default_value: '1024', read_only: true }),
      param({ key: 'inbox_human_ttl', value: '3600', default_value: '86400' })
    ])
    await w.vm.loadAll()
    await settle(w)
    // 两条都「已修改」，但只读那条不参与 ⇒ 计数 1。
    expect(w.find('.reset-badge').text()).toBe('1')
  })

  it('只读项不让左侧栏分组亮起「已修改」角标', async () => {
    // 分组栏那个角标是「这组有本页能处理的偏离」的指路牌。只读项在本页三处入口
    // （编辑、单条重置、批量重置）全都动不了，把它算进角标等于把人往一扇锁着的门上引。
    // 行内的「已修改」徽标仍然显示，所以状态本身没有被藏起来。
    api.list.mockResolvedValue([
      param({ key: 'embedding_dimension', value: '512', default_value: '1024', read_only: true })
    ])
    const w = mountPage()
    await settle(w)
    expect(w.find('.group-mod').exists()).toBe(false)
    expect(w.find('.changed-badge').exists()).toBe(true)
    api.list.mockResolvedValue([param({ key: 'embedding_dimension', value: '512', default_value: '1024' })])
    await w.vm.loadAll()
    await settle(w)
    // 正控制：同一行改成可写后角标必须亮起来，否则上面的「不存在」是渲染坏了不是判据生效。
    expect(w.find('.group-mod').text()).toBe('1')
  })
})

// 写入载荷形状：整页用例都把 api mock 掉断「调用发生过」，这一族断「传过去的是什么」。
//
// 服务端把 value 当一段 JSON 标量解析（controller.decodeScalarValue）后再按 value_type
// 校验，所以每种类型必须送出**那一种**标量：int 送数字 42 而不是 "42"、bool 送 boolean
// 而不是 "1"。这条腿以前完全没被测过 —— 前端送字符串在服务端还没收紧时照样存进去，
// 服务端一收紧就是整页 400；反过来前端形状错了也没有任何门会响。
describe('ConfigParams.vue 写入载荷形状', () => {
  beforeEach(() => {
    api.list.mockReset()
    api.update.mockReset().mockResolvedValue({})
    api.listGroups.mockResolvedValue([...GROUPS])
  })

  // 走真实入口：从表格渲染出来的那一行点「编辑」开对话框，再改编辑框绑定的值。
  // 不直接 openEdit(手造 row)，否则测的是「我以为的 row 形状」而不是后端真回的形状。
  async function editFirstRow(rows) {
    api.list.mockResolvedValue(rows)
    const w = mountPage()
    await settle(w)
    const editBtn = w.findAll('button').find((b) => b.text().includes('编辑'))
    expect(editBtn).toBeTruthy()
    await editBtn.trigger('click')
    await settle(w)
    return w
  }

  function sent(over) {
    expect(api.update).toHaveBeenCalledTimes(1)
    const call = api.update.mock.calls[0]
    expect(call[0]).toBe('lock')
    expect(call[1]).toBe(over)
    return call[2]
  }

  it('int 送取整后的数字', async () => {
    // 取整来自 confirmEdit 的 Math.round 而不是输入框精度：把那一处短路成 if (false)
    // 后这一格即红（42.6 原样送出），所以这一格同时钉住了形状与取整两条腿。
    const w = await editFirstRow([
      param({ key: 'page_size', name: '分页条数', value_type: 'int', value: '30', default_value: '30', min: '1', max: '1000', step: '1' })
    ])
    w.vm.editValueNum = 42.6
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('page_size')).toBe(43)
    expect(typeof api.update.mock.calls[0][2]).toBe('number')
  })

  it('float 送数字且不取整', async () => {
    const w = await editFirstRow([
      param({ key: 'ratio', name: '采样比例', value_type: 'float', value: '0.7', default_value: '0.7', min: '0', max: '1', step: '0.1' })
    ])
    w.vm.editValueNum = 0.25
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('ratio')).toBe(0.25)
  })

  it('duration 送秒数的数字形态', async () => {
    const w = await editFirstRow([param({ key: 'inbox_human_ttl', value: '3600', default_value: '86400' })])
    w.vm.editValueNum = 7200
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('inbox_human_ttl')).toBe(7200)
    expect(typeof api.update.mock.calls[0][2]).toBe('number')
  })

  it('bool 送真正的布尔值', async () => {
    // 这条最容易被漏：bool 行的编辑控件是 el-switch，值是 boolean；
    // 一旦某次改动把它写成字符串 "true"，服务端的 bool 校验会把它当非法值拒掉，
    // 而「拒掉」在页面上只是一条红字，不看载荷断言根本发现不了。
    const w = await editFirstRow([
      param({ key: 'verbose_log', name: '详细日志', value_type: 'bool', value: 'false', default_value: 'false', min: undefined, max: undefined, step: undefined })
    ])
    w.vm.editValueBool = true
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('verbose_log')).toBe(true)
    expect(typeof api.update.mock.calls[0][2]).toBe('boolean')
  })

  it('json 送字符串且逐字不改', async () => {
    // 正控制：合法 JSON 的改动必须真的发出去。下面两格「不发请求」的断言只有配上这一格
    // 才有意义，否则「confirmEdit 从不提交」也能让三格全绿。
    const w = await editFirstRow([
      param({ key: 'global_override', name: '全局覆盖', value_type: 'json', value: '{"a":1}', default_value: '{"a":1}' })
    ])
    w.vm.editValueStr = '{"a":2}'
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('global_override')).toBe('{"a":2}')
    expect(typeof api.update.mock.calls[0][2]).toBe('string')
  })

  it('string 送字符串', async () => {
    const w = await editFirstRow([
      param({ key: 'industry_profile', name: '行业画像', value_type: 'string', value: 'retail', default_value: 'retail' })
    ])
    w.vm.editValueStr = 'manufacture'
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('industry_profile')).toBe('manufacture')
  })

  it('JSON 参数不能置空：拦在前端，不发请求', async () => {
    const w = await editFirstRow([
      param({ key: 'global_override', name: '全局覆盖', value_type: 'json', value: '{"a":1}', default_value: '{"a":1}' })
    ])
    w.vm.editValueStr = ''
    await settle(w)
    await w.vm.confirmEdit()
    expect(api.update).not.toHaveBeenCalled()
    expect(String(w.vm.editError)).toContain('不能置空')
  })

  it('坏 JSON 提交不出去', async () => {
    const w = await editFirstRow([
      param({ key: 'global_override', name: '全局覆盖', value_type: 'json', value: '{"a":1}', default_value: '{"a":1}' })
    ])
    w.vm.editValueStr = '{"a":1,}'
    await settle(w)
    await w.vm.confirmEdit()
    expect(api.update).not.toHaveBeenCalled()
    expect(String(w.vm.editError)).toContain('JSON 语法错误')
  })

  it('值本身是 JSON 的 string 参数同样受空值与语法约束', async () => {
    // looksLikeJson 不只认 value_type：库里存量的 string 旋钮照样可能装着一整份 JSON
    // （这一轮把 3 条 retyped 成 json，但读侧与历史行仍会回字符串），
    // 空串对它们同样是「把整份策略抹掉」，不能让类型名成为漏网的口子。
    const w = await editFirstRow([
      param({ key: 'provider_failover_policy', name: '容灾策略', value_type: 'string', value: '[{"p":"a"}]', default_value: '[{"p":"a"}]' })
    ])
    w.vm.editValueStr = ''
    await settle(w)
    await w.vm.confirmEdit()
    expect(api.update).not.toHaveBeenCalled()
    expect(String(w.vm.editError)).toContain('不能置空')
  })

  it('普通字符串参数允许显式置空', async () => {
    // 反向一格：上面的空值拦截只能打在 JSON 上。普通串参数「置空」是有意义的
    // （服务端的 string 分支明确接受空串＝显式清空），前端把它一起拦掉就是多了一处假墙。
    const w = await editFirstRow([
      param({ key: 'industry_profile', name: '行业画像', value_type: 'string', value: 'retail', default_value: 'retail' })
    ])
    w.vm.editValueStr = ''
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('industry_profile')).toBe('')
  })

  it('数字输入框会把越界值夹回上下限后提交', async () => {
    // 这一格原本断的是「validateEdit 报『不能大于最大值』」，实测读数是提交成功、
    // 值为 100：el-input-number 在 modelValue 变化时就先把值夹进 [min,max] 并回写，
    // 那条范围报错在配了 min/max 的行上根本走不到。
    // 所以这里如实钉住**真实防线**（客户端夹价），而不是钉一条打不着的错误文案；
    // 后端 validateValue 的范围校验仍是权威，见 controller/service 那侧的用例。
    const w = await editFirstRow([
      param({ key: 'page_size', name: '分页条数', value_type: 'int', value: '30', default_value: '30', min: '1', max: '100', step: '1' })
    ])
    w.vm.editValueNum = 5000
    await settle(w)
    await w.vm.confirmEdit()
    expect(sent('page_size')).toBe(100)
    // 下界同样夹：填 0 不会把 0 发出去（int 的 0 对分页条数同样是「填错了」）。
    w.vm.editValueNum = 0
    await settle(w)
    api.update.mockClear()
    await w.vm.confirmEdit()
    expect(sent('page_size')).toBe(1)
    expect(String(w.vm.editError)).toBe('')
  })

  it('min 为 0 的 duration 填 0 时由前端拦下', async () => {
    // 锁 TTL / 缓冲区上限填 0 的后果是「不报错、只是功能悄悄不工作」，
    // 而这种行的 min 就是 0 ⇒ 客户端夹价拦不住，服务端按范围校验也放行，
    // 唯一还在出声的就是 looksBlockingZero 这一层。
    const w = await editFirstRow([
      param({ key: 'inbox_human_ttl', value: '3600', default_value: '86400', min: '0', max: '604800', step: '60' })
    ])
    w.vm.editValueNum = 0
    await settle(w)
    await w.vm.confirmEdit()
    expect(api.update).not.toHaveBeenCalled()
    expect(String(w.vm.editError)).toContain('0 会让这个参数失效')
  })

  it('值没改时保存按钮禁用（不会留一条空审计）', async () => {
    // 每一次 update 都写一条 config_param_audit_logs。原值不改也提交，
    // 审计里就会堆满「A 改成 A」这种噪声行，把真改动埋掉。
    api.list.mockResolvedValue([param({ key: 'inbox_human_ttl', value: '86400', default_value: '86400' })])
    const w = mountPage()
    await settle(w)
    const editBtn = w.findAll('button').find((b) => b.text().includes('编辑'))
    await editBtn.trigger('click')
    await settle(w)
    const save = w.findAll('button').find((b) => b.text().includes('保存'))
    expect(save).toBeTruthy()
    expect(save.attributes('disabled')).toBeDefined()
  })
})
