/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { EndpointKind } from './endpointKind';

export interface Endpoint {
  kind: EndpointKind;
  address: string;
  port: number;
  protocol: string;
  /** @nullable */
  resolved_ips: string[] | null;
}
