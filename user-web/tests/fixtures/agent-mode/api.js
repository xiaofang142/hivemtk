async function request(path, method = 'GET', data) {
  const response = await fetch(`/fixture${path}`, {
    method, headers: { 'Content-Type': 'application/json' },
    body: data === undefined ? undefined : JSON.stringify(data),
  })
  const value = await response.json()
  if (!response.ok) throw new Error(value.error || `受控接口 HTTP ${response.status}`)
  return value
}
export const createAgent = data => request('/agents', 'POST', data)
export const updateAgent = (id, data) => request(`/agents/${id}`, 'PUT', data)
export const getAgent = id => request(`/agents/${id}`)
export const testAgent = async () => { throw new Error('此夹具不运行外部智能体') }
const emptyList = async () => ({ list: [] })
export const ragProductConfigAPI = { getRagProducts: emptyList }
export const sopApi = { list: emptyList }
export const faqApi = { list: emptyList }
export const sopTemplateApi = { list: emptyList }
export const getScriptTemplateList = emptyList
export const listByType = emptyList
export const listByAgent = async () => []
export const replaceAgentKBs = async () => ({})
export const listBundles = emptyList
export const listLocalAssets = emptyList
export default { global: { t: key => key } }
