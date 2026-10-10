export default [
  {
    path: '/reachPipeline/list',
    name: 'ReachPipelineList',
    component: () => import('@/views/reachPipeline/List.vue'),
    meta: { menuKey: 'reachPipeline', menuSection: 'aiAgent', menuParents: ["agentActive"], menuOrder: 1, menuRoles: ["admin","manager"], menuIcon: 'Promotion', title: '触达Pipeline', group: 'reach', icon: 'Promotion' }
  },
  {
    path: '/reachPipeline/editor/:id?',
    name: 'ReachPipelineEditor',
    component: () => import('@/views/reachPipeline/Editor.vue'),
    meta: { menuHidden: true, title: 'Pipeline 编辑', group: 'reach', icon: 'Edit', requiresAuth: true, hideInMenu: true }
  }
]
