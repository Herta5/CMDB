/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export interface CreateProjectRequest {
  /** @minLength 1 */
  code: string;
  /** @minLength 1 */
  name: string;
  /** @nullable */
  description?: string | null;
  /**
   * @minLength 1
   * @maxLength 64
   * @nullable
   * @pattern ^[A-Za-z0-9_]+$
   */
  owner_username?: string | null;
}
