export default [
  {
    path: '/intentRecognition/list',
    name: 'IntentRecognitionList',
    component: () => import('@/views/intentRecognition/List.vue'),
    meta: { menuKey: 'intentRecognition', menuSection: 'aiAgent', menuParents: ["agentPassive"], menuOrder: 2, menuIcon: 'Aim', title: '意图识别', group: 'aiAgent', icon: 'Aim', requiresAuth: true }
  }
]
