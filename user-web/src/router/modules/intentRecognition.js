export default [
  {
    path: '/intentRecognition/list',
    name: 'IntentRecognitionList',
    component: () => import('@/views/intentRecognition/List.vue'),
    meta: { title: '意图识别', group: 'aiAgent', icon: 'Aim', requiresAuth: true }
  },
  {
    path: '/intentRecognition/tool-flow',
    name: 'IntentToolFlow',
    component: () => import('@/views/intentRecognition/ToolFlow.vue'),
    meta: { title: '意图工具流', group: 'aiAgent', icon: 'Operation', requiresAuth: true, roles: ['admin'] }
  }
]
