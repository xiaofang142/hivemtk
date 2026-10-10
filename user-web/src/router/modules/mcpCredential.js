export default [
  {
    path: '/mcp/credentials',
    name: 'McpCredentials',
    component: () => import('@/views/mcp/Credentials.vue'),
    meta: { menuKey: 'mcpCredentials', menuSection: 'aiAgent', menuOrder: 1, menuRoles: ["admin", "team_admin"], menuIcon: 'Key', title: 'MCP凭证', group: 'aiAgent', icon: 'Key', requiresAuth: true }
  },
  {
    path: '/mcp/audits',
    name: 'McpAudits',
    component: () => import('@/views/mcp/Audits.vue'),
    meta: { menuKey: 'mcpAudits', menuSection: 'aiAgent', menuOrder: 2, menuRoles: ["admin", "team_admin"], menuIcon: 'Document', title: 'MCP调用审计', group: 'aiAgent', icon: 'Document', requiresAuth: true }
  },
  {
    path: '/mcp/skills',
    name: 'McpSkills',
    component: () => import('@/views/mcp/Skills.vue'),
    meta: { menuKey: 'mcpSkills', menuSection: 'aiAgent', menuOrder: 3, menuRoles: ["admin", "team_admin"], menuIcon: 'MagicStick', title: 'Skill注册表', group: 'aiAgent', icon: 'MagicStick', requiresAuth: true }
  }
]