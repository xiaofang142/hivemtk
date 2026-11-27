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

  it('渠道 tab 页保留完整归属链，左侧系统菜单不消失', () => {
    const tree = [{ key: 'system', children: [
      { key: 'systemConfig', path: '/system/config' },
      { key: 'channels', children: [
        { key: 'channelAccount', path: '/channels/accounts' },
        { key: 'channelIdentifyCode', path: '/channels/identify-codes' },
        { key: 'channelManage', path: '/channels/manage' }
      ] }
    ] }]
    const navigation = projectNavigation(tree)
    for (const path of ['/channels/accounts', '/channels/identify-codes']) {
      const trail = findNavigationTrail(navigation, { path, matched: [{ path }] })
      expect(trail).toHaveLength(2)
      expect(trail[0].key).toBe('system')
      expect(trail[1].key).toBe('channels')
    }
    // 主 tab（合成叶子的 path）仍走原有精确匹配，归属链不变。
    const manage = findNavigationTrail(navigation, { path: '/channels/manage', matched: [{ path: '/channels/manage' }] })
    expect(manage.map(node => node.key)).toEqual(['system', 'channels'])
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
