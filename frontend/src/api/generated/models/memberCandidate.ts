/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export interface MemberCandidate {
  /**
   * @minLength 1
   * @maxLength 64
   * @pattern ^[A-Za-z0-9_]+$
   */
  username: string;
  display_name: string;
}
