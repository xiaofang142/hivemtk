import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePermissionStore } from '@/stores/permission'
import { useUserStore } from '@/stores/user'
import { getUserMenus } from '@/api/menu'
import { getMenuLanding } from '@/utils/menuNavigation'

vi.mock('@/api/menu', () => ({ getUserMenus: vi.fn() }))

const item = (key, path) => ({ key, path })
const group = (key, children, path) => ({ key, children, path })
const agents = item('aiAgentList', '/aiAgent/list')
const newAgent = item('aiAgentCreate', '/aiAgent/create')
const tasks = item('baTasks', '/browser-automation/tasks')
const newTask = item('baCreate', '/browser-automation/tasks/create')
const assetBundles = item('assetBundleList', '/asset-bundle/list')
const newMerchantBundle = item('assetBundleMerchant', '/asset-bundle/merchant-new')
const documents = item('knowledgeManagement', '/knowledge/management')
const batchImport = item('knowledgeBatchImport', '/knowledge/batch-import')
const agentMenus = (...items) => group('aiAgent', [group('agentManage', items)])

describe('用户端菜单展示与路由上下文', () => {
  let permission

  beforeEach(() => {
    setActivePinia(createPinia())
    permission = usePermissionStore()
    permission.menusLoaded = true
    getUserMenus.mockReset()
  })

  it('列表可见时隐藏四个动作入口，不修改授权、原始菜单树或独立画布', () => {
    const playground = item('assetBundlePlayground', '/asset-bundle/playground')
    permission.menus = [
      agentMenus(newAgent, agents),
      group('analytics', [group('browserAutomation', [newTask, tasks], tasks.path)]),
      group('knowledge', [batchImport, documents]),
      group('system', [group('assetBundle', [assetBundles, playground, newMerchantBundle])])
    ]
    const authorization = JSON.parse(JSON.stringify(permission.menus))

    expect(permission.navigationPaths).toEqual([
      agents.path, documents.path, assetBundles.path, playground.path, tasks.path
    ])
    expect(permission.menus).toEqual(authorization)
    for (const action of [newAgent, newTask, newMerchantBundle, batchImport]) {
      expect(permission.canAccessPath(action.path)).toBe(true)
      expect(permission.hasMenuKey(action.key)).toBe(true)
    }
  })

  it('角色只获动作权限时保留原入口，不把无权限列表映射成可访问', () => {
    permission.menus = [agentMenus(newAgent), group('knowledge', [batchImport])]

    expect(permission.navigationPaths).toEqual([newAgent.path, batchImport.path])
    expect(permission.getNavigationTrail({ path: newAgent.path }).map(node => node.key))
      .toEqual(['aiAgent', 'agentManage', 'aiAgentCreate'])
    expect(permission.canAccessPath(agents.path)).toBe(false)
    expect(permission.canAccessPath(documents.path)).toBe(false)
  })

  it('浏览器自动化转到系统设置；不会给只获数据分析权限的角色添加系统入口', () => {
    const analysis = item('dashboardScreen', '/dashboardScreen/list')
    const browser = group('browserAutomation', [tasks], tasks.path)
    permission.menus = [group('analytics', [analysis, browser])]

    expect(permission.navigationMenus.map(node => node.key)).toEqual(['analytics', 'system'])
    expect(permission.navigationMenus[0].children).toEqual([analysis])
    expect(permission.navigationMenus[1].children).toEqual([browser])
    permission.menus = [group('analytics', [analysis])]
    expect(permission.navigationMenus.map(node => node.key)).toEqual(['analytics'])
  })

  it('采用真实子菜单而非同址父组，并为创建、编辑和详情保留列表上下文', () => {
    const keywordMining = item('geoKeywordMining', '/geo-tools/keyword-mining')
    permission.menus = [
      agentMenus(agents, newAgent),
      group('analytics', [
        group('browserAutomation', [tasks, newTask], tasks.path),
        group('geoTools', [group('geoProd', [keywordMining])], keywordMining.path)
      ])
    ]
    const trail = (path, pattern = path) => permission.getNavigationTrail({ path, matched: [{ path: pattern }] })
      .map(node => node.key)

    expect(trail(keywordMining.path)).toEqual(['analytics', 'geoTools', 'geoProd', 'geoKeywordMining'])
    expect(trail(tasks.path)).toEqual(['system', 'browserAutomation', 'baTasks'])
    expect(trail(newTask.path)).toEqual(['system', 'browserAutomation', 'baTasks'])
    expect(trail('/browser-automation/tasks/12/edit')).toEqual(['system', 'browserAutomation', 'baTasks'])
    expect(trail('/aiAgent/edit/12', '/aiAgent/edit/:id')).toEqual(['aiAgent', 'agentManage', 'aiAgentList'])
    expect(trail('/aiAgent/listing')).toEqual([])
    expect(trail('/profile')).toEqual([])
  })

  it('同前缀入口精确匹配优先，后代匹配到最具体的菜单', () => {
    permission.menus = [group('reach', [group('emailReach', [
      item('emailList', '/email'), item('emailDrafts', '/email/drafts')
    ])])]
    expect(permission.getNavigationTrail({ path: '/email/drafts' }).at(-1)?.key).toBe('emailDrafts')
    expect(permission.getNavigationTrail({ path: '/email/drafts/3' }).at(-1)?.key).toBe('emailDrafts')
  })

  it('草稿编辑器只归属草稿，不在缺少草稿授权时冒充邮件列表', () => {
    const email = item('emailList', '/email')
    const drafts = item('emailDrafts', '/email/drafts')
    const editor = { path: '/email/drag-editor', matched: [{ path: '/email/drag-editor' }] }
    permission.menus = [group('reach', [email, drafts])]
    expect(permission.getNavigationTrail(editor).at(-1)?.key).toBe('emailDrafts')

    permission.menus = [group('reach', [email])]
    expect(permission.getNavigationTrail(editor)).toEqual([])
  })

  it('使用路由已匹配的静态记录定位旧链接，但不改变实际地址的授权结果', () => {
    const route = { path: '/Knowledge/batch-import/', matched: [{ path: batchImport.path }] }
    permission.menus = [group('knowledge', [documents, batchImport])]
    expect(permission.getNavigationTrail(route).at(-1)?.key).toBe('knowledgeManagement')
    expect(permission.canAccessPath(route.path)).toBe(false)
  })

  it('动作以已加载的真实授权叶子为准，不依赖守卫的放行或可见侧栏', () => {
    permission.menus = [agentMenus(agents), group('knowledge', [documents])]
    expect(permission.hasGrantedMenuPath(newAgent.path)).toBe(false)
    permission.allAccess = true
    expect(permission.canAccessPath(batchImport.path)).toBe(true)
    expect(permission.hasGrantedMenuPath(batchImport.path)).toBe(false)

    permission.menus = [agentMenus(agents, newAgent), group('knowledge', [documents, batchImport])]
    expect(permission.navigationPaths).not.toContain(batchImport.path)
    expect(permission.hasGrantedMenuPath(batchImport.path)).toBe(true)
    expect(permission.hasGrantedMenuPath(newAgent.path)).toBe(true)
    permission.menusLoaded = false
    expect(permission.hasGrantedMenuPath(batchImport.path)).toBe(false)
  })

  it('直接或保存后返回优先用授权列表，其次用统一落点，无菜单时回个人资料', () => {
    const keywords = item('geoKeywordMining', '/geo-tools/keyword-mining')
    const settings = item('geoSettings', '/geo-tools/settings')
    permission.menus = [group('analytics', [group('geoTools', [settings, keywords], keywords.path)]), agentMenus(agents)]
    expect(permission.getMenuReturnPath(agents.path)).toBe(agents.path)
    expect(permission.getMenuReturnPath(documents.path)).toBe(keywords.path)
    permission.menus = [agentMenus(newAgent)]
    expect(permission.getMenuReturnPath(agents.path, newAgent.path)).toBe('/profile')
    permission.menus = []
    expect(permission.getMenuReturnPath(agents.path)).toBe('/profile')
  })

  it('顶级分组落地时遵从首个可见子组的默认页，不打开排在前面的配置页', () => {
    const keywords = item('geoKeywordMining', '/geo-tools/keyword-mining')
    const settings = item('geoSettings', '/geo-tools/settings')
    const geo = group('geoTools', [settings, keywords], keywords.path)

    expect(getMenuLanding(group('analytics', [geo]))).toBe(keywords.path)
    expect(getMenuLanding(group('analytics', [group('geoTools', [settings], keywords.path)])))
      .toBe(settings.path)
  })

  it('并发菜单加载调用都等待同一个授权请求完成', async () => {
    let resolveMenus
    getUserMenus.mockReturnValue(new Promise(resolve => { resolveMenus = resolve }))
    permission.menusLoaded = false
    useUserStore().login({ id: 'user-1', role: 'member' }, 'token')

    const first = permission.ensureMenus()
    const second = permission.ensureMenus()
    const secondState = await Promise.race([
      second.then(() => 'settled'),
      new Promise(resolve => setTimeout(() => resolve('pending'), 0))
    ])

    expect(secondState).toBe('pending')
    expect(getUserMenus).toHaveBeenCalledTimes(1)

    resolveMenus({ tree: [{ key: 'workspace', path: '/workspace' }] })
    await Promise.all([first, second])
    expect(permission.menusLoaded).toBe(true)
  })

  it('重置菜单后允许新的授权请求接管旧请求', async () => {
    let resolveOld
    getUserMenus.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    permission.menusLoaded = false
    useUserStore().login({ id: 'user-1', role: 'member' }, 'token')

    const first = permission.ensureMenus()
    permission.resetMenus()
    getUserMenus.mockResolvedValueOnce({ tree: [{ key: 'new-session', path: '/new-session' }] })
    const second = permission.ensureMenus()
    expect(getUserMenus).toHaveBeenCalledTimes(2)
    await second
    expect(permission.menus).toEqual([{ key: 'new-session', path: '/new-session' }])

    resolveOld({ tree: [{ key: 'old-session', path: '/old-session' }] })
    await first
    expect(permission.menus).toEqual([{ key: 'new-session', path: '/new-session' }])
  })
})
