import { createI18n } from 'vue-i18n'
import zh from '../../../src/i18n/locales/zh.json'

export const fixtureI18n = createI18n({ legacy: false, locale: 'zh', messages: { zh } })
export default fixtureI18n

async function request(path, method = 'GET', data) {
  const response = await fetch(`/fixture${path}`, {
    method, headers: { 'Content-Type': 'application/json' },
    body: data === undefined ? undefined : JSON.stringify(data),
  })
  const value = await response.json()
  if (!response.ok) throw new Error(value.error || `本地模拟接口 HTTP ${response.status}`)
  return value
}
export const http = {
  get: (path, params) => request(`${path}${params ? `${path.includes('?') ? '&' : '?'}${new URLSearchParams(params)}` : ''}`),
  post: (path, data) => request(path, 'POST', data),
}
