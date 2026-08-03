import request from '@/utils/request'

export function getRelationRules()                    { return request.get('/relations/rules') }
export function createRelationRule(data: any)          { return request.post('/relations/rules', data) }
export function updateRelationRule(id: number, data: any) { return request.put(`/relations/rules/${id}`, data) }
export function deleteRelationRule(id: number)         { return request.delete(`/relations/rules/${id}`) }
export function getRelationInstances(params?: any)     { return request.get('/relations/instances', { params }) }
export function createRelationInstance(data: any)      { return request.post('/relations/instances', data) }
export function deleteRelationInstance(id: number)     { return request.delete(`/relations/instances/${id}`) }
export function getTopology(ciId: number, depth?: number) { return request.get('/relations/topology', { params: { ci_id: ciId, depth: depth || 3 } }) }
export function getImpact(ciId: number, depth?: number)   { return request.get('/relations/impact', { params: { ci_id: ciId, depth: depth || 5 } }) }