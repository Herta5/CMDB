import request from '@/utils/request'

export function getSummary()       { return request.get('/dashboard/summary') }
export function getDistribution()  { return request.get('/dashboard/distribution') }
export function getTrends()        { return request.get('/dashboard/trends') }