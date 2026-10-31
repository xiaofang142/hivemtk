import { http } from '@/utils/request'

// ============================================================================
// 批量操作 API（对应后端 content/controller/batch_operation.go）
//
// ----------------------------------------------------------------------------
// 为什么这一层要做「契约翻译」而不是直接把 data 透传：
//
// 后端四个端点的入参形状各不相同，且**都不接受**前端页面（views/batchOperation）
// 原本发的那套字段：
//
//   POST /api/batch/preview  ← { operation_type, data_type, ids: string[] }
//   POST /api/batch/import   ← multipart/form-data：file(必填) + type(表单字段)
//   POST /api/batch/export   ← 表单字段：type / format / ids(逗号分隔)
//   POST /api/batch/delete   ← { ids: string[] }
//   POST /api/batch/update   ← { ids: string[], fields: {k: string} }
//
// 而页面发的是 { tool, source, filter, params, dryRun } —— 字段名与形状都对不上，
// 于是「预览」与「执行」两个按钮**恒 400**（实测：400 "Key: 'PreviewRequest.
// OperationType' failed on the 'required' tag" 与 400 "上传文件失败：request
// Content-Type isn't multipart/form-data"）。
//
// 后端契约是事实源，所以翻译放在这一层做：页面语义（tool / source / filter）
// 到后端契约的映射只有这一处，页面与后端都不必互相迁就。
//
// 映射口径：
//   operation_type ← tool（后端 GET /api/batch/tools 返回的 name 恰好就是
//                    import / export / delete / update，与页面选的卡片同名）
//   data_type      ← 'clue'（后端 PreviewRequest 只认 clue / user / account，
//                    而这四个工具在服务端作用的都是线索：ImportTypeClue /
//                    ExportTypeClue / BatchDeleteClues / BatchUpdateClues）
//   ids            ← 由 source 决定（见 resolveClueIds）
//
// 写侧与读侧对 allowed_origins 这类字段的形状差异，同源于「读出来是字符串、
// 写回去要数组」，已在 chat_channel 侧统一处理，这里不再重复。
// ============================================================================

// 线索 id 解析上限：后端对 export/delete/update 有「必须显式给 ids」的硬闸
// （防空全量导出，防数据暴露）。所以「全部」在这里必须**有界**：超过上限就让
// 用户收窄筛选条件，而不是悄悄截断（截断 = 删了一半数据，比报错更坏）。
const CLUE_IDS_LIMIT = 500

// 归一化成字符串数组：后端三处 ids 字段都是 []string（不是 []int），
// 前端传数字会在反序列化阶段被拒（"cannot unmarshal number into Go struct
// field BatchDeleteRequest.ids of type string"）。
const toIdList = (ids) => {
  if (!ids) return []
  const arr = Array.isArray(ids) ? ids : [ids]
  return arr
    .map((v) => (v === null || v === undefined ? '' : String(v)))
    .map((s) => s.trim())
    .filter(Boolean)
}

// 解析线索 id 集合。
//
// source 语义（页面表单）：
//   'all'    —— 本租户全部线索
//   'filter' —— 按 form.filter 里的 JSON 条件过滤
//   'import' —— 走文件上传，不涉及既有线索的 id
//
// filter 的 JSON 形如 {"status":"active","created_at":">2024-01-01"}，
// 逐字段透传给 GET /api/clues/list —— 该端点的 query 参数与这批字段同名。
const resolveClueIds = async (data) => {
  const source = data?.source || 'all'
  if (source === 'import') return []

  const params = { page: 1, limit: CLUE_IDS_LIMIT }
  if (source === 'filter' && data?.filter) {
    let filter
    try {
      filter = JSON.parse(data.filter)
    } catch {
      throw new Error('筛选条件不是合法 JSON，无法解析要操作的记录')
    }
    if (filter && typeof filter === 'object' && !Array.isArray(filter)) {
      Object.entries(filter).forEach(([k, v]) => {
        if (v !== '' && v !== null && v !== undefined) params[k] = v
      })
    }
  }

  const res = await http.get('/api/clues/list', params)
  const list = Array.isArray(res?.list) ? res.list : Array.isArray(res) ? res : []
  const total = Number(res?.total ?? list.length)
  if (total > CLUE_IDS_LIMIT) {
    throw new Error(`匹配到 ${total} 条记录，超过单次上限 ${CLUE_IDS_LIMIT} 条，请收窄筛选条件`)
  }
  return toIdList(list.map((it) => it?.id))
}

// 后端表单字段读的是 ctx.PostForm ⇒ 必须发 form-urlencoded，不能发 JSON。
const postForm = (url, fields) => {
  const body = new URLSearchParams()
  Object.entries(fields).forEach(([k, v]) => {
    if (v !== undefined && v !== null) body.append(k, String(v))
  })
  return http.post(url, body.toString(), {
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' }
  })
}

