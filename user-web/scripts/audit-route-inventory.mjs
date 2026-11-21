import { readdir, writeFile, mkdir } from 'node:fs/promises'
import { fileURLToPath, pathToFileURL } from 'node:url'
import path from 'node:path'
const root = fileURLToPath(new URL('../', import.meta.url))
const directory = path.join(root, 'src/router/modules')
const modules = (await readdir(directory)).filter(name => name.endsWith('.js')).sort()
const routes = []
function collect(items, parent = '', module = '') {
  for (const route of items) {
    const routePath = route.path.startsWith('/') ? route.path : `${parent}/${route.path}`.replace(/\/+/g, '/')
    const component = route.component ? String(route.component).match(/@\/views\/([^'")]+)/)?.[1] || '(component)' : ''
    if (component || route.redirect) routes.push({ path: routePath, component, redirect: route.redirect || '', module, name: route.name || '' })
    if (route.children) collect(route.children, routePath, module)
  }
}
for (const name of modules) {
  const { default: items } = await import(pathToFileURL(path.join(directory, name)).href)
  collect(Array.isArray(items) ? items : [items], '', name)
}
const target = path.join(root, 'docs/audit/user-web-interaction-audit')
await mkdir(target, { recursive: true })
const details = {
  '/messageHub/list': '真实实例首屏统计与API一致，20条第一页/第二页核对；请求失败/搜索竞态/详情隔离/已读刷新有专项。搜索与详情浏览器补验遇工具超时，整页未完成。',
  '/messageHub/dashboard': '真实页面观察API暂时连接失败横幅及自动恢复；窗口统计0、DLQ0与接口一致，重试投递未执行。',
  '/batchOperation/list': '受控浏览器验证默认删除只预览；文件选择与执行边界另有组件/API测试。未完成全部交互。',
  '/intentRecognition/tool-flow': '受控浏览器验证词表、列表、自定义意图首次加载；未完成全部交互。',
  '/knowledge/batch-import': '受控浏览器验证初始禁用和错误 tags 拒绝；CSV/旧预览另有单测。未完成全部交互。',
  '/geo-tools/seo-infra': '受控浏览器逐标签验证 llms、robots 与 sitemap 内容；3 项单测验证部分失败隔离、重试、非法数据和生成去重；真实站点部署未验。',
  '/system/config-params': '受控浏览器验证平台探针显示可达；其他配置写入未验。',
  '/system/material-library': '受控浏览器验证空列表、打开上传、必填分类；大小/部分成功/去重另有单测。未完成全部交互。',
  '/customer360/list': '已改服务端唯一客户分页/关键词，前端10项和8217真实PG仓储/服务/HTTP契约通过；8401旧二进制未更新，新端到端浏览器未验。',
  '/customerSession/list': '组合函数测试验证画像字段、消息清空与迟到隔离；真实浏览器及后端未验。',
}
const global = [
  { path: '/login', component: 'Login.vue', result: '真实前端浏览器首屏已见；提交去重有组件测试；真实认证未验。' },
  { path: '/help-center', component: 'public/HelpCenter.vue', result: '真实前端验证 API 不可用时错误与重试；受控浏览器验证分类、列表、文章正文、故障与恢复。7 项请求状态单测通过；真实租户数据未验。' },
  ...['/setup', '/chat/embed/default', '/chat/embed/:channel_ref', '/profile', '/notifications', '/:pathMatch(.*)*'].map(routePath => ({ path: routePath, component: '(global)', result: '未进行本轮逐元素浏览器验证。' }))
]
const text = ['# user-web 路由覆盖台账', '', '状态：本轮审计进行中，未完成全站验收。', '', `静态路由清单由 ${modules.length} 个业务路由模块生成，共 ${routes.filter(r => r.component).length} 条带组件的业务路由、${routes.filter(r => r.redirect).length} 条重定向。此脚本只读取路由定义，不访问页面、不自动点击。`, '', '真实后端审计阻塞：本机 API、PostgreSQL、Redis 未运行，浏览器没有已认证会话。受控夹具证据不代表真实接口数据正确。', '', '| 路径 | 组件/目标 | 本轮证据与状态 |', '|---|---|---|', ...global.map(r => `| \`${r.path}\` | ${r.component} | ${r.result} |`), ...routes.map(r => `| \`${r.path}\` | ${r.component || `→ ${r.redirect}`} | ${details[r.path] || '未进行本轮逐元素浏览器验证；真实后端链路未验。'} |`), '', '动态 ID 需真实存在记录，不能用猜测 ID 当验收。每页全部按钮、表单、分页、错误态和写入回读仍需逐项证据；本台账没有把源码存在、构建通过或单测通过计为全页验收。', ''].join('\n')
await writeFile(path.join(target, 'routes.md'), text)
process.stdout.write(JSON.stringify({ modules: modules.length, components: routes.filter(r => r.component).length, redirects: routes.filter(r => r.redirect).length }) + '\n')
