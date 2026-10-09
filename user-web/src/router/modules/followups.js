// 跟进提醒落点（A11）。两条路径与后端深链对齐：
// - /followups/today —— quick action"今日跟进"与菜单入口；
// - /followups/:id —— sales_workbench 聚合待办生成的深链（URL = "/followups/" + r.ID），
//   复用同一页（页面按 param 在三区里定位提醒）；静态 'today' 段在 vue-router
//   里优先于 :id 通配，不会被参数路由吃掉。
export default [{
  path: '/followups/today',
  name: 'FollowUpToday',
  component: () => import('@/views/followups/Today.vue'),
  meta: { title: '今日跟进', group: 'workspace', icon: 'Calendar', requiresAuth: true }
}, {
  path: '/followups/:id',
  name: 'FollowUpDetail',
  component: () => import('@/views/followups/Today.vue'),
  meta: { title: '跟进详情', group: 'workspace', icon: 'Calendar', requiresAuth: true }
}];
