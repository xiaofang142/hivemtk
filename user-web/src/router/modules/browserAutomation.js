export default [
  {
    path: '/browser-automation',
    name: 'BrowserAutomation',
    meta: { title: '浏览器自动化', icon: 'Monitor', group: 'automation', requiresAuth: true },
    children: [
      { path: '', redirect: '/browser-automation/tasks' },
      {
        path: 'tasks',
        name: 'BrowserAutomationList',
        component: () => import('@/views/browserAutomation/List.vue'),
        meta: { title: '任务列表', requiresAuth: true }
      },
      {
        path: 'tasks/create',
        name: 'BrowserAutomationCreate',
        component: () => import('@/views/browserAutomation/Editor.vue'),
        meta: { title: '新建任务', requiresAuth: true }
      },
      {
        path: 'tasks/:id/edit',
        name: 'BrowserAutomationEditor',
        component: () => import('@/views/browserAutomation/Editor.vue'),
        meta: { title: '编辑任务', requiresAuth: true }
      },
      {
        path: 'tasks/:id',
        name: 'BrowserAutomationDetail',
        component: () => import('@/views/browserAutomation/Detail.vue'),
        meta: { title: '任务详情', requiresAuth: true }
      },
      {
        path: 'sessions/:id',
        name: 'BrowserAutomationMonitor',
        component: () => import('@/views/browserAutomation/Monitor.vue'),
        meta: { title: '执行监控', requiresAuth: true }
      },
      {
        path: 'cron',
        name: 'BrowserAutomationCron',
        component: () => import('@/views/browserAutomation/Cron.vue'),
        meta: { title: '定时触发器', requiresAuth: true }
      },
      {
        path: 'status',
        name: 'BrowserAutomationStatus',
        component: () => import('@/views/browserAutomation/Status.vue'),
        meta: { title: 'Host 状态', requiresAuth: true }
      }
    ]
  }
]
