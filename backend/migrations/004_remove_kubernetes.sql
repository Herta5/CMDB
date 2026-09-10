-- CMDB 平台收窄迁移：物理清理已下线平台业务数据，独立审计日志继续保留。
DELETE endpoint FROM resource_endpoints AS endpoint
INNER JOIN resources AS resource ON resource.id = endpoint.resource_id
WHERE resource.provider = 'kubernetes';

DELETE FROM resources WHERE provider = 'kubernetes';

DELETE job FROM sync_jobs AS job
INNER JOIN resource_sources AS source ON source.id = job.source_id
WHERE source.provider = 'kubernetes';

DELETE FROM resource_sources WHERE provider = 'kubernetes';

ALTER TABLE resource_sources
  MODIFY COLUMN provider ENUM('aliyun','aws') NOT NULL;
