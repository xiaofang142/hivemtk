export default [
  {
    path: '/mcp/credentials',
    name: 'McpCredentials',
    component: () => import('@/views/mcp/Credentials.vue'),
    meta: { menuKey: 'mcpCredentials', menuSection: 'aiAgent', menuOrder: 1, menuRoles: ["admin"], menuIcon: 'Key', title: 'MCP凭证', group: 'aiAgent', icon: 'Key', requiresAuth: true }
  }
]
