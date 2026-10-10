#!/usr/bin/env node
/**
 * 菜单 / 路由 / i18n 三道闸门（CI 与本地 `npm run menu:check` 共用）
 * ---------------------------------------------------------------------------
 * 锁住四类已发生过的真实故障：
 *   A. 路由「按 URL 首段 + 手工白名单」懒装配 ⇒ 漏登记/映射错 = 点菜单进「页面不存在」
 *      （实测 /cap/matrix、/tiktok-card-stats/:id）
 *   B. 侧边栏是 Layout.vue 里手写的固定菜单树 ⇒ 新页面不登记就永不可见
 *   C. 菜单标题走 t('menu.<key>')，键缺失时把裸 key 印到界面上（menu.capabilityMatrix）
 *   D. 语言包被脚本误追加扁平 "menu.xxx": "中文" ⇒ 9 种语言里都是中文
 * 判据全部打在「机制」上，不做人工清单比对：
 *   1) index.js 必须全量 eager 注册 modules/*.js，且不再出现白名单符号
 *   2) 每个路由模块的每条静态路径都必须在注册集合里（无遗漏、无幽灵）
 *   3) 每条带 menuSection 的路由，其 menuSection/menuParents 必须在注册表里有定义
 *   4) 每个菜单键在 9 份语言包里都有非空译文；非 zh/ja 的语言包 menu 段不得含汉字
 *   5) 任何语言包都不得有扁平 "menu.*" 键，也不得残留 [XX] 未翻译占位
 *   6) Layout.vue 不得再出现手写菜单树，且必须走 buildMenuTree
 * 用法：node scripts/check-menu-autogen.cjs [--json report.json]
 */
const fs = require('fs')
const path = require('path')

const ROOT = path.resolve(__dirname, '..')
const LOCALES = ['zh', 'en', 'ja', 'de', 'es', 'fr', 'pt', 'ru', 'ar']
const failures = []
const observations = []
const fail = (msg) => failures.push(msg)

