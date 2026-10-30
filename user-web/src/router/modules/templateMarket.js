// 模板/技能市场（F2 最小可用版 + S-22 闭环）路由模块。
//
// 路由懒装配机制（src/router/index.js 的 import.meta.glob('./modules/*.js')）
// 自动捡到这个文件；router/index.js 两处登记（specs/013 delivery.md「建议接线点」）：
//   1) moduleNames 数组追加 "templateMarket"；
//   2) pathToModule 增加 'template-market': 'templateMarket'。
// S-22 闭环的两个工作台挂同模块（首段同为 template-market，懒装配按首段命中）：
//   /template-market/my     我的发布/订单（登录档）
//   /template-market/review 发布审核台（admin 档，后端接口 AdminAuthMiddleware 收口）
export default [
  {
    path: '/template-market',
    name: 'TemplateMarket',
    component: () => import('@/views/templateMarket/Market.vue'),
    // 市场浏览是公开橱窗档：未登录也能逛（对齐 module-market 的 list 公开档）。
    meta: { title: '模板/技能市场', icon: 'Shop', requiresAuth: false, public: true }
  },
  {
    path: '/template-market/my',
    name: 'TemplateMarketMy',
    component: () => import('@/views/templateMarket/MyWorkspace.vue'),
    // 我的发布/订单是登录档：读的是提交人/买家自己的数据。
    meta: { title: '我的发布/订单', icon: 'Tickets', requiresAuth: true }
  },
  {
    path: '/template-market/review',
    name: 'TemplateMarketReview',
    component: () => import('@/views/templateMarket/ReviewConsole.vue'),
    // 审核台是 admin 档：审核是公开橱窗的唯一写通道（菜单侧栏也只给 admin）。
    meta: { title: '发布审核', icon: 'Operation', requiresAuth: true, roles: ['admin'] }
  }
]
