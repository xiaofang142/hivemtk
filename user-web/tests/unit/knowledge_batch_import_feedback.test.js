import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ batchImport: vi.fn(), batchUpload: vi.fn(), listProducts: vi.fn(), getDocumentProgress: vi.fn(), reindexDocument: vi.fn() }))
const ui = vi.hoisted(() => ({ confirm: vi.fn() }))

vi.mock('@/api/knowledgeMerchant', () => ({ knowledgeMerchantAPI: api }))
vi.mock('@/api/knowledge', () => ({ knowledgeAPI: api }))
vi.mock('@/api/ragProductConfig', () => ({
  ragProductConfigAPI: { listProducts: api.listProducts }
}))
vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return { ...actual, ElMessageBox: { confirm: ui.confirm } }
})

import i18n from '@/i18n'
import BatchImport from '@/views/KnowledgeWorkspace/BatchImport.vue'

describe('knowledge batch import result feedback', () => {
  beforeEach(() => {
    api.batchImport.mockReset()
    api.batchUpload.mockReset()
    api.listProducts.mockReset().mockResolvedValue([{ id: 'product-1', name: 'Product 1' }])
    ui.confirm.mockReset().mockResolvedValue(true)
    api.getDocumentProgress.mockReset()
    api.reindexDocument.mockReset().mockResolvedValue({})
  })
  afterEach(() => { vi.useRealTimers(); vi.restoreAllMocks() })

  const importBatch = async () => {
    const wrapper = mount(BatchImport, { global: { plugins: [ElementPlus, i18n] } })
    await settle(wrapper)
    await wrapper.findAll('.el-tabs__item').find(tab => tab.text().includes('JSON 粘贴')).trigger('click')
    await settle(wrapper)
    await wrapper.find('textarea').setValue('[{"content":"answer"}]')
    await wrapper.findAll('button').find(button => button.text().includes('解析预览')).trigger('click')
    await wrapper.findAllComponents({ name: 'ElSelect' })[1].setValue('product-1')
    await wrapper.findAll('button').find(button => button.text().includes('确认导入')).trigger('click')
    await settle(wrapper)
    return wrapper
  }

  it('polls pending documents to indexed and stops at terminal state', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 1, accepted: 1, rejected: 0, document_ids: [42] })
    api.getDocumentProgress.mockResolvedValueOnce({ id: 42, title: 'FAQ', embed_status: 'processing', embed_progress: 30 })
      .mockResolvedValue({ id: 42, title: 'FAQ', embed_status: 'indexed', embed_progress: 100 })
    const wrapper = await importBatch()
    expect(wrapper.find('[data-testid="batch-document-42"]').attributes('data-status')).toBe('processing')
    expect(wrapper.text()).toContain('30%')
    expect(api.getDocumentProgress).toHaveBeenCalledWith(42, { product_id: 'product-1' })
    await vi.advanceTimersByTimeAsync(2000)
    await settle(wrapper)
    expect(wrapper.find('[data-testid="batch-document-42"]').attributes('data-status')).toBe('indexed')
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.getDocumentProgress).toHaveBeenCalledTimes(2)
    wrapper.unmount()
  })

  it('retries failed indexing with the batch product and then confirms indexed', async () => {
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 1, accepted: 1, rejected: 0, document_ids: [42] })
    api.getDocumentProgress.mockResolvedValueOnce({ id: 42, embed_status: 'failed', error_msg: 'sensitive provider detail' })
      .mockResolvedValue({ id: 42, embed_status: 'indexed', embed_progress: 100 })
    const wrapper = await importBatch()
    expect(wrapper.text()).toContain('Indexing failed; retry is available')
    expect(wrapper.text()).not.toContain('sensitive provider detail')
    await wrapper.find('[data-testid="retry-document-42"]').trigger('click')
    await settle(wrapper)
    expect(api.reindexDocument).toHaveBeenCalledWith(42, { product_id: 'product-1' })
    expect(wrapper.find('[data-testid="batch-document-42"]').attributes('data-status')).toBe('indexed')
    wrapper.unmount()
  })

  it('keeps failed state retryable and hides provider details when reindex request fails', async () => {
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 1, accepted: 1, rejected: 0, document_ids: [42] })
    api.getDocumentProgress.mockResolvedValue({ id: 42, embed_status: 'failed', error_msg: 'raw embedding endpoint failure' })
    api.reindexDocument.mockRejectedValue(new Error('private provider response'))
    const wrapper = await importBatch()

    await wrapper.find('[data-testid="retry-document-42"]').trigger('click')
    await settle(wrapper)

    expect(wrapper.find('[data-testid="batch-document-42"]').attributes('data-status')).toBe('failed')
    expect(wrapper.find('[data-testid="retry-document-42"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('Indexing failed; retry is available')
    expect(wrapper.text()).not.toContain('raw embedding endpoint failure')
    expect(wrapper.text()).not.toContain('private provider response')
    wrapper.unmount()
  })

  it('does not render raw errors when progress lookup fails', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 1, accepted: 1, rejected: 0, document_ids: [42] })
    api.getDocumentProgress.mockRejectedValue(new Error('private progress endpoint detail'))
    const wrapper = await importBatch()

    expect(wrapper.text()).toContain('Could not load status; refresh progress to try again')
    expect(wrapper.text()).not.toContain('private progress endpoint detail')
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.getDocumentProgress).toHaveBeenCalledTimes(1)

    api.getDocumentProgress.mockResolvedValue({ id: 42, embed_status: 'indexed', embed_progress: 100 })
    await wrapper.findAll('button').find(button => button.text().includes('Refresh progress')).trigger('click')
    await settle(wrapper)
    expect(wrapper.find('[data-testid="batch-document-42"]').attributes('data-status')).toBe('indexed')
    wrapper.unmount()
  })

  it('stops progress polling on unmount and ignores a late response', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 6, accepted: 6, rejected: 0, document_ids: [42, 43, 44, 45, 46, 47] })
    const resolveProgress = []
    let inFlight = 0
    let maxInFlight = 0
    api.getDocumentProgress.mockImplementation(() => {
      inFlight++
      maxInFlight = Math.max(maxInFlight, inFlight)
      return new Promise(resolve => resolveProgress.push((value) => { inFlight--; resolve(value) }))
    })
    const wrapper = await importBatch()
    expect(api.getDocumentProgress).toHaveBeenCalledTimes(4)
    expect(maxInFlight).toBe(4)
    wrapper.unmount()
    resolveProgress.forEach((resolve, index) => resolve({ id: 42 + index, embed_status: 'processing' }))
    await settle(wrapper)
    await vi.advanceTimersByTimeAsync(10000)
    expect(api.getDocumentProgress).toHaveBeenCalledTimes(4)
  })

  it('stops progress polling when the result card is closed', async () => {
    vi.useFakeTimers({ toFake: ['setTimeout', 'clearTimeout'] })
    api.batchImport.mockResolvedValue({ batch_no: 'b', total: 6, accepted: 6, rejected: 0, document_ids: [42, 43, 44, 45, 46, 47] })
    const resolveProgress = []
    api.getDocumentProgress.mockImplementation(() => new Promise(resolve => resolveProgress.push(resolve)))
    const wrapper = await importBatch()
    expect(api.getDocumentProgress).toHaveBeenCalledTimes(4)

    await wrapper.findAll('button').find(button => button.text().includes('关闭')).trigger('click')
    resolveProgress.forEach((resolve, index) => resolve({ id: 42 + index, embed_status: 'processing' }))
    await settle(wrapper)
    await vi.advanceTimersByTimeAsync(10000)

    expect(api.getDocumentProgress).toHaveBeenCalledTimes(4)
    expect(wrapper.find('[data-testid="batch-document-42"]').exists()).toBe(false)
    wrapper.unmount()
  })

  it('shows string rejection details and says accepted while indexing continues', async () => {
    api.batchImport.mockResolvedValue({
      batch_no: 'batch-1', total: 2, accepted: 1, rejected: 1,
      errors: ['content is required']
    })
    const wrapper = mount(BatchImport, { global: { plugins: [ElementPlus, i18n] } })
    await settle(wrapper)

    await wrapper.findAll('.el-tabs__item').find(tab => tab.text().includes('JSON 粘贴')).trigger('click')
    await settle(wrapper)
    await wrapper.find('textarea').setValue('[{"title":"FAQ","content":"answer"}]')
    await wrapper.findAll('button').find(button => button.text().includes('解析预览')).trigger('click')
    const selects = wrapper.findAllComponents({ name: 'ElSelect' })
    await selects[1].setValue('product-1')
    await settle(wrapper)
    await wrapper.findAll('button').find(button => button.text().includes('确认导入')).trigger('click')
    await settle(wrapper)

    expect(wrapper.text()).toContain('content is required')
    expect(wrapper.text()).toContain('still being indexed in the background')
    expect(wrapper.text()).toContain('Accepted')
    expect(wrapper.text()).not.toContain('导入完成')
    wrapper.unmount()
  })
})
