export default [
  {
    path: '/dialogueMemory/list',
    name: 'DialogueMemoryList',
    component: () => import('@/views/dialogueMemory/List.vue'),
    meta: { menuKey: 'dialogueMemory', menuSection: 'aiAgent', menuParents: ["agentPassive"], menuOrder: 3, menuIcon: 'ChatDotRound', title: '对话记忆', group: 'aiAgent', icon: 'ChatDotRound', requiresAuth: true }
  }
]
