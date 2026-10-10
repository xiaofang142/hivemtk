export default [{
  path: '/customerJourney/dashboard',
  name: 'CustomerJourneyDashboard',
  component: () => import('@/views/customerJourney/Dashboard.vue'),
  meta: { menuKey: 'customerJourney', menuSection: 'analytics', menuOrder: 7, menuRoles: ["admin","manager","sales","viewer"], menuIcon: 'TrendCharts', title: '客户旅程大屏', group: 'analytics', icon: 'TrendCharts', requiresAuth: true }
}];
