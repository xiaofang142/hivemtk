import { http } from '@/utils/request'

export const createCaptureLink = (data) => http.post('/api/geo/capture/links', data)

export const listCaptureLinks = (params = {}) => http.get('/api/geo/capture/links', params)

export const getCaptureLink = (id) => http.get(`/api/geo/capture/links/${id}`)

export const deleteCaptureLink = (id) => http.delete(`/api/geo/capture/links/${id}`)
