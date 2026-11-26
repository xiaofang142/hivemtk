// 展示规则不是授权规则。只收起已在对应列表提供入口的动作。
const listActions = new Map([
  ['/aiAgent/create', { owner: '/aiAgent/list', key: 'aiAgentCreate' }],
  ['/browser-automation/tasks/create', { owner: '/browser-automation/tasks', key: 'baCreate' }],
  ['/asset-bundle/merchant-new', { owner: '/asset-bundle/list', key: 'assetBundleMerchant' }],
  ['/knowledge/batch-import', { owner: '/knowledge/management', key: 'knowledgeBatchImport' }]
])

const channelPaths = ['/channels/manage', '/channels/accounts', '/channels/identify-codes']

// 同一工作流的非子路径；使用路由记录而非猜测 URL 字段或相邻菜单。
const routeOwners = new Map([
  ['/aiAgent', ['/aiAgent/list']],
  ['/aiAgent/edit/:id', ['/aiAgent/list']],
  // 2026-11-24：客户主数据 / 线索治理深链归各自列表菜单（面包屑回跳，授权不变）。
  ['/crm/dedupe', ['/crm/list']],
  ['/crm/profile360/:id?', ['/crm/list']],
  ['/crm/import-export', ['/crm/list']],
  ['/crm/contacts', ['/crm/list']],
  ['/crm/suppliers', ['/crm/list']],
  ['/crm/service-providers', ['/crm/list']],
  ['/crm/companies', ['/crm/list']],
  ['/leads/notify-routes', ['/leads/rules']],
  ['/leads/evidence', ['/leads/rules']],
  ['/leads/archive', ['/leads/rules']],
  ['/leads/scoped-list', ['/leads/rules']],
  ['/leads/mining-agents', ['/leads/rules']],
  ['/asset-bundle/merchant/:aid', ['/asset-bundle/list', '/asset-bundle/merchant-new']],
  ['/browser-automation/sessions/:id', ['/browser-automation/tasks']],
  ['/chatChannel/create', ['/chatChannel/list']],
  ['/chatChannel/edit/:id', ['/chatChannel/list']],
  ['/chatChannel/install-guide/:id?', ['/chatChannel/list']],
  ['/dashboardScreen/builder', ['/dashboardScreen/list']],
  ['/douyin-card-stats/:id', ['/douyinCard']],
  ['/email/drag-editor', ['/email/drafts']],
  ['/faq', ['/faq/list']],
  ['/faq/editor', ['/faq/list']],
  ['/faq/editor/:id', ['/faq/list']],
  ['/feishu', ['/feishu/account']],
  ['/followups/:id', ['/followups/today']],
  ['/kuaishou-card-stats/:id', ['/kuaishouCard']],
  ['/llmRouting/cost', ['/llmRouting/list']],
  ['/operationLog/enhanced', ['/operationLog/list']],
  ['/qq', ['/qq/account']],
  ['/reachPipeline/editor/:id?', ['/reachPipeline/list']],
  ['/sop-template', ['/sop-template/list']],
  ['/sop-template/editor', ['/sop-template/list']],
  ['/sop-template/editor/:id', ['/sop-template/list']],
  ['/telegram', ['/telegram/account']],
  ['/userSegment/builder', ['/userSegment/list']],
  ['/userSegment/rfm-matrix', ['/userSegment/list']],
  ['/wecomAccount/data', ['/wecomAccount/list']],
  ['/whatsapp', ['/whatsapp/account']],
  ['/workflow-orchestrator/editor/:workflow_id', ['/workflow-orchestrator/list']],
  ['/workflow-orchestrator/execution/:id', ['/workflow-orchestrator/list']],
  ['/xiaohongshu-card-stats/:id', ['/xiaohongshuCard']],
  ['/xianyu-card-stats/:id', ['/xianyuCard']]
])

function* menuTrails(nodes, parents = []) {
  for (const node of nodes) {
    const trail = [...parents, node]
    if (node.children?.length) yield* menuTrails(node.children, trail)
    else if (node.path) yield trail
  }
}

export function getNavigationPaths(menus) {
  return Array.from(menuTrails(menus), trail => trail.at(-1).path)
}

export function projectNavigation(menus) {
  const visiblePaths = new Set(getNavigationPaths(menus))
  const grantedChannels = channelPaths.filter(path => visiblePaths.has(path))
  function prune(nodes) {
    return nodes.flatMap(node => {
      if (node.children?.length) {
        const children = prune(node.children)
        return children.length ? [{ ...node, children }] : []
      }
      if (channelPaths.includes(node.path)) return []
      const action = listActions.get(node.path)
      return node.path && !(action && visiblePaths.has(action.owner)) ? [node] : []
    })
  }

  const navigation = prune(menus)
  if (grantedChannels.length) {
    navigation.push({ key: 'channels', icon: 'Connection', path: grantedChannels[0], channelPaths: grantedChannels })
  }
  const analytics = navigation.find(node => node.key === 'analytics')
  const browser = analytics?.children?.find(node => node.key === 'browserAutomation')
  if (!browser) return navigation

  analytics.children = analytics.children.filter(node => node !== browser)
  const system = navigation.find(node => node.key === 'system')
  if (system?.children) {
    system.children.push(browser)
  } else if (system?.path) {
    // 当前授权树里的 system 是分组；保留任何未来的可点击顶级入口。
    analytics.children.push(browser)
  } else {
    navigation.push({ key: 'system', icon: 'Setting', children: [browser] })
  }
  return navigation.filter(node => node !== analytics || node.children.length)
}

export function getMenuLanding(menu, excludedPath) {
  if (!menu) return ''
  if (!menu.children?.length) return menu.path !== excludedPath ? menu.path || '' : ''
  if (menu.path && menu.path !== excludedPath && getNavigationPaths(menu.children).includes(menu.path)) return menu.path
  for (const child of menu.children) {
    const path = getMenuLanding(child, excludedPath)
    if (path) return path
  }
  return ''
}

// 仅用于展示：使用 Router 已匹配的静态记录，不修改守卫接收的实际 URL。
export function getNavigationPath(route) {
  const matched = route.matched?.at(-1)?.path
  return ((matched && !matched.includes(':') ? matched : route.path).replace(/\/+$/, '') || '/')
}

export function findNavigationTrail(menus, route) {
  const path = getNavigationPath(route)
  const trails = new Map(Array.from(menuTrails(menus), trail => [trail.at(-1).path, trail]))
  if (trails.has(path)) return trails.get(path)

  const channels = menus.find(node => node.key === 'channels' && node.channelPaths?.includes(path))
  if (channels) return [channels]

  const action = listActions.get(path)
  const pattern = route.matched?.at(-1)?.path || path
  const owners = action ? [action.owner] : routeOwners.get(pattern)
  if (owners) return trails.get(owners.find(owner => trails.has(owner))) || []

  // 按路径段逐层回退，优先保留最具体的菜单（如 /email/drafts）。
  for (let end = path.lastIndexOf('/'); end > 0; end = path.lastIndexOf('/', end - 1)) {
    const parent = path.slice(0, end)
    if (trails.has(parent)) return trails.get(parent)
  }
  return []
}

export function getMenuActionKey(route) {
  return listActions.get(getNavigationPath(route))?.key
}
