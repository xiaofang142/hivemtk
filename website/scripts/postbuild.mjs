// website postbuild 脚本（Node 跨平台版本）
// Vue 3 + Vite SPA 构建完成后，做最后的清理与补充
// - 清理 Vite 默认生成的多入口产物（如果有 index-*.html / 多余 css）
// - 复制 favicon.svg 到 dist（如果缺失）
// - 硬门：断言客服浮标仍在壳里（官网唯一运行期外部依赖，见 2.5 节）
// - 为每个静态路由铺设 <route>/index.html，让深链拿到真 200
// - 生成 dist/404.html：nginx error_page 404 兜底，未铺设的路径仍能渲染 SPA 壳
import { cpSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, writeFileSync } from 'fs'
import { resolve, dirname } from 'path'
import { fileURLToPath } from 'url'

const __dirname = dirname(fileURLToPath(import.meta.url))
const distDir = resolve(__dirname, '../dist')
const publicDir = resolve(__dirname, '../public')

if (!existsSync(distDir)) {
  console.error('✗ dist 目录不存在，请先执行 vite build')
  process.exit(1)
}

// 1. 清理 Vite 默认生成的多入口产物（index-*.html），本项目是 SPA 只需一个 index.html
console.log('  → 清理 Vite 默认生成的多入口产物（index-*.html）...')
try {
  for (const name of readdirSync(distDir)) {
    if (/^index-.*\.html$/.test(name)) {
      rmSync(resolve(distDir, name))
      console.log(`    ✓ removed ${name}`)
    }
  }
} catch (e) {
  // 忽略清理错误
}

// 2. 确保 favicon.svg 存在
console.log('  → 确保 favicon.svg 存在...')
const distFavicon = resolve(distDir, 'favicon.svg')
const publicFavicon = resolve(publicDir, 'favicon.svg')
if (!existsSync(distFavicon) && existsSync(publicFavicon)) {
  cpSync(publicFavicon, distFavicon)
  console.log('    ✓ public/favicon.svg -> dist/favicon.svg')
}

// 3. 为静态路由铺设 <route>/index.html
// 站点没有 rewrite rule 让深链落到 index.html：只有 404.html 时 /docs 的响应码是 404——页面照样渲染，
// 但 sitemap.xml 里公布的 URL 会被搜索引擎按「已删除」处理，等于自己把子页面踢出索引。
// 把 SPA 壳按路由铺成目录，深链才拿到真 200。路由清单从 src/router/index.js 现场解析，
// 避免两处维护；解析结果为空说明正则失效，必须当场失败而不是静默铺 0 个目录。
// 依赖 vite.config.js 的 base：壳内资源引用是 /assets/… 绝对路径（base='/'，线上站点根），
// 从任意深度目录加载都能解析；base 一旦改成相对路径，这些副本会全部取不到资源。
console.log('  → 铺设静态路由的 <route>/index.html ...')
const distIndex = resolve(distDir, 'index.html')
if (!existsSync(distIndex)) {
  console.error('✗ dist/index.html 不存在，无法铺设路由壳文件')
  process.exit(1)
}
const shellText = readFileSync(distIndex, 'utf-8')

// 2.5 硬门：官网唯一运行期外部依赖 = 右下角网页客服浮标，必须留在壳里
// 背景（2026-10-23 事故）：它被当成「指向已下线域名的死代码」删过一次并发布上线，
// 右下角客服整块消失——而当时构建、断言、发布全部是绿的：没有任何门禁看这一项。
// 浮动 SDK 由 work.xapptool.cn 提供（实测 200 / 11984 B），是**在服能力**，不是死代码。
// 判定规约与在服依赖清单见 ~/Documents/www/go/hivemtk/deploy/README.md §9（deploy/ 不入 git）。
// 这里只断言「壳里还挂着」两件事：配置全局 + SDK 地址；两者缺一，浮标就加载不出来。
// 断言对象是 dist/index.html 而不是源文件——改名/重构 index.html 时，只要产物里少了就拦。
const WIDGET_MARKERS = [
  ['配置全局 window.MarketingChatWidgetConfig', 'window.MarketingChatWidgetConfig'],
  ['SDK 地址 https://work.xapptool.cn/embed/marketing-chat-widget.iife.js',
    'https://work.xapptool.cn/embed/marketing-chat-widget.iife.js'],
]
const missing = WIDGET_MARKERS.filter(([, marker]) => !shellText.includes(marker))
if (missing.length > 0) {
  console.error('✗ dist/index.html 里缺少网页客服浮标（这是官网唯一的运行期外部依赖，不是死代码）：')
  for (const [label] of missing) console.error(`    - ${label}`)
  console.error('  浮标由 work.xapptool.cn 的 web 渠道智能体提供，删掉后线上右下角客服直接消失，')
  console.error('  而构建/发布全程仍会全绿。确实要下线客服，请先改这道门并在 deploy/README.md §9 登记。')
  process.exit(1)
}
console.log('    ✓ 客服浮标仍在壳里（配置全局 + SDK 地址）')

const routerSrc = readFileSync(resolve(__dirname, '../src/router/index.js'), 'utf-8')
const routes = [...new Set(
  [...routerSrc.matchAll(/path:\s*'\/([^'*:?]+)'/g)].map((m) => m[1])
)]
if (routes.length < 5) {
  console.error(`✗ 从 src/router/index.js 只解析到 ${routes.length} 条静态路由（<5），疑似解析失效`)
  process.exit(1)
}
const shell = shellText
for (const route of routes) {
  const dir = resolve(distDir, route)
  mkdirSync(dir, { recursive: true })
  writeFileSync(resolve(dir, 'index.html'), shell)
}
console.log(`    ✓ ${routes.length} 个路由目录: ${routes.join(', ')}`)

// 4. 生成 404.html（nginx error_page 404 /404.html 的 SPA 兜底）
// 未铺设的路径（含手输错的深链）由它加载 SPA，再交给 catch-all 路由渲染 NotFoundPage。
console.log('  → 生成 404.html（SPA 兜底）...')
writeFileSync(resolve(distDir, '404.html'), shell)
console.log('    ✓ dist/index.html -> dist/404.html')

console.log('✓ postbuild 完成')
console.log('  dist 内容:')
try {
  for (const name of readdirSync(distDir).sort()) {
    const p = resolve(distDir, name)
    if (statSync(p).isFile()) {
      console.log(`    ${name}`)
    } else if (statSync(p).isDirectory()) {
      console.log(`    ${name}/`)
    }
  }
} catch (e) {
  // 忽略列举错误
}
