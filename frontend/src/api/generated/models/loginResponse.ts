/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { PublicUser } from './publicUser';

export interface LoginResponse {
  /** 有效期24小时的会话令牌 */
  token: string;
  user: PublicUser;
}
