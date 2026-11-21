import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle, settleBody } from './settle-helper'
import i18n from '@/i18n'

// UWA-23：线索页「分配」「编辑」此前只弹 toast（"已触发分配"），没有任何指派
// 或编辑界面 —— 页面副标题承诺的"分配"是空壳。接入真实 PUT /api/clues/:id 后，
// 本文件钉住三条交互契约：编辑弹窗预填并按白名单提交；分配弹窗提交跟进人账号；
// 名称为空/跟进人为空不发请求。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  statistics: vi.fn(),
  update: vi.fn(),
  delete: vi.fn(),
  import: vi.fn(),
}))

vi.mock('@/api/clue', () => ({ clueApi: api }))

import ClueList from '@/views/clue/List.vue'

const mounted = []

const row = (over = {}) => ({
  id: 'clue-1',
  name: '审计线索',
  account: 'acc-1',
  type: 1,
  city: '北京',
  address: '朝阳区',
  desc: '备注内容',
  source_id: 'baidu_ad',
  is_verify: 0,
  owner_account: '',
  create_time: '2026-11-08 08:48',
  updated_at: '2026-11-08 08:48',
  ...over,
})

async function mountPage() {
  const wrapper = mount(ClueList, {
    attachTo: document.body,
    global: { plugins: [ElementPlus, i18n] }
  })
  mounted.push(wrapper)
  await settle(wrapper)
  return wrapper
}

const rowButtons = (wrapper, label) =>
  wrapper.findAll('.el-table__body button').filter(b => b.text().trim() === label)

beforeEach(() => {
  api.list.mockResolvedValue({ list: [row()], total: 1 })
  api.statistics.mockResolvedValue({ total: 1, verified: 0, unverified: 1, today: 0 })
  api.update.mockReset().mockResolvedValue({})
  api.delete.mockReset()
  api.import.mockReset()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
})

describe('线索编辑/分配弹窗（UWA-23）', () => {
  it('编辑：预填当前行字段，保存按白名单提交并刷新列表', async () => {
    const wrapper = await mountPage()
    await rowButtons(wrapper, '编辑')[0].trigger('click')
    await settleBody()

    const dialog = document.querySelector('.el-dialog')
    expect(dialog).not.toBeNull()
    expect(dialog.textContent).toContain(i18n.global.t('编辑线索'))
    const inputs = [...dialog.querySelectorAll('.el-input__inner, textarea')]
    expect(inputs[0].value).toBe('审计线索')

    api.update.mockClear()
    api.list.mockClear()
    const save = [...dialog.querySelectorAll('button')].find(b => b.textContent.trim() === i18n.global.t('保存'))
    save.click()
    await settleBody()

    expect(api.update).toHaveBeenCalledTimes(1)
    expect(api.update).toHaveBeenCalledWith('clue-1', {
      name: '审计线索',
      city: '北京',
      address: '朝阳区',
      desc: '备注内容'
    })
    expect(api.list).toHaveBeenCalled()
  })

  it('分配：提交跟进人账号（owner_account），成功后刷新', async () => {
    const wrapper = await mountPage()
    await rowButtons(wrapper, '分配')[0].trigger('click')
    await settleBody()

    const dialog = document.querySelector('.el-dialog')
    expect(dialog.textContent).toContain(i18n.global.t('分配线索'))
    const input = dialog.querySelector('input')
    input.value = 'agent-9'
    input.dispatchEvent(new Event('input', { bubbles: true }))

    api.update.mockClear()
    api.list.mockClear()
    const confirm = [...dialog.querySelectorAll('button')].find(b => b.textContent.trim() === i18n.global.t('确认分配'))
    confirm.click()
    await settleBody()

    expect(api.update).toHaveBeenCalledTimes(1)
    expect(api.update).toHaveBeenCalledWith('clue-1', { owner_account: 'agent-9' })
    expect(api.list).toHaveBeenCalled()
  })

  it('名称为空 / 跟进人为空：不发请求', async () => {
    const wrapper = await mountPage()

    await rowButtons(wrapper, '编辑')[0].trigger('click')
    await settleBody()
    let dialog = document.querySelector('.el-dialog')
    const nameInput = dialog.querySelector('.el-input__inner')
    nameInput.value = ''
    nameInput.dispatchEvent(new Event('input', { bubbles: true }))
    const save = [...dialog.querySelectorAll('button')].find(b => b.textContent.trim() === i18n.global.t('保存'))
    save.click()
    await settleBody()
    expect(api.update).not.toHaveBeenCalled()

    await rowButtons(wrapper, '分配')[0].trigger('click')
    await settleBody()
    // 编辑弹窗仍开着（querySelector 取 DOM 第一个），按标题定位分配弹窗
    const assignDialog = [...document.querySelectorAll('.el-dialog')]
      .find(d => d.textContent.includes(i18n.global.t('分配线索')))
    expect(assignDialog).not.toBeUndefined()
    const confirm = [...assignDialog.querySelectorAll('button')].find(b => b.textContent.trim() === i18n.global.t('确认分配'))
    confirm.click()
    await settleBody()
    expect(api.update).not.toHaveBeenCalled()
  })
})
