/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { EmptyConfig } from './emptyConfig';
import type { UpdateSourceCredential } from './updateSourceCredential';

export interface UpdateSourceRequest {
  /** @minLength 1 */
  name: string;
  /** @nullable */
  region?: string | null;
  config?: EmptyConfig;
  /**
   * @minimum 5
   * @maximum 10080
   */
  sync_interval_minutes: number;
  credential?: UpdateSourceCredential;
  /** @nullable */
  enabled?: boolean | null;
}
