/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { AuditLog } from './auditLog';

export interface AuditLogPage {
  items: AuditLog[];
  /** @minimum 0 */
  total: number;
  page: number;
  page_size: number;
  /** @minimum 0 */
  snapshot_id: number;
}
