import { beforeEach, describe, expect, it, vi } from 'vitest'

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))
vi.mock('@/utils/request', () => ({ http }))
import * as api from '@/api/commerce'

describe('commerce API contract', () => {
  beforeEach(() => vi.clearAllMocks())
  it('scopes reads using explicit profile and shop and encodes operation identifiers', () => {
    api.listCommerceProfiles()
    expect(http.get).toHaveBeenCalledWith('/api/v1/commerce/profiles')
    api.listCommerceOperations({ profile: 'douyin_shop', shop_id: 'shop-a' })
    expect(http.get).toHaveBeenCalledWith('/api/v1/commerce/operations', { profile: 'douyin_shop', shop_id: 'shop-a' })
    api.getCommerceOperation('op/foreign?')
    expect(http.get).toHaveBeenCalledWith('/api/v1/commerce/operations/op%2Fforeign%3F')
  })
  it('sends a version and preflight digest without inventing completion', () => {
    const body = { expected_version: 4, preflight_hash: 'digest' }
    api.submitCommerceOperation('op-a', body)
    expect(http.post).toHaveBeenCalledWith('/api/v1/commerce/operations/op-a/submit', body)
    api.observeCommerceOperation('op-a', body)
    expect(http.post).toHaveBeenCalledWith('/api/v1/commerce/operations/op-a/observe', body)
  })
  it('keeps attestation separate from independent review', () => {
    const body = { expected_version: 8, reference: 'controlled-evidence', outcome: 'processing' }
    api.appendCommerceEvidence('op-a', body)
    expect(http.post).toHaveBeenCalledWith('/api/v1/commerce/operations/op-a/evidence', body)
    api.reviewCommerceEvidence('op-a', body)
    expect(http.post).toHaveBeenCalledWith('/api/v1/commerce/operations/op-a/review', body)
  })
})
