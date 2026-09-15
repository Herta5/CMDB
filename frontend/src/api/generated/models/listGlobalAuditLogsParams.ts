/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type ListGlobalAuditLogsParams = {
page?: number;
page_size?: number;
action?: string;
actor_username?: string;
resource_type?: string;
resource_id?: string;
/**
 * @minimum 1
 */
snapshot_id?: number;
start_at?: string;
end_at?: string;
};
