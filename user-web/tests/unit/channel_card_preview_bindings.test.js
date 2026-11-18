import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createI18n } from 'vue-i18n'
import DouyinCardPreview from '@/components/DouyinCardPreview.vue'
import KuaishouCardPreview from '@/components/KuaishouCardPreview.vue'
import XiaohongshuCardPreview from '@/components/XiaohongshuCardPreview.vue'
import zh from '@/i18n/locales/zh.json'
import { settle } from './settle-helper'

describe.each([
  ['抖音', DouyinCardPreview, ['view']],
  ['快手', KuaishouCardPreview, ['view', 'like', 'share']],
  ['小红书', XiaohongshuCardPreview, ['view']],
])('%s卡片预览绑定兼容', (_channel, component, events) => {
  it('保留文字、图片、属性更新及模式切换，不新增业务事件', async () => {
    const wrapper = mount(component, {
      props: { cardData: { title: '<b>合成标题</b>', description: '合成描述', image_url: '/fixture-card.png' } },
      global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh } })] },
    })
    try {
      await settle(wrapper)
      expect(wrapper.get('.card-title').text()).toBe('<b>合成标题</b>')
      expect(wrapper.find('.card-title b').exists()).toBe(false)
      expect(wrapper.get('.card-description').text()).toBe('合成描述')
      expect(wrapper.findComponent({ name: 'ElImage' }).props('src')).toBe('/fixture-card.png')
      expect(wrapper.vm.$options.emits).toEqual(events)
      expect(wrapper.get('.preview-container').classes()).not.toContain('preview-mode')

      await wrapper.get('[role="switch"]').trigger('click')
      await settle(wrapper)
      expect(wrapper.get('.preview-container').classes()).toContain('preview-mode')
      expect(wrapper.emitted('view')).toBeUndefined()
      expect(wrapper.emitted('like')).toBeUndefined()
      expect(wrapper.emitted('share')).toBeUndefined()

      await wrapper.setProps({ cardData: { title: '更新标题', description: '更新描述' } })
      await settle(wrapper)
      expect(wrapper.get('.card-title').text()).toBe('更新标题')
      expect(wrapper.get('.card-description').text()).toBe('更新描述')
      expect(wrapper.findComponent({ name: 'ElImage' }).exists()).toBe(false)
      await wrapper.get('[role="switch"]').trigger('click')
      await settle(wrapper)
      expect(wrapper.get('.preview-container').classes()).not.toContain('preview-mode')
    } finally {
      wrapper.unmount()
    }
  })
})
