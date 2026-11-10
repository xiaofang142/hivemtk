/**
 * 平台钱包离线镜像展示测试（PLT-01 刀3）
 *
 * MyModules.vue 的钱包卡片是 GET /api/llm/wallet 的消费方。离线档（平台不可达）
 * 后端返回本地镜像余额 + updated_at（synced_at）且 platform_enabled=false。
 * 钉两件事：
 *   1. 离线档显示「同步于」时间标注 —— 旧数字不许被当成实时余额；
 *   2. 在线档（platform_enabled=true）不显示该标注（实时数据无需自证新鲜度）。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({
  myModules: vi.fn(),
  syncModules: vi.fn(),
  moduleSyncLogs: vi.fn(),
  listMarketModules: vi.fn(),
  llmWallet: vi.fn(),
  llmPlans: vi.fn(),
  createLLMOrder: vi.fn()
}))

vi.mock('@/api/moduleMarket', () => ({
  default: {},
  myModules: api.myModules,
  syncModules: api.syncModules,
  moduleSyncLogs: api.moduleSyncLogs,
  listMarketModules: api.listMarketModules,
  llmWallet: api.llmWallet,
  llmPlans: api.llmPlans,
  createLLMOrder: api.createLLMOrder,
  priceLabel: (p) => (p ? `¥${(p / 100).toFixed(2)}` : ''),
  fmtPrice: (p) => (p ? `¥${(p / 100).toFixed(2)}` : ''),
  fmtTokens: (n) => Number(n || 0).toLocaleString(),
  MODULE_CATEGORY_LABEL: {},
  MODULE_SOURCE_LABEL: {},
  MODULE_STATUS_LABEL: {}
}))

vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ userInfo: { id: 1 }, isLoggedIn: true })
}))

vi.mock('@/components/CashierDialog.vue', () => ({
  default: { name: 'CashierDialog', template: '<div />' }
}))

import MyModules from '@/views/moduleMarket/MyModules.vue'
import { settle } from './settle-helper'

const mounted = []

beforeEach(() => {
  api.myModules.mockReset()
  api.myModules.mockResolvedValue({ modules: [] })
  api.syncModules.mockReset()
  api.moduleSyncLogs.mockReset()
  api.moduleSyncLogs.mockResolvedValue({ list: [], total: 0 })
  api.listMarketModules.mockReset()
  api.listMarketModules.mockResolvedValue({ modules: [] })
  api.llmPlans.mockReset()
  api.llmPlans.mockResolvedValue([])
  api.createLLMOrder.mockReset()
  api.llmWallet.mockReset()
})

afterEach(() => {
  while (mounted.length) {
    try {
      mounted.pop().unmount()
    } catch {
      /* 重复卸载不抛即可 */
    }
  }
  document.body.innerHTML = ''
})

function mountPage() {
  const w = mount(MyModules, { global: { plugins: [ElementPlus] } })
  mounted.push(w)
  return w
}

describe('我的模块 · 平台钱包离线镜像展示', () => {
  it('离线档（platform_enabled=false + updated_at）显示同步时间标注与镜像余额', async () => {
    api.llmWallet.mockResolvedValue({
      exists: true,
      platform_enabled: false,
      input_tokens_remaining: 700,
      output_tokens_remaining: 300,
      low: false,
      expired: false,
      updated_at: '2026-11-11T08:30:00+08:00'
    })

    const w = mountPage()
    await settle(w)

    const text = w.text()
    expect(text).toContain('离线展示')
    expect(text).toContain('同步于')
    // 镜像余额原样呈现（不冒充实时、不编判定）
    expect(text).toContain('700')
    expect(text).toContain('300')
  })

  it('在线档（platform_enabled=true）不显示离线标注', async () => {
    api.listMarketModules.mockResolvedValue({ modules: [], platform_enabled: true })
    api.llmWallet.mockResolvedValue({
      exists: true,
      platform_enabled: true,
      input_tokens_remaining: 500,
      output_tokens_remaining: 500,
      low: false,
      expired: false
    })

    const w = mountPage()
    await settle(w)

    expect(w.text()).not.toContain('离线展示')
  })
})