const read = (p) => fs.readFileSync(path.join(ROOT, p), 'utf8')
// 判据只看代码，不看注释：本次改造的注释里正解释着被废掉的旧符号名，
// 拿注释当证据会让闸门对着自己的说明文档报红。
const stripComments = (src) =>
  src
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .split('\n')
    .map((l) => l.replace(/^\s*\/\/.*$/, '').replace(/\s+\/\/[^'"]*$/, ''))
    .join('\n')

// ---------------------------------------------------------------- 1) index.js
const indexSrc = stripComments(read('src/router/index.js'))
if (!/import\.meta\.glob\(\s*'\.\/modules\/\*\.js'\s*,\s*\{\s*eager:\s*true\s*\}\s*\)/.test(indexSrc)) {
  fail('router/index.js 未使用 import.meta.glob(..., { eager: true }) 全量注册路由模块')
}
for (const sym of [
  /const\s+moduleNames\s*=/,
  /const\s+pathToModule\s*=/,
  /function\s+ensureRouteLoaded\s*\(/,
  /const\s+lazyModule\s*=/,
]) {
  if (sym.test(indexSrc)) {
    fail(`router/index.js 仍残留懒装配白名单（${sym}）—— 这类白名单就是「页面不存在」的根因`)
  }
}

// ------------------------------------------------- 2) 模块路由 vs 注册面
const MOD_DIR = path.join(ROOT, 'src/router/modules')
const moduleFiles = fs.readdirSync(MOD_DIR).filter((f) => f.endsWith('.js'))
const load = (src) => new Function(src.replace(/export\s+default/, 'return'))()
const joinPath = (base, p) => (p.startsWith('/') ? p : `${base.replace(/\/$/, '')}/${p}`).replace(/\/{2,}/g, '/')

function flattenRoutes(list, base = '') {
  const out = []
  for (const r of list) {
    if (!r || typeof r !== 'object' || typeof r.path !== 'string') continue
    const full = joinPath(base, r.path)
    out.push({ ...r, fullPath: full })
    if (Array.isArray(r.children)) out.push(...flattenRoutes(r.children, full))
  }
  return out
}

const allRoutes = []
const routeNames = []
for (const f of moduleFiles) {
  let mod
  try {
    mod = load(fs.readFileSync(path.join(MOD_DIR, f), 'utf8'))
  } catch (e) {
    fail(`路由模块 ${f} 解析失败：${e.message}`)
    continue
  }
  const list = Array.isArray(mod) ? mod : [mod]
  for (const r of flattenRoutes(list)) {
    allRoutes.push({ file: f, ...r })
    if (r.name) routeNames.push({ name: r.name, file: f })
  }
}
if (allRoutes.length < 100) fail(`展平后路由只有 ${allRoutes.length} 条，疑似解析异常`)

// 每条静态路径必须唯一（重复会静默顶掉前一条）
const pathCount = new Map()
for (const r of allRoutes) pathCount.set(r.fullPath, (pathCount.get(r.fullPath) || 0) + 1)
for (const [p, n] of pathCount) if (n > 1) fail(`路由路径重复 ${p} × ${n}`)
const nameCount = new Map()
for (const { name } of routeNames) nameCount.set(name, (nameCount.get(name) || 0) + 1)
for (const [n, c] of nameCount) if (c > 1) fail(`路由 name 重复 ${n} × ${c}`)

// ------------------------------------------------- 3) 菜单 meta vs 注册表
const registrySrc = read('src/layout/menuRegistry.js')
const sectionKeys = new Set([...registrySrc.matchAll(/\{\s*key:\s*"([A-Za-z0-9_]+)"/g)].map((m) => m[1]))
const groupKeys = new Set(
  [...registrySrc.matchAll(/^\s{2}([A-Za-z0-9_]+):\s*\{/gm)].map((m) => m[1])
)
if (sectionKeys.size < 5) fail(`menuRegistry.js 只解析到 ${sectionKeys.size} 个分区，疑似解析异常`)

const menuLeaves = allRoutes.filter((r) => r.meta && r.meta.menuSection)
const menuHidden = allRoutes.filter((r) => r.meta && r.meta.menuHidden)
for (const r of allRoutes) {
  const m = r.meta || {}
  if (!m.menuSection && !m.menuHidden && !m.hideLayout && !m.public && !r.redirect) {
    // 未声明归属也没标隐藏：允许（例如 /token 这类纯重定向），但要在下面统计里可见
    continue
  }
  if (m.menuSection && !sectionKeys.has(m.menuSection)) {
    fail(`${r.file} ${r.fullPath} 的 meta.menuSection='${m.menuSection}' 不在 MENU_SECTIONS 里`)
  }
  for (const p of Array.isArray(m.menuParents) ? m.menuParents : []) {
    if (!groupKeys.has(p)) fail(`${r.file} ${r.fullPath} 的 meta.menuParents 含未登记分组 '${p}'`)
  }
}
if (menuLeaves.length < 100) fail(`带 menuSection 的路由只有 ${menuLeaves.length} 条，菜单被改空的概率很大`)
if (menuHidden.length < 20) fail(`标了 menuHidden 的路由只有 ${menuHidden.length} 条，详情/编辑页疑似漏标`)

// ------------------------------------------------- 4/5) i18n 语言包
const loaded = {}
for (const loc of LOCALES) {
  const raw = read(`src/i18n/locales/${loc}.json`)
  let json
  try {
    json = JSON.parse(raw)
  } catch (e) {
    fail(`${loc}.json 不是合法 JSON：${e.message}`)
    continue
  }
  loaded[loc] = json
  const flatMenu = Object.keys(json).filter((k) => /^menu\./.test(k))
  if (flatMenu.length) {
    fail(`${loc}.json 残留扁平键 ${flatMenu.length} 个（例：${flatMenu.slice(0, 3).join(', ')}）—— 键名带点会绕过 menu 段`)
  }
}
const zhMenu = (loaded.zh || {}).menu || {}
const enMenu = (loaded.en || {}).menu || {}
const requiredMenuKeys = new Set([
  ...menuLeaves.map((r) => r.meta.menuKey).filter(Boolean),
  ...sectionKeys,
  ...groupKeys,
])
for (const loc of LOCALES) {
  const menu = (loaded[loc] || {}).menu || {}
  for (const key of requiredMenuKeys) {
    const v = menu[key]
    if (typeof v !== 'string' || !v.trim()) fail(`${loc}.json 缺 menu.${key} 译文`)
    if (/^\[[A-Z]{2}\]/.test(String(v))) fail(`${loc}.json 的 menu.${key} 仍是 [XX] 未翻译占位：${v}`)
  }
  if (loc !== 'zh' && loc !== 'ja') {
    for (const [k, v] of Object.entries(menu)) {
      if (/[\u4e00-\u9fff]/.test(String(v))) fail(`${loc}.json 的 menu.${k} 仍是中文：${v}`)
    }
  }
  // 非 zh/ja 语言包里，菜单键若与英文逐字相同则记入观察名单（不判红：
  // Team/Roles/Personnel/Configuration 等本就同形，人工翻完也仍是同形）。
  const BRAND = /^(douyin|kuaishou|xiaohongshu|xianyu|tiktok|whatsapp|whatsappCloud|feishu|telegramAccount|emailContentPreview)$/
  if (!['zh', 'en', 'ja'].includes(loc)) {
    for (const key of requiredMenuKeys) {
      if (BRAND.test(key)) continue
      if (typeof menu[key] === 'string' && menu[key] === enMenu[key]) {
        observations.push(`${loc}.json menu.${key} 与英文同形：${menu[key]}`)
      }
    }
  }
}
for (const loc of LOCALES) {
  const raw = read(`src/i18n/locales/${loc}.json`)
  const hits = [...raw.matchAll(/\[(JA|AR|KO|DE|FR|ES|PT|RU|EN)\]/g)].map((m) => m[0])
  if (hits.length) fail(`${loc}.json 残留 ${hits.length} 处 [XX] 未翻译占位，例：${hits.slice(0, 5).join(' ')}`)
}

// ------------------------------------------------- 6) Layout.vue 手写树已废除
const layoutSrc = stripComments(read('src/layout/Layout.vue'))
if (/const\s+topMenus\s*=\s*ref\(\[/.test(layoutSrc)) {
  fail('Layout.vue 又出现手写菜单树（const topMenus = ref([...])）—— 必须由路由 meta 自动生成')
}
if (!/buildMenuTree/.test(layoutSrc) || !/router\.getRoutes\(\)/.test(layoutSrc)) {
  fail('Layout.vue 没有走 buildMenuTree(router.getRoutes(), ...) 自动生成菜单')
}
const subItemSrc = stripComments(read('src/components/SubMenuItem.vue'))
if (/t\(\s*'menu\.'\s*\+/.test(subItemSrc) || /t\(\s*`menu\.\$\{/.test(subItemSrc)) {
  fail("SubMenuItem.vue 又自己拼 t('menu.' + key)—— 键缺失时会把裸 key 印到菜单上")
}
// Breadcrumb.vue 曾经自带 topMenusMap + subMenusMap 两张硬编码菜单表（第二份事实源，
// 新页面只改一边就会出现"侧边栏有、面包屑没有"）。
const bcSrc = stripComments(read('src/components/Breadcrumb.vue'))
if (/topMenusMap|subMenusMap/.test(bcSrc)) {
  fail('Breadcrumb.vue 又出现硬编码菜单表（topMenusMap / subMenusMap）—— 面包屑必须与侧边栏同源')
}
if (!/findTitleChain/.test(bcSrc)) {
  fail('Breadcrumb.vue 没有走 layout/menu.js 的 findTitleChain 生成标题链')
}

// ---------------------------------------------------------------- 输出
console.log('=== 菜单 / 路由 / i18n 闸门 ===')
console.log(`路由模块 ${moduleFiles.length} 个，展平路由 ${allRoutes.length} 条（菜单入口 ${menuLeaves.length}，隐藏 ${menuHidden.length}）`)
console.log(`语言包 ${LOCALES.length} 份，菜单键校验 ${requiredMenuKeys.size} 个 × ${LOCALES.length} 语言`)
if (observations.length) {
  console.log(`\n观察名单（不判红，人工复核）${observations.length} 条：`)
  for (const o of observations.slice(0, 20)) console.log('  ~ ' + o)
}
if (failures.length === 0) {
  console.log('✅ 全部通过')
  process.exit(0)
}
console.error(`\n❌ ${failures.length} 项不达标：`)
for (const f of failures.slice(0, 60)) console.error('  - ' + f)
if (failures.length > 60) console.error(`  ... 共 ${failures.length} 项`)
process.exit(1)
