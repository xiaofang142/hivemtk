// 独立测试入口，不属于生产路由：node tests/fixtures/agent-mode/server.mjs。
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
let agent, failNext = false
const events = []
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'controlled-agent-mode',
    configureServer(vite) {
      vite.middlewares.use('/fixture', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1'), path = url.pathname
        const reply = (status, value) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify(value))
        }
        if (req.method === 'POST' && path === '/fail-next') { failNext = true; return reply(200, { armed: true }) }
        if (req.method === 'GET' && path === '/evidence') return reply(200, { agent, events })
        if (req.method === 'GET' && path === '/agents/by-code') return reply(200, agent?.agent_code === url.searchParams.get('agent_code') ? agent : null)
        if (req.method === 'GET' && path === '/agents/1') return agent ? reply(200, agent) : reply(404, { error: '记录不存在' })
        if ((req.method === 'POST' && path === '/agents') || (req.method === 'PUT' && path === '/agents/1')) {
          let raw = ''
          for await (const chunk of req) raw += chunk
          let payload
          try { payload = JSON.parse(raw) } catch { return reply(400, { error: '无效请求' }) }
          events.push({ method: req.method, payload })
          if (failNext) { failNext = false; return reply(503, { error: '受控保存拒绝' }) }
          if (!['active', 'passive'].includes(payload.agent_mode)) return reply(400, { error: '无效模式' })
          agent = { ...payload, id: 1 }
          return reply(200, agent)
        }
        reply(404, { error: 'fixture route not found' })
      })
    },
  }],
  resolve: { alias: [
    { find: /^@\/api\/(aiAgent\.js|ragProductConfig|sopAgent\.js|scriptTemplate\.js|faq|sopTemplate|knowledgeBase|agentKBBinding|assetBundle|assetMarket)$/, replacement: `${root}api.js` },
    { find: /^@\/i18n$/, replacement: `${root}api.js` },
    { find: '@', replacement: source },
  ] },
  server: { host: '127.0.0.1', port: 18212, strictPort: true },
})
await server.listen()
process.stdout.write('Controlled Agent Edit fixture: http://127.0.0.1:18212 (synthetic data only)\n')
