import { beforeEach, describe, expect, it, vi } from 'vitest'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/utils/request', () => ({ http }))
import { merchantConnectionApi } from '@/api/merchantConnection'

describe('merchant connection API privacy boundary', () => {
  beforeEach(() => vi.clearAllMocks())

  it('uses the safe status projection and sends only target identifiers in GET', () => {
    merchantConnectionApi.get({ provider: 'fixture', shop_id: 'shop-1', state: 'private-state', code: 'private-code' })
    expect(http.get).toHaveBeenCalledWith('/api/v1/integrations/merchant-connections', { provider: 'fixture', shop_id: 'shop-1' }, expect.objectContaining({ _silent: true, _skipRefresh: true, timeout: 10000 }))
  })

  it('sends OAuth material only in the completion POST body, never query or legacy credentials', () => {
    merchantConnectionApi.start({ provider: 'fixture', shop_id: 'shop-1', api_key: 'legacy' })
    merchantConnectionApi.complete({ provider: 'fixture', shop_id: 'shop-1', state: 'private-state', code: 'private-code', verifier: 'never-accepted' })
    merchantConnectionApi.refresh({ provider: 'fixture', shop_id: 'shop-1', credential_version: 7, token: 'never-accepted' })
    merchantConnectionApi.revoke({ provider: 'fixture', shop_id: 'shop-1', credential_version: 7 })
    expect(http.post.mock.calls.map(([url, data]) => [url, data])).toEqual([
      ['/api/v1/integrations/merchant-connections/start', { provider: 'fixture', shop_id: 'shop-1' }],
      ['/api/v1/integrations/merchant-connections/complete', { provider: 'fixture', shop_id: 'shop-1', code: 'private-code', state: 'private-state' }],
      ['/api/v1/integrations/merchant-connections/refresh', { provider: 'fixture', shop_id: 'shop-1', credential_version: 7 }],
      ['/api/v1/integrations/merchant-connections/revoke', { provider: 'fixture', shop_id: 'shop-1' }]
    ])
    for (const [url, , config] of http.post.mock.calls) {
      expect(url).not.toContain('?')
      expect(config).toEqual(expect.objectContaining({ _silent: true, _skipRefresh: true, timeout: 10000 }))
      expect(config.params).toBeUndefined()
    }
  })
})
