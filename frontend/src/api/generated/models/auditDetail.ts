/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

/**
 * 递归敏感键过滤后的审计详情；不同动作包含不同业务字段，客户端按unknown收窄，不实施业务键允许列表。
 * @nullable
 */
export type AuditDetail = { [key: string]: unknown } | null;
