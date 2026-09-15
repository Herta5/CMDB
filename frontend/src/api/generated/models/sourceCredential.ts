/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { AliyunCredential } from './aliyunCredential';
import type { AWSCredential } from './aWSCredential';

/**
 * 创建时必须提交所属平台完整凭证，服务端保留原始JSON以拒绝重复键；平台匹配由专用严格边界再次验证。
 */
export type SourceCredential = AliyunCredential | AWSCredential;
