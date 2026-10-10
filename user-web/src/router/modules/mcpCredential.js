export default [
  {
    path: '/mcp/credentials',
    name: 'McpCredentials',
    component: () => import('@/views/mcp/Credentials.vue'),
    meta: { title: 'MCP凭证', group: 'aiAgent', icon: 'Key', requiresAuth: true }
  }
]
