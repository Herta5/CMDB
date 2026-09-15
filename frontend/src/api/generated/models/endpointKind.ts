/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type EndpointKind = typeof EndpointKind[keyof typeof EndpointKind];


// eslint-disable-next-line @typescript-eslint/no-redeclare
export const EndpointKind = {
  private: 'private',
  public: 'public',
  hostname: 'hostname',
} as const;
