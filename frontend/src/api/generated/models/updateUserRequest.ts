/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { GlobalRole } from './globalRole';
import type { UserStatus } from './userStatus';
import type { ProjectPermissionInput } from './projectPermissionInput';

export interface UpdateUserRequest {
  /** @minLength 1 */
  display_name: string;
  /** @nullable */
  email?: string | null;
  global_role: GlobalRole;
  status: UserStatus;
  /** @nullable */
  project_permissions?: ProjectPermissionInput[] | null;
  /**
   * 缺失、null或空字符串保留密码；非空值必须为12至72字节
   * @nullable
   */
  password?: string | null;
}
