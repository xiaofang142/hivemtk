import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { h } from 'vue'
import ElementPlus, { ElMenu } from 'element-plus'
import i18n from '@/i18n'
import SubMenuItem from '@/components/SubMenuItem.vue'
import { findNavigationTrail, projectNavigation } from '@/utils/menuNavigation'
import { settle } from './settle-helper'

describe('权威菜单与页面导航', () => {
  it('前端不再自行迁移后端菜单分组', () => {
    const tree = [{ key: 'analytics', children: [{ key: 'browserAutomation', children: [
      { key: 'baTasks', path: '/browser-automation/tasks' }
    ] }] }]
    expect(projectNavigation(tree)).toEqual(tree)
  })

  it('TikTok 卡片详情回到已授权卡片列表，缺少列表时没有伪造归属', () => {
    const route = { path: '/tiktok-card-stats/42', matched: [{ path: '/tiktok-card-stats/:id' }] }
    const tree = [{ key: 'reach', children: [{ key: 'tiktok', children: [
      { key: 'tiktokList', path: '/tiktok/list' }
    ] }] }]
    expect(findNavigationTrail(tree, route).at(-1)?.key).toBe('tiktokList')
    expect(findNavigationTrail([], route)).toEqual([])
  })

  it('单子分组直接呈现可访问叶子', async () => {
    const leaf = { key: 'qqAccount', path: '/qq/account' }
    const wrapper = mount({ render: () => h(ElMenu, {}, () => h(SubMenuItem, { menu: { key: 'qq', children: [leaf] } })) }, {
      global: { plugins: [ElementPlus, i18n] }
    })
    await settle(wrapper)
    expect(wrapper.find('.el-sub-menu').exists()).toBe(false)
    expect(wrapper.find('.el-menu-item').exists()).toBe(true)
    wrapper.unmount()
  })
})
