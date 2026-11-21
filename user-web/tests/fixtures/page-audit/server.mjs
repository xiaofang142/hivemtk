import { createServer } from 'vite'
import vue from '@vitejs/plugin-vue'
import { fileURLToPath } from 'node:url'
const root = fileURLToPath(new URL('.', import.meta.url))
const source = fileURLToPath(new URL('../../../src/', import.meta.url))
const events = []
let helpFailure = false
const helpArticle = { id: 1, title: '审计帮助文章', category: '指南', summary: '受控文章摘要', content: '受控帮助正文，不访问外部服务。', updated_at: '2026-11-20T00:00:00Z' }
const server = await createServer({
  configFile: false, root, plugins: [vue(), {
    name: 'page-audit-fixture',
    configureServer(vite) {
      vite.middlewares.use('/api', async (req, res) => {
        const url = new URL(req.url, 'http://127.0.0.1')
        const path = url.pathname
        let body = ''
        for await (const chunk of req) body += chunk
        events.push({ method: req.method, path, body })
        res.setHeader('Content-Type', 'application/json; charset=utf-8')
        const reply = (data) => res.end(JSON.stringify({ code: 0, data, message: 'ok' }))
        if (path === '/evidence') return reply(events)
        if (path === '/fixture/help-failure' && req.method === 'POST') {
          helpFailure = JSON.parse(body).enabled === true
          return reply({ helpFailure })
        }
        if (path.startsWith('/public/help-center/')) {
          if (helpFailure) {
            res.statusCode = 503
            return res.end(JSON.stringify({ code: 503, message: '受控帮助服务暂不可用' }))
          }
          if (path.endsWith('/categories')) return reply({ list: [{ category: '指南', count: 1 }], total: 1 })
          if (path.endsWith('/articles/1')) return reply(helpArticle)
          if (path.endsWith('/articles')) {
            const matches = (!url.searchParams.get('q') || helpArticle.title.includes(url.searchParams.get('q'))) && (!url.searchParams.get('category') || url.searchParams.get('category') === helpArticle.category)
            return reply({ list: matches ? [helpArticle] : [], total: matches ? 1 : 0 })
          }
        }
        if (path === '/batch/tools') return reply({ list: ['import', 'export', 'delete', 'update'].map((name, i) => ({ name, label: ['批量导入', '批量导出', '批量删除', '批量更新'][i] })), total: 4 })
        if (path === '/batch/histories') return reply({ list: [], total: 0 })
        if (path === '/clues/list') return reply({ list: [{ id: 'fixture-1', name: '审计线索' }], total: 1 })
        if (path === '/batch/preview') return reply({ operation_type: JSON.parse(body).operation_type, data_type: 'clue', selected_count: 1, preview: true })
        if (path === '/batch/delete') return reply({ deleted_count: 1 })
        if (path === '/intent/tool-chains/vocab') return reply({ intents: ['greeting'], intent_families: { greeting: 'both' }, template_vars: ['customer_id'], tools: ['knowledge.search'] })
        if (path === '/intent/tool-chains') return reply({ list: [{ id: 1, name: '审计调用流', intent_code: 'greeting', steps_json: '[{"tool_name":"knowledge.search","enabled":true}]', enabled: true }], total: 1 })
        if (path === '/intent/custom') return reply({ list: [{ id: 1, type: 'audit_intent', name: '审计自定义意图', keywords_json: '["审计"]', enabled: true }], total: 1 })
        if (path === '/rag-config/products') return reply({ list: [{ id: 1, name: '审计知识产品' }], total: 1 })
        if (path === '/manage/config-params') return reply([{ group: 'platform', key: 'api_host', value: 'http://127.0.0.1:18000', default_value: '', value_type: 'string', name: '平台地址' }])
        if (path === '/manage/config-params/groups') return reply([{ group: 'platform', label: '平台连接', sort_order: 1 }])
        if (path === '/manage/platform-host/connectivity') return reply({ status: 'ok', reachable: true, message: '受控平台可达' })
        if (path === '/config/runtime') return reply({})
        if (path === '/geo/site/llms-txt-preview') return reply({ content: '# 受控 SEO 站点\n\n> 审计站点说明' })
        if (path === '/geo/site/robots-preview') return reply({ content: 'User-agent: *\nAllow: /' })
        if (path === '/geo/push/sitemap') return reply({ content: '<?xml version="1.0"?><urlset />' })
        if (path === '/material/list') return reply({ list: [], total: 0 })
        if (path === '/material/categories') return reply({ list: [{ id: 1, name: '审计分类' }], total: 1 })
        res.statusCode = 501
        res.end(JSON.stringify({ code: 501, message: '夹具未实现此接口，不会访问外部服务' }))
      })
    },
  }],
  resolve: { alias: [
    { find: '@/utils/request', replacement: `${root}request.js` },
    { find: '@', replacement: source },
  ] },
  server: { host: '127.0.0.1', port: 18215, strictPort: true },
})
await server.listen()
process.stdout.write('Page audit fixture: http://127.0.0.1:18215\n')
