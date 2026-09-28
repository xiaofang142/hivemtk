/**
 * Host 状态页的三个读数：连上没有、能不能干活、最后一次干成活是什么时候。
 *
 * 走查实测到的形态：状态那一列写死「在线」——模板连 row 都没接（<template #default> 直接出标签），
 * 于是 servable=false（WS 注册在场、但扩展侧没有一次成功回包）的 Host 也显示绿色「在线」，
 * 用户按这个读数去点执行，然后拿到 8001。接口本来就把 servable 与 last_cmd_ok_at 都给了，
 * 这一页只是没读。
 * 另一侧：空列表的安装指引指向 user-web/browser_automation/dist，而 dist 是构建产物、
 * 不在版本控制里（实测 git ls-files 命中 0 个文件）——照抄指引的新机器那一步根本选不到目录。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({ getBrowserHostStatus: vi.fn(), resetBrowserHostToken: vi.fn() }))
vi.mock('@/api/browserAutomation', () => api)

import Status from '@/views/browserAutomation/Status.vue'

const HOST = {
  online: true, servable: true, user_id: 1, version: '1.4.0', pid: 4242,
  last_cmd_ok_at: '2026-09-28T10:20:30+08:00',
}

async function mountStatus() {
  const wrapper = mount(Status, { global: { plugins: [ElementPlus] } })
  for (let i = 0; i < 3; i++) await flushPromises()
  return wrapper
}

describe('Host 状态页的读数', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.resetBrowserHostToken.mockResolvedValue({ token: 't' })
  })
  afterEach(() => vi.restoreAllMocks())

  it('连上但不可服务：不许显示成「在线」了事', async () => {
    api.getBrowserHostStatus.mockResolvedValue({
      online: true, servable: false, count: 1, last_cmd_ok_at: null,
      hosts: [{ ...HOST, servable: false, last_cmd_ok_at: null }],
    })
    const wrapper = await mountStatus()
    expect(wrapper.text(), 'servable=false 的 Host 页面上找不到「不可服务」').toContain('不可服务')
    // 无条件的那枚绿色「在线」必须没了：状态列要按 row 说话
    expect(wrapper.findAll('.el-tag').map((t) => t.text())).not.toContain('在线')
    wrapper.unmount()
  })

  it('可服务且有回包证据：把最近成功时间这一格供出来', async () => {
    api.getBrowserHostStatus.mockResolvedValue({
      online: true, servable: true, count: 1, last_cmd_ok_at: HOST.last_cmd_ok_at,
      hosts: [HOST],
    })
    const wrapper = await mountStatus()
    expect(wrapper.text()).toContain('可服务')
    expect(wrapper.text(), '有 last_cmd_ok_at 却不显示，用户无法区分「刚成过」与「从没成过」').toContain('最近成功')
    wrapper.unmount()
  })

  it('接口顶层读数与列表一致时也要说出来（servable 汇总自每台 Host）', async () => {
    api.getBrowserHostStatus.mockResolvedValue({
      online: false, servable: false, count: 0, last_cmd_ok_at: null, hosts: [],
    })
    const wrapper = await mountStatus()
    expect(wrapper.text()).toContain('没有 Host')
    wrapper.unmount()
  })

  it('空列表的安装指引包含构建扩展这一步', async () => {
    api.getBrowserHostStatus.mockResolvedValue({
      online: false, servable: false, count: 0, last_cmd_ok_at: null, hosts: [],
    })
    const wrapper = await mountStatus()
    // dist 不在版本控制里：只说「加载 dist」等于让新机器第一步就卡住
    expect(wrapper.text()).toContain('npm run build')
    wrapper.unmount()
  })

  it('列表里的每台 Host 也带上扩展 ID 的确认路径（引导与真实安装一致）', async () => {
    api.getBrowserHostStatus.mockResolvedValue({
      online: false, servable: false, count: 0, last_cmd_ok_at: null, hosts: [],
    })
    const wrapper = await mountStatus()
    // install.sh 自己从 manifest 的固定 key 推导 ID，指引不该再让用户手抄一个 ID 参数
    expect(wrapper.text()).toContain('install.sh')
    expect(wrapper.text(), '指引还要求手抄扩展 ID').not.toContain('<扩展ID>')
    wrapper.unmount()
  })
})
