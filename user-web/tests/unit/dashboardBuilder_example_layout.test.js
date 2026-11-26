import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessageBox } from 'element-plus'
import i18n from '@/i18n'
import zh from '@/i18n/locales/navigationContent.zh.json'
import en from '@/i18n/locales/navigationContent.en.json'
import { settle } from './settle-helper'

const http = vi.hoisted(() => ({ post: vi.fn() }))
vi.mock('@/utils/request', () => ({ http }))
vi.mock('@/utils/echarts', () => ({ safeInit: () => ({ setOption: vi.fn() }), safeDispose: vi.fn() }))
import Builder from '@/views/dashboardScreen/Builder.vue'

let wrapper
beforeEach(() => {
  vi.clearAllMocks()
  i18n.global.mergeLocaleMessage('zh', zh)
  i18n.global.mergeLocaleMessage('en', en)
  i18n.global.locale.value = 'zh'
  http.post.mockResolvedValue({ id: 1 })
  vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm')
})
afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
})
async function page() {
  wrapper = mount(Builder, { global: { plugins: [ElementPlus, i18n] } })
  await settle(wrapper)
}

describe('自定义看板示例布局', () => {
  it('数字卡只显示未绑定占位，新增和拖入组件均明确标为示例', async () => {
    await page()
    const kpi = wrapper.findAll('.palette-item').find(item => item.text() === '数字卡')
    await kpi.trigger('click')
    await settle(wrapper)
    expect(wrapper.find('.kpi').text()).toBe('—')
    expect(wrapper.find('.canvas-block').text()).toContain('布局示例')
    expect(wrapper.find('.canvas-block').text()).toContain('未绑定业务数据源')
    await wrapper.find('.canvas').trigger('drop', { dataTransfer: { getData: () => 'bar' } })
    await settle(wrapper)
    expect(wrapper.findAll('.canvas-block')).toHaveLength(2)
    expect(wrapper.findAll('.canvas-block')[1].text()).toContain('布局示例')
    expect(wrapper.findAll('.canvas-block')[1].text()).toContain('未绑定业务数据源')
    expect(wrapper.text()).toContain('保存或公开后仍是示例布局')
  })

  it('公开保存确认说明无业务数据，持久化布局与每个块都带示例标记且没有伪造数值', async () => {
    await page()
    await wrapper.findAll('.palette-item').find(item => item.text() === '数字卡').trigger('click')
    await wrapper.findAll('button').find(button => /保存/.test(button.text())).trigger('click')
    await settle(wrapper)
    expect(ElMessageBox.confirm.mock.calls[0][0]).toContain('所有组件均未绑定业务数据源')
    expect(ElMessageBox.confirm.mock.calls[0][0]).toContain('公开链接也只展示示例布局')
    expect(http.post).toHaveBeenCalledTimes(1)
    const payload = http.post.mock.calls[0][1]
    expect(payload.is_public).toBe(true)
    expect(payload.layout.kind).toBe('example')
    expect(payload.layout.data_source).toBeNull()
    expect(payload.layout.blocks[0].title).toContain('布局示例')
    expect(payload.layout.blocks[0].data).toEqual({ is_example: true, data_source: null })
  })

  it('未知拖放类型不保存为无法渲染的组件', async () => {
    await page()
    await wrapper.find('.canvas').trigger('drop', { dataTransfer: { getData: () => 'unsupported' } })
    await settle(wrapper)
    expect(wrapper.find('.canvas-block').exists()).toBe(false)
  })

  it('关闭公开开关后保存为私有示例，确认期间不重复提交', async () => {
    await page()
    await wrapper.find('.el-switch input').setValue(false)
    let confirm
    ElMessageBox.confirm.mockReturnValue(new Promise(resolve => { confirm = resolve }))
    const save = wrapper.findAll('button').find(button => button.text() === '保存示例布局')
    await save.trigger('click')
    await settle(wrapper)
    expect(save.attributes('disabled')).toBeDefined()
    expect(ElMessageBox.confirm.mock.calls[0][0]).toContain('非公开示例布局')
    expect(http.post).not.toHaveBeenCalled()
    confirm('confirm')
    await settle(wrapper)
    expect(http.post.mock.calls[0][1].is_public).toBe(false)
    expect(save.attributes('disabled')).toBeUndefined()
  })
})
