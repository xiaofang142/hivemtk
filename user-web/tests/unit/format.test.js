import { describe, it, expect, beforeEach } from 'vitest'
import { formatNumber, formatCurrency, formatPercent, formatDate, formatRelativeTime as _formatRelativeTime, formatList as _formatList, formatCompactNumber as _formatCompactNumber, formatFileSize, getLocale } from '@/utils/format.js'

describe('formatNumber', () => {
  it('格式化整数', () => {
    const result = formatNumber(1234567, 'en-US')
    expect(result).toContain('1')
    expect(result).toContain('234')
    expect(result).toContain('567')
  })

  it('处理零', () => {
    expect(formatNumber(0, 'en-US')).toBe('0')
  })
})

describe('formatCurrency', () => {
  it('格式化人民币', () => {
    const result = formatCurrency(99.99, 'CNY', 'zh-CN')
    expect(result).toContain('99')
  })
})

describe('formatPercent', () => {
  it('格式化百分比', () => {
    const result = formatPercent(85.5, 'en-US', 1)
    expect(result).toContain('85')
    expect(result).toContain('%')
  })
})

describe('formatDate', () => {
  it('处理无效日期', () => {
    expect(formatDate('invalid', 'en-US')).toBe('')
  })
})

describe('formatFileSize', () => {
  it('格式化字节', () => {
    expect(formatFileSize(500, 'en-US')).toBe('500 B')
  })
  it('格式化 KB', () => {
    const result = formatFileSize(1024, 'en-US')
    expect(result).toContain('KB')
  })
})

describe('getLocale', () => {
  beforeEach(() => {
    localStorage.clear()
  })

  it('返回默认 locale', () => {
    const locale = getLocale()
    expect(typeof locale).toBe('string')
    expect(locale.length).toBeGreaterThan(0)
  })
})
