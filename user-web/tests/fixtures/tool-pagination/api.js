async function request(path, options) {
  const response = await fetch(`/fixture${path}`, options)
  if (!response.ok) throw new Error(`受控接口 HTTP ${response.status}`)
  return response.json()
}

export const listTools = (params) => request(`/tools?${new URLSearchParams(params)}`)
export const updateToolStatus = (name, enabled) => request(`/tools/${name}/status`, {
  method: 'PUT', headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({ is_enabled: enabled }),
})
export const bindToolAccount = () => Promise.reject(new Error('本夹具不模拟账号绑定'))
