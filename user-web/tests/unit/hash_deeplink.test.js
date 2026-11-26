import { describe, expect, it, vi } from 'vitest'
import { channelHashRedirect, normalizeChannelHashDeepLink } from '@/utils/hashDeepLink'

describe('T08 渠道裸路径 hash 归一化', () => {
  it('裸 /channels/identify-codes 归一化到 #/channels/identify-codes', () => {
    expect(channelHashRedirect({ pathname: '/channels/identify-codes', search: '', hash: '' }))
      .toBe('/#/channels/identify-codes')
  })

  it('保留 query 字符串', () => {
    expect(channelHashRedirect({ pathname: '/channels/accounts', search: '?from=menu', hash: '' }))
      .toBe('/#/channels/accounts?from=menu')
  })

  it('已有正确渠道 hash 时不再重定向（防循环）', () => {
    expect(channelHashRedirect({ pathname: '/', search: '', hash: '#/channels/accounts' })).toBeNull()
    expect(channelHashRedirect({ pathname: '/channels/accounts', search: '', hash: '#/channels/accounts' })).toBeNull()
  })

  it('router 补过默认 hash 的裸路径仍归一化到目标渠道页', () => {
    expect(channelHashRedirect({ pathname: '/channels/identify-codes', search: '', hash: '#/messageHub/list' }))
      .toBe('/#/channels/identify-codes')
  })

  it('非渠道路径与首页不处理，避免无界重定向', () => {
    expect(channelHashRedirect({ pathname: '/system/settings', search: '', hash: '' })).toBeNull()
    expect(channelHashRedirect({ pathname: '/', search: '', hash: '' })).toBeNull()
    expect(channelHashRedirect({ pathname: '/api/channels', search: '', hash: '' })).toBeNull()
  })

  it('归一化函数调用 location.replace 并返回是否跳转', () => {
    const location = { pathname: '/channels/manage', search: '', hash: '', replace: vi.fn() }
    expect(normalizeChannelHashDeepLink(location)).toBe(true)
    expect(location.replace).toHaveBeenCalledWith('/#/channels/manage')

    const noop = { pathname: '/', search: '', hash: '', replace: vi.fn() }
    expect(normalizeChannelHashDeepLink(noop)).toBe(false)
    expect(noop.replace).not.toHaveBeenCalled()
  })
})
