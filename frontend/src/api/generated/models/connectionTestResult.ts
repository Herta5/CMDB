/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { ResourceType } from './resourceType';

export interface ConnectionTestResult {
  /** @nullable */
  reachable_types: ResourceType[] | null;
  /** @nullable */
  failed_types: ResourceType[] | null;
}
