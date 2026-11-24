export default [
  {
    path: '/aiInsightDraft/list',
    name: 'AiInsightDraftList',
    component: () => import('@/views/aiInsightDraft/List.vue'),
    meta: { title: 'AI洞察草稿', group: 'customer', icon: 'MagicStick', requiresAuth: true }
  }
]
