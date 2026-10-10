// AI 成单草稿的前端路由。两条路径刻意与工作台聚合待办生成的深链对齐
// （service/sales_workbench.go: URL = "/dashboard/drafts/" + d.ID）——
// 这条深链在此前是死链：后端待办指向它，前端没有这个路由。
export default [{
  path: '/dashboard/drafts',
  name: 'OrderDraftList',
  component: () => import('@/views/orderDraft/List.vue'),
  meta: { menuKey: 'orderDraft', menuSection: 'workspace', menuOrder: 7, menuRoles: ["admin","manager","sales"], menuIcon: 'Document', title: '订单草稿', group: 'workspace', icon: 'Document', requiresAuth: true }
}, {
  path: '/dashboard/drafts/:id',
  name: 'OrderDraftDetail',
  component: () => import('@/views/orderDraft/Detail.vue'),
  meta: { menuHidden: true, title: '草稿详情', group: 'workspace', icon: 'Document', requiresAuth: true }
}];
