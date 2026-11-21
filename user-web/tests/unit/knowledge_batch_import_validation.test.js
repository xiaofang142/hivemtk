import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createPinia } from 'pinia'
import { settle, settleBody } from './settle-helper'
const products = vi.hoisted(() => vi.fn())
vi.mock('@/api/ragProductConfig', () => ({ ragProductConfigAPI: { listProducts: products } }))
vi.mock('@/api/knowledgeMerchant', () => ({ knowledgeMerchantAPI: { batchImport: vi.fn(), batchUpload: vi.fn() } }))
import BatchImport from '@/views/KnowledgeWorkspace/BatchImport.vue'
import i18n from '@/i18n'
let wrapper
beforeEach(() => { products.mockResolvedValue([{ id: 1, name: '审计产品' }]) })
afterEach(() => { wrapper?.unmount(); ElMessage.closeAll() })
async function page() {
  wrapper = mount(BatchImport, { global: { plugins: [createPinia(), ElementPlus, i18n] }, attachTo: document.body })
  await settle(wrapper)
  return wrapper
}
describe('知识批量导入解析边界', () => {
  it.each(['null', '{"items":{}}', '[null]', '[{"content":"正文","tags":"标签"}]', '[{"content":5}]', '[{"content":"正文","tags":[{}]}]'])('拒绝错误结构 %s，不产生可提交预览', async input => {
    await page()
    wrapper.vm.parseTextToPreview(input, 'json')
    await settleBody()
    expect(document.body.textContent).toContain('解析失败')
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
  })
  it('CSV 引号内换行作为同一行，未闭合引号拒绝', async () => {
    await page()
    wrapper.vm.parseTextToPreview('title,content,tags\n标题,"第一行\n第二行","售后,流程"', 'csv')
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(1)
    expect(wrapper.text()).toContain('第一行')
    expect(wrapper.text()).toContain('第二行')
    wrapper.vm.parseTextToPreview('title,content\n标题,"未闭合', 'csv')
    await settleBody()
    expect(document.body.textContent).toContain('CSV 引号未闭合')
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
  })
  it('修改 JSON 或移除文件后旧预览立即失效', async () => {
    await page()
    wrapper.vm.activeTab = 'paste'
    wrapper.vm.pasteForm.jsonText = '[{"content":"旧正文"}]'
    wrapper.vm.loadPastePreview()
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(1)
    wrapper.vm.pasteForm.jsonText = '[{"content":"新正文"}]'
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
    wrapper.vm.loadPastePreview()
    wrapper.vm.handleFileRemove()
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
  })
})
