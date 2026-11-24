// FA-WF-10/11：确认取消判定 + 明确的错误消息生成（纯函数单测，不依赖页面/网络）。
import { describe, it, expect } from 'vitest'
import { isConfirmCancel, actionErrorMessage } from '../../src/utils/workflowAction.js'

describe('isConfirmCancel', () => {
  it('Element Plus 取消/关闭字符串 → true', () => {
    expect(isConfirmCancel('cancel')).toBe(true)
    expect(isConfirmCancel('close')).toBe(true)
  })
  it('取消消息（含 cancel/close）→ true', () => {
    expect(isConfirmCancel(new Error('cancel'))).toBe(true)
    expect(isConfirmCancel({ message: 'user close dialog' })).toBe(true)
  })
  it('普通错误/网络错误 → false', () => {
    expect(isConfirmCancel(new Error('Network Error'))).toBe(false)
    expect(isConfirmCancel({ message: '500 Internal Server Error' })).toBe(false)
    expect(isConfirmCancel(undefined)).toBe(false)
    expect(isConfirmCancel(null)).toBe(false)
  })
})

describe('actionErrorMessage', () => {
  it('带 message → label失败: detail', () => {
    expect(actionErrorMessage('停止', new Error('Network Error'))).toBe('停止失败: Network Error')
  })
  it('带 response.data.message → 用之', () => {
    expect(actionErrorMessage('发布', { response: { data: { message: '无权限' } } })).toBe('发布失败: 无权限')
  })
  it('无 detail → 通用重试提示', () => {
    expect(actionErrorMessage('删除', undefined)).toBe('删除失败，请稍后重试')
  })
})
