import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'
import { createPinia, setActivePinia } from 'pinia'
import { settleBody } from './settle-helper'
import { useUserStore } from '@/stores/user'
import { getRequestInstance } from '@/utils/request'
import { merchantConnectionApi } from '@/api/merchantConnection'

vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

let request, adapter, store
beforeEach(() => {
  setActivePinia(createPinia())
  store = useUserStore()
  store.userInfo = { id: 17, tenant_id: 42, role: 'admin' }
  store.token = 'fixture-session'
  request = getRequestInstance()
  adapter = request.defaults.adapter
  document.body.innerHTML = ''
})
afterEach(() => {
  request.defaults.adapter = adapter
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('merchant connection real request interceptor boundary', () => {
  it('serializes code and state in one completion POST and never issues a GET or refresh replay', async () => {
    const seen = []
    request.defaults.adapter = async (config) => {
      seen.push({ method: config.method, url: config.url, params: config.params, data: config.data, signal: config.signal })
      return { data: { code: 0, data: { connection_state: 'ready', production_allowed: false } }, headers: { 'content-type': 'application/json' }, status: 200, config }
    }
    const controller = new AbortController()
    await merchantConnectionApi.complete({ provider: 'fixture', shop_id: 'shop-1', code: 'private-code', state: 'private-state' }, controller.signal)
    expect(seen).toEqual([{ method: 'post', url: '/api/v1/integrations/merchant-connections/complete', params: undefined,
      data: JSON.stringify({ provider: 'fixture', shop_id: 'shop-1', code: 'private-code', state: 'private-state' }), signal: controller.signal }])
  })

  it.each([409, 502, 503])('HTTP %s cannot surface raw provider bodies as a toast or log', async (status) => {
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    request.defaults.adapter = async (config) => {
      const response = { status, statusText: 'fixture', config, headers: { 'content-type': 'application/json' }, data: { code: status, message: '<img src=x onerror=alert(1)> private-provider-body' } }
      throw new axios.AxiosError('fixture-provider-error', 'ERR_BAD_RESPONSE', config, {}, response)
    }
    await expect(merchantConnectionApi.refresh({ provider: 'fixture', shop_id: 'shop-1', credential_version: 7 })).rejects.toBeTruthy()
    await settleBody()
    expect(document.body.textContent).not.toContain('private-provider-body')
    expect(document.querySelector('.el-message')).toBeNull()
    expect(log).not.toHaveBeenCalled()
    expect(store.token).toBe('fixture-session')
  })

  it('local revoke leaves the current login session intact', async () => {
    request.defaults.adapter = async (config) => ({ status: 200, config, headers: { 'content-type': 'application/json' }, data: { code: 0, data: { connection_state: 'revoked', reason_code: 'provider_revocation_unconfirmed' } } })
    await expect(merchantConnectionApi.revoke({ provider: 'fixture', shop_id: 'shop-1' })).resolves.toMatchObject({ reason_code: 'provider_revocation_unconfirmed' })
    expect(store.token).toBe('fixture-session')
    expect(store.isLoggedIn).toBe(true)
  })
})
