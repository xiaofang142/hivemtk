// 修复 i18n 语言包：
//   1) 删掉被 scripts/fix_menu_i18n.cjs（大括号计数把字符串里的 {n} 当括号）误追加到文件尾部的
//      扁平 "menu.xxx": "中文" 键 —— 这是「翻译文件全是中文」的直接来源；
//   2) 把 menu 段补全为各语言自己的译文（scripts/i18n/menu-translations.json）；
//   3) 修掉仍为中文的条目与 ja/ar 的 [JA]/[AR] 未翻译占位（scripts/i18n/plain-translations.json）。
// 全程字符串感知的定位 + 逐行改写，不重排文件，保持 diff 可读；改完必须能 JSON.parse。
// 用法：node scripts/apply-menu-i18n.cjs [--dry]
const fs = require('fs')
const path = require('path')

const ROOT = path.resolve(__dirname, '..')
const LOCALES = ['zh', 'en', 'ja', 'de', 'es', 'fr', 'pt', 'ru', 'ar']
const DRY = process.argv.includes('--dry')
const menuDict = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/i18n/menu-translations.json'), 'utf8'))
const plainDict = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/i18n/plain-translations.json'), 'utf8'))

const escapeRe = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')

/** 字符串感知地找到 `"key": {` 对应的 `}` 下标（处理字符串内的括号与转义） */
function matchBrace(raw, openIdx) {
  let depth = 0
  let inStr = false
  for (let i = openIdx; i < raw.length; i++) {
    const ch = raw[i]
    if (inStr) {
      if (ch === '\\') { i++; continue }
      if (ch === '"') inStr = false
      continue
    }
    if (ch === '"') { inStr = true; continue }
    if (ch === '{') depth++
    else if (ch === '}') { depth--; if (depth === 0) return i }
  }
  throw new Error('brace not closed')
}

function sliceBlock(raw, key) {
  const marker = `"${key}": {`
  const i = raw.indexOf(marker)
  if (i === -1) throw new Error(`找不到块 "${key}": {`)
  const open = i + marker.length - 1
  return [i, matchBrace(raw, open) + 1]
}

/** 在给定的片段里把顶层子键的值改成 value；不存在则插到收尾行之前 */
function setInSlice(slice, key, value, indent) {
  const re = new RegExp(`^([ \\t]*)"${escapeRe(key)}":[ \\t]*("(?:[^"\\\\]|\\\\.)*")[ \\t]*(,?)[ \\t]*\\r?$`, 'm')
  if (re.test(slice)) return slice.replace(re, (m, ind, _old, comma) => `${ind}"${key}": ${JSON.stringify(value)}${comma}`)
  // 插入：找最后一行 `  }`（收尾）
  const lines = slice.split('\n')
  let closeIdx = -1
  for (let i = lines.length - 1; i >= 0; i--) {
    if (/^[ \t]*\}[ \t]*,?[ \t]*\r?$/.test(lines[i])) { closeIdx = i; break }
  }
  if (closeIdx === -1) throw new Error(`插入失败，找不到收尾行：${key}`)
  let prev = closeIdx - 1
  while (prev >= 0 && lines[prev].trim() === '') prev--
  if (prev >= 0 && !/,[ \t]*\r?$/.test(lines[prev])) lines[prev] = lines[prev].replace(/\r?$/, ',')
  lines.splice(closeIdx, 0, `${indent}"${key}": ${JSON.stringify(value)}`)
  return lines.join('\n')
}

const summary = []
for (const loc of LOCALES) {
  const file = path.join(ROOT, `src/i18n/locales/${loc}.json`)
  let raw = fs.readFileSync(file, 'utf8')
  const before = JSON.parse(raw)
  const removedFlat = Object.keys(before).filter((k) => /^menu\./.test(k))

  // 1) 删除误追加的扁平 menu.* 行
  raw = raw.replace(/^[ \t]*"menu\.[^"]+":.*?\r?\n/gm, '')
  raw = raw.replace(/,\s*(\r?\n\}\s*)$/, '$1')

  // 2) menu 段：补齐/覆盖为对应语言
  const dict = menuDict[loc] || {}
  const [mi, me] = sliceBlock(raw, 'menu')
  let block = raw.slice(mi, me)
  for (const [k, v] of Object.entries(dict)) block = setInSlice(block, k, v, '    ')
  raw = raw.slice(0, mi) + block + raw.slice(me)

  // 3) 非 menu 修正
  for (const [key, perLoc] of Object.entries(plainDict)) {
    if (key === '_readme' || key === '_comment') continue
    const value = perLoc[loc]
    if (!value) continue
    if (key.includes('.')) {
      const segs = key.split('.')
      const [si, se] = sliceBlock(raw, segs[0])
      const sub = setInSlice(raw.slice(si, se), segs.slice(1).join('.'), value, '    ')
      raw = raw.slice(0, si) + sub + raw.slice(se)
    } else {
      raw = setInSlice(raw, key, value, '  ')
    }
  }
  if (plainDict._comment && plainDict._comment[loc]) {
    raw = setInSlice(raw, '_comment', plainDict._comment[loc], '  ')
  }

  // 4) 校验
  let after
  try {
    after = JSON.parse(raw)
  } catch (e) {
    console.error(`❌ ${loc}.json 改完不是合法 JSON：${e.message}`)
    process.exitCode = 1
    continue
  }
  const leak = Object.keys(after).filter((k) => /^menu\./.test(k))
  const flatMenu = Object.keys(after.menu || {}).length
  if (!DRY) fs.writeFileSync(file, raw)
  summary.push({ loc, removedFlat: removedFlat.length, leak: leak.length, menuKeys: flatMenu })
}

console.log(DRY ? '--- DRY RUN ---' : '--- APPLIED ---')
for (const s of summary) console.log(`${s.loc}: 删除扁平 menu.*=${s.removedFlat} 残留=${s.leak} menu 键=${s.menuKeys}`)
