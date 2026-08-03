import request from '@/utils/request'

export function getAuditLogs(params?: any) { return request.get('/audit/logs', { params }) }