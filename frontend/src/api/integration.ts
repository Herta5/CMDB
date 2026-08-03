import request from '@/utils/request'

export function getPrometheusTargets()     { return request.get('/integration/prometheus/targets') }
export function getAnsibleInventory()       { return request.get('/integration/ansible/inventory') }
export function getWebhookHistory(params?: any) { return request.get('/integration/webhooks', { params }) }