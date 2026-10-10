export default [
  {
    path: '/aiAgent',
    name: 'AIAgent',
    component: () => import('@/views/aiAgent/List.vue'),
    meta: { menuHidden: true, title: '智能体', group: 'aiAgent', icon: 'Cpu' }
  },
  {
    path: '/aiAgent/list',
    name: 'AIAgentList',
    component: () => import('@/views/aiAgent/List.vue'),
    meta: { menuKey: 'aiAgentList', menuSection: 'aiAgent', menuParents: ["agentManage"], menuOrder: 1, menuIcon: 'List', title: '智能体列表', group: 'aiAgent', icon: 'List' }
  },
  {
    path: '/aiAgent/create',
    name: 'AIAgentCreate',
    component: () => import('@/views/aiAgent/Edit.vue'),
    meta: { menuKey: 'aiAgentCreate', menuSection: 'aiAgent', menuParents: ["agentManage"], menuOrder: 2, menuIcon: 'Plus', title: '创建智能体', group: 'aiAgent', icon: 'Plus' }
  },
  {
    path: '/aiAgent/edit/:id',
    name: 'AIAgentEdit',
    component: () => import('@/views/aiAgent/Edit.vue'),
    meta: { menuHidden: true, title: '编辑智能体', group: 'aiAgent', icon: 'Edit' }
  },
  {
    path: '/aiAgent/tools',
    name: 'AIToolManagement',
    component: () => import('@/views/aiAgent/ToolManagement/ToolList.vue'),
    meta: { menuKey: 'aiToolManagement', menuSection: 'aiAgent', menuParents: ["agentSupport"], menuOrder: 8, menuIcon: 'Tools', title: 'AI 工具管理', group: 'aiAgent', icon: 'Tools' }
  }
]
