/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { AuditDetail } from './auditDetail';

export interface AuditLog {
  /** @minimum 1 */
  id: number;
  actor_username: string;
  actor_display_name: string;
  /**
   * @minimum 1
   * @nullable
   */
  project_id: number | null;
  project_name: string;
  action: string;
  resource_type: string;
  resource_id: string;
  resource_name: string;
  detail: AuditDetail;
  request_ip: string;
  created_at: string;
}
