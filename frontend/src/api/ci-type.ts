import request from '@/utils/request'

export function getCITypeTree()            { return request.get('/ci-types') }
export function getCIType(id: number)      { return request.get(`/ci-types/${id}`) }
export function createCIType(data: any)    { return request.post('/ci-types', data) }
export function updateCIType(id: number, data: any) { return request.put(`/ci-types/${id}`, data) }
export function deleteCIType(id: number)   { return request.delete(`/ci-types/${id}`) }
export function getAttributes(typeId: number)      { return request.get(`/ci-types/${typeId}/attributes`) }
export function createAttribute(typeId: number, data: any) { return request.post(`/ci-types/${typeId}/attributes`, data) }
export function updateAttribute(typeId: number, attrId: number, data: any) { return request.put(`/ci-types/${typeId}/attributes/${attrId}`, data) }
export function deleteAttribute(typeId: number, attrId: number) { return request.delete(`/ci-types/${typeId}/attributes/${attrId}`) }