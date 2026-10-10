export default {
  path: '/whatsapp-cloud',
  name: 'WhatsappCloud',
  component: () => import('@/views/whatsappCloud/WhatsappCloudAccount.vue'),
  meta: { menuKey: 'whatsappCloud', menuSection: 'aiAgent', menuParents: ["agentChannel"], menuOrder: 2, menuIcon: 'Connection',
    title: 'WhatsApp Cloud',
    icon: 'ChatDotRound',
    group: 'community'
  }
}
