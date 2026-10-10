// 侧边栏「分区 / 分组」注册表
// ---------------------------------------------------------------------------
// 这里只登记**分组骨架**（顶层分区 section 与中间分组 group）的图标、顺序、可见角色。
// 菜单叶子（真正可点的页面）不在这里维护：它们全部来自路由 meta ——
//   meta.menuKey      i18n 键名（用于 t('menu.<key>')，缺失自动回退 meta.title）
//   meta.menuSection  所属顶层分区（对应 MENU_SECTIONS[].key）
//   meta.menuParents  可选的中间分组键数组，从外到内（最多三层）
//   meta.menuOrder    同级排序
//   meta.menuIcon     菜单图标（不写则用 meta.icon）
//   meta.menuRoles    菜单可见角色（不写则按 meta.requiresAdmin 推导）
//   meta.menuHidden   true 表示该页不进菜单（详情/编辑等）
// 新页面接入 = 在 src/router/modules/*.js 的 meta 里写上述字段，本文件不用改。
// 未登记的 menuParents 键会按 key 自动兜底渲染（icon 用 Menu、标题走 menu.<key>、
// 再回退到 key），所以「漏登记」只会影响外观，不会让页面从菜单里消失或显示裸 key。
// 本文件由 scripts/gen-menu-structure.cjs 从改造前的手写菜单树一次性生成（保持原有 IA 与顺序）。
// ---------------------------------------------------------------------------

export const MENU_SECTIONS = [
  { key: "workspace", icon: "Monitor", order: 1, fallback: "工作台", roles: ["admin","manager","sales","viewer","staff"] },
  { key: "customer", icon: "UserFilled", order: 2, fallback: "客户中心", roles: ["admin","manager","sales","viewer","staff"] },
  { key: "aiAgent", icon: "Cpu", order: 3, fallback: "智能体", roles: ["admin","manager","customer_service","staff"] },
  { key: "reach", icon: "Promotion", order: 4, fallback: "触达运营", roles: ["admin","manager","sales","viewer","staff"] },
  { key: "knowledge", icon: "Collection", order: 5, fallback: "知识中心", roles: ["admin","manager","sales","viewer","staff"] },
  { key: "analytics", icon: "DataAnalysis", order: 6, fallback: "数据分析", roles: ["admin","manager","sales","viewer","staff"] },
  { key: "system", icon: "Setting", order: 7, fallback: "系统设置", roles: ["admin","manager","sales","viewer","staff"] },
]

export const MENU_GROUPS = {
  agentManage: { icon: "Cpu", section: "aiAgent", order: 2, fallback: "智能体管理" },
  agentPassive: { icon: "ChatDotRound", section: "aiAgent", order: 3, fallback: "被动应答" },
  agentActive: { icon: "Promotion", section: "aiAgent", order: 4, fallback: "主动触达" },
  agentSupport: { icon: "Connection", section: "aiAgent", order: 5, fallback: "能力支撑" },
  agentWorkbench: { icon: "Headset", section: "aiAgent", order: 6, fallback: "客服工作台", roles: ["admin","manager","sales","customer_service"] },
  agentChannel: { icon: "Connection", section: "aiAgent", order: 7, fallback: "客服渠道", roles: ["admin","manager"] },
  agentSalesIntel: { icon: "Trophy", section: "aiAgent", order: 8, fallback: "销售智能", roles: ["admin","manager","sales"] },
  browserAutomation: { icon: "Monitor", section: "analytics", order: 12, fallback: "浏览器自动化" },
  geoTools: { icon: "MagicStick", section: "analytics", order: 13, fallback: "GEO 智能优化" },
  geoConfigGroup: { icon: "Setting", section: "analytics", parents: ["geoTools"], order: 1, fallback: "配置" },
  geoProd: { icon: "EditPen", section: "analytics", parents: ["geoTools"], order: 2, fallback: "生产" },
  geoPublish: { icon: "Connection", section: "analytics", parents: ["geoTools"], order: 3, fallback: "发布" },
  geoMonitor: { icon: "Monitor", section: "analytics", parents: ["geoTools"], order: 4, fallback: "监控" },
  customerIdentity: { icon: "Connection", section: "customer", order: 9, fallback: "客户身份", roles: ["admin","manager"] },
  knowledgeWorkspace: { icon: "Files", section: "knowledge", order: 1, fallback: "知识库工作台", roles: ["admin","manager"] },
  ragProductConfig: { icon: "ChatLineRound", section: "knowledge", order: 2, fallback: "RAG 知识库", roles: ["admin","manager"] },
  i18nManage: { icon: "Coin", section: "knowledge", order: 4, fallback: "多语言管理", roles: ["admin","manager"] },
  emailReach: { icon: "Message", section: "reach", order: 1, fallback: "邮件触达" },
  smsReach: { icon: "ChatDotSquare", section: "reach", order: 2, fallback: "短信触达" },
  douyin: { icon: "VideoPlay", section: "reach", order: 3, fallback: "抖音" },
  kuaishou: { icon: "Bell", section: "reach", order: 4, fallback: "快手" },
  xiaohongshu: { icon: "Picture", section: "reach", order: 5, fallback: "小红书" },
  xianyu: { icon: "ShoppingBag", section: "reach", order: 6, fallback: "闲鱼" },
  tiktok: { icon: "VideoPlay", section: "reach", order: 7, fallback: "TikTok" },
  whatsapp: { icon: "ChatDotRound", section: "reach", order: 8, fallback: "WhatsApp" },
  telegram: { icon: "ChatDotRound", section: "reach", order: 9, fallback: "电报社群" },
  qq: { icon: "ChatDotRound", section: "reach", order: 10, fallback: "QQ 机器人" },
  feishu: { icon: "ChatDotRound", section: "reach", order: 11, fallback: "飞书", roles: ["admin","manager"] },
  shortLink: { icon: "Link", section: "reach", order: 13, fallback: "短链管理" },
  siteConfig: { icon: "Tools", section: "system", order: 2, fallback: "站点配置", roles: ["admin"] },
  teamGroup: { icon: "UserFilled", section: "system", order: 3, fallback: "团队", roles: ["admin"] },
  permissionGroup: { icon: "Lock", section: "system", order: 4, fallback: "权限设置", roles: ["admin"] },
  assetBundle: { icon: "Box", section: "system", order: 5, fallback: "资产包", roles: ["admin","manager","sales","viewer","staff"] },
}

export const DEFAULT_GROUP_ICON = 'Menu'
export const DEFAULT_LEAF_ICON = 'Document'
