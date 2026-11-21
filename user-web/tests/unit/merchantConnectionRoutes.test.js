import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import integrationRoutes from '@/router/modules/integration'

const session = vi.hoisted(() => ({ token: 'fixture-session', isLoggedIn: true, role: 'admin', userInfo: { id: 17, tenant_id: 42 }, restoreSession: vi.fn(async () => {}) }))
vi.mock('@/stores/user', () => ({ useUserStore: () => session }))
vi.mock('@/stores/permission', () => ({ usePermissionStore: () => ({ ensureMenus: async () => {}, canAccessPath: () => true, menus: [] }) }))
vi.mock('@/utils/bootstrapState', () => ({ ensureBootstrap: async () => ({ initialized: true, tenantMode: true }) }))
vi.mock('@/utils/clientDevice', () => ({ reportClientDeviceOnce: vi.fn() }))
vi.mock('@/layout/Layout.vue', () => ({ default: { template: '<router-view />' } }))
vi.mock('@/views/integration/List.vue', () => ({ default: { template: '<p>integration</p>' } }))
vi.mock('@/views/Login.vue', () => ({ default: { template: '<p>login</p>' } }))
vi.mock('@/views/NotFound.vue', () => ({ default: { template: '<p>forbidden</p>' } }))
import router from '@/router'

beforeEach(async () => {
  session.token = 'fixture-session'
  session.isLoggedIn = true
  session.role = 'admin'
  await router.push('/login')
})
afterEach(() => vi.restoreAllMocks())

describe('integration connection route authorization', () => {
  it('retains the existing authenticated administrator route without adding a GET callback route', () => {
    expect(integrationRoutes).toHaveLength(1)
    expect(integrationRoutes[0]).toMatchObject({ path: '/integration/list', name: 'IntegrationList', meta: { requiresAuth: true, requiresAdmin: true } })
  })

  it('allows a signed-in administrator through the real lazy route guard', async () => {
    await router.push('/integration/list')
    expect(router.currentRoute.value.name).toBe('IntegrationList')
  })

  it.each(['agent', 'staff', 'super_admin', ''])('rejects role %s even with menu access', async (role) => {
    session.role = role
    await router.push('/integration/list')
    expect(router.currentRoute.value.name).toBe('NotFound')
    expect(router.currentRoute.value.query.status).toBe('403')
  })

  it('redirects an anonymous visitor to login', async () => {
    session.token = ''
    session.isLoggedIn = false
    await router.push('/integration/list')
    expect(router.currentRoute.value.path).toBe('/login')
  })
})
