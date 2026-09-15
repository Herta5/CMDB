/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export interface ChangeMyPasswordRequest {
  /** 本人当前密码，仅用于本次验证 */
  current_password: string;
  /** 按UTF-8字节计算，必须为12至72字节；成功后全部旧会话失效 */
  new_password: string;
}
