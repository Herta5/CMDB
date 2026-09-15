/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type ResourceType = typeof ResourceType[keyof typeof ResourceType];


// eslint-disable-next-line @typescript-eslint/no-redeclare
export const ResourceType = {
  ecs: 'ecs',
  ec2: 'ec2',
  rds: 'rds',
  slb: 'slb',
  clb: 'clb',
  alb: 'alb',
  nlb: 'nlb',
  gwlb: 'gwlb',
} as const;
