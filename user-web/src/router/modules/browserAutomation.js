export default [
  {
    path: '/browser-automation',
    name: 'BrowserAutomation',
    // redirect 挂在父路由上，与全仓其余模块一致（tiktok.js:5、bridgeToken.js:4、index.js:47 等）。
    // 原先这里写的是子路由 { path: '', redirect: ... }：vue-router 遇到「有 name 的父路由 + 无 name
    // 且空 path 的子路由」必报 The route named "BrowserAutomation" has a child without a name and
    // an empty path（实测每次进入该模块刷 3 条）；全仓也只有这一个模块用这种写法。
    redirect: '/browser-automation/tasks',
    meta: { title: '浏览器自动化', icon: 'Monitor', group: 'automation', requiresAuth: true },
    children: [
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
