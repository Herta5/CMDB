-- 本增量迁移只由 cmdb-migrate 在管理员事务内执行，不包含独立事务边界。
-- 先拒绝孤儿资产或跨项目来源，避免新增限制外键掩盖历史归属错误。
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM resources_servers AS resource
        LEFT JOIN projects AS project ON project.id = resource.project_id
        LEFT JOIN resource_sources AS source ON source.id = resource.source_id
        WHERE project.id IS NULL OR source.id IS NULL OR source.project_id <> resource.project_id
        UNION ALL
        SELECT 1
        FROM resources_databases AS resource
        LEFT JOIN projects AS project ON project.id = resource.project_id
        LEFT JOIN resource_sources AS source ON source.id = resource.source_id
        WHERE project.id IS NULL OR source.id IS NULL OR source.project_id <> resource.project_id
        UNION ALL
        SELECT 1
        FROM resources_load_balancers AS resource
        LEFT JOIN projects AS project ON project.id = resource.project_id
        LEFT JOIN resource_sources AS source ON source.id = resource.source_id
        WHERE project.id IS NULL OR source.id IS NULL OR source.project_id <> resource.project_id
    ) THEN
        RAISE EXCEPTION 'CMDB 存在归属异常资产，未执行 P0 迁移';
    END IF;
END
$$;

ALTER TABLE resource_sources
    ADD COLUMN cloud_account_id VARCHAR(128),
    ADD COLUMN identity_status VARCHAR(32),
    ADD COLUMN identity_verified_at TIMESTAMPTZ(3);

-- 历史接入源尚未经过云端身份确认，不得猜测账号身份或占用全局唯一键。
UPDATE resource_sources
SET identity_status = 'pending',
    cloud_account_id = NULL,
    identity_verified_at = NULL;

ALTER TABLE resource_sources
    ALTER COLUMN identity_status SET DEFAULT 'verified',
    ALTER COLUMN identity_status SET NOT NULL,
    ADD CONSTRAINT ck_resource_sources_identity CHECK (
        (identity_status = 'pending' AND cloud_account_id IS NULL AND identity_verified_at IS NULL)
        OR
        (identity_status = 'verified' AND cloud_account_id IS NOT NULL AND cloud_account_id <> '' AND identity_verified_at IS NOT NULL)
    );

CREATE UNIQUE INDEX uk_resource_sources_provider_account_verified
    ON resource_sources (provider, cloud_account_id)
    WHERE identity_status = 'verified' AND cloud_account_id IS NOT NULL;

ALTER TABLE resources_servers
    DROP CONSTRAINT fk_resources_servers_project,
    DROP CONSTRAINT fk_resources_servers_source,
    ADD CONSTRAINT fk_resources_servers_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_resources_servers_source FOREIGN KEY (source_id) REFERENCES resource_sources (id) ON DELETE RESTRICT;

ALTER TABLE resources_databases
    DROP CONSTRAINT fk_resources_databases_project,
    DROP CONSTRAINT fk_resources_databases_source,
    ADD CONSTRAINT fk_resources_databases_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_resources_databases_source FOREIGN KEY (source_id) REFERENCES resource_sources (id) ON DELETE RESTRICT;

ALTER TABLE resources_load_balancers
    DROP CONSTRAINT fk_resources_load_balancers_project,
    DROP CONSTRAINT fk_resources_load_balancers_source,
    ADD CONSTRAINT fk_resources_load_balancers_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE RESTRICT,
    ADD CONSTRAINT fk_resources_load_balancers_source FOREIGN KEY (source_id) REFERENCES resource_sources (id) ON DELETE RESTRICT;

COMMENT ON COLUMN public.resource_sources.cloud_account_id IS '云平台侧账号或租户唯一标识，仅由身份确认流程维护';
COMMENT ON COLUMN public.resource_sources.identity_status IS '接入源云账号身份确认状态，历史来源迁移后保持待确认';
COMMENT ON COLUMN public.resource_sources.identity_verified_at IS '最近一次成功确认云账号身份的时间';
COMMENT ON TABLE public.schema_migrations IS 'CMDB 数据库结构迁移版本记录，仅管理员可写';
COMMENT ON COLUMN public.schema_migrations.version IS '已完成的数据库结构版本';
COMMENT ON COLUMN public.schema_migrations.applied_at IS '该结构版本完成迁移的时间';

-- 应用进程只读取结构版本，所有版本推进必须经过管理员迁移命令。
REVOKE ALL ON TABLE schema_migrations FROM cmdb;
GRANT SELECT ON TABLE schema_migrations TO cmdb;
