-- CMDB 项目角色收窄迁移：删除旧只读成员关系，不自动提升为项目成员。
DELETE FROM project_members WHERE role = 'viewer';

ALTER TABLE project_members
  MODIFY COLUMN role ENUM('project_admin','member') NOT NULL COMMENT '用户在该业务项目内的角色';
