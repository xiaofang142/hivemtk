import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'

// 渲染用例：证明"平台集成关闭态不露上架入口"这条判据**穿过了模板**。
//
// 后端 403 已经锁住写接口，但那是"点了会说失败"；这里要的是"根本没有可点的东西"。
// 两者不等价：/api/system/info 取回的字段名写错、v-if 挂错元素、或者有人把标题
// 从 `{{ platformEnabled ? ... }}` 改回常量，后端全都照样绿，用户却会对着
// 一个必然 403 的"审核上架到官方蜂巢商城"按钮。
// 所以这里数的是**渲染出来的文本与按钮调用**。

const api = vi.hoisted(() => ({
  getBundleByAssetID: vi.fn(),
  publishBundle: vi.fn(),
  submitToPlatform: vi.fn(),
  getInfo: vi.fn()
}))

vi.mock('@/api/assetBundle', () => ({
  createBundle: vi.fn(),
  updateBundle: vi.fn(),
  getBundleByAssetID: api.getBundleByAssetID,
  weaveBundle: vi.fn(),
  publishBundle: api.publishBundle,
  submitToPlatform: api.submitToPlatform
}))
vi.mock('@/api/system', () => ({ SystemApi: { getInfo: api.getInfo } }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ userInfo: { id: '1', role: 'admin' } })
}))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { aid: 'ASSET-1' }, query: {} }),
  useRouter: () => ({ replace: vi.fn(), push: vi.fn() })
}))
vi.mock('element-plus', async (importOriginal) => {
  const mod = await importOriginal()
  return {
    ...mod,
    ElMessage: { success: vi.fn(), error: vi.fn(), warning: vi.fn() },
    ElMessageBox: { confirm: vi.fn().mockResolvedValue(true) }
  }
})

import Playground from '@/views/assetBundle/Playground.vue'

const mountPlayground = async () => {
  const wrapper = mount(Playground, {
    global: { plugins: [ElementPlus, createPinia()] }
  })
  await flushPromises()
  return wrapper
}

const localPublishBtn = (wrapper) =>
  wrapper.findAll('button').find((b) => b.text().includes('发布到本地资产库'))

describe('资产包 Playground 的上架入口：跟随 platform_enabled', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    api.getBundleByAssetID.mockResolvedValue({
      id: 7, asset_id: 'ASSET-1', title: '测试包', version: '1.0.0',
      scope: 'private', status: 'draft', messages: []
    })
    api.publishBundle.mockResolvedValue({})
  })

  it('关态：没有"生态上架/蜂巢商城/商业买断价"，只留本地发布', async () => {
    api.getInfo.mockResolvedValue({ platform_enabled: false })
    const wrapper = await mountPlayground()
    const text = wrapper.text()
    expect(text).not.toContain('生态上架配置')
    expect(text).not.toContain('官方蜂巢商城')
    expect(text).not.toContain('商业买断价')
    expect(text).toContain('本地发布')
    expect(localPublishBtn(wrapper)).toBeTruthy()
  })

  it('开态：上架表单与上架按钮都在（关掉才有的降级不许泄漏到开态）', async () => {
    api.getInfo.mockResolvedValue({ platform_enabled: true })
    const wrapper = await mountPlayground()
    const text = wrapper.text()
    expect(text).toContain('生态上架配置')
    expect(text).toContain('官方蜂巢商城')
    expect(text).toContain('商业买断价')
    expect(text).not.toContain('本地发布')
  })

  it('关态点发布：只走本地 publishBundle，一次 submitToPlatform 都不发', async () => {
    api.getInfo.mockResolvedValue({ platform_enabled: false })
    const wrapper = await mountPlayground()
    await localPublishBtn(wrapper).trigger('click')
    await flushPromises()
    expect(api.publishBundle).toHaveBeenCalledWith(7)
    expect(api.submitToPlatform).not.toHaveBeenCalled()
  })

  it('开态点发布：本地发布之后再提交平台审核', async () => {
    api.getInfo.mockResolvedValue({ platform_enabled: true })
    const wrapper = await mountPlayground()
    const btn = wrapper.findAll('button').find((b) => b.text().includes('官方蜂巢商城'))
    await btn.trigger('click')
    await flushPromises()
    expect(api.publishBundle).toHaveBeenCalledWith(7)
    expect(api.submitToPlatform).toHaveBeenCalledWith(7)
  })

  it('/api/system/info 取不到时按关态处理（宁可不露入口，也不露一个必然 403 的按钮）', async () => {
    api.getInfo.mockRejectedValue(new Error('network down'))
    const wrapper = await mountPlayground()
    expect(wrapper.text()).not.toContain('官方蜂巢商城')
    expect(api.submitToPlatform).not.toHaveBeenCalled()
  })
})
