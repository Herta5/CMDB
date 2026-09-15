/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export interface UpdateMyProfileRequest {
  /**
   * 去除首尾空白后不能为空，仅用于辅助展示
   * @minLength 1
   * @maxLength 128
   */
  display_name: string;
}
