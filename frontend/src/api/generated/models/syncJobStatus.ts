/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type SyncJobStatus = typeof SyncJobStatus[keyof typeof SyncJobStatus];


// eslint-disable-next-line @typescript-eslint/no-redeclare
export const SyncJobStatus = {
  queued: 'queued',
  running: 'running',
  success: 'success',
  partial_success: 'partial_success',
  failed: 'failed',
} as const;
