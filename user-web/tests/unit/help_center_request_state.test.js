import { beforeEach, afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import i18n from '@/i18n'
import HelpCenter from '@/views/public/HelpCenter.vue'
const fetchMock = vi.fn()
let wrapper
const reply = (data, status = 200, code = 0, message = 'ok') => ({ ok: status >= 200 && status < 300, status, json: async () => ({ code, data, message }) })
const article = (id, title) => ({ id, title, content: `${title}正文`, category: '指南' })
beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock)
  fetchMock.mockReset().mockImplementation(async url => String(url).includes('/categories') ? reply({ list: [{ category: '指南', count: 1 }] }) : reply({ list: [] }))
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.unstubAllGlobals(); vi.useRealTimers() })
async function page() {
  wrapper = mount(HelpCenter, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
  await settle(wrapper)
}
describe('帮助中心请求与显示状态', () => {
  it('HTTP/业务失败不显示空态，重试恢复正常列表', async () => {
    let fail = true
    fetchMock.mockImplementation(async url => String(url).includes('/categories') ? reply({ list: [] }) : fail ? reply(null, 400, 400, '缺少租户短码') : reply({ list: [article(1, '恢复文章')] }))
    await page()
    expect(wrapper.text()).toContain('缺少租户短码')
    expect(wrapper.text()).not.toContain('暂无公开文章')
    fail = false
    await wrapper.findAll('button').find(button => button.text() === i18n.global.t('重试')).trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('恢复文章')
    expect(wrapper.text()).not.toContain('缺少租户短码')
  })
  it.each([null, { list: 'invalid' }])('非法成功载荷 %s 不能作为空列表', async data => {
    fetchMock.mockImplementation(async url => String(url).includes('/categories') ? reply({ list: [] }) : reply(data))
    await page()
    expect(wrapper.text()).toContain(i18n.global.t('加载失败'))
    expect(wrapper.text()).not.toContain('暂无公开文章')
  })
  it('旧搜索响应不能覆盖新关键词结果', async () => {
    await page()
    let resolveOld
    fetchMock.mockImplementation(url => {
      if (String(url).includes('q=old')) return new Promise(resolve => { resolveOld = resolve })
      return Promise.resolve(reply({ list: [article(2, '新结果')] }))
    })
    wrapper.vm.keyword = 'old'
    const old = wrapper.vm.loadArticles()
    wrapper.vm.keyword = 'new'
    await wrapper.vm.loadArticles()
    resolveOld(reply({ list: [article(1, '过时结果')] }))
    await old; await settle(wrapper)
    expect(wrapper.text()).toContain('新结果')
    expect(wrapper.text()).not.toContain('过时结果')
  })
  it('切换分类后迟到的文章详情不能重新打开', async () => {
    await page()
    let resolveDetail
    fetchMock.mockImplementation(url => String(url).includes('/articles/1') ? new Promise(resolve => { resolveDetail = resolve }) : Promise.resolve(reply({ list: [] })))
    const pending = wrapper.vm.openArticle(1)
    await wrapper.vm.setCat('指南')
    resolveDetail(reply(article(1, '迟到详情')))
    await pending; await settle(wrapper)
    expect(wrapper.find('.article-detail').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('迟到详情')
  })
  it('分类读取失败独立显示，文章为空不掩盖分类故障', async () => {
    fetchMock.mockImplementation(async url => String(url).includes('/categories') ? reply(null, 503, 503, '分类暂不可用') : reply({ list: [] }))
    await page()
    expect(wrapper.text()).toContain('分类暂不可用')
    expect(wrapper.text()).toContain('暂无公开文章')
  })
  it('卸载时清理搜索防抖，不再发起请求', async () => {
    await page()
    vi.useFakeTimers()
    fetchMock.mockClear()
    await wrapper.get('input[placeholder="搜索文章…"]').setValue('待搜索')
    wrapper.unmount(); wrapper = null
    await vi.advanceTimersByTimeAsync(350)
    expect(fetchMock).not.toHaveBeenCalled()
  })
})
