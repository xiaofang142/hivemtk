import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import zh from '@/i18n/locales/navigationContent.zh.json'
import en from '@/i18n/locales/navigationContent.en.json'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ getLlmsTxtPreview: vi.fn(), getRobotsTxtPreview: vi.fn() }))
vi.mock('@/api/geo', () => ({ geoApi: api }))
import BrandConfig from '@/views/geo/BrandConfig.vue'

let wrapper
beforeEach(() => {
  vi.clearAllMocks()
  i18n.global.mergeLocaleMessage('zh', zh)
  i18n.global.mergeLocaleMessage('en', en)
  i18n.global.locale.value = 'zh'
  api.getLlmsTxtPreview.mockResolvedValue({ content: '# preview' })
  api.getRobotsTxtPreview.mockResolvedValue({ content: 'User-agent: *' })
})
afterEach(() => wrapper?.unmount())
async function page() {
  wrapper = mount(BrandConfig, { global: { plugins: [ElementPlus, i18n] } })
  await settle(wrapper)
}

describe('爬虫文件预览状态', () => {
  it('两个请求失败时显示可重试错误，不把异常文案变成内容或合规成功', async () => {
    api.getLlmsTxtPreview.mockRejectedValue(new Error('502'))
    api.getRobotsTxtPreview.mockRejectedValue(new Error('502'))
    await page()
    expect(wrapper.findAll('.el-alert--error')).toHaveLength(2)
    expect(wrapper.findAll('pre')).toHaveLength(0)
    expect(wrapper.find('.el-tag--success').exists()).toBe(false)
    expect(wrapper.text()).toContain('预览生成失败，请重试。')
  })

  it('成功拿到文件只显示未验证预览，不声称规范合规或覆盖数量', async () => {
    await page()
    expect(wrapper.text()).toContain('# preview')
    expect(wrapper.text()).toContain('User-agent: *')
    expect(wrapper.findAll('.el-tag--info')).toHaveLength(2)
    expect(wrapper.text()).toContain('预览内容 · 未验证')
    expect(wrapper.find('.el-tag--success').exists()).toBe(false)
    expect(wrapper.text()).not.toMatch(/符合 llms|覆盖 21/)
  })

  it('加载时隐藏上次内容并显示生成状态，重复请求不并发', async () => {
    let resolve
    api.getLlmsTxtPreview.mockReturnValue(new Promise(done => { resolve = done }))
    await page()
    expect(wrapper.text()).toContain('正在生成预览…')
    const generate = wrapper.findAll('button').find(button => button.text() === '生成预览')
    expect(generate.attributes('disabled')).toBeDefined()
    expect(api.getLlmsTxtPreview).toHaveBeenCalledTimes(1)
    resolve({ content: '# ready' })
    await settle(wrapper)
    expect(wrapper.text()).toContain('# ready')
    expect(wrapper.text()).not.toContain('正在生成预览…')
  })

  it('重试失败清掉旧预览，恢复后仍然未验证', async () => {
    await page()
    api.getLlmsTxtPreview.mockRejectedValueOnce(new Error('offline'))
    await wrapper.findAll('button').find(button => button.text() === '生成预览').trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).not.toContain('# preview')
    expect(wrapper.findAll('.el-alert--error')).toHaveLength(1)
    await wrapper.findAll('button').find(button => button.text() === '重试').trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('# preview')
    expect(wrapper.find('.el-alert--error').exists()).toBe(false)
    expect(wrapper.find('.el-tag--success').exists()).toBe(false)
  })

  it('空内容与无效响应分开显示，均不能显示合规标签', async () => {
    api.getLlmsTxtPreview.mockResolvedValue({ content: '' })
    api.getRobotsTxtPreview.mockResolvedValue({ content: { invalid: true } })
    await page()
    expect(wrapper.text()).toContain('未返回预览内容，请检查配置后重新生成。')
    expect(wrapper.findAll('.el-alert--error')).toHaveLength(1)
    expect(wrapper.findAll('pre')).toHaveLength(0)
    expect(wrapper.find('.el-tag--success').exists()).toBe(false)
  })
})
