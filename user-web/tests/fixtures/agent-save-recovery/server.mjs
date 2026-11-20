// 独立测试入口：node tests/fixtures/agent-save-recovery/server.mjs；不访问生产数据。
import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
let agents, bindings, failBinding, loseCreateAck, events
function reset(scenario) {
  agents = scenario === 'update' ? [{ id: 1, agent_code: 'controlled-save', name: '部分保存验收', persona: '仅本地合成信号', agent_type: 'sales', agent_mode: 'passive', status: 1 }] : []
  bindings = { 1: ['11'] }
  failBinding = scenario !== 'lost'
  loseCreateAck = scenario === 'lost'
  events = []
}
reset('update')
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'controlled-agent-save-recovery',
    configureServer(vite) {
      vite.middlewares.use('/fixture', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1'), path = url.pathname
        const reply = (status, value) => { res.statusCode = status; res.setHeader('Content-Type', 'application/json; charset=utf-8'); res.end(JSON.stringify(value)) }
        if (req.method === 'POST' && /^\/reset\/(update|create|lost)$/.test(path)) { reset(path.split('/').pop()); return reply(200, { reset: true }) }
        if (req.method === 'GET' && path === '/evidence') return reply(200, { agents, bindings, events })
        if (req.method === 'GET' && path === '/agents/by-code') return reply(200, agents.find(agent => agent.agent_code === url.searchParams.get('agent_code')) || null)
        if (req.method === 'GET' && path === '/knowledge-bases') return reply(200, { list: url.searchParams.get('type') === 'faq' ? [{ id: 11, name: '原知识库' }, { id: 22, name: '新知识库' }] : [] })
        const agentMatch = /^\/agents\/(\d+)$/.exec(path), bindingMatch = /^\/bindings\/(\d+)$/.exec(path)
        const agent = agentMatch && agents.find(item => String(item.id) === agentMatch[1])
        if (req.method === 'GET' && agentMatch) return agent ? reply(200, agent) : reply(404, { error: '记录不存在' })
        if (req.method === 'GET' && bindingMatch) return reply(200, (bindings[bindingMatch[1]] || []).map(kb_id => ({ kb_id })))
        if ((req.method === 'POST' && path === '/agents') || (req.method === 'PUT' && (agentMatch || bindingMatch))) {
          let raw = ''
          for await (const chunk of req) raw += chunk
          let payload
          try { payload = JSON.parse(raw) } catch { return reply(400, { error: '无效请求' }) }
          events.push({ method: req.method, path, payload })
          if (bindingMatch) {
            if (failBinding) { failBinding = false; return reply(503, { error: '受控绑定写入拒绝' }) }
            bindings[bindingMatch[1]] = payload.kb_ids.slice()
            return reply(200, { count: payload.kb_ids.length })
          }
          if (req.method === 'POST') {
            if (agents.some(item => item.agent_code === payload.agent_code)) return reply(409, { error: '编码重复' })
            const created = { ...payload, id: 2 }; agents.push(created)
            if (loseCreateAck) { loseCreateAck = false; return reply(503, { error: '受控创建回执丢失，主记录已存储' }) }
            return reply(200, created)
          }
          if (!agent) return reply(404, { error: '记录不存在' })
          Object.assign(agent, payload)
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
  server: { host: '127.0.0.1', port: 18214, strictPort: true },
})
await server.listen()
process.stdout.write('Controlled save recovery: http://127.0.0.1:18214 (synthetic data only)\n')
