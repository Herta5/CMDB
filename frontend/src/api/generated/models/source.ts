/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { Provider } from './provider';
import type { EmptyConfig } from './emptyConfig';

export interface Source {
  /** @minimum 1 */
  id: number;
  /** @minimum 1 */
  project_id: number;
  provider: Provider;
  name: string;
  region: string;
  /** 仅表示已安全配置，不含凭证片段 */
  credential_hint: string;
  config: EmptyConfig;
  enabled: boolean;
  /**
   * @minimum 5
   * @maximum 10080
   */
  sync_interval_minutes: number;
  /** @nullable */
  last_sync_at: string | null;
  /** @nullable */
  next_sync_at: string | null;
  created_at: string;
  updated_at: string;
}
