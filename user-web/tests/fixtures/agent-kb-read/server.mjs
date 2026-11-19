// 测试入口：node tests/fixtures/agent-kb-read/server.mjs。无生产路由、认证或外部请求。
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
let agent = { id: 1, agent_code: 'controlled-kb-read', name: '绑定保护验收', persona: '仅本地合成验收', agent_type: 'sales', agent_mode: 'passive', status: 1 }
let kbIds = ['11'], readAllowed = false
const events = []
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'controlled-agent-kb-read',
    configureServer(vite) {
      vite.middlewares.use('/fixture', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1'), path = url.pathname
        const reply = (status, value) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify(value))
        }
        if (req.method === 'POST' && path === '/allow-read') { readAllowed = true; return reply(200, { readAllowed }) }
        if (req.method === 'GET' && path === '/evidence') return reply(200, { agent, kbIds, events })
        if (req.method === 'GET' && path === '/agents/1') return reply(200, agent)
        if (req.method === 'GET' && path === '/knowledge-bases') return reply(200, { list: url.searchParams.get('type') === 'faq' ? [{ id: 11, name: '原知识库' }, { id: 22, name: '新知识库' }] : [] })
        if (req.method === 'GET' && path === '/bindings/1') {
          events.push({ method: 'GET', resource: 'bindings', status: readAllowed ? 200 : 503 })
          return readAllowed ? reply(200, kbIds.map(kb_id => ({ kb_id }))) : reply(503, { error: '受控绑定读取故障' })
        }
        if (req.method === 'PUT' && ['/agents/1', '/bindings/1'].includes(path)) {
          let raw = ''
          for await (const chunk of req) raw += chunk
          let payload
          try { payload = JSON.parse(raw) } catch { return reply(400, { error: '无效请求' }) }
          events.push({ method: 'PUT', resource: path === '/agents/1' ? 'agent' : 'bindings', payload })
          if (path === '/agents/1') { agent = { ...agent, ...payload }; return reply(200, agent) }
          if (!Array.isArray(payload.kb_ids) || payload.kb_ids.some(id => !['11', '22'].includes(id))) return reply(400, { error: '无效知识库' })
          kbIds = payload.kb_ids.slice()
          return reply(200, { count: kbIds.length })
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
  server: { host: '127.0.0.1', port: 18213, strictPort: true },
})
await server.listen()
process.stdout.write('Controlled KB read fixture: http://127.0.0.1:18213 (synthetic data only)\n')
