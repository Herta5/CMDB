/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { SyncJobStatus } from './syncJobStatus';
import type { SyncJobTrigger } from './syncJobTrigger';
import type { SyncStatistics } from './syncStatistics';

export interface SyncJob {
  /** @minimum 1 */
  id: number;
  /** @minimum 1 */
  project_id: number;
  /** @minimum 1 */
  source_id: number;
  /**
   * @minimum 1
   * @nullable
   */
  previous_job_id: number | null;
  status: SyncJobStatus;
  trigger: SyncJobTrigger;
  statistics: SyncStatistics;
  error_summary: string;
  started_at: string;
  /** @nullable */
  finished_at: string | null;
}
