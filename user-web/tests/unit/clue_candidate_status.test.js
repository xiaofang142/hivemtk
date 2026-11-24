/**
 * D11 待审队列：线索列表必须提供 candidate(待审) 筛选与展示。
 */
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, it, expect } from 'vitest'

const listSrc = readFileSync(resolve(process.cwd(), 'src/views/clue/List.vue'), 'utf8')

describe('clue/List 待审状态', () => {
  it('状态筛选包含 candidate 待审', () => {
    expect(listSrc).toMatch(/'candidate'[\s\S]*'new'[\s\S]*'notified'/)
  })

  it('candidate 标签文案为待审', () => {
    expect(listSrc).toContain("candidate: '待审'")
  })

  it('待审在表格中以 warning 标签突出', () => {
    expect(listSrc).toContain("row.status === 'candidate' ? 'warning' : 'info'")
  })
})
