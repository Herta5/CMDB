/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { UpdateProjectRequestStatus } from './updateProjectRequestStatus';

export interface UpdateProjectRequest {
  /** @minLength 1 */
  name: string;
  /** @nullable */
  description?: string | null;
  status: UpdateProjectRequestStatus;
  /**
   * @minLength 1
   * @maxLength 64
   * @nullable
   * @pattern ^[A-Za-z0-9_]+$
   */
  owner_username?: string | null;
  /**
   * 兼容提交时忽略此字段，项目编码创建后不可修改。
   * @nullable
   */
  code?: string | null;
}
