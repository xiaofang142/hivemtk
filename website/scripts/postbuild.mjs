// website postbuild 脚本（Node 跨平台版本）
// Vue 3 + Vite SPA 构建完成后，做最后的清理与补充
// - 清理 Vite 默认生成的多入口产物（如果有 index-*.html / 多余 css）
// - 复制 favicon.svg 到 dist（如果缺失）
// - 为每个静态路由铺设 <route>/index.html，让 GitHub Pages 对深链返回真 200
// - 生成 dist/404.html：只有 404.html 时未铺设的路径仍能渲染 SPA 壳
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
// Pages 没有 rewrite：只有 404.html 时 /hivemtk/docs 的响应码是 404——页面照样渲染，
// 但 sitemap.xml 里公布的 URL 会被搜索引擎按「已删除」处理，等于自己把子页面踢出索引。
// 把 SPA 壳按路由铺成目录，深链才拿到真 200。路由清单从 src/router/index.js 现场解析，
// 避免两处维护；解析结果为空说明正则失效，必须当场失败而不是静默铺 0 个目录。
// 依赖 vite.config.js 的 base：壳内资源引用是 /hivemtk/assets/… 绝对路径，
// 从任意深度目录加载都能解析；base 一旦改成相对路径，这些副本会全部取不到资源。
console.log('  → 铺设静态路由的 <route>/index.html ...')
const distIndex = resolve(distDir, 'index.html')
if (!existsSync(distIndex)) {
  console.error('✗ dist/index.html 不存在，无法铺设路由壳文件')
  process.exit(1)
}
const routerSrc = readFileSync(resolve(__dirname, '../src/router/index.js'), 'utf-8')
const routes = [...new Set(
  [...routerSrc.matchAll(/path:\s*'\/([^'*:?]+)'/g)].map((m) => m[1])
)]
if (routes.length < 5) {
  console.error(`✗ 从 src/router/index.js 只解析到 ${routes.length} 条静态路由（<5），疑似解析失效`)
  process.exit(1)
}
const shell = readFileSync(distIndex)
for (const route of routes) {
  const dir = resolve(distDir, route)
  mkdirSync(dir, { recursive: true })
  writeFileSync(resolve(dir, 'index.html'), shell)
}
console.log(`    ✓ ${routes.length} 个路由目录: ${routes.join(', ')}`)

// 4. 生成 404.html（GitHub Pages 的 SPA 兜底）
// 未铺设的路径（含手输错的深链）由它加载 SPA，再交给 catch-all 路由渲染 NotFoundPage。
console.log('  → 生成 404.html（Pages SPA 兜底）...')
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
