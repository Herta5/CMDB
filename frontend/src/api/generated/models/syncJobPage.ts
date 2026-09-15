/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { SyncJob } from './syncJob';

export interface SyncJobPage {
  /** @nullable */
  items: SyncJob[] | null;
  /** @minimum 0 */
  total: number;
  page: number;
  page_size: number;
}
