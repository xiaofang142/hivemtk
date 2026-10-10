export default {
  path: '/dingtalk-app',
  name: 'DingtalkApp',
  component: () => import('@/views/dingtalkApp/DingtalkAppAccount.vue'),
  meta: { menuKey: 'dingtalkApp', menuSection: 'aiAgent', menuParents: ["agentChannel"], menuOrder: 3, menuIcon: 'Connection',
    title: '钉钉应用',
    icon: 'ChatDotRound',
    group: 'community'
  }
}
