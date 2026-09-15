/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */
import type { AliyunCredential } from './aliyunCredential';
import type { AWSCredential } from './aWSCredential';

/**
 * 缺失、null、空字符串均保留原凭证；完整对象整体替换，空对象及平台不匹配拒绝。
 * @nullable
 */
export type UpdateSourceCredential = AliyunCredential | AWSCredential | '' | null | null;
