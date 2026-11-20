const MAX_AGE_MS = 5 * 60 * 1000
let pendingSelection

export function leadSelectionScope(user) {
  const tenant = String(user?.tenant_id ?? '').trim()
  const account = String(user?.id ?? '').trim()
  return tenant && tenant !== '0' && account && account !== '0'
    ? JSON.stringify([tenant, account]) : ''
}

export function stageLeadSelection(ids, user) {
  pendingSelection = undefined
  const scope = leadSelectionScope(user)
  const normalized = [...new Set(ids.map(id => typeof id === 'string' || typeof id === 'number' ? String(id).trim() : ''))]
  if (!scope || !normalized.length || normalized.some(id => !id || id === '0')) return null
  const token = crypto.randomUUID()
  pendingSelection = { token, scope, ids: normalized, expires: Date.now() + MAX_AGE_MS }
  return token
}

export function takeLeadSelection(token, user) {
  const pending = pendingSelection
  if (!pending || typeof token !== 'string' || pending.token !== token) return null
  pendingSelection = undefined
  if (pending.scope !== leadSelectionScope(user) || Date.now() >= pending.expires) return null
  return pending.ids
}

export function discardLeadSelection(token) {
  if (pendingSelection?.token === token) pendingSelection = undefined
}

export async function fetchBulkLeads(list, selectedIds = []) {
  const leads = new Map()
  // A selection may come from any source page; resolve it against current tenant data.
  for (let page = 1; page <= 100; page++) {
    const response = await list(page, 1000)
    const rows = Array.isArray(response) ? response : response?.list
    if (!Array.isArray(rows)) throw new Error('Invalid lead list')
    const previousSize = leads.size
    for (const row of rows) {
      const id = row.id ?? row.ID
      if (id === undefined || id === null || String(id).trim() === '') continue
      const name = row.name ?? row.Name ?? ''
      const phone = row.account ?? row.Account ?? ''
      leads.set(String(id), { id: String(id), name: String(name), phone: String(phone),
        email: '', company: row.address ?? row.Address ?? row.city ?? row.City ?? '', source: 'clue' })
    }
    if (!selectedIds.length || selectedIds.every(id => leads.has(String(id)))) break
    if (leads.size === previousSize || rows.length === 0 || leads.size >= Number(response?.total ?? rows.length)) break
  }
  const resolved = selectedIds.map(id => leads.get(String(id)))
  if (resolved.some(lead => !lead || !lead.phone.trim())) throw new Error('Selected leads unavailable')
  return [...leads.values()]
}
