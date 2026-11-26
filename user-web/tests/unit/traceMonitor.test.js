import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createI18n } from 'vue-i18n'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settle } from './settle-helper'
import zh from '../../src/i18n/locales/zh.json'

const api = vi.hoisted(() => ({ traceTree: vi.fn() }))
vi.mock('@/api/monitor', () => ({ MonitorApi: api }))

import TraceMonitor from '@/views/system/TraceMonitor.vue'

describe('链路明细真实渲染', () => {
  let wrapper
  afterEach(() => { wrapper?.unmount(); vi.clearAllMocks() })

  it('查询后未测展示横线、实测零展示0ms、跳过不标为正常', async () => {
    api.traceTree.mockResolvedValue({ trace_id: 'same-session', turn_id: 'turn-one', account_id: 'account', channel: 'douyin', spans: [
      { id: 1, node: 'ingest', status: 'ok', duration_ms: 0, created_at: '2026-11-16T07:00:00Z' },
      { id: 2, node: 'ai_result', status: 'skipped', duration_ms: 0, duration_measured: true, timing_version: 1, started_at: '2026-11-16T07:00:01Z' }
    ] })
    const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
    wrapper = mount(TraceMonitor, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
    await wrapper.find('input[placeholder="turn_id"]').setValue('turn-one')
    await wrapper.find('input[placeholder="account_id"]').setValue('account')
    const query = wrapper.findAll('button').find(button => button.text() === '查询')
    expect(query).toBeTruthy()
    await query.trigger('click')
    await settle(wrapper)
    expect(api.traceTree).toHaveBeenCalledExactlyOnceWith({ turn_id: 'turn-one', account_id: 'account' })
    expect(wrapper.findAll('.span-dur').map(node => node.text())).toEqual(['—', '0 ms'])
    const cards = wrapper.findAll('.span-card')
    expect(cards[1].find('.el-tag').text()).toBe('跳过')
    expect(cards[1].find('.el-tag').classes()).toContain('el-tag--info')
    expect(cards[0].find('.span-time').text()).not.toContain('07:00')
    expect(wrapper.text()).toContain('turn-one')
  })
})
