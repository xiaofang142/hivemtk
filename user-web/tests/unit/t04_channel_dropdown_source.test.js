import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

vi.mock('@/api/channelCatalog.js', () => ({
  default: {
    catalog: vi.fn(),
    setTenantEnabled: vi.fn(),
    managedAccounts: vi.fn(),
    updateManagedAccount: vi.fn(),
    setAccountStatus: vi.fn()
  }
}))
vi.mock('@/api/channelAccount.js', () => ({
  default: { listChannels: vi.fn() }
}))

import channelCatalogApi from '@/api/channelCatalog.js'
import channelAccountApi from '@/api/channelAccount.js'
import { useChannelCatalogStore } from '@/stores/channelCatalog.js'

// T04（2026-11-26）：机器人归一到渠道——tg/qq/whatsapp/飞书/企微在业务页面里
// 一律走下拉关联，下拉源只有 /api/channels/catalog 一处。本用例钉死三件事：
//   ① 五个机器人渠道都在 channelOptions 里（不会出现「机器人没得选」）；
//   ② channelOptions 完全由接口返回决定，不夹带本地自由输入/私有码表；
//   ③ 账号下拉按渠道收敛，关联值就是业务表用的规范码/账号 ID。
describe('T04 机器人统一渠道下拉源', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    channelCatalogApi.catalog.mockReset()
    channelAccountApi.listChannels.mockReset()
  })

  it('五个机器人渠道全部出现在统一渠道下拉，选项数量与接口一致', async () => {
    const botChannels = [
      { id: 31, code: 'telegram', name: 'Telegram', label: 'Telegram', enabled: true },
      { id: 21, code: 'qq', name: 'QQ', label: 'QQ', enabled: true },
      { id: 30, code: 'whatsapp', name: 'WhatsApp', label: 'WhatsApp', enabled: true },
      { id: 32, code: 'feishu', name: '飞书', label: '飞书', enabled: true },
      { id: 26, code: 'wecom', name: '企业微信', label: '企业微信', enabled: true }
    ]
    channelCatalogApi.catalog.mockResolvedValue({ channels: botChannels, accounts: [] })
    channelAccountApi.listChannels.mockResolvedValue({ list: botChannels })

    const store = useChannelCatalogStore()
    await store.ensure()

    const values = store.channelOptions.map(o => o.value)
    for (const code of ['telegram', 'qq', 'whatsapp', 'feishu', 'wecom']) {
      expect(values, `${code} 必须可从统一渠道下拉选择`).toContain(code)
    }
    expect(store.channelOptions).toHaveLength(botChannels.length)
    expect(channelCatalogApi.catalog).toHaveBeenCalledTimes(1)
  })

  it('账号下拉按渠道收敛，关联值是账号数字 ID', async () => {
    channelCatalogApi.catalog.mockResolvedValue({
      channels: [{ id: 31, code: 'telegram', name: 'Telegram', label: 'Telegram' }],
      accounts: [
        { id: 7, channel_id: 31, channel_code: 'telegram', name: 'TG 客服号' },
        { id: 8, channel_id: 21, channel_code: 'qq', name: 'QQ 号' }
      ]
    })
    channelAccountApi.listChannels.mockResolvedValue([])
    const store = useChannelCatalogStore()
    await store.ensure()

    const tg = store.accountOptions('telegram')
    expect(tg).toHaveLength(1)
    expect(tg[0]).toMatchObject({ value: 7, channel_code: 'telegram' })
    expect(store.accountOptions(31)[0].value).toBe(7)
    expect(store.accountLabel(8)).toBe('QQ 号')
  })
})
