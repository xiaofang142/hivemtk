import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import { useUserStore } from '@/stores/user'
import { messages } from '@/i18n/catalog'
import { createPageTours } from '@/config/onboarding/pageTours'

const api = vi.hoisted(() => ({ get: vi.fn(), start: vi.fn(), complete: vi.fn(), refresh: vi.fn(), revoke: vi.fn() }))
vi.mock('@/api/merchantConnection', () => ({ merchantConnectionApi: api }))
const guide = vi.hoisted(() => vi.fn())
vi.mock('@/composables/useOnboarding', () => ({ useOnboarding: () => ({ startPageTour: guide }) }))
import MerchantConnectionWizard from '@/views/integration/MerchantConnectionWizard.vue'
const legacy = vi.hoisted(() => ({ getIntegrations: vi.fn(async () => []), createIntegration: vi.fn(), updateIntegration: vi.fn(), deleteIntegration: vi.fn(), toggleIntegrationStatus: vi.fn(), testIntegration: vi.fn(), getIntegrationLogs: vi.fn() }))
vi.mock('@/api/integration.js', () => legacy)
import IntegrationList from '@/views/integration/List.vue'

const state = 's'.repeat(43)
const ready = (version = 7) => ({ provider: 'fixture', shop_id: 'shop-1', connection_state: 'ready', credential_ref: 12, credential_version: version, refresh_available: true, production_allowed: false, reason_code: 'action_preflight_required', expires_at: new Date(Date.now() + 3600000).toISOString() })
const unconfigured = () => ({ provider: 'fixture', connection_state: 'unconfigured', refresh_available: false, production_allowed: false })
const deferred = () => { let resolve, reject; const promise = new Promise((a, b) => { resolve = a; reject = b }); return { promise, resolve, reject } }
let wrapper, router, store, locale, open

async function render(lang = 'zh', component = MerchantConnectionWizard) {
  const pinia = createPinia()
  setActivePinia(pinia)
  store = useUserStore()
  store.userInfo = { id: 17, role: 'admin', tenant_id: 42 }
  store.token = 'fixture-user-session'
  locale = createI18n({ legacy: false, locale: lang, messages })
  router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/integration/list', component, meta: { requiresAuth: true, requiresAdmin: true } }, { path: '/away', component: { template: '<p>away</p>' } }] })
  await router.push('/integration/list')
  wrapper = mount(RouterView, { attachTo: document.body, global: { plugins: [pinia, router, locale, ElementPlus] } })
  await settle(wrapper)
}
async function target() {
  await wrapper.get('#merchant-provider').setValue('fixture')
  await wrapper.get('#merchant-shop').setValue('shop-1')
  await wrapper.get('[data-test="check"]').trigger('click')
  await settle(wrapper)
}
async function start() {
  await target()
  await wrapper.get('[data-test="start"]').trigger('click')
  await settle(wrapper)
  await wrapper.get('[data-test="open"]').trigger('click')
  await settle(wrapper)
}

beforeEach(() => {
  guide.mockClear()
  for (const fn of Object.values(legacy)) fn.mockClear()
  for (const fn of Object.values(api)) fn.mockReset()
  api.get.mockImplementation(async () => unconfigured())
  api.start.mockImplementation(async () => ({ authorization_url: `https://identity.example/authorize?state=${state}`, expires_at: new Date(Date.now() + 60000).toISOString() }))
  api.complete.mockImplementation(async () => ready())
  api.refresh.mockImplementation(async () => ready(8))
  api.revoke.mockResolvedValue({ provider: 'fixture', connection_state: 'revoked', reason_code: 'provider_revocation_unconfirmed', production_allowed: false })
  open = vi.spyOn(window, 'open').mockReturnValue(null)
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined; document.body.innerHTML = ''; vi.restoreAllMocks(); vi.useRealTimers() })

