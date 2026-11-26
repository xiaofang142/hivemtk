import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { createPinia } from 'pinia'
import i18n from '@/i18n'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  rotateForAccount: vi.fn(),
  catalog: vi.fn(),
  managedAccounts: vi.fn()
}))

vi.mock('@/api/channelIdentifyCode.js', () => ({
  default: {
    list: api.list,
    rotateForAccount: api.rotateForAccount
  },
  channelIdentifyCodeApi: {
    list: api.list,
    rotateForAccount: api.rotateForAccount
  }
}))

vi.mock('@/api/channelCatalog.js', () => ({
  default: {
    catalog: api.catalog,
    managedAccounts: api.managedAccounts,
    setTenantEnabled: vi.fn(),
    updateManagedAccount: vi.fn(),
    setAccountStatus: vi.fn()
  }
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    ElMessage: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn()
    },
    ElMessageBox: { confirm: vi.fn() }
  }
})

import IdentifyCodeManage from '@/views/channels/identify-code-manage.vue'

const ROWS = [
  { id: 10, channel_id: 20, account_id: 10, account_name: '微信账号', code: 'cid_wx', version: 1 },
  { id: 99, channel_id: 20, account_id: 0, account_name: '存量渠道码（兼容）', code: 'cid_legacy', version: 2 }
]
const CHANNELS = [{ id: 20, name: '微信' }]

let wrapper

// 条件式等待：settle() 用「连续两轮 HTML 相同」早停，在高负载全量并行时会早于
// mock 接口 resolve 就返回（表现为 No Data / 调用次数漂移）。这里显式等到目标
// 条件成立，再用 settle 收尾，用例不依赖机器快慢。
async function waitUntil(predicate, label) {
  for (let i = 0; i < 100; i++) {
    if (predicate()) return
    await flushPromises()
    await new Promise((resolve) => setTimeout(resolve, 0))
  }
  throw new Error(`等待条件超时：${label}`)
}

function resetButtons() {
  // 按钮文案走 i18n（zh: 重置账号识别码 / en: Reset account access code），
  // 用 ElementPlus 的 type=warning 类名定位，避免依赖当前语言。
  return wrapper.findAll('button.el-button--warning')
}

async function mountPage() {
  wrapper = mount(IdentifyCodeManage, {
    global: { plugins: [createPinia(), ElementPlus, i18n] }
  })
  await waitUntil(() => resetButtons().length === 2, '列表渲染出两个重置按钮')
  await settle(wrapper)
  return wrapper
}

beforeEach(() => {
  // 文案断言用中文，显式锁定 zh 防止 localStorage 里残留 en。
  i18n.global.locale.value = 'zh'
  vi.clearAllMocks()
  api.list.mockReset().mockResolvedValue({ list: ROWS.map((r) => ({ ...r })) })
  api.rotateForAccount.mockReset().mockResolvedValue({ code: 'cid_account_new' })
  api.catalog.mockReset().mockResolvedValue({
    channels: CHANNELS.map((c) => ({ id: c.id, code: String(c.id), name: c.name, label: c.name, enabled: true })),
    accounts: []
  })
  api.managedAccounts.mockReset().mockResolvedValue({ list: [] })
  ElMessageBox.confirm.mockReset().mockResolvedValue('confirm')
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = null
})

describe('账号级识别码管理：列表/重置/错误恢复/历史兼容', () => {
  it('渲染账号归属列，且存量渠道码行的重置按钮被禁用', async () => {
    await mountPage()
    const text = wrapper.text()
    expect(text).toContain('账号识别码')
    expect(text).toContain('微信账号')
    expect(text).toContain('存量渠道码（兼容）')
    expect(text).toContain('短暂在途窗口')
    expect(resetButtons()).toHaveLength(2)
    expect(resetButtons()[0].attributes('disabled')).toBeUndefined()
    expect(resetButtons()[1].attributes('disabled')).toBeDefined()
  })

  it('重置账号码走账号级 API，展示新码并重载列表', async () => {
    await mountPage()
    await resetButtons()[0].trigger('click')
    await waitUntil(() => api.list.mock.calls.length >= 2, '重置后重载列表')
    await settle(wrapper)

    expect(ElMessageBox.confirm).toHaveBeenCalledTimes(1)
    expect(api.rotateForAccount).toHaveBeenCalledWith(20, 10)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('cid_account_new'))
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('取消确认不调用写入接口', async () => {
    ElMessageBox.confirm.mockRejectedValueOnce('cancel')
    await mountPage()
    await resetButtons()[0].trigger('click')
    await settle(wrapper)

    expect(api.rotateForAccount).not.toHaveBeenCalled()
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('写入失败展示行内错误且无成功提示', async () => {
    api.rotateForAccount.mockRejectedValueOnce(new Error('rotate failed'))
    await mountPage()
    await resetButtons()[0].trigger('click')
    await waitUntil(() => wrapper.find('.rotation-error').exists(), '展示行内错误')
    await settle(wrapper)

    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('rotate failed'))
    expect(wrapper.find('.rotation-error').text()).toContain('rotate failed')
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it('失败后可重试并清除行内错误', async () => {
    api.rotateForAccount
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({ code: 'cid_recovered' })
    await mountPage()

    await resetButtons()[0].trigger('click')
    await waitUntil(() => wrapper.find('.rotation-error').exists(), '首次失败展示错误')

    await resetButtons()[0].trigger('click')
    await waitUntil(() => api.list.mock.calls.length >= 2, '重试成功后重载列表')
    await settle(wrapper)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('cid_recovered'))
    expect(wrapper.find('.rotation-error').exists()).toBe(false)
    expect(api.list).toHaveBeenCalledTimes(2)
  })
})
