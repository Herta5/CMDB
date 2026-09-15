/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { Provider } from './provider';
import type { ResourceType } from './resourceType';
import type { ResourceAssetStatus } from './resourceAssetStatus';
import type { Endpoint } from './endpoint';
import type { ServerDisk } from './serverDisk';

/**
 * 统一只读资产视图；未适用或不可确定的类型专属字段省略。memory单位MiB，容量单位GiB；不返回原始云属性。
 */
export interface Resource {
  /** @minimum 1 */
  id: number;
  /** @minimum 1 */
  project_id: number;
  /** @minimum 1 */
  source_id: number;
  provider: Provider;
  resource_type: ResourceType;
  external_id: string;
  name: string;
  region: string;
  zone: string;
  cloud_status: string;
  asset_status: ResourceAssetStatus;
  first_seen_at: string;
  last_seen_at: string;
  /** @nullable */
  missing_since: string | null;
  created_at: string;
  updated_at: string;
  source_name: string;
  /** @nullable */
  endpoints: Endpoint[] | null;
  /** @nullable */
  disks: ServerDisk[] | null;
  project_name?: string;
  engine?: string;
  engine_version?: string;
  network_type?: string;
  instance_type?: string;
  storage_type?: string;
  vcpu?: number;
  memory?: number;
  storage_size_gib?: number;
}
