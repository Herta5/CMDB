/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { SyncTypeStatistics } from './syncTypeStatistics';

/**
 * 以资源类型为键；排队或整体失败时允许null或空对象。
 * @nullable
 */
export type SyncStatistics = {[key: string]: SyncTypeStatistics} | null;
