/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { ServerDiskKind } from './serverDiskKind';

export interface ServerDisk {
  id: string;
  kind: ServerDiskKind;
  type: string;
  size_gib: number;
  device: string;
  encrypted: boolean;
}
