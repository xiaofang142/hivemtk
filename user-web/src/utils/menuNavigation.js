// 展示规则不是授权规则。只收起已在对应列表提供入口的动作。
const listActions = new Map([
  ['/aiAgent/create', { owner: '/aiAgent/list', key: 'aiAgentCreate' }],
  ['/browser-automation/tasks/create', { owner: '/browser-automation/tasks', key: 'baCreate' }],
  ['/asset-bundle/merchant-new', { owner: '/asset-bundle/list', key: 'assetBundleMerchant' }],
  ['/knowledge/batch-import', { owner: '/knowledge/management', key: 'knowledgeBatchImport' }]
])

const channelPaths = ['/channels/manage', '/channels/accounts', '/channels/identify-codes']

// 外部知识源的两段能力（2026-11-27 菜单审计）：
//   /knowledge/connectors —— Notion/飞书/钉钉/CRM 凭据与连通测试（/api/knowledge/connectors）
//   /kb/connectors        —— IMA/HTTP 导入连接器，拉取落库（/api/kb-connectors）
// 两者互补而非重复：前者能测连通但不落库，后者能落库但没有连通测试。
// 后端两套都保留，前端收敛为一个入口 + 两个 tab（views/KnowledgeWorkspace/ExternalSources.vue），
// 否则侧栏并列两个语义相同的「连接器」项，用户无法判断该进哪个。
const connectorPaths = ['/knowledge/connectors', '/kb/connectors']

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
  ['/tiktok-card-stats/:id', ['/tiktok/list']],
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
  const grantedConnectors = connectorPaths.filter(path => visiblePaths.has(path))
  function prune(nodes) {
    return nodes.flatMap(node => {
      if (node.children?.length) {
        const children = prune(node.children)
        return children.length ? [{ ...node, children }] : []
      }
      if (channelPaths.includes(node.path)) return []
      if (connectorPaths.includes(node.path)) return []
      const action = listActions.get(node.path)
      return node.path && !(action && visiblePaths.has(action.owner)) ? [node] : []
    })
  }

  const navigation = prune(menus)
  if (grantedChannels.length) {
    const channelsNode = { key: 'channels', icon: 'Connection', path: grantedChannels[0], channelPaths: grantedChannels }
    const systemNode = navigation.find(node => node.key === 'system')
    if (systemNode) {
      systemNode.children = [...(systemNode.children || []), channelsNode]
    } else {
      navigation.push(channelsNode)
    }
  }
  // 外部知识源收敛：插回原位置。
  //
  // 两个原始节点都在「知识中心 → 知识库工作台」这个子分组里
  // （knowledgeConnectors 在 knowledgeOpenAPI 之后，kbImportConnectors 在其后），
  // 所以合并节点必须插回 knowledgeWorkspace 的 children，而不是 knowledge 顶层——
  // 挂错层级会让它跑到「知识库工作台」外面，与其余工作台子页分家。
  if (grantedConnectors.length) {
    const connectorNode = {
      key: 'kbSources',
      icon: 'Connection',
      path: grantedConnectors[0],
      connectorPaths: grantedConnectors
    }
    const knowledgeNode = navigation.find(node => node.key === 'knowledge')
    const workspaceNode = knowledgeNode?.children?.find(child => child.key === 'knowledgeWorkspace')
    if (workspaceNode) {
      const children = workspaceNode.children || []
      // 锚点：knowledgeOpenAPI（原 knowledgeConnectors 的前一个兄弟）。
      const anchorIndex = children.findIndex(child => child.key === 'knowledgeOpenAPI')
      if (anchorIndex >= 0) children.splice(anchorIndex + 1, 0, connectorNode)
      else children.push(connectorNode)
      workspaceNode.children = children
    } else if (knowledgeNode) {
      knowledgeNode.children = [...(knowledgeNode.children || []), connectorNode]
    } else {
      navigation.push(connectorNode)
    }
  }
  return navigation
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

// 渠道三页被 projectNavigation 收敛成一个菜单叶子（path = 首个已授权页），
// 因此 /channels/accounts、/channels/identify-codes 只能靠 channelPaths 反查。
// 必须返回**完整归属链**（含 system 父节点），只返回叶子会让 Layout 的
// currentTopMenu 变成无 children 的渠道叶子 → 左侧菜单整块消失。
function findChannelsTrail(nodes, path, parents = []) {
  for (const node of nodes) {
    const trail = [...parents, node]
    if (node.key === 'channels' && node.channelPaths?.includes(path)) return trail
    if (node.children?.length) {
      const found = findChannelsTrail(node.children, path, trail)
      if (found) return found
    }
  }
  return null
}

// 外部知识源同渠道口径：/knowledge/connectors 与 /kb/connectors 被收敛成一个
// 叶子（key=kbSources，path=首个已授权页），另一个只能靠 connectorPaths 反查。
// 同样必须返回完整归属链，否则 Layout 拿不到 knowledge 父节点 ⇒ 侧栏整块消失。
function findConnectorsTrail(nodes, path, parents = []) {
  for (const node of nodes) {
    const trail = [...parents, node]
    if (node.key === 'kbSources' && node.connectorPaths?.includes(path)) return trail
    if (node.children?.length) {
      const found = findConnectorsTrail(node.children, path, trail)
      if (found) return found
    }
  }
  return null
}

export function findNavigationTrail(menus, route) {
  const path = getNavigationPath(route)
  const trails = new Map(Array.from(menuTrails(menus), trail => [trail.at(-1).path, trail]))
  if (trails.has(path)) return trails.get(path)

  const channels = findChannelsTrail(menus, path)
  if (channels) return channels

  const connectors = findConnectorsTrail(menus, path)
  if (connectors) return connectors

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
