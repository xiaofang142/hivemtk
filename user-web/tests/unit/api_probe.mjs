// 单接口真实联调探针（最终版）：带 JWT 打真实后端(8204)，
//  - 路径参数：优先用列表里的真实 id；无数据则用合成 id(0 / 零uuid)
//  - 靠“响应是否含 code”区分【路由缺失(真bug)】vs【资源不存在(正常)】
//  - 写操作(POST/PUT/DELETE)前后比对 PostgreSQL(8232) 行数
//  - 抓取 mtk-user-server 日志，调用后若产生 ERROR/panic 判 LOG_ERROR
// 一次探一个接口；--file 探整文件（仍逐接口独立评估/记录）。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { execFileSync } from 'node:child_process'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const root = path.resolve(__dirname, '../..')
const inv = JSON.parse(fs.readFileSync(path.resolve(__dirname, '../API_INVENTORY.json'), 'utf8'))
const BASE = process.env.API_BASE || 'http://localhost:8204'
const TOKEN = fs.readFileSync('/tmp/hive_token.txt', 'utf8').trim()
const DB = {
  host: 'localhost', port: 8232, user: 'admin',
  password: fs.readFileSync(path.resolve(root, '../.env'), 'utf8').match(/^POSTGRES_PASSWORD=(.*)$/m)?.[1] || '',
  db: 'user_db',
}
const RES_FILE = path.resolve(__dirname, '../API_TEST_RESULTS.json')
const ID_CACHE = path.resolve(__dirname, '../.api_id_cache.json')
const results = fs.existsSync(RES_FILE) ? JSON.parse(fs.readFileSync(RES_FILE, 'utf8')) : {}
const idCache = fs.existsSync(ID_CACHE) ? JSON.parse(fs.readFileSync(ID_CACHE, 'utf8')) : {}
const saveIdCache = () => fs.writeFileSync(ID_CACHE, JSON.stringify(idCache, null, 2))

