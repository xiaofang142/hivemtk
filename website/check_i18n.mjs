import { readdirSync, readFileSync } from 'fs'
import path from 'path'
import { pathToFileURL } from 'url'

const SRC = new URL('./src', import.meta.url).pathname

const files = []
;(function walk(d){
  for (const e of readdirSync(d, { withFileTypes: true })) {
    const p = path.join(d, e.name)
    if (e.isDirectory()) { if (e.name === 'i18n') continue; walk(p) }
    else if (/\.(vue|js)$/.test(e.name)) files.push(p)
  }
})(SRC)

function stripComments(src){
  return src
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(?<![:'\\"`])\/\/[^\n]*/g, '')
    .replace(/<!--[\s\S]*?-->/g, '')
}

const byFile = {}
const LIT = /(['"`])((?:[^'"\\\n\r]|\\.)*?)\1/g
const HAS_CJK = /[\u3400-\u9fff\u3000-\u303f\uff00-\uffef]/
for (const f of files) {
  let c = readFileSync(f, 'utf8')
  c = c.replace(/<style[\s\S]*?<\/style>/g, '')
  c = stripComments(c)
  const hits = new Set()
  for (const m of c.matchAll(LIT)) {
    const s = m[2].replace(/\\'/g, "'").replace(/\\"/g, '"').replace(/\\\\/g, '\\')
    if (s.length && s.length <= 600 && HAS_CJK.test(s)) hits.add(s)
  }
  if (hits.size) byFile[path.relative(SRC, f)] = [...hits]
}

// collect dictionary keys
const dir = path.join(SRC, 'i18n/modules')
let dictSize = 0
const have = new Set()
// allKeys 覆盖 4 种语言（have 只取 zh∪en，用于判"字面量有没有译文键"）；
// 孤儿判据必须按 4 种语言并集算，否则只存在于某一语言的键会漏报。
const allKeys = new Set()
const keyFiles = new Map()
for (const f of readdirSync(dir)) {
  if (!f.endsWith('.js')) continue
  try {
    const mod = await import(pathToFileURL(path.join(dir, f)).href)
    const d = mod.default || mod
    for (const loc of ['zh', 'en']) if (d?.[loc]) Object.keys(d[loc]).forEach(k => have.add(k))
    for (const loc of ['zh', 'en', 'ja', 'ar']) {
      if (!d?.[loc]) continue
      for (const k of Object.keys(d[loc])) {
        allKeys.add(k)
        const slot = f + '|' + k
        if (!keyFiles.has(slot)) keyFiles.set(slot, new Set())
        keyFiles.get(slot).add(loc)
      }
    }
  } catch (err) { console.error('IMPORT_FAIL', f, err.message.slice(0, 120)) }
}
dictSize = have.size

let total = 0
const entries = []
for (const [f, arr] of Object.entries(byFile).sort()) {
  const miss = arr.filter(k => !have.has(k))
  total += miss.length
  console.log('\n### ' + f + '  (' + miss.length + '/' + arr.length + ')')
  miss.forEach(k => console.log(JSON.stringify(k)))
}
const all = [...Object.entries(byFile)]
const uniqMap = {}
for (const [f, arr] of all) for (const k of arr) if (!have.has(k)) (uniqMap[k] ??= []).push(f)
for (const [k, fs] of Object.entries(uniqMap).sort((a,b)=>a[0]<b[0]?-1:a[0]>b[0]?1:0))
  entries.push({ k, f: fs })
import { writeFileSync } from 'fs'
writeFileSync(new URL('./.i18n-missing.jsonl', import.meta.url),
  entries.map(e => JSON.stringify(e)).join('\n'))
console.log('\nTOTAL_LITERAL_KEYS=' + [...Object.values(byFile)].flat().length,
            'DICT=' + dictSize,
            'MISSING_PER_FILE_SUM=' + total,
            'MISSING_UNIQ=' + entries.length)

// 反向口径（只报告，不判红；发布前置仍是 MISSING_UNIQ=0）：
//   ORPHAN_KEYS      词典里有、站内没有任何字面量用它的键 —— 加译文时顺手带来的死键
//   PARITY_INCOMPLETE 同一个 (文件, key) 没凑齐 zh/en/ja/ar 四份 —— 少哪一语言就漏哪种界面
const usedLiterals = new Set(Object.values(byFile).flat())
const orphans = [...allKeys].filter(k => !usedLiterals.has(k)).sort()
const localesOf = new Map()
for (const [slot, locs] of keyFiles) {
  const k = slot.slice(slot.indexOf('|') + 1)
  if (!localesOf.has(k)) localesOf.set(k, new Set())
  locs.forEach(l => localesOf.get(k).add(l))
}
writeFileSync(new URL('./.i18n-orphans.jsonl', import.meta.url),
  orphans.map(k => JSON.stringify({ k, locales: [...localesOf.get(k)] })).join('\n'))
const gaps = [...keyFiles.entries()].filter(([, locs]) => locs.size !== 4)
  .sort((a, b) => a[0] < b[0] ? -1 : 1)
writeFileSync(new URL('./.i18n-parity.jsonl', import.meta.url),
  gaps.map(([slot, locs]) => JSON.stringify({ slot, have: [...locs] })).join('\n'))
console.log('ORPHAN_KEYS=' + orphans.length, 'PARITY_INCOMPLETE=' + gaps.length)
orphans.slice(0, 5).forEach(k => console.log('  ORPHAN ' + JSON.stringify(k).slice(0, 80)))
gaps.slice(0, 5).forEach(([slot, locs]) => console.log('  GAP ' + slot.split('|')[0] + ' ' + JSON.stringify([...locs]) + ' ' + JSON.stringify(slot.slice(slot.indexOf('|') + 1)).slice(0, 60)))
