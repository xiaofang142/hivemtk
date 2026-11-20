import { describe, expect, it } from 'vitest'
import { parseBatchPreview } from '@/views/KnowledgeWorkspace/batchImportParser'

describe('knowledge batch import preview parser', () => {
  it('auto-detects TXT and previews the same trimmed text and filename title as the server', () => {
    expect(parseBatchPreview('  第一段\n第二段  ', 'faq.TXT')).toEqual([
      { title: 'faq', content: '第一段\n第二段', category: '', tags: [] }
    ])
  })

  it('matches the server CRLF normalization inside quoted CSV fields and parses the next record', () => {
    const input = 'title,content,tags\r\n"FAQ","第一行\r\n第二行",流程\r\n"FAQ 2","含逗号,与""引号""",标签'
    expect(parseBatchPreview(input, 'faq.csv')).toEqual([
      { title: 'FAQ', content: '第一行\n第二行', category: '', tags: ['流程'] },
      { title: 'FAQ 2', content: '含逗号,与"引号"', category: '', tags: ['标签'] }
    ])
  })

  it('supports the backend CSV content aliases and ignores blank records', () => {
    expect(parseBatchPreview('name,question\n标题,正文\n\n', 'faq.csv')).toEqual([
      { title: '标题', content: '正文', category: '', tags: [] }
    ])
  })

  it('keeps blank and whitespace content rows so the server can report them as rejected', () => {
    expect(parseBatchPreview('title,content\n,\nFAQ,   ', 'faq.csv')).toEqual([
      { title: '', content: '', category: '', tags: [] },
      { title: 'FAQ', content: '   ', category: '', tags: [] }
    ])
  })

  it('preserves empty CSV tag tokens just like the server parser', () => {
    expect(parseBatchPreview('title,content,tags\nFAQ,answer,"x,, y "', 'faq.csv')).toEqual([
      { title: 'FAQ', content: 'answer', category: '', tags: ['x', '', 'y'] }
    ])
  })
})