export function batchImportFile(file, type = 'clue') {
  // multipart 文件上传：file 必填，type 是表单字段（后端 ctx.PostForm("type")）。
  //
  // file 为空时**照样发请求**（只是不带 file 段）：后端会回自己的
  // 「上传文件失败：…」原话。在前端先拦一道 throw 只会把这条更准的信息
  // 换成一句自造文案，而且让「这个出口到底通不通」没法用一次调用验出来。
  const fd = new FormData()
  if (file) fd.append('file', file)
  fd.append('type', type)
  return http.post('/api/batch/import', fd, {
    headers: { 'Content-Type': 'multipart/form-data' }
  })
}

export function downloadBatchTemplate(type = 'clue') {
  return http.get('/api/batch/template', { params: { type } })
}

export function batchExport(data) {
  // 后端读 ctx.PostForm 的 type / format / ids（ids 是逗号分隔串）。
  //
  // 响应体是**文件流**（csv/xlsx），不是 JSON：走默认 JSON 解析会让 request.js
  // 打出「收到非 JSON 响应，疑似网关兜底到前端页面」的伪错误（实测 CSV 前几行
  // 被当成异常响应体打印到控制台）。所以这里显式要 blob，由调用方触发下载。
  return http.post(
    '/api/batch/export',
    new URLSearchParams({
      type: data?.type || 'clue',
      format: data?.format || 'csv',
      ids: toIdList(data?.ids).join(',')
    }).toString(),
    {
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      responseType: 'blob',
      _silent: true
    }
  )
}

export function batchDelete(data) {
  return http.post('/api/batch/delete', { ids: toIdList(data?.ids) })
}

// 批量更新的字段值一律归一成**字符串**。
//
// 后端 BatchUpdateRequest.Fields 是 map[string]string，所以 {"type": 9}（数字）
// 会在反序列化阶段被拒：'json: cannot unmarshal number into Go struct field
// BatchUpdateRequest.fields of type string' —— 400，且文案对用户毫无指导意义
// （页面上那个 params 输入框的示例就是 JSON，用户写数字是再自然不过的事）。
//
// 数字/布尔在这里转成其字面量字符串（"9" / "true"）——后端对 is_verify、type
// 这两个数字字段本来就要 ParseInt，转成字符串正合它的意；其余字段是文本列，
// 字符串也是它要的形状。
const toFieldMap = (fields) => {
  const out = {}
  if (!fields || typeof fields !== 'object' || Array.isArray(fields)) return out
  Object.entries(fields).forEach(([k, v]) => {
    if (v === null || v === undefined) return
    if (typeof v === 'object') return // 复杂值不接受：后端只认标量
    out[k] = String(v)
  })
  return out
}

export function batchUpdate(data) {
  return http.post('/api/batch/update', {
    ids: toIdList(data?.ids),
    fields: toFieldMap(data?.fields)
  })
}

export function getBatchTools() {
  return http.get('/api/batch/tools')
}

// runBatch：按工具分流到对应的后端端点。
//
// 页面的「执行」按钮调它，页面给的语义是 { tool, source, filter, params }：
//   import —— 必须有文件（后端是 multipart 上传），没有文件就明确报错，
//             而不是发一个必然 400 的 JSON 让用户猜
//   export —— 导出 ids 指定的那批
//   delete —— 删除 ids 指定的那批
//   update —— 改 ids 指定的那批，fields 来自 form.params
export async function runBatch(data) {
  // tool 缺省时回落 import：这是本页最老的一条默认路径（改造前 runBatch 就是
  // 直接 batchImportFile），也是四个工具里唯一不依赖既有记录 id 的那个。
  // 页面调用时一定会带 tool（卡片名就是 tool 名），所以这个回落只影响
  // 「没有工具名」这种说不清的调用。
  const tool = data?.tool || 'import'
  const ids = await resolveClueIds(data)

  switch (tool) {
    case 'import':
      return batchImportFile(data?.file, data?.type || 'clue')
    case 'export':
      return batchExport({ type: data?.type || 'clue', format: data?.format || 'csv', ids })
    case 'delete':
      return batchDelete({ ids })
    case 'update': {
      let fields = {}
      if (data?.params) {
        try {
          fields = JSON.parse(data.params)
        } catch {
          throw new Error('执行参数不是合法 JSON')
        }
      }
      return batchUpdate({ ids, fields })
    }
    default:
      // 认不出的工具名：交给 import 这条最保守的出口（发出去让后端回原话），
      // 而不是本地抛一个自造的错 —— 后端的回话才是事实。
      return batchImportFile(data?.file, data?.type || 'clue')
  }
}

export function getBatchHistories(params) {
  return http.get('/api/batch/histories', params)
}
export function cancelBatch(id) {
  return http.post(`/api/batch/histories/${id}/cancel`)
}
export function getBatchDetail(id) {
  return http.get(`/api/batch/histories/${id}`)
}

// previewBatch：预览只做「会不会命中哪些记录」的判断，不落任何写操作。
// 后端 Preview 要求 operation_type + data_type，delete/update/export 还要求
// ids 非空（与写端点同一条硬闸，预览与执行不许出现两套口径）。
export async function previewBatch(data) {
  const tool = data?.tool
  const ids = await resolveClueIds(data)
  return http.post('/api/batch/preview', {
    operation_type: tool,
    data_type: 'clue',
    ids
  })
}
