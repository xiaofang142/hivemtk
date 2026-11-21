import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import CapabilityAdminPanel from '@/views/cap/CapabilityAdminPanel.vue'
import en from '@/i18n/locales/en.json'
import zh from '@/i18n/locales/zh.json'

const api = vi.hoisted(() => ({ listCapabilityCells: vi.fn(), getCapabilityCell: vi.fn() }))
vi.mock('@/api/capabilityAdmin', () => api)

const textCell = { product: 'douyin_shop', region: 'CN', action: 'message.text.send', carrier: 'bridge' }
const workbench = { product: 'jd_pop', region: 'CN', action: 'aftersale.write', carrier: 'official_ui' }
const observation = { product: 'vipshop', region: 'CN', action: 'refund.observe', carrier: 'platform_managed' }
const keyFor = ({ product, region, action, carrier }) => [product, region, action, carrier].join('/')
const validUntil = new Date(Date.now() + 300_000).toISOString()
const responseFor = (cell, shop = 'shop-a', extra = {}) => ({
  ...cell,
  tenant_id: 71, shop_id: shop, execution_mode: cell.carrier, connection_state: 'unconfigured',
  status: 'blocked', production_allowed: false, required_scopes: ['message.text.send'], contract_version: 'bridge-v1',
  documentation_version: 'design-cell-v1', state_limits: [], reason_code: 'shop_evidence_missing',
  owner: 'tenant_admin', next_step: 'configure_connection', valid_until: validUntil, ...extra
})

let wrapper
let capturedBlob
beforeEach(() => {
  api.listCapabilityCells.mockReset().mockResolvedValue([textCell, workbench, observation])
  api.getCapabilityCell.mockReset()
  capturedBlob = undefined
  vi.stubGlobal('URL', {
    createObjectURL: vi.fn(blob => { capturedBlob = blob; return 'blob:test' }),
    revokeObjectURL: vi.fn()
  })
  vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})
})
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

async function openPanel(locale = 'en') {
  wrapper = mount(CapabilityAdminPanel, {
    global: {
      plugins: [ElementPlus, createI18n({ legacy: false, locale, messages: { en, zh } })],
      stubs: {
        ElSelect: { template: '<select :value="modelValue" @change="$emit(\'update:modelValue\', $event.target.value)"><option value=""></option><slot /></select>', props: ['modelValue'] },
        ElOption: { template: '<option :value="value">{{ label }}</option>', props: ['value', 'label'] }
      }
    }
  })
  await settle(wrapper)
  await wrapper.get('[data-test="capability-shop"]').setValue('shop-a')
}

const selectCell = (cell) => wrapper.get('select').setValue(keyFor(cell))
const resolve = () => wrapper.get('[data-test="capability-resolve"]').trigger('click')
const download = () => wrapper.get('[data-test="capability-export"]')
const exportDisabled = () => download().element.disabled

function readBlob(blob) {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result)
    reader.onerror = reject
    reader.readAsText(blob)
  })
}

it('lists exact registered tuples and exports the same server projection shown for the entered shop', async () => {
  const result = responseFor(textCell, 'shop-a', {
    connection_state: 'reauth', reason_code: 'permission_revoked', next_step: 'reconnect',
    observed_scopes: ['message.text.send'], observed_version: 'previous-version', observed_documentation_version: 'previous-docs'
  })
  api.getCapabilityCell.mockResolvedValue(result)
  await openPanel()
  expect(wrapper.findAll('option').map(option => option.element.value)).toEqual(['', ...[textCell, workbench, observation].map(keyFor)])
  expect(exportDisabled()).toBe(true)
  await selectCell(textCell)
  await resolve()
  await settle(wrapper)

  expect(api.getCapabilityCell).toHaveBeenCalledWith({ ...textCell, shop_id: 'shop-a' })
  for (const text of ['message.text.send', 'Bridge', 'Reconnect required', 'Blocked', 'Tenant administrator', 'Reconnect the shop', 'previous-version', 'previous-docs', 'design-cell-v1', 'permission_revoked']) {
    expect(wrapper.text()).toContain(text)
  }
  await download().trigger('click')
  expect(JSON.parse(await readBlob(capturedBlob))).toEqual(result)
})

it.each([
  [workbench, 'merchant_operator', 'use_official_workbench', '商户操作员', '官方工作台'],
  [observation, 'platform', 'observe_platform_result', '平台', '观察平台处理结果']
])('shows the declared mode and responsibility for $carrier while the decision stays blocked', async (cell, owner, next, ownerText, nextText) => {
  api.getCapabilityCell.mockResolvedValue(responseFor(cell, 'shop-a', { owner, next_step: next, reason_code: next }))
  await openPanel('zh')
  await selectCell(cell)
  await resolve()
  await settle(wrapper)
  expect(wrapper.text()).toContain(ownerText)
  expect(wrapper.text()).toContain(nextText)
  expect(wrapper.text()).toContain('未就绪')
})

it('discards superseded responses and clears the export on edits or failed refreshes', async () => {
  let finishOld
  api.getCapabilityCell.mockImplementationOnce(() => new Promise(resolve => { finishOld = resolve }))
  api.getCapabilityCell.mockResolvedValueOnce(responseFor(workbench, 'shop-b'))
  api.getCapabilityCell.mockRejectedValueOnce(new Error('lookup failed'))
  await openPanel()
  await selectCell(textCell)
  await resolve()

  await wrapper.get('[data-test="capability-shop"]').setValue('shop-b')
  await selectCell(workbench)
  expect(exportDisabled()).toBe(true)
  await resolve()
  await settle(wrapper)
  expect(wrapper.get('[data-test="capability-result"]').text()).toContain('shop-b')
  finishOld(responseFor(textCell))
  await settle(wrapper)
  await download().trigger('click')
  expect(JSON.parse(await readBlob(capturedBlob))).toEqual(responseFor(workbench, 'shop-b'))

  await resolve()
  expect(exportDisabled()).toBe(true)
  await settle(wrapper)
  expect(exportDisabled()).toBe(true)
  expect(wrapper.find('[data-test="capability-result"]').exists()).toBe(false)
  expect(wrapper.text()).toContain('Could not resolve this capability')
})

it('drops an expired summary and does not enable export for an already stale response', async () => {
  const result = responseFor(textCell, 'shop-a', { valid_until: new Date(Date.now() + 60_000).toISOString() })
  api.getCapabilityCell.mockResolvedValue(result)
  await openPanel()
  await selectCell(textCell)
  vi.useFakeTimers()
  try {
    await resolve()
    expect(exportDisabled()).toBe(false)
    await vi.advanceTimersByTimeAsync(60_000)
    expect(exportDisabled()).toBe(true)
    expect(wrapper.text()).toContain('This summary expired')
    await resolve()
    expect(exportDisabled()).toBe(true)
  } finally {
    vi.useRealTimers()
  }
})
