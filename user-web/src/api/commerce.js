import { http } from '@/utils/request'

const base = '/api/v1/commerce'
const operation = (id) => `${base}/operations/${encodeURIComponent(id)}`

export const listCommerceProfiles = () => http.get(`${base}/profiles`)
export const listCommerceOrders = (params) => http.get(`${base}/orders`, params)
export const listCommerceOperations = (params) => http.get(`${base}/operations`, params)
export const getCommerceOperation = (id) => http.get(operation(id))
export const createCommerceOperation = (data) => http.post(`${base}/operations`, data)
export const preflightCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/preflight`, data)
export const approveCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/approve`, data)
export const submitCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/submit`, data)
export const observeCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/observe`, data)
export const cancelCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/cancel`, data)
export const claimCommerceOperation = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/claim`, data)
export const appendCommerceEvidence = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/evidence`, data)
export const reviewCommerceEvidence = (id, data) => http.post(`${base}/operations/${encodeURIComponent(id)}/review`, data)
export const updateCommercePolicy = (data) => http.put(`${base}/policy`, data)
export const updateCommerceMember = (data) => http.put(`${base}/members`, data)
