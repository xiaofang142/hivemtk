import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createPinia } from 'pinia'
import { settle } from './settle-helper'
const api = vi.hoisted(() => ({ upload: vi.fn(), list: vi.fn(), categories: vi.fn(), create: vi.fn(), update: vi.fn() }))
vi.mock('@/api/material', () => ({ getMaterialList: api.list, getMaterialCategories: api.categories, uploadMaterial: api.upload, deleteMaterial: vi.fn(), createMaterialCategory: api.create, updateMaterialCategory: api.update, deleteMaterialCategory: vi.fn() }))
import Material from '@/views/system/MaterialLibrary.vue'
import i18n from '@/i18n'
let wrapper
beforeEach(() => { vi.clearAllMocks(); api.list.mockResolvedValue({ list: [], total: 0 }); api.categories.mockResolvedValue({ list: [{ id: 1, name: '分类' }] }) })
afterEach(() => { wrapper?.unmount(); ElMessage.closeAll() })
async function page() {
  wrapper = mount(Material, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
  await settle(wrapper)
  wrapper.vm.uploadForm.categoryId = 1
}
const file = (name) => ({ uid: name, name, raw: new File(['content'], name) })
describe('素材手工上传', () => {
  it('实际提交前校验文件大小，超限不请求', async () => {
    await page()
    wrapper.vm.fileList = [{ raw: { size: 11 * 1024 * 1024 } }]
    const error = vi.spyOn(ElMessage, 'error')
    await wrapper.vm.handleUploadSubmit()
    expect(api.upload).not.toHaveBeenCalled()
    expect(error).toHaveBeenCalledWith(expect.stringContaining('10MB'))
    error.mockRestore()
  })
  it('部分成功后重试只上传剩余失败文件', async () => {
    await page()
    const first = file('first.pdf'), second = file('second.pdf')
    wrapper.vm.fileList = [first, second]
    api.upload.mockResolvedValueOnce({ id: 1 }).mockRejectedValueOnce(new Error('controlled failure')).mockResolvedValue({ id: 2 })
    await wrapper.vm.handleUploadSubmit()
    expect(api.upload).toHaveBeenCalledTimes(2)
    await wrapper.vm.handleUploadSubmit()
    expect(api.upload).toHaveBeenCalledTimes(3)
    expect(api.upload.mock.calls[2][0].get('file').name).toBe('second.pdf')
  })
  it('双击上传只提交一次文件', async () => {
    await page()
    let resolve
    api.upload.mockReturnValueOnce(new Promise(r => { resolve = r }))
    wrapper.vm.fileList = [file('only.pdf')]
    const first = wrapper.vm.handleUploadSubmit()
    await wrapper.vm.handleUploadSubmit()
    expect(api.upload).toHaveBeenCalledTimes(1)
    resolve({ id: 1 }); await first
  })
})


describe('素材分类保存', () => {
  it('新建分类补齐后端要求的默认素材类型', async () => {
    await page()
    api.create.mockResolvedValue({ id: 'cat-1', name: 'AI数智学堂', type: 'image' })
    wrapper.vm.handleAddCategory()
    wrapper.vm.categoryForm.name = 'AI数智学堂'
    await wrapper.vm.handleSaveCategory()
    expect(api.create).toHaveBeenCalledWith({ id: null, name: 'AI数智学堂', type: 'image' })
    expect(api.update).not.toHaveBeenCalled()
  })

  it('编辑分类不擅自覆盖既有素材类型', async () => {
    await page()
    api.update.mockResolvedValue({ id: 'video-cat', name: '视频资料', type: 'video' })
    wrapper.vm.handleEditCategory({ id: 'video-cat', name: '视频资料', type: 'video' })
    await wrapper.vm.handleSaveCategory()
    expect(api.update).toHaveBeenCalledWith('video-cat', { id: 'video-cat', name: '视频资料' })
    expect(api.create).not.toHaveBeenCalled()
  })
})
