// node tests/fixtures/bulk-lead-handoff/server.mjs; never connects to a real sender.
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
const leads = [{ id: 'nav04-a', name: '合成联系人甲', account: '+15550001001' },
  { id: 'nav04-b', name: '合成联系人乙', account: '+15550001002' }]
const events = []
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'controlled-bulk-lead-handoff',
    configureServer(vite) {
      vite.middlewares.use('/fixture', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1')
        const reply = (status, body) => {
          res.statusCode = status
          res.setHeader('Content-Type', 'application/json; charset=utf-8')
          res.end(JSON.stringify(body))
        }
        if (req.method === 'GET' && url.pathname === '/evidence') return reply(200, { leads, events, realExternalSends: 0 })
        if (req.method === 'GET' && url.pathname === '/api/clues/list') return reply(200, { list: leads, total: leads.length })
        if (req.method === 'GET' && url.pathname === '/api/whatsapp/templates') return reply(200, [{ id: 'nav04-template', name: '合成问候模板', content: '<p>你好 {{name}}</p>' }])
        if (req.method === 'GET' && url.pathname === '/api/whatsapp/accounts') return reply(200, [{ id: 'nav04-account', name: '本地合成账号' }])
        if (req.method === 'GET' && url.pathname === '/api/whatsapp/group-messaging/records') return reply(200, [])
        if (req.method === 'POST' && url.pathname === '/api/whatsapp/group-messaging/send') {
          let raw = ''
          for await (const chunk of req) raw += chunk
          events.push({ method: req.method, payload: JSON.parse(raw) })
          return reply(200, { message: '仅创建合成任务，未连接第三方' })
        }
        return reply(404, { error: 'Fixture route not found' })
      })
    },
  }],
  resolve: { alias: [
    { find: /^@\/(utils\/request|i18n)$/, replacement: `${root}api.js` },
    { find: '@', replacement: source },
  ] },
  server: { host: '127.0.0.1', port: 18224, strictPort: true },
})
await server.listen()
process.stdout.write('NAV04 synthetic fixture: http://127.0.0.1:18224\n')
