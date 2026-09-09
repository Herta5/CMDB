-- CMDB 新版基础领域迁移：仅建立身份、项目成员和审计的最小数据边界。

CREATE TABLE IF NOT EXISTS users (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '全局用户唯一标识',
    username VARCHAR(64) NOT NULL COMMENT '登录用户名，全局唯一',
    password_hash VARCHAR(255) NOT NULL COMMENT 'bcrypt 密码哈希，禁止保存明文',
    display_name VARCHAR(128) NOT NULL COMMENT '页面展示的用户名称',
    email VARCHAR(255) NULL COMMENT '用户联系邮箱',
    global_role ENUM('system_admin', 'user') NOT NULL DEFAULT 'user' COMMENT '全局角色，仅系统管理员或普通用户',
    status ENUM('active', 'disabled') NOT NULL DEFAULT 'active' COMMENT '用户启用状态',
    last_login_at DATETIME(3) NULL COMMENT '最近一次成功登录时间',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '最后更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_username (username)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CMDB 全局用户身份表';

CREATE TABLE IF NOT EXISTS projects (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '业务项目唯一标识',
    code VARCHAR(64) NOT NULL COMMENT '业务项目稳定编码，全局唯一且创建后不可修改',
    name VARCHAR(128) NOT NULL COMMENT '业务项目名称',
    description VARCHAR(500) NOT NULL DEFAULT '' COMMENT '业务项目说明',
    status ENUM('enabled', 'disabled') NOT NULL DEFAULT 'enabled' COMMENT '业务项目启用状态',
    owner_user_id BIGINT UNSIGNED NULL COMMENT '业务项目负责人，可在尚未分配负责人时为空',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '最后更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_projects_code (code),
    KEY idx_projects_owner_user_id (owner_user_id),
    CONSTRAINT fk_projects_owner_user_id FOREIGN KEY (owner_user_id) REFERENCES users (id) ON DELETE SET NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CMDB 业务项目表，是数据归属和权限隔离边界';

CREATE TABLE IF NOT EXISTS project_members (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '项目成员关系唯一标识',
    project_id BIGINT UNSIGNED NOT NULL COMMENT '所属业务项目标识',
    user_id BIGINT UNSIGNED NOT NULL COMMENT '成员用户标识',
    role ENUM('project_admin', 'member', 'viewer') NOT NULL COMMENT '用户在该业务项目内的角色',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '加入项目时间',
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '成员关系最后更新时间',
    PRIMARY KEY (id),
    UNIQUE KEY uk_project_members_project_user (project_id, user_id),
    KEY idx_project_members_user_id (user_id),
    CONSTRAINT fk_project_members_project FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE,
    CONSTRAINT fk_project_members_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CMDB 业务项目成员及项目内角色表';

CREATE TABLE IF NOT EXISTS audit_logs (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT COMMENT '审计日志唯一标识',
    actor_id BIGINT UNSIGNED NULL COMMENT '执行操作的用户标识，保留数值以支持用户删除后的追溯',
    project_id BIGINT UNSIGNED NULL COMMENT '操作所属业务项目标识，保留数值以支持项目归档后的追溯',
    action VARCHAR(128) NOT NULL COMMENT '审计动作名称',
    resource_type VARCHAR(64) NOT NULL COMMENT '被操作资源的类型',
    resource_id VARCHAR(255) NULL COMMENT '被操作资源的业务标识',
    detail JSON NULL COMMENT '已过滤敏感字段的审计上下文',
    request_ip VARCHAR(45) NULL COMMENT '请求来源 IP 地址',
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) COMMENT '审计记录创建时间',
    PRIMARY KEY (id),
    KEY idx_audit_logs_project_created_at (project_id, created_at),
    KEY idx_audit_logs_actor_created_at (actor_id, created_at),
    KEY idx_audit_logs_resource (resource_type, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci COMMENT='CMDB 审计日志表，保留资源物理删除后的操作记录';
