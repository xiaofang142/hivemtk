export default [
  {
    path: '/reachPipeline/list',
    name: 'ReachPipelineList',
    component: () => import('@/views/reachPipeline/List.vue'),
    meta: { title: '触达Pipeline', group: 'reach', icon: 'Promotion' }
  },
  {
    path: '/reachPipeline/editor/:id?',
    name: 'ReachPipelineEditor',
    component: () => import('@/views/reachPipeline/Editor.vue'),
    meta: { title: 'Pipeline 编辑', group: 'reach', icon: 'Edit', requiresAuth: true, hideInMenu: true }
  },
  {
    // D02 延迟出站：静默时段 / 合规窗口内暂缓的主动触达
    path: '/reachPipeline/deferred-outbound',
    name: 'ReachDeferredOutbound',
    component: () => import('@/views/reachPipeline/DeferredOutbound.vue'),
    meta: { title: '延迟出站管理', group: 'reach', icon: 'Timer', requiresAuth: true }
  }
]
