import request from '@/utils/request'

export function getCIInstances(params: any)   { return request.get('/ci-instances', { params }) }
export function getCIInstance(id: number)     { return request.get(`/ci-instances/${id}`) }
export function createCIInstance(data: any)   { return request.post('/ci-instances', data) }
export function updateCIInstance(id: number, data: any) { return request.put(`/ci-instances/${id}`, data) }
export function deleteCIInstance(id: number)  { return request.delete(`/ci-instances/${id}`) }
export function updateStatus(id: number, status: string) { return request.patch(`/ci-instances/${id}/status`, { status }) }