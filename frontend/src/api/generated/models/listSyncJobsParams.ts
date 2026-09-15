/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type ListSyncJobsParams = {
page?: number;
page_size?: number;
/**
 * @minimum 0
 */
source_id?: number;
provider?: string;
};
