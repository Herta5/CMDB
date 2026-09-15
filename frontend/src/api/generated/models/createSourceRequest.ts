/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { Provider } from './provider';
import type { SourceCredential } from './sourceCredential';
import type { EmptyConfig } from './emptyConfig';

export interface CreateSourceRequest {
  provider: Provider;
  credential: SourceCredential;
  /** @minLength 1 */
  name: string;
  /** @nullable */
  region?: string | null;
  config?: EmptyConfig;
  /**
   * 创建缺失或0表示默认60分钟，其他值必须为5至10080分钟
   * @nullable
   */
  sync_interval_minutes?: number | null;
}
