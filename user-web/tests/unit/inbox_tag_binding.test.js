import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { ESLint } from 'eslint'
import List from '@/views/inbox/List.vue'
import i18n from '@/i18n'
import { settle, settleBody } from './settle-helper'

const boundary = vi.hoisted(() => ({ tags: [], delete: vi.fn() }))
vi.mock('@/utils/request', () => ({
  http: {
    get: vi.fn(url => {
      const conversation = { id: 'tag-conversation', customer_name: '标签测试客户', platform: 'web', status: 'open', unread_count: 0, tags: boundary.tags }
      if (url === '/api/inbox/conversations') return Promise.resolve({ list: [conversation], total: 1 })
      if (url === '/api/inbox/conversations/tag-conversation') return Promise.resolve(conversation)
      if (url === '/api/inbox/stats') return Promise.resolve({ total: 1, open: 1 })
      return Promise.resolve({ list: [], total: 0 })
    }),
    delete: boundary.delete,
  },
}))
vi.mock('@/stores/user', () => ({ useUserStore: () => ({ userInfo: { id: 7, role: 'customer_service' } }) }))

let wrapper
let previousLocale
beforeEach(() => {
  vi.clearAllMocks()
  boundary.tags = ['标签 / A&B']
  boundary.delete.mockImplementation(() => { boundary.tags = []; return Promise.resolve({}) })
  previousLocale = i18n.global.locale.value
  i18n.global.locale.value = 'zh'
})
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  i18n.global.locale.value = previousLocale
})

async function selectConversation() {
  wrapper = mount(List, { attachTo: document.body, global: { plugins: [createPinia(), ElementPlus, i18n] } })
  await settle(wrapper)
  await wrapper.get('.conv-item').trigger('click')
  await settle(wrapper)
}

describe('标签局部变量不遮蔽翻译函数', () => {
  it('两个标签循环均通过实际模板作用域检查', async () => {
    const reports = await new ESLint().lintFiles('src/views/inbox/List.vue')
    expect(reports.flatMap(report => report.messages).filter(message => message.ruleId === 'vue/no-template-shadow')).toEqual([])
  })

  it('列表关闭标签仍删除原始值，并由API模块进行URL编码', async () => {
    await selectConversation()
    const tag = wrapper.findAll('.conv-item-bottom .el-tag').find(item => item.find('.el-tag__close').exists())
    expect(tag?.text()).toBe('标签 / A&B')
    await tag.get('.el-tag__close').trigger('click')
    await settle(wrapper)
    expect(boundary.delete).toHaveBeenCalledExactlyOnceWith('/api/inbox/conversations/tag-conversation/tags/%E6%A0%87%E7%AD%BE%20%2F%20A%26B')
    expect(wrapper.findAll('.conv-item-bottom .el-tag').some(item => item.find('.el-tag__close').exists())).toBe(false)
  })

  it('对话框关闭标签同样保留原始值，不调用翻译函数作为参数', async () => {
    await selectConversation()
    const tagButton = wrapper.findAll('.thread-actions button').find(button => button.text() === '标签')
    expect(tagButton).toBeTruthy()
    await tagButton.trigger('click')
    await settleBody()
    const tag = document.querySelector('.tag-list .el-tag')
    expect(tag?.textContent).toBe('标签 / A&B')
    tag.querySelector('.el-tag__close').dispatchEvent(new MouseEvent('click', { bubbles: true }))
    await settleBody()
    expect(boundary.delete).toHaveBeenCalledExactlyOnceWith('/api/inbox/conversations/tag-conversation/tags/%E6%A0%87%E7%AD%BE%20%2F%20A%26B')
    expect(document.querySelector('.tag-list .el-tag')).toBeNull()
  })
})
