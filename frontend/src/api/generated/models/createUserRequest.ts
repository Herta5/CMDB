/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { GlobalRole } from './globalRole';
import type { UserStatus } from './userStatus';
import type { ProjectPermissionInput } from './projectPermissionInput';

export interface CreateUserRequest {
  /**
   * @minLength 1
   * @maxLength 64
   * @pattern ^[A-Za-z0-9_]+$
   */
  username: string;
  /** 按UTF-8字节计算，必须为12至72字节；服务端领域校验，不按Unicode字符数替代 */
  password: string;
  /** @minLength 1 */
  display_name: string;
  /** @nullable */
  email?: string | null;
  global_role: GlobalRole;
  status: UserStatus;
  /** @nullable */
  project_permissions?: ProjectPermissionInput[] | null;
}
