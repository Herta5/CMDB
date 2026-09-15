/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export interface AWSCredential {
  /**
   * @minLength 1
   * @pattern \S
   */
  access_key_id: string;
  /**
   * @minLength 1
   * @pattern \S
   */
  secret_access_key: string;
  /**
   * @minLength 1
   * @pattern \S
   */
  session_token?: string;
}
