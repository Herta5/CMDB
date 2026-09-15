/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { ProjectRole } from './projectRole';

export interface ProjectPermissionInput {
  /** @minimum 1 */
  project_id: number;
  /** @nullable */
  project_name?: string | null;
  role: ProjectRole;
}
