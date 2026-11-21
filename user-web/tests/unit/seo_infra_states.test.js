import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import i18n from '@/i18n'
const api = vi.hoisted(() => ({ getLlmsTxtPreview: vi.fn(), getRobotsTxtPreview: vi.fn(), getSitemapPreview: vi.fn() }))
vi.mock('@/api/geo', () => ({ geoApi: api }))
import SeoInfra from '@/views/geo/SeoInfra.vue'
let wrapper
beforeEach(() => {
  vi.clearAllMocks()
  api.getLlmsTxtPreview.mockResolvedValue({ content: '# 受控站点' })
  api.getRobotsTxtPreview.mockResolvedValue({ content: 'User-agent: *' })
  api.getSitemapPreview.mockResolvedValue({ content: '<urlset />' })
})
afterEach(() => wrapper?.unmount())
async function page() { wrapper = mount(SeoInfra, { global: { plugins: [ElementPlus, i18n] } }); await settle(wrapper) }
describe('SEO 预览独立状态', () => {
  it('robots 失败不丢 llms 成功内容，重试能恢复', async () => {
    api.getRobotsTxtPreview.mockRejectedValueOnce(new Error('robots 暂不可用'))
    await page()
    expect(wrapper.text()).toContain('# 受控站点')
    expect(wrapper.text()).not.toContain('✅')
    await wrapper.findAll('[role="tab"]').find(tab => tab.text() === 'robots.txt').trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('robots 暂不可用')
    await wrapper.findAll('button').find(button => button.text() === i18n.global.t('重试')).trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('User-agent: *')
    expect(wrapper.text()).not.toContain('robots 暂不可用')
  })
  it('空文本显示空态，非法 content 显示失败', async () => {
    api.getLlmsTxtPreview.mockResolvedValue({ content: '' })
    await page()
    expect(wrapper.text()).toContain(i18n.global.t('暂无数据'))
    api.getLlmsTxtPreview.mockResolvedValue({})
    await wrapper.vm.loadPreview('llms')
    await settle(wrapper)
    expect(wrapper.text()).toContain(i18n.global.t('加载失败'))
  })
  it('生成请求在途时重复点击只读取一次', async () => {
    await page()
    let resolve
    api.getSitemapPreview.mockReturnValueOnce(new Promise(done => { resolve = done }))
    const first = wrapper.vm.loadPreview('sitemap')
    await wrapper.vm.loadPreview('sitemap')
    expect(api.getSitemapPreview).toHaveBeenCalledTimes(1)
    resolve({ content: '<urlset>合成地址</urlset>' })
    await first
    await wrapper.findAll('[role="tab"]').find(tab => tab.text() === 'sitemap.xml').trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('<urlset>合成地址</urlset>')
  })
})
