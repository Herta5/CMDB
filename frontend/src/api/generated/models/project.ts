/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { ProjectStatus } from './projectStatus';
import type { ProjectRole } from './projectRole';

export interface Project {
  /** @minimum 1 */
  id: number;
  code: string;
  name: string;
  description: string;
  status: ProjectStatus;
  /**
   * @minLength 1
   * @maxLength 64
   * @nullable
   * @pattern ^[A-Za-z0-9_]+$
   */
  owner_username: string | null;
  created_at: string;
  updated_at: string;
  current_role?: ProjectRole;
}
