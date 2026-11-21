import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settleBody } from './settle-helper'

const payStatus = vi.hoisted(() => vi.fn())
vi.mock('@/api/moduleMarket', () => ({ orderPayStatus: payStatus }))
import CashierDialog from '@/components/CashierDialog.vue'

describe('CashierDialog 轮询生命周期', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    payStatus.mockReset()
    payStatus.mockResolvedValue({ status: 'pending' })
  })
  const mounted = []
  afterEach(() => {
    while (mounted.length) mounted.pop().unmount()
    document.body.innerHTML = ''
    vi.useRealTimers()
  })

  function mountDialog(props = {}) {
    const wrapper = mount(CashierDialog, {
      props: {
        modelValue: true,
        orderNo: 'order-a',
        payment: { pay_url: 'https://pay.invalid' },
        ...props
      },
      global: { plugins: [ElementPlus, createPinia()] },
      attachTo: document.body
    })
    mounted.push(wrapper)
    return wrapper
  }

  it('同一订单请求未完成时不会重叠轮询', async () => {
    let resolve
    payStatus.mockReturnValueOnce(new Promise((r) => { resolve = r }))
    mountDialog()
    await Promise.resolve()
    expect(payStatus).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(payStatus).toHaveBeenCalledTimes(1)
    resolve({ status: 'pending' })
    await Promise.resolve()
    await vi.advanceTimersByTimeAsync(3000)
    expect(payStatus).toHaveBeenCalledTimes(2)
  })

  it('关闭收银台后迟到的已支付响应不会发出 paid', async () => {
    let resolve
    payStatus.mockReturnValueOnce(new Promise((r) => { resolve = r }))
    const wrapper = mountDialog()
    await Promise.resolve()
    await wrapper.setProps({ modelValue: false })
    resolve({ status: 'paid' })
    await settleBody()
    expect(wrapper.emitted('paid')).toBeUndefined()
  })

  it('切换订单后旧订单已支付响应不会污染新订单', async () => {
    let resolveOld
    payStatus.mockReturnValueOnce(new Promise((r) => { resolveOld = r }))
    const wrapper = mountDialog()
    await Promise.resolve()
    await wrapper.setProps({ orderNo: 'order-b' })
    resolveOld({ status: 'paid' })
    await settleBody()
    expect(wrapper.emitted('paid')).toBeUndefined()
    expect(payStatus).toHaveBeenLastCalledWith('order-b', { _silent: true })
  })
})
