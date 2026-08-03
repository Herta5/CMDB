import request from '@/utils/request'

export function getChanges(params?: any)          { return request.get('/changes', { params }) }
export function getChange(id: number)              { return request.get(`/changes/${id}`) }
export function createChange(data: any)            { return request.post('/changes', data) }
export function updateChange(id: number, data: any){ return request.put(`/changes/${id}`, data) }
export function deleteChange(id: number)           { return request.delete(`/changes/${id}`) }
export function submitChange(id: number)           { return request.post(`/changes/${id}/submit`) }
export function approveChange(id: number, data: { approved_by: string }) { return request.post(`/changes/${id}/approve`, data) }
export function rejectChange(id: number, data: { rejected_by: string })  { return request.post(`/changes/${id}/reject`, data) }
export function executeChange(id: number, data: { executed_by: string }) { return request.post(`/changes/${id}/execute`, data) }
export function completeChange(id: number)         { return request.post(`/changes/${id}/complete`) }
export function rollbackChange(id: number)         { return request.post(`/changes/${id}/rollback`) }
export function failChange(id: number)             { return request.post(`/changes/${id}/fail`) }