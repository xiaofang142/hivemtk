import { describe, it, expect } from 'vitest'
import { fmtTime, fmtDate } from '@/utils/time'

// 回归：Go 零值时间在部分浏览器会被 Date 宽松解析成 2001 年，
// 只靠 getFullYear() <= 1 拦不住，表格会显示「2001年1月1日 08:05」。
describe('fmtTime 统一时间展示', () => {
  it('RFC3339 与数据库直出的零值时间都回落占位符', () => {
    expect(fmtTime('0001-01-01T00:00:00Z')).toBe('-')
    expect(fmtTime('0001-01-01 08:05:43')).toBe('-')
    expect(fmtTime(' 0001-01-01T08:05:43+08:05 ')).toBe('-')
  })

  it('空值/非法值返回占位符（可自定义）', () => {
    for (const v of [null, undefined, '']) expect(fmtTime(v)).toBe('-')
    expect(fmtTime(null, undefined, '—')).toBe('—')
    expect(fmtTime('not-a-date')).toBe('-')
  })

  it('正常时间照常格式化，fmtDate 只保留日期', () => {
    expect(fmtTime('2026-10-22T17:07:04+08:00')).toContain('2026')
    expect(fmtDate('2026-10-22T17:07:04+08:00')).toContain('2026')
    expect(fmtDate('2026-10-22T17:07:04+08:00')).not.toContain('17:07')
  })
})
