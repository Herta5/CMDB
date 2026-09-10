-- CMDB 云资源核心迁移：建立接入源、统一资源、访问端点和同步任务。
CREATE TABLE IF NOT EXISTS resource_sources (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, project_id BIGINT UNSIGNED NOT NULL, provider ENUM('aliyun','aws') NOT NULL,
 name VARCHAR(128) NOT NULL, region VARCHAR(128) NOT NULL DEFAULT '', encrypted_credential TEXT NOT NULL, credential_hint VARCHAR(128) NOT NULL DEFAULT '', config JSON NULL,
 enabled BOOLEAN NOT NULL DEFAULT TRUE, sync_interval_minutes INT NOT NULL DEFAULT 60, last_sync_at DATETIME(3) NULL, next_sync_at DATETIME(3) NULL,
 created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (id), KEY idx_sources_project_provider (project_id, provider), CONSTRAINT fk_sources_project FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='公有云平台接入源';
CREATE TABLE IF NOT EXISTS resources (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, project_id BIGINT UNSIGNED NOT NULL, source_id BIGINT UNSIGNED NOT NULL, provider VARCHAR(32) NOT NULL,
 resource_type VARCHAR(64) NOT NULL, external_id VARCHAR(255) NOT NULL, name VARCHAR(255) NOT NULL DEFAULT '', region VARCHAR(128) NOT NULL DEFAULT '', zone VARCHAR(128) NOT NULL DEFAULT '', cloud_status VARCHAR(64) NOT NULL DEFAULT '', lifecycle_status ENUM('active','lost') NOT NULL DEFAULT 'active', raw_attributes JSON NULL,
 first_seen_at DATETIME(3) NOT NULL, last_seen_at DATETIME(3) NOT NULL, missing_since DATETIME(3) NULL, created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3), updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
 PRIMARY KEY (id), UNIQUE KEY uk_resource_identity (source_id, resource_type, external_id), KEY idx_resources_project_provider (project_id, provider), KEY idx_resources_lifecycle (lifecycle_status), CONSTRAINT fk_resources_project FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE, CONSTRAINT fk_resources_source FOREIGN KEY(source_id) REFERENCES resource_sources(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='跨平台统一云资源';
CREATE TABLE IF NOT EXISTS resource_endpoints (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, resource_id BIGINT UNSIGNED NOT NULL, kind ENUM('private','public','hostname') NOT NULL, address VARCHAR(512) NOT NULL, port INT NOT NULL DEFAULT 0, protocol VARCHAR(32) NOT NULL DEFAULT '', resolved_ips JSON NULL, resolved_at DATETIME(3) NULL,
 PRIMARY KEY(id), KEY idx_endpoints_resource(resource_id), CONSTRAINT fk_endpoints_resource FOREIGN KEY(resource_id) REFERENCES resources(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='云资源访问端点';
CREATE TABLE IF NOT EXISTS sync_jobs (
 id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT, project_id BIGINT UNSIGNED NOT NULL, source_id BIGINT UNSIGNED NOT NULL, status ENUM('running','success','partial_success','failed') NOT NULL, `trigger` ENUM('manual','scheduled') NOT NULL, statistics JSON NULL, error_summary VARCHAR(500) NOT NULL DEFAULT '', started_at DATETIME(3) NOT NULL, finished_at DATETIME(3) NULL,
 PRIMARY KEY(id), KEY idx_jobs_source_started(source_id, started_at), KEY idx_jobs_project_started(project_id, started_at), CONSTRAINT fk_jobs_project FOREIGN KEY(project_id) REFERENCES projects(id) ON DELETE CASCADE, CONSTRAINT fk_jobs_source FOREIGN KEY(source_id) REFERENCES resource_sources(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='云资源同步任务';