describe('tenant merchant connection wizard', () => {
  it('starts blocked with accessible labels and explains the EC01 boundary', async () => {
    await render()
    expect(api.get).not.toHaveBeenCalled()
    expect(wrapper.get('label[for="merchant-provider"]').text()).toBeTruthy()
    expect(wrapper.get('label[for="merchant-shop"]').text()).toBeTruthy()
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.text()).toContain('EC-01')
    expect(wrapper.text()).toContain('blocked')
    expect(wrapper.find('[data-test="guidance"]').exists()).toBe(true)
  })

  it('embeds the wizard in the existing integration page without using legacy credential mutations', async () => {
    api.get.mockResolvedValue({ ...unconfigured(), reason_code: 'pending_live_validation' })
    await render('zh', IntegrationList)
    expect(wrapper.find('.integration-page .merchant-connection-panel').exists()).toBe(true)
    expect(wrapper.find('.integration-page .header-card').exists()).toBe(true)
    expect(legacy.getIntegrations).toHaveBeenCalledTimes(1)
    await target()
    expect(api.get).toHaveBeenCalledTimes(1)
    expect(legacy.createIntegration).not.toHaveBeenCalled()
    expect(legacy.updateIntegration).not.toHaveBeenCalled()
    expect(legacy.toggleIntegrationStatus).not.toHaveBeenCalled()
    expect(legacy.testIntegration).not.toHaveBeenCalled()
    expect(wrapper.find('.merchant-connection-panel [name="api_key"]').exists()).toBe(false)
    expect(wrapper.find('.merchant-connection-panel [name="api_secret"]').exists()).toBe(false)
  })

  it('shows pending_live_validation without enabling start, refresh, revoke or capability', async () => {
    api.get.mockResolvedValue({ ...unconfigured(), reason_code: 'pending_live_validation' })
    await render(); await target()
    expect(wrapper.text()).toContain('pending_live_validation')
    for (const action of ['start', 'refresh', 'revoke']) expect(wrapper.get(`[data-test="${action}"]`).attributes('disabled')).toBeDefined()
    expect(open).not.toHaveBeenCalled()
  })

  it('opens HTTPS with noopener,noreferrer and completes via a masked POST form only', async () => {
    let submitted
    api.complete.mockImplementationOnce(async (payload, signal) => { submitted = { ...payload, signal }; return ready() })
    await render(); await start()
    expect(open).toHaveBeenCalledWith(`https://identity.example/authorize?state=${state}`, '_blank', 'noopener,noreferrer')
    expect(wrapper.html()).not.toContain(state)
    expect(wrapper.find('a[href]').exists()).toBe(false)
    const code = wrapper.get('#merchant-code')
    expect(code.attributes('type')).toBe('password')
    expect(code.attributes('autocomplete')).toBe('off')
    await code.setValue('fixture-code')
    await wrapper.get('#merchant-state').setValue(state)
    const local = vi.spyOn(Storage.prototype, 'setItem')
    const push = vi.spyOn(window.history, 'pushState')
    await wrapper.get('[data-test="complete"]').trigger('click'); await settle(wrapper)
    expect(api.complete).toHaveBeenCalledTimes(1)
    expect(submitted).toEqual({ provider: 'fixture', shop_id: 'shop-1', code: 'fixture-code', state, signal: expect.any(AbortSignal) })
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(wrapper.find('#merchant-state').exists()).toBe(false)
    expect(local).not.toHaveBeenCalled()
    expect(push).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('EC-01')
    expect(wrapper.text()).toContain('blocked')
  })

  it.each(['http://identity.example/a', 'javascript:alert(1)', 'https://user:secret@identity.example/a', '/api/unsafe?state=x', 'https://identity.example/a#code=secret', 'https://identity.example/a?state=x&state=y', 'https://identity.example/a?code=secret', 'https://identity.example/a?code_verifier=secret'])('rejects an unsafe authorization URL: %s', async (url) => {
    api.start.mockResolvedValue({ authorization_url: url, expires_at: new Date(Date.now() + 60000).toISOString() })
    await render(); await target(); await wrapper.get('[data-test="start"]').trigger('click'); await settle(wrapper)
    expect(open).not.toHaveBeenCalled()
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
    expect(wrapper.html()).not.toContain(url)
  })

  it('rejects URL-like code and mismatched state before completion', async () => {
    await render(); await start()
    await wrapper.get('#merchant-code').setValue('https://app.example/?code=secret')
    await wrapper.get('#merchant-state').setValue(state)
    expect(wrapper.get('[data-test="complete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#merchant-code').setValue('fixture-code')
    await wrapper.get('#merchant-state').setValue('wrong-state')
    expect(wrapper.get('[data-test="complete"]').attributes('disabled')).toBeDefined()
    expect(api.complete).not.toHaveBeenCalled()
  })

  it.each(['shop', 'provider', 'tenant', 'actor', 'role', 'pagehide', 'navigation'])('clears transient handoff and ignores late start after %s change', async (change) => {
    const pending = deferred(); api.start.mockReturnValue(pending.promise)
    await render(); await target(); await wrapper.get('[data-test="start"]').trigger('click'); await settle(wrapper)
    if (change === 'shop') await wrapper.get('#merchant-shop').setValue('shop-2')
    if (change === 'provider') await wrapper.get('#merchant-provider').setValue('fixture-two')
    if (change === 'tenant') store.userInfo.tenant_id = 43
    if (change === 'actor') store.userInfo.id = 18
    if (change === 'role') store.userInfo.role = 'staff'
    if (change === 'pagehide') window.dispatchEvent(new Event('pagehide'))
    if (change === 'navigation') await router.push('/away')
    await settle(wrapper)
    pending.resolve({ authorization_url: `https://identity.example/authorize?state=${state}`, expires_at: new Date(Date.now() + 60000).toISOString() })
    await settle(wrapper)
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(open).not.toHaveBeenCalled()
  })

  it('resets code/state on target change and on leaving and revisiting the page', async () => {
    await render(); await start(); await wrapper.get('#merchant-code').setValue('fixture-code'); await wrapper.get('#merchant-state').setValue(state)
    await wrapper.get('#merchant-shop').setValue('shop-2'); await settle(wrapper)
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    await router.push('/away'); await router.push('/integration/list'); await settle(wrapper)
    expect(wrapper.get('#merchant-shop').element.value).toBe('')
    expect(wrapper.find('#merchant-state').exists()).toBe(false)
  })

  it('expires a handoff, clears masked fields and disables completion', async () => {
    vi.useFakeTimers()
    await render(); await start(); await wrapper.get('#merchant-code').setValue('fixture-code')
    await vi.advanceTimersByTimeAsync(60001); await settle(wrapper)
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(api.complete).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })

  it('never renders or logs raw error bodies and clears a failed completion attempt', async () => {
    const raw = '<img src=x onerror=alert(1)> private-provider-body'
    api.complete.mockRejectedValue({ message: raw, response: { data: { message: raw, token: 'secret' } } })
    const log = vi.spyOn(console, 'error').mockImplementation(() => {})
    await render(); await start(); await wrapper.get('#merchant-code').setValue('fixture-code'); await wrapper.get('#merchant-state').setValue(state)
    await wrapper.get('[data-test="complete"]').trigger('click'); await settle(wrapper)
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(wrapper.html()).not.toContain('private-provider-body')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(log).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })

  it('refreshes once with the exact current version and reloads after stale/error without retry', async () => {
    api.get.mockResolvedValue(ready()); api.refresh.mockRejectedValue({ response: { status: 409 }, message: 'private-stale' })
    await render(); await target()
    api.get.mockResolvedValue(ready(9))
    await wrapper.get('[data-test="refresh"]').trigger('click'); await settle(wrapper)
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect(api.refresh).toHaveBeenCalledWith({ provider: 'fixture', shop_id: 'shop-1', credential_version: 7 }, expect.any(AbortSignal))
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(wrapper.text()).not.toContain('private-stale')
    await wrapper.get('[data-test="refresh"]').trigger('click'); await settle(wrapper)
    expect(api.refresh.mock.calls[1][0].credential_version).toBe(9)
  })

  it('reloads status with a new signal after refresh timeout and ignores the late mutation result', async () => {
    vi.useFakeTimers()
    const pending = deferred(); api.refresh.mockReturnValue(pending.promise)
    api.get.mockResolvedValue(ready())
    await render(); await target()
    await wrapper.get('[data-test="refresh"]').trigger('click'); await settle(wrapper)
    const signal = api.refresh.mock.calls[0][1]
    api.get.mockResolvedValue({ ...ready(), connection_state: 'refreshing', reason_code: 'refresh_in_progress', refresh_available: false })
    await vi.advanceTimersByTimeAsync(10001); await settle(wrapper)
    expect(signal.aborted).toBe(true)
    expect(api.get).toHaveBeenCalledTimes(2)
    expect(api.get.mock.calls[1][1]).not.toBe(signal)
    expect(api.get.mock.calls[1][1].aborted).toBe(false)
    expect(wrapper.get('[role="alert"]').text()).toBe(locale.global.t('merchantConnection.errors.requestExpired'))
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    pending.resolve(ready(8)); await settle(wrapper)
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(api.refresh).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledTimes(2)
  })

  it('requires explicit local-revoke confirmation, keeps the user session and explains remote uncertainty', async () => {
    api.get.mockResolvedValue(ready())
    await render(); await target()
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="revoke-confirm"] input').setValue(true)
    await wrapper.get('[data-test="revoke"]').trigger('click'); await settle(wrapper)
    expect(api.revoke).toHaveBeenCalledWith({ provider: 'fixture', shop_id: 'shop-1' }, expect.any(AbortSignal))
    expect(store.token).toBe('fixture-user-session')
    expect(wrapper.text()).toContain('provider_revocation_unconfirmed')
  })

  it('disables destructive actions while pending and after the status confirmation expires', async () => {
    vi.useFakeTimers(); api.get.mockResolvedValue(ready())
    await render(); await target(); await wrapper.get('[data-test="revoke-confirm"] input').setValue(true)
    await vi.advanceTimersByTimeAsync(30001); await settle(wrapper)
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
    await wrapper.get('[data-test="check"]').trigger('click'); await settle(wrapper)
    const pending = deferred(); api.refresh.mockReturnValue(pending.promise)
    await wrapper.get('[data-test="refresh"]').trigger('click'); await settle(wrapper)
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
    pending.resolve(ready(8)); await settle(wrapper)
  })

  it('ignores old status/refresh responses after the tenant or target changes', async () => {
    const first = deferred(); api.get.mockReturnValueOnce(first.promise)
    await render(); await target(); await wrapper.get('#merchant-shop').setValue('shop-2')
    first.resolve(ready()); await settle(wrapper)
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#merchant-shop').setValue('shop-1'); api.get.mockResolvedValue(ready()); await wrapper.get('[data-test="check"]').trigger('click'); await settle(wrapper)
    const pending = deferred(); api.refresh.mockReturnValue(pending.promise)
    await wrapper.get('[data-test="refresh"]').trigger('click'); store.userInfo.tenant_id = 99; await settle(wrapper)
    pending.resolve(ready(8)); await settle(wrapper)
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
  })

  it.each(['en', 'zh'])('renders translated flow and accessible errors in %s', async (lang) => {
    api.get.mockRejectedValue(new Error('raw-provider-error'))
    await render(lang); await target()
    expect(wrapper.text()).not.toContain('merchantConnection.')
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
    expect(wrapper.get('#merchant-provider').attributes('aria-describedby')).toBeTruthy()
    expect(wrapper.html()).not.toContain('raw-provider-error')
  })

  it('fails closed for a non-admin even if mounted outside the route guard', async () => {
    await render(); store.userInfo.role = 'staff'; await settle(wrapper)
    expect(wrapper.find('#merchant-provider').exists()).toBe(false)
    expect(api.get).not.toHaveBeenCalled()
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
  })

  it.each(['ar', 'de', 'en', 'es', 'fr', 'ja', 'pt', 'ru', 'zh'])('has complete translated messages and registered guide targets in %s', async (lang) => {
    // 原始词条检查防止中文 fallback 掩盖漏译；DOM 检查覆盖真实消息编译和引导锚点。
    const flatten = (value, prefix = '') => Object.entries(value).flatMap(([key, item]) => typeof item === 'object' ? flatten(item, `${prefix}${key}.`) : [[`${prefix}${key}`, item]])
    expect(flatten(messages[lang].merchantConnection).map(([key]) => key)).toEqual(flatten(messages.zh.merchantConnection).map(([key]) => key))
    for (const [, value] of flatten(messages[lang].merchantConnection)) expect(typeof value === 'string' || typeof value === 'function').toBe(true)
    await render(lang)
    expect(wrapper.text()).not.toContain('merchantConnection.')
    const tour = createPageTours(locale.global.t)['/integration/list']
    expect(tour.steps).toHaveLength(4)
    for (const step of tour.steps) {
      expect(wrapper.find(step.element).exists()).toBe(true)
      expect(step.title).toBeTruthy()
      expect(step.description).not.toContain('merchantConnection.')
    }
    await wrapper.get('[data-test="guide"]').trigger('click')
    expect(guide).toHaveBeenCalledWith('/integration/list', true)
    expect(wrapper.get('ol').attributes('aria-label')).toBeTruthy()
  })

  it.each([
    { provider: 'another-provider' }, { shop_id: 'another-shop' }, { shop_id: '' }, { credential_version: 0 },
    { credential_version: '7' }, { credential_ref: 0 }, { expires_at: 'not-a-date' },
    { connection_state: '<img src=x onerror=alert(1)>' }, { reason_code: 'private-raw-provider-reason' }
  ])('rejects malformed or mismatched safe status: %j', async (extra) => {
    api.get.mockResolvedValue({ ...ready(), ...extra })
    await render(); await target()
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('private-raw-provider-reason')
  })

  it('accepts registered Unicode identifiers within the backend byte limits', async () => {
    api.get.mockResolvedValue({ provider: '商店', connection_state: 'unconfigured', reason_code: 'pending_live_validation' })
    await render()
    await wrapper.get('#merchant-provider').setValue('商店')
    await wrapper.get('#merchant-shop').setValue('店舗:一')
    await wrapper.get('[data-test="check"]').trigger('click'); await settle(wrapper)
    expect(api.get).toHaveBeenCalledWith({ provider: '商店', shop_id: '店舗:一' }, expect.any(AbortSignal))
    await wrapper.get('#merchant-provider').setValue('店'.repeat(22))
    expect(wrapper.get('[data-test="check"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#merchant-provider').setValue('fixture')
    await wrapper.get('#merchant-shop').setValue('https://callback.example?code=private')
    expect(wrapper.get('[data-test="check"]').attributes('disabled')).toBeDefined()
  })

  it.each([
    { authorization_url: 'https://identity.example/authorize' },
    { authorization_url: 'https://identity.example/authorize?state=short' },
    { authorization_url: `https://identity.example/authorize?state=${'s'.repeat(32)}` },
    { expires_at: 'not-a-date' },
    { expires_at: new Date(0).toISOString() },
    { expires_at: new Date(Date.now() + 3600000).toISOString() }
  ])('rejects invalid handoff state or expiry: %j', async (extra) => {
    api.start.mockResolvedValue({ authorization_url: `https://identity.example/authorize?state=${state}`, expires_at: new Date(Date.now() + 60000).toISOString(), ...extra })
    await render(); await target(); await wrapper.get('[data-test="start"]').trigger('click'); await settle(wrapper)
    expect(wrapper.get('[role="alert"]').text()).toBeTruthy()
    expect(wrapper.find('[data-test="open"]').exists()).toBe(false)
    expect(open).not.toHaveBeenCalled()
  })

  it('enforces code byte and control-character limits before the POST', async () => {
    await render(); await start(); await wrapper.get('#merchant-state').setValue(state)
    await wrapper.get('#merchant-code').setValue('码'.repeat(1366))
    expect(wrapper.get('[data-test="complete"]').attributes('disabled')).toBeDefined()
    await wrapper.get('#merchant-code').setValue('code\u0085')
    expect(wrapper.get('[data-test="complete"]').attributes('disabled')).toBeDefined()
    expect(api.complete).not.toHaveBeenCalled()
  })

  it('clears handoff on query-only navigation and never reads callback material from the route', async () => {
    await render(); await start(); await wrapper.get('#merchant-code').setValue('fixture-code')
    await router.push({ path: '/integration/list', query: { tab: 'connections' } }); await settle(wrapper)
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(api.complete).not.toHaveBeenCalled()
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it('aborts timed-out completion and ignores its late success without resubmitting', async () => {
    vi.useFakeTimers()
    const pending = deferred(); api.complete.mockReturnValue(pending.promise)
    await render(); await start(); await wrapper.get('#merchant-code').setValue('fixture-code'); await wrapper.get('#merchant-state').setValue(state)
    await wrapper.get('[data-test="complete"]').trigger('click'); await settle(wrapper)
    const signal = api.complete.mock.calls[0][1]
    expect(wrapper.find('#merchant-code').exists()).toBe(false)
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
    await vi.advanceTimersByTimeAsync(10001); await settle(wrapper)
    expect(signal.aborted).toBe(true)
    expect(wrapper.get('[role="alert"]').text()).toBe(locale.global.t('merchantConnection.errors.requestExpired'))
    pending.resolve(ready()); await settle(wrapper)
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(api.complete).toHaveBeenCalledTimes(1)
    expect(api.get).toHaveBeenCalledTimes(1)
  })

  it.each(['complete', 'revoke'])('ignores late %s and preserves the new target after actor change', async (action) => {
    const pending = deferred(); api[action].mockReturnValue(pending.promise)
    await render()
    if (action === 'complete') {
      await start(); await wrapper.get('#merchant-code').setValue('fixture-code'); await wrapper.get('#merchant-state').setValue(state)
      await wrapper.get('[data-test="complete"]').trigger('click')
    } else {
      api.get.mockResolvedValue(ready()); await target()
      await wrapper.get('[data-test="revoke-confirm"] input').setValue(true)
      await wrapper.get('[data-test="revoke"]').trigger('click')
    }
    const signal = api[action].mock.calls[0][1]
    store.userInfo.id = 77; await settle(wrapper)
    pending.resolve(ready(88)); await settle(wrapper)
    expect(signal.aborted).toBe(true)
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
  })

  it('never displays unexpected fields or grants actions from production_allowed', async () => {
    api.get.mockResolvedValue({ ...ready(), production_allowed: true, api_key: 'private-key', access_token: 'private-token', scopes: ['<img src=x onerror=alert(1)>'] })
    await render(); await target()
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('private-key')
    expect(wrapper.text()).not.toContain('private-token')
    expect(wrapper.text()).toContain('blocked')
    expect(wrapper.text()).toContain('EC-01')
  })

  it.each(['connecting', 'refreshing'])('disables lifecycle writes while server state is %s', async (connection_state) => {
    api.get.mockResolvedValue({ ...ready(), connection_state })
    await render(); await target()
    expect(wrapper.get('[data-test="start"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="refresh"]').attributes('disabled')).toBeDefined()
    expect(wrapper.get('[data-test="revoke"]').attributes('disabled')).toBeDefined()
  })
})
