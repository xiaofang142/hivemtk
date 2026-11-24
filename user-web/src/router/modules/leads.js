export default [
  {
    path: '/leads/rules',
    name: 'LeadRules',
    component: () => import('@/views/leads/Rules.vue'),
    meta: { title: '线索三粒度规则', group: 'lead', icon: 'Setting', requiresAuth: true }
  },
  {
    path: '/leads/notify-routes',
    name: 'LeadNotifyRoutes',
    component: () => import('@/views/leads/NotifyRoutes.vue'),
    meta: { title: '线索通知路由', group: 'lead', icon: 'Bell', requiresAuth: true }
  },
  {
    path: '/leads/evidence',
    name: 'LeadEvidence',
    component: () => import('@/views/leads/Evidence.vue'),
    meta: { title: '判定证据回放', group: 'lead', icon: 'Document', requiresAuth: true }
  },
  {
    path: '/leads/archive',
    name: 'LeadArchive',
    component: () => import('@/views/leads/Archive.vue'),
    meta: { title: '线索归档', group: 'lead', icon: 'FolderChecked', requiresAuth: true }
  },
  {
    path: '/leads/scoped-list',
    name: 'LeadScopedList',
    component: () => import('@/views/leads/ScopedList.vue'),
    meta: { title: '权限内客户与消息', group: 'lead', icon: 'List', requiresAuth: true }
  },
  {
    path: '/leads/mining-agents',
    name: 'LeadMiningAgents',
    component: () => import('@/views/leads/MiningAgents.vue'),
    meta: { title: '线索发现智能体', group: 'lead', icon: 'AiPin', requiresAuth: true }
  }
]
