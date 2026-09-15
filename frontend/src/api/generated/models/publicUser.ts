/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { GlobalRole } from './globalRole';
import type { UserStatus } from './userStatus';
import type { ProjectPermission } from './projectPermission';

export interface PublicUser {
  /**
   * @minLength 1
   * @maxLength 64
   * @pattern ^[A-Za-z0-9_]+$
   */
  username: string;
  display_name: string;
  email: string;
  global_role: GlobalRole;
  status: UserStatus;
  /** @nullable */
  project_permissions: ProjectPermission[] | null;
}
