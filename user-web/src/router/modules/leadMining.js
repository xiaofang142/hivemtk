export default [{
  path: '/lead-mining/index',
  name: 'LeadMining',
  component: () => import('@/views/leadMining/Index.vue'),
  meta: { menuKey: 'leadMining', menuSection: 'customer', menuOrder: 3, menuIcon: 'Search', title: '线索发掘', group: '线索发掘', icon: 'Magic' }
}];
