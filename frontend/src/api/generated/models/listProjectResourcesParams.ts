/**
 * 本文件由 Orval 根据 api/openapi.yaml 自动生成，请勿手工修改。
 */

export type ListProjectResourcesParams = {
page?: number;
page_size?: number;
/**
 * @minimum 0
 */
source_id?: number;
/**
 * 以逗号分隔的可组合筛选值；去除空白并忽略空项
 */
provider?: string;
/**
 * 以逗号分隔的可组合筛选值；去除空白并忽略空项
 */
resource_type?: string;
keyword?: string;
engine?: string;
network_type?: string;
region?: string;
cloud_status?: string;
asset_status?: string;
sort_by?: string;
sort_order?: string;
};
