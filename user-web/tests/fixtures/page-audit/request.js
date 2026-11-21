async function request(method, path, params, body) {
  const url = new URL(path, location.origin)
  for (const [key, value] of Object.entries(params?.params || params || {})) {
    if (value !== undefined) url.searchParams.set(key, String(value))
  }
  const response = await fetch(url, { method, ...(body === undefined ? {} : { headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) }) })
  const envelope = await response.json()
  if (!response.ok || envelope.code !== 0) throw new Error(envelope.message)
  return envelope.data
}
export const http = {
  get: (path, params) => request('GET', path, params),
  post: (path, body) => request('POST', path, null, body),
  put: (path, body) => request('PUT', path, null, body),
  delete: (path) => request('DELETE', path),
  upload: (path, body) => request('POST', path, null, body),
}
