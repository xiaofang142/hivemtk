// 独立测试入口：node tests/fixtures/tool-pagination/server.mjs；不属于生产路由或构建。
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
let tools = [], failNext = false
const events = []
const reset = (count) => {
  tools = Array.from({ length: count }, (_, i) => ({
    id: i + 1, tool_name: `tool-${i + 1}`, display_name: `受控工具 ${i + 1}`,
    description: '本地合成工具，不调用第三方', category: i % 2 ? 'customer' : 'business',
    is_enabled: true, accounts: [],
  }))
  failNext = false
  events.length = 0
}
reset(21)
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'controlled-tool-pagination',
    configureServer(vite) {
      vite.middlewares.use('/fixture', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1')
        const reply = (status, value) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify(value))
        }
        if (req.method === 'POST' && url.pathname.startsWith('/control/')) {
          const action = url.pathname.slice('/control/'.length)
          if (action === '21' || action === '101') reset(Number(action))
          else if (action === 'remove-last') tools.pop()
          else if (action === 'fail-next') failNext = true
          else return reply(404, { error: 'unknown control' })
          return reply(200, { action, count: tools.length, failNext })
        }
        if (req.method === 'GET' && url.pathname === '/evidence') return reply(200, { tools, events })
        if (req.method === 'GET' && url.pathname === '/tools') {
          const params = Object.fromEntries(url.searchParams)
          events.push({ operation: 'list', params })
          if (failNext) { failNext = false; return reply(503, { error: 'controlled read failure' }) }
          const page = Number(params.page || 1), size = Number(params.page_size || 20)
          const filtered = tools.filter(tool => (!params.category || tool.category === params.category) &&
            (!('enabled' in params) || tool.is_enabled === (params.enabled === 'true')))
          return reply(200, { list: filtered.slice((page - 1) * size, page * size), total: filtered.length })
        }
        const match = url.pathname.match(/^\/tools\/(tool-\d+)\/status$/)
        if (req.method === 'PUT' && match) {
          let body = ''
          for await (const chunk of req) body += chunk
          let payload
          try { payload = JSON.parse(body) } catch { return reply(400, { error: 'invalid body' }) }
          const tool = tools.find(item => item.tool_name === match[1])
          if (!tool || typeof payload.is_enabled !== 'boolean') return reply(400, { error: 'invalid tool' })
          tool.is_enabled = payload.is_enabled
          events.push({ operation: 'status', name: tool.tool_name, enabled: tool.is_enabled })
          return reply(200, { saved: true })
        }
        reply(404, { error: 'fixture route not found' })
      })
    },
  }],
  resolve: { alias: [
    { find: '@/api/aiTool.js', replacement: `${root}api.js` },
    { find: '@', replacement: source },
  ] },
  server: { host: '127.0.0.1', port: 18211, strictPort: true },
})
await server.listen()
process.stdout.write('Controlled ToolList fixture: http://127.0.0.1:18211 (synthetic data only)\n')
