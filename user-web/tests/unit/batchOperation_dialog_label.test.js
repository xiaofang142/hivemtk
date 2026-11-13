import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settle, settleBody } from './settle-helper'

// 弹窗标题/「操作类型」必须显示中文 label，不能裸秀后端工具名。
//
// 背景（11-13 O 轮线上实测）：点「批量导入」卡片，弹窗标题与操作类型
// 只读框都渲染成 "import"——卡片区用 tool.label || tool.name 所以是对的，
// 弹窗两处却直接绑 currentTool?.name。后端 /api/batch/tools 同时返回
// name 与 label（卡片能显示「批量导入」就是证据），label 明明就在手里
// 没用它。这类「数据有、渲染挑错字段」的缺陷，只有渲染级用例能钉住。

const api = vi.hoisted(() => ({
  getBatchTools: vi.fn(),
  getBatchHistories: vi.fn(),
  previewBatch: vi.fn(),
  runBatch: vi.fn(),
  cancelBatch: vi.fn(),
  getBatchDetail: vi.fn(),
  batchImportFile: vi.fn()
}))

vi.mock('@/api/batchOperation.js', () => ({
  getBatchTools: api.getBatchTools,
  getBatchHistories: api.getBatchHistories,
  previewBatch: api.previewBatch,
  runBatch: api.runBatch,
  cancelBatch: api.cancelBatch,
  getBatchDetail: api.getBatchDetail,
  batchImportFile: api.batchImportFile
}))

import List from '@/views/batchOperation/List.vue'
import i18n from '@/i18n'

const TOOLS = {
  list: [
    { name: 'import', label: '批量导入', description: '通过 CSV 文件批量导入数据', usedCount: 3 },
    { name: 'export', label: '批量导出', description: '导出数据为 CSV 文件', usedCount: 0 }
  ],
  total: 2
}

const mounted = []

async function mountPage(tools = TOOLS) {
  api.getBatchTools.mockResolvedValue(tools)
  api.getBatchHistories.mockResolvedValue({ list: [], total: 0 })
  const wrapper = mount(List, {
    attachTo: document.body,
    global: { plugins: [createPinia(), ElementPlus, i18n] }
  })
  mounted.push(wrapper)
  await settle(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('批量操作弹窗：工具名展示用 label 而非裸 name', () => {
  it('点「批量导入」卡片，弹窗标题与操作类型只读框都显示「批量导入」而不是 import', async () => {
    const wrapper = await mountPage()

    // 卡片标题（label || name 的老路径）先自证一次：数据里 label 确实存在
    const cardTitles = Array.from(wrapper.findAll('h3').map((h) => h.text()))
    expect(cardTitles).toContain('批量导入')

    // 显式点第一张工具卡（批量导入）
    const card = wrapper.findAll('.tool-card')[0]
    await card.trigger('click')
    await settleBody()

    // el-dialog teleport 到 body；标题节点是 .el-dialog__title
    const title = document.querySelector('.el-dialog__title')
    expect(title).not.toBeNull()
    expect(title.textContent.trim()).toBe('批量导入')
    expect(title.textContent.trim()).not.toBe('import')

    // 「操作类型」只读输入框（label-width 120px 的第一项）
    const dialogInputs = Array.from(document.querySelectorAll('.el-dialog .el-input__inner'))
    expect(dialogInputs.length).toBeGreaterThan(0)
    const opTypeValue = dialogInputs[0].value
    expect(opTypeValue).toBe('批量导入')
    expect(opTypeValue).not.toBe('import')

    // 弹窗里不应再出现裸工具名
    const dialogText = document.querySelector('.el-dialog').textContent
    expect(dialogText).not.toMatch(/(^|[\s|])import([\s|]|$)/)
  })

  it('后端未给 label 时回退显示 name（不出现空白标题）', async () => {
    const wrapper = await mountPage({
      list: [{ name: 'import', description: 'x', usedCount: 0 }],
      total: 1
    })

    const card = wrapper.findAll('.tool-card')[0]
    await card.trigger('click')
    await settleBody()

    const title = document.querySelector('.el-dialog__title')
    expect(title).not.toBeNull()
    expect(title.textContent.trim()).toBe('import')

    const dialogInputs = Array.from(document.querySelectorAll('.el-dialog .el-input__inner'))
    expect(dialogInputs[0].value).toBe('import')
  })
})
