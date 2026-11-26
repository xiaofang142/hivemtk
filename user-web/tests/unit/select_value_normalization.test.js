import { describe, expect, it } from 'vitest'
import { normalizeSelectValue } from '@/utils/selectValue'

// 线上 T06 复核：CRM 客户行的 source_account_id 是后端的 bigint 字符串 "10"，
// 而 /api/channels/catalog 的账号 id 是数字 10，el-select 严格相等匹配失败会显示裸 "10"。
describe('外键下拉选中值归一化', () => {
  it('数字选项下把字符串 id 转成数字，选中后能显示关联名称', () => {
    const options = [{ value: 10, label: '四川同咨微信账号' }]
    expect(normalizeSelectValue('10', options)).toBe(10)
    expect(normalizeSelectValue(10, options)).toBe(10)
  })

  it('字符串选项（客户 UUID）保持原类型', () => {
    const options = [{ value: 'abc-123', label: '客户甲' }]
    expect(normalizeSelectValue('abc-123', options)).toBe('abc-123')
  })

  it('0 与 "0" 表示未关联，返回 null 让下拉显示占位符', () => {
    expect(normalizeSelectValue(0, [{ value: 18 }])).toBeNull()
    expect(normalizeSelectValue('0', [{ value: 18 }])).toBeNull()
    expect(normalizeSelectValue('', [{ value: 18 }])).toBeNull()
    expect(normalizeSelectValue(null, [{ value: 18 }])).toBeNull()
  })

  it('多选数组逐项归一化', () => {
    const options = [{ value: 18, label: 'admin' }]
    expect(normalizeSelectValue(['18', '19'], options)).toEqual([18, 19])
  })

  it('选项尚未加载时不臆造候选，保留原值等待响应式重算', () => {
    expect(normalizeSelectValue('10', [])).toBe('10')
  })
})
