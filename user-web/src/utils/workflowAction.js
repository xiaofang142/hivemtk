// FA-WF-10/11：Element Plus 的 ElMessageBox.confirm 在用户取消时抛出
// { message: 'cancel' } 或 'close'（取决于关闭方式）；而后端/网络失败抛 Error。
// 这两个 handler 场景必须区分二者：取消是正常用户意图（静默），API 失败必须
// 显式报错并回读状态，不能被同一个空 catch 一起吞掉。
export function isConfirmCancel(e) {
  if (e === 'cancel' || e === 'close') return true
  const msg = e?.message
  return typeof msg === 'string' && (msg.includes('cancel') || msg.includes('close'))
}

// 生成明确的失败消息，避免空 catch 后用户看不到任何反馈。
export function actionErrorMessage(label, e) {
  const detail = e?.message || e?.response?.data?.message || ''
  return detail ? `${label}失败: ${detail}` : `${label}失败，请稍后重试`
}