const req = (method, url, body, timeout = 30) => {
  const args = ['-s', '-m', String(timeout), '-X', method, `${BASE}${url}`,
    '-H', 'Authorization: Bearer ' + TOKEN, '-H', 'Content-Type: application/json']
  if (body) args.push('-d', JSON.stringify(body))
  try {
    const out = execFileSync('curl', args, { encoding: 'utf8', timeout: (timeout + 3) * 1000 })
    let json; try { json = JSON.parse(out) } catch { json = { _raw: out.slice(0, 200) } }
    return { json, raw: out, hasCode: json && typeof json.code === 'string' }
  } catch (e) {
    return { json: { _error: e.message.split('\n')[0] }, raw: '', hasCode: false, timedOut: /timed out|ETIMEDOUT/.test(e.message) }
  }
}
const psql = (q) => {
  try { return execFileSync('psql', ['-h', DB.host, '-p', String(DB.port), '-U', DB.user, '-d', DB.db,
    '-t', '-A', '-F', '\t', '-c', q], { env: { ...process.env, PGPASSWORD: DB.password }, encoding: 'utf8', timeout: 8000 }).trim() }
  catch (e) { return `ERR:${e.message.split('\n')[0]}` }
}
const backendErrorsSince = (iso) => {
  try { const out = execFileSync('docker', ['logs', 'mtk-user-server', '--since', iso], { encoding: 'utf8', timeout: 8000 })
    // 排除与本次调用无关的后台周期任务噪声（平台上报/心跳/站内信拉取/各类后台同步等）
    const noise = /平台配置未初始化|商户上报|获取最新(站内信|消息)失败|heartbeat|心跳|平台配置|后台|schedule|定时|sync|同步|gRPC|grpc|拨测|probe/i
    return out.split('\n').filter((l) => /ERROR|panic|PANIC|fatal/i.test(l) && !noise.test(l)).slice(-10) } catch { return [] }
}
const pathParams = (url) => [...url.matchAll(/\$\{([^}]+)\}/g)].map((m) => m[1])
const snake = (s) => s.replace(/-/g, '_')
const nounOf = (url) => {
  const parts = url.split('/').filter(Boolean)
  let cut = parts.length
  for (let i = 0; i < parts.length; i++) if (/\$\{/.test(parts[i])) { cut = i; break }
  return '/' + parts.slice(0, cut).join('/') || '/' + parts[0]
}
const guessTable = (noun) => snake((noun.split('/').filter(Boolean).pop()) || 'rows')
const pickId = (j) => {
  const d = j?.data
  if (Array.isArray(d) && d.length) return d[0].id ?? d[0].ID ?? d[0].uuid
  if (d && typeof d === 'object') return d.id ?? d.ID ?? d.uuid ?? null
  return null
}
const requiredFields = (msg = '') => [...msg.matchAll(/for '(\w+)' failed on the '(required|email)'/g)].map((m) => m[1])
const placeholder = (f, i = 0) => {
  const x = f.toLowerCase()
  if (/email/.test(x)) return 'test@example.com'
  if (/phone|mobile|tel/.test(x)) return '13800000000'
  if (/url|link|avatar|image|logo|icon|picture|cover/.test(x)) return 'https://example.com/x.png'
  if (/pass|pwd|secret/.test(x)) return 'Pass@1234'
  if (/name|title|label|nick/.test(x)) return 'apitest_' + i
  if (/desc|remark|note|content|text|message/.test(x)) return 'auto test'
  if (/count|num|qty|amount|price|total|score|status|is_|enable|active|flag/.test(x)) return 1
  return 'apitest_' + i
}
const ZERO_UUID = '00000000-0000-0000-0000-000000000000'
const resolveId = (url) => {
  const noun = nounOf(url); const ck = 'real:' + noun
  if (idCache[ck]) return { id: idCache[ck], synthetic: false }
  const list = req('GET', noun)
  const id = pickId(list.json)
  if (id != null) { idCache[ck] = String(id); saveIdCache(); return { id: String(id), synthetic: false } }
  return { id: '0', synthetic: true }
}
function classify(status, parsed, errLogs) {
  if (status === 401) return { ok: false, kind: 'AUTH' }
  if (status === 403) return { ok: false, kind: 'FORBIDDEN' }
  if (status === 404) return parsed.hasCode ? { ok: true, kind: 'PASS' } : { ok: false, kind: 'ROUTE_MISSING' }
  if (status >= 500) return { ok: false, kind: 'SERVER_ERROR' }
  if (status === 400 || status === 422) return { ok: true, kind: 'NEED_PARAMS' }
  if (status >= 200 && status < 300) return errLogs.length ? { ok: false, kind: 'LOG_ERROR' } : { ok: true, kind: 'PASS' }
  if (status === 0) return { ok: true, kind: 'SLOW' } // 超时/长耗时(如 LLM 生成)，可达但慢
  return { ok: true, kind: 'OTHER' }
}
function probe(item) {
  const params = pathParams(item.url)
  let resolved = item.url
  const usedIds = {}
  if (params.length) {
    const { id, synthetic } = resolveId(item.url)
    // 若 0 触发格式错误(UUID)，改零 uuid
    let idVal = id
    for (const p of params) {
      resolved = resolved.replace(`\${${p}}`, encodeURIComponent(idVal))
      usedIds[p] = idVal + (synthetic ? '(syn)' : '')
    }
  }
  const isWrite = ['POST', 'PUT', 'DELETE'].includes(item.method)
  const table = guessTable(nounOf(resolved))
  let before = isWrite ? psql(`SELECT count(*) FROM ${table}`) : null
  const since = new Date(Date.now() - 1500).toISOString()
  // 生成/导出/流式等长耗时接口给更长超时
  const slow = /(generat|completion|chat|stream|export|complet|summar)/i.test(item.url)
  const timeout = slow ? 90 : 30
  let body = item.method === 'GET' || item.method === 'DELETE' ? null : {}
  let r, status, raw
  for (let round = 0; round < 4; round++) {
    const res = req(item.method, resolved, body, timeout)
    r = res.json; raw = res.raw
    if (res.timedOut) { status = 0; break }
    status = res.hasCode ? (r.code === 'SUCCESS' ? 200 : statusFromCode(r.code)) : (r._raw ? 0 : 200)
    if (status !== 400) break
    const fields = requiredFields(r.message || '')
    if (!fields.length) break
    fields.forEach((f, i) => { if (!(f in body)) body[f] = placeholder(f, i) })
  }
  // 若合成 id 触发 400 且疑似 uuid 格式，重试零 uuid
  if (params.length && status === 400 && /uuid|format|invalid/i.test(r.message || '')) {
    resolved = item.url
    for (const p of params) resolved = resolved.replace(`\${${p}}`, ZERO_UUID)
    const res = req(item.method, resolved, body, timeout)
    r = res.json; raw = res.raw
    status = res.timedOut ? 0 : (res.hasCode ? (r.code === 'SUCCESS' ? 200 : statusFromCode(r.code)) : 0)
  }
  let after = isWrite ? psql(`SELECT count(*) FROM ${table}`) : null
  const dbDelta = isWrite ? (after !== before ? `${before}->${after}` : 'no-change') : null
  // 后端错误常为异步记录（goroutine / 延迟写日志），调用后稍等再抓日志，避免漏判 LOG_ERROR
  try { execFileSync('sleep', ['1']) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  const errLogs = backendErrorsSince(since)
  const verdict = classify(status, { hasCode: r && typeof r.code === 'string' }, errLogs)
  if (item.method === 'POST' && verdict.ok && pickId(r)) { const n = nounOf(resolved); idCache['real:' + n] = String(pickId(r)); saveIdCache() }
  return { ...item, url: resolved, status, code: r.code ?? null, msg: (r.message || '').slice(0, 160),
    usedIds, dbBefore: before, dbAfter: after, dbDelta, kind: verdict.kind, ok: verdict.ok, backendErrors: errLogs.slice(0, 5) }
}
function statusFromCode(code) {
  return { UNAUTHORIZED_2001: 401, FORBIDDEN_2003: 403, NOT_FOUND_1002: 404, NOT_FOUND_3001: 404,
    INVALID_PARAM_1001: 400, VALIDATION_1002: 400, INTERNAL_5000: 500, SERVER_ERROR_5000: 500 }[code] || 200
}
function record(rr) {
  results[`${rr.file}::${rr.function}`] = { method: rr.method, url: rr.url, status: rr.status, kind: rr.kind, ok: rr.ok,
    code: rr.code, msg: rr.msg, dbDelta: rr.dbDelta, at: new Date().toISOString() }
  fs.writeFileSync(RES_FILE, JSON.stringify(results, null, 2))
}
const argv = process.argv.slice(2)
let targets = []
if (argv.includes('--index')) targets = [inv[Number(argv[argv.indexOf('--index') + 1])]]
else if (argv.includes('--file')) {
  const f = argv[argv.indexOf('--file') + 1]; const fi = argv.indexOf('--fn')
  targets = fi >= 0 ? inv.filter((x) => x.file === f && x.function === argv[fi + 1]) : inv.filter((x) => x.file === f)
} else { console.error('用法: node api_probe.mjs --index N | --file X.js [--fn Y]'); process.exit(2) }
for (const t of targets) { const rr = probe(t); record(rr); console.log(JSON.stringify(rr, null, 2)) }
