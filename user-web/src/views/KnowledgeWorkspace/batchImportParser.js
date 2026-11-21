const detectFormat = (filename) => {
  const lower = String(filename || '').toLowerCase()
  if (lower.endsWith('.csv')) return 'csv'
  if (lower.endsWith('.txt')) return 'text'
  return 'json'
}

const parseCSVRecords = (text) => {
  const rows = []
  let row = []
  let field = ''
  let inQuotes = false
  let recordHasInput = false

  const finishRecord = () => {
    row.push(field)
    // encoding/csv skips truly empty physical lines, but keeps whitespace,
    // quoted empty fields and delimiter-only records for server validation.
    if (recordHasInput) rows.push(row)
    row = []
    field = ''
    recordHasInput = false
  }

  for (let i = 0; i < text.length; i++) {
    const char = text[i]
    if (char === '"') {
      recordHasInput = true
      if (inQuotes && text[i + 1] === '"') {
        field += '"'
        i++
      } else if (inQuotes || field.length === 0) {
        inQuotes = !inQuotes
      } else {
        throw new Error('CSV 引号格式错误')
      }
    } else if (char === ',' && !inQuotes) {
      row.push(field)
      field = ''
      recordHasInput = true
    } else if (char === '\r' && text[i + 1] === '\n') {
      if (inQuotes) {
        // Go's encoding/csv reader normalizes CRLF inside quoted fields to LF.
        field += '\n'
        recordHasInput = true
      } else {
        finishRecord()
      }
      i++
    } else if (char === '\n') {
      if (inQuotes) {
        field += '\n'
        recordHasInput = true
      } else {
        finishRecord()
      }
    } else {
      field += char
      recordHasInput = true
    }
  }

  if (inQuotes) throw new Error('CSV 引号未闭合')
  if (recordHasInput) finishRecord()
  return rows
}

const parseCSV = (text) => {
  const rows = parseCSVRecords(String(text))
  if (rows.length < 2) throw new Error('CSV 必须至少包含表头和一行数据')

  const headers = rows[0].map(header => header.trim().toLowerCase())
  const findIndex = (aliases) => headers.findIndex(header => aliases.includes(header))
  const titleIndex = findIndex(['title', '标题', 'name'])
  const contentIndex = findIndex(['content', '内容', 'text', 'body', 'q', 'question'])
  const categoryIndex = findIndex(['category', '分类'])
  const tagsIndex = findIndex(['tags', '标签'])
  if (contentIndex < 0) throw new Error('CSV 缺少 content 列')

  return rows.slice(1).flatMap(columns => {
    const content = columns[contentIndex]
    if (content === undefined) return []
    const tags = tagsIndex >= 0 && tagsIndex < columns.length
      ? columns[tagsIndex].split(',').map(tag => tag.trim())
      : []
    return [{
      title: titleIndex >= 0 ? (columns[titleIndex] || '') : '',
      content,
      category: categoryIndex >= 0 ? (columns[categoryIndex] || '') : '',
      tags
    }]
  })
}

const parseJSON = (text) => {
  let data
  try {
    data = JSON.parse(text)
  } catch (e) {
    throw new Error(`JSON 语法错误：${e.message}`, { cause: e })
  }
  if (data === null || typeof data !== 'object') {
    throw new Error('JSON 结构不匹配,需为数组或包含 items/data/list/documents 数组字段')
  }
  const rows = Array.isArray(data) ? data : data.items || data.data || data.list || data.documents
  if (!Array.isArray(rows)) {
    throw new Error('JSON 结构不匹配,需为数组或包含 items/data/list/documents 数组字段')
  }
  // 行级校验与模板渲染契约对齐（UWA-16）：tags 必须是文本数组、content/title/category
  // 必须是文本，否则 tags.join / truncateText 会在渲染阶段抛错把整页打崩。
  // 空白 content 仍保留原样，交给服务端按既有口径计为 rejected。
  return rows.map((row, index) => {
    if (!row || typeof row !== 'object' || Array.isArray(row)) throw new Error(`第 ${index + 1} 行必须是对象`)
    if (typeof row.content !== 'string') throw new Error(`第 ${index + 1} 行 content 必须是文本`)
    if (row.tags !== undefined && (!Array.isArray(row.tags) || row.tags.some(tag => typeof tag !== 'string'))) {
      throw new Error(`第 ${index + 1} 行 tags 必须是文本数组`)
    }
    for (const key of ['title', 'category']) {
      if (row[key] !== undefined && typeof row[key] !== 'string') throw new Error(`第 ${index + 1} 行 ${key} 必须是文本`)
    }
    return row
  })
}

export const parseBatchPreview = (text, filename, format = 'auto') => {
  const detectedFormat = format === 'auto' ? detectFormat(filename) : format
  if (detectedFormat === 'text') {
    const content = String(text).trim()
    if (!content) return []
    const baseName = String(filename || '').split(/[\\/]/).pop() || ''
    const title = baseName.replace(/\.txt$/i, '')
    return [{ title, content, category: '', tags: [] }]
  }
  if (detectedFormat === 'csv') return parseCSV(text)
  return parseJSON(text)
}
