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
  }
]
