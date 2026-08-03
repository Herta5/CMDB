# CMDB 企业级配置管理数据库系统 架构设计文档

技术栈: Gin + Vue 3 + MySQL 8.4 | 版本: v1.1

---

## 1. 核心功能模块设计

### 1.1 模块全景

```
+------------------------------------------------------------------+
|                        CMDB 系统                                  |
|  +-------------+  +-------------+  +---------------------------+ |
|  |  资产管理    |  |  配置管理    |  |  自动发现                  | |
|  |  CI 全生命   |  |  参数模板    |  |  Agent 采集               | |
|  |  资产导入    |  |  版本控制    |  |  SSH/WMI Agentless        | |
|  |  资产盘点    |  |  配置比对    |  |  K8s/Cloud API 同步       | |
|  +-----+-------+  +------+------+  +-------------+-------------+ |
|        |                 |                       |               |
|  +-----+-------+  +------+------+  +-------------+-------------+ |
|  |  关系拓扑    |  |  变更管理    |  |  审计 & 日志               | |
|  |  拓扑图谱    |  |  变更审批    |  |  操作审计                  | |
|  |  影响分析    |  |  回滚记录    |  |  变更追溯                  | |
|  |  依赖溯源    |  |  变更日历    |  |  合规报告                  | |
|  +-----+-------+  +------+------+  +-------------+-------------+ |
|        |                 |                       |               |
|  +-----+-------+  +------+------+  +-------------+-------------+ |
|  |  Dashboard  |  |  用户管理    |  |  集成中心                  | |
|  |  资产大盘    |  |  用户CRUD    |  |  Prometheus/Zabbix        | |
|  |  容量看板    |  |  角色分配    |  |  Jira/ITSM 工单           | |
|  |  合规报表    |  |  密码管理    |  |  Ansible/Jenkins 自动化    | |
|  +-------------+  +-------------+  +---------------------------+ |
+------------------------------------------------------------------+
```

### 1.2 模块职责

| 模块 | 核心职责 | 关键能力 |
|------|---------|---------|
| **资产管理** | CI 全生命周期管理：注册、变更、退役、盘点 | 批量导入/导出、资产标签、自定义属性、折旧计算 |
| **配置管理** | 配置项参数的结构化管理与版本控制 | 配置模板、配置比对 Diff、合规检查、配置快照 |
| **自动发现** | 多协议自动探测基础设施资源 | Agent/Agentless 双模、定时巡检、增量同步 |
| **关系拓扑** | CI 间关系的建模、查询与可视化 | 拓扑图谱、影响分析、依赖追溯、关系校验 |
| **变更管理** | 控制配置变更的申请、审批、执行与回滚 | 变更单、审批流、灰度窗口、回滚记录 |
| **审计日志** | 全量操作审计与变更轨迹追踪 | 操作日志、数据快照、合规报告、异常告警 |
| **用户管理** | 系统用户生命周期管理与权限分配 | 用户CRUD、角色分配、密码策略、登录审计 |
| **Dashboard** | 多维度的资产与配置可视化 | 资产大盘、容量预测、合规评分、趋势分析 |
| **集成中心** | 与监控、工单、自动化等平台的双向同步 | ESB/Webhook 双通道、字段映射、同步策略 |

---

## 2. 数据模型设计（CI 模型）

### 2.1 设计理念

使用 MySQL 8.4 的 JSON 类型存储动态属性，避免为每种 CI 类型建独立子表。每个 CI 类型的属性定义写入 `ci_attribute`，实例的实际值存入 `ci_instance.attributes` JSON 字段。对于查询频繁的关键字段（IP、SN 等），单独冗余一列并建索引，这是 "EAV + JSON 混合模型"，兼顾灵活性与查询性能。

### 2.2 资源类型定义表 `ci_type`

```sql
CREATE TABLE ci_type (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(128)  NOT NULL COMMENT '类型唯一标识，如 Server, MySQL',
    display_name    VARCHAR(256)  NOT NULL COMMENT '显示名称',
    parent_id       BIGINT UNSIGNED NULL COMMENT '父类型ID，实现层级继承',
    icon            VARCHAR(64)   DEFAULT 'server' COMMENT '前端图标标识',
    description     TEXT          COMMENT '类型说明',
    is_abstract     TINYINT(1)    DEFAULT 0 COMMENT '是否抽象类型(不可实例化)',
    sort_order      INT           DEFAULT 0 COMMENT '排序权重',
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_name (name),
    KEY idx_parent (parent_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CI资源类型定义';
```

### 2.3 属性定义表 `ci_attribute`

```sql
CREATE TABLE ci_attribute (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    ci_type_id      BIGINT UNSIGNED NOT NULL COMMENT '所属CI类型',
    name            VARCHAR(128)  NOT NULL COMMENT '属性名，如 ip_address',
    display_name    VARCHAR(256)  NOT NULL COMMENT '属性显示名',
    value_type      ENUM('string','int','float','bool','date','datetime','json','enum') NOT NULL,
    is_required     TINYINT(1)    DEFAULT 0 COMMENT '是否必填',
    is_unique       TINYINT(1)    DEFAULT 0 COMMENT '是否唯一',
    is_indexed      TINYINT(1)    DEFAULT 0 COMMENT '是否建索引',
    default_value   TEXT          COMMENT '默认值(JSON)',
    enum_values     JSON          COMMENT '枚举可选值列表',
    validation_rule VARCHAR(512)  COMMENT '校验规则(regex/custom)',
    sort_order      INT           DEFAULT 0,
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_type_attr (ci_type_id, name),
    KEY idx_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CI属性定义';
```

### 2.4 CI 实例表 `ci_instance`

```sql
CREATE TABLE ci_instance (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    ci_type_id      BIGINT UNSIGNED NOT NULL COMMENT 'CI类型ID',
    ci_code         VARCHAR(128)  NOT NULL COMMENT 'CI唯一编号，如 SRV-20240101-0001',
    name            VARCHAR(512)  NOT NULL COMMENT 'CI名称',
    status          ENUM('active','inactive','maintenance','retired') DEFAULT 'active',
    attributes      JSON          NOT NULL COMMENT '动态属性(JSON)，灵活存储不同类型属性',
    -- 用于快速检索的关键字段(冗余,从attributes中同步)
    ip_address      VARCHAR(45)   COMMENT '主IP地址(v4/v6)',
    mac_address     VARCHAR(17)   COMMENT 'MAC地址',
    sn              VARCHAR(128)  COMMENT '序列号',
    asset_tag       VARCHAR(128)  COMMENT '资产标签/固资编号',
    -- 组织归属
    department_id   BIGINT UNSIGNED COMMENT '所属部门',
    owner           VARCHAR(128)  COMMENT '责任人',
    -- 生命周期
    discovered_at   DATETIME      COMMENT '首次发现时间',
    last_seen_at    DATETIME      COMMENT '最后在线时间',
    retired_at      DATETIME      COMMENT '退役时间',
    -- 来源追踪
    source          ENUM('manual','auto_discovery','api','import') DEFAULT 'manual',
    source_detail   VARCHAR(512)  COMMENT '来源详情',
    -- 审计字段
    created_by      VARCHAR(128)  COMMENT '创建人',
    updated_by      VARCHAR(128)  COMMENT '更新人',
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    --
    UNIQUE KEY uk_ci_code (ci_code),
    KEY idx_type (ci_type_id),
    KEY idx_ip (ip_address),
    KEY idx_status (status),
    KEY idx_department (department_id),
    KEY idx_last_seen (last_seen_at),
    KEY idx_asset_tag (asset_tag),
    KEY idx_sn (sn)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CI实例主表';
```

### 2.5 配置快照表 `config_snapshot`

```sql
CREATE TABLE config_snapshot (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    ci_id           BIGINT UNSIGNED NOT NULL COMMENT 'CI实例ID',
    snapshot_data   JSON          NOT NULL COMMENT '某一时刻的attributes完整快照',
    change_type     ENUM('create','update','delete','discovery') DEFAULT 'update',
    change_summary  VARCHAR(1024) COMMENT '变更摘要（Diff结果）',
    source          VARCHAR(256)  COMMENT '触发源(auto/manual/api)',
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    KEY idx_ci_id (ci_id),
    KEY idx_created (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='配置变更快照';
```

### 2.6 用户表 `cmdb_user`

```sql
CREATE TABLE cmdb_user (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    username        VARCHAR(64)   NOT NULL COMMENT '登录用户名',
    password_hash   VARCHAR(256)  NOT NULL COMMENT 'bcrypt 哈希密码',
    display_name    VARCHAR(128)  COMMENT '显示姓名',
    email           VARCHAR(256)  COMMENT '邮箱',
    phone           VARCHAR(32)   COMMENT '手机号',
    roles           JSON          NOT NULL COMMENT '角色列表，如 ["super_admin","viewer"]',
    departments     JSON          COMMENT '所属部门ID列表',
    status          VARCHAR(32)   DEFAULT 'active' COMMENT '状态: active/disabled',
    last_login_at   DATETIME      COMMENT '最后登录时间',
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_username (username),
    KEY idx_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统用户表';
```

密码使用 Go `golang.org/x/crypto/bcrypt` 加盐哈希存储，首次启动时自动创建默认管理员 `admin / admin123`。

### 2.7 模型 ER 关系

```
ci_type (1) ----< (N) ci_attribute
  |
  | (1)
  |
  +-------------< (N) ci_instance (1) >-----< (N) config_snapshot
                          |
                          | (N)                   (N) |
                          +--- ci_relation_rule ------+
                          |       (source_ci / target_ci)
                          |
                          +--- ci_relation_instance --+
                                  (source / target)

cmdb_user (独立表，不直接关联 CI)
```

---

## 3. 资产分类体系

### 3.1 完整分类树

```
IT 基础设施 (root)
|
+-- 机房设施
|   +-- 数据中心 (Datacenter)
|   +-- 机柜 (Rack)
|       +-- 机柜位 (RackSlot)
|
+-- 硬件
|   +-- 物理服务器 (PhysicalServer)
|   +-- 网络设备
|   |   +-- 交换机 (Switch)
|   |   +-- 路由器 (Router)
|   |   +-- 防火墙 (Firewall)
|   |   +-- 负载均衡 (LoadBalancer)
|   +-- 存储设备
|   |   +-- SAN (StorageAreaNetwork)
|   |   +-- NAS (NetworkAttachedStorage)
|   +-- 配件
|       +-- 硬盘 (Disk)
|       +-- 内存 (Memory)
|       +-- 网卡 (NIC)
|
+-- 虚拟化 & 容器
|   +-- VMware 集群 (VMwareCluster)
|   +-- VMware 宿主机 (VMwareHost)
|   +-- 虚拟机 (VirtualMachine)
|   +-- Kubernetes 集群 (K8sCluster)
|   +-- K8s 节点 (K8sNode)
|   +-- K8s 命名空间 (K8sNamespace)
|   +-- K8s 工作负载
|   |   +-- Deployment
|   |   +-- StatefulSet
|   |   +-- DaemonSet
|   |   +-- Job / CronJob
|   +-- K8s Pod
|   +-- K8s Service
|   +-- K8s Ingress
|   +-- K8s ConfigMap
|   +-- K8s Secret
|
+-- 云资源
|   +-- 阿里云 / AWS / 腾讯云
|   |   +-- ECS / EC2 / CVM        (云服务器)
|   |   +-- RDS / Aurora / CDB     (云数据库)
|   |   +-- SLB / ELB / CLB        (负载均衡)
|   |   +-- OSS / S3 / COS         (对象存储)
|   |   +-- VPC                    (虚拟网络)
|   |   +-- NAT 网关
|   |   +-- CDN
|   |   +-- 域名 / DNS 记录
|
+-- 数据库
|   +-- MySQL
|   +-- PostgreSQL
|   +-- Redis
|   +-- MongoDB
|   +-- Elasticsearch
|   +-- InfluxDB / TDengine         (时序)
|   +-- etcd                        (K8s 依赖)
|
+-- 中间件
|   +-- 消息队列
|   |   +-- Kafka
|   |   +-- RabbitMQ
|   |   +-- RocketMQ
|   +-- 反向代理 & Web 服务器
|   |   +-- Nginx
|   |   +-- HAProxy
|   +-- 注册中心
|   |   +-- Nacos
|   |   +-- Consul
|   +-- 网关
|       +-- Kong
|       +-- APISIX
|
+-- 应用 & 服务
|   +-- 业务应用 (BusinessApplication)
|   +-- 微服务 (MicroService)
|   +-- 定时任务 (ScheduledTask)
|   +-- 第三方服务 (ThirdPartyService)
|
+-- 域名 & 证书
|   +-- 域名 (Domain)
|   +-- SSL/TLS 证书 (SSLCertificate)
|
+-- 软件 & 许可
|   +-- 操作系统 (OS)
|   +-- 商业软件许可 (SoftwareLicense)
|   +-- Docker 镜像 (DockerImage)
|
+-- 人员 & 组织
|   +-- 部门 (Department)
|   +-- 用户 (User)
|   +-- 团队 (Team)
|
+-- 文档 & 知识
    +-- 运维手册 (Runbook)
    +-- 架构图 (ArchitectureDiagram)
```

### 3.2 分类设计原则

- **单一根节点 `IT 基础设施`：** 所有资产归入一棵统一分类树，避免多根造成管理混乱。
- **抽象类型不可实例化：** `IT 基础设施`、`硬件`、`数据库` 等中间节点设为抽象类型，仅作为分类容器，叶子类型才可注册实例。
- **云资源按提供商区分类型但仍属统一分支：** `ECS` / `EC2` / `CVM` 是独立 CI 类型，共享同一套云资源分类路径，通过 `attributes` 中的 `provider` 字段做筛选。

---

## 4. 关系模型设计

### 4.1 关系类型定义表 `ci_relation_rule`

```sql
CREATE TABLE ci_relation_rule (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name                VARCHAR(128)  NOT NULL COMMENT '关系名, 如 RunsOn',
    display_name        VARCHAR(256)  NOT NULL COMMENT '显示名',
    reverse_name        VARCHAR(128)  NOT NULL COMMENT '反向关系名, 如 HasRunning',
    source_type_id      BIGINT UNSIGNED NOT NULL COMMENT '源CI类型',
    target_type_id      BIGINT UNSIGNED NOT NULL COMMENT '目标CI类型',
    cardinality         ENUM('1:1','1:N','N:1','N:M') DEFAULT 'N:1',
    is_hard_dependency  TINYINT(1)    DEFAULT 0 COMMENT '是否强依赖(删除影响分析用)',
    description         TEXT,
    created_at          DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_name (name),
    KEY idx_source_type (source_type_id),
    KEY idx_target_type (target_type_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CI关系规则定义';
```

### 4.2 关系实例表 `ci_relation_instance`

```sql
CREATE TABLE ci_relation_instance (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    rule_id         BIGINT UNSIGNED NOT NULL COMMENT '关系规则ID',
    source_ci_id    BIGINT UNSIGNED NOT NULL COMMENT '源CI实例ID',
    target_ci_id    BIGINT UNSIGNED NOT NULL COMMENT '目标CI实例ID',
    properties      JSON          COMMENT '关系属性(如端口号、连接字符串)',
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_relation (rule_id, source_ci_id, target_ci_id),
    KEY idx_source (source_ci_id),
    KEY idx_target (target_ci_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='CI关系实例表';
```

### 4.3 预设关系类型矩阵

| 关系名 | 反向关系 | 源CI类型 | 目标CI类型 | 基数 | 强依赖 |
|--------|---------|---------|-----------|------|--------|
| `RunsOn` | `HasRunning` | Application, Pod, VM | PhysicalServer, VM, K8sNode | N:1 | Yes |
| `Contains` | `BelongsTo` | Rack, Cluster, Namespace | Server, Node, Pod | 1:N | No |
| `DependsOn` | `DependedBy` | Application, Service | Database, Middleware | N:M | Yes |
| `ConnectsTo` | `ConnectedBy` | Application, Service, Pod | Database, Redis, Kafka | N:M | No |
| `ManagedBy` | `Manages` | Server, VM, K8sNode | K8sCluster, VMwareHost | N:1 | No |
| `OwnedBy` | `Owns` | CI | Department, Team, User | N:1 | No |
| `Exposes` | `ExposedBy` | K8sService, Nginx, SLB | Application, Pod, VM | 1:N | No |
| `ResolvedTo` | `Resolves` | Domain | IP, SLB, K8sIngress | 1:N | No |
| `Certifies` | `CertifiedBy` | SSLCertificate | Domain | N:1 | No |

### 4.4 递归影响分析查询

获取某个 MySQL 实例故障时受影响的全部上游应用：

```sql
WITH RECURSIVE impact_chain AS (
    -- 起点：直接依赖该 MySQL 的 CI
    SELECT source_ci_id AS ci_id, 1 AS depth
    FROM ci_relation_instance ri
    JOIN ci_relation_rule rr ON ri.rule_id = rr.id
    WHERE ri.target_ci_id = :mysql_ci_id AND rr.is_hard_dependency = 1

    UNION ALL

    -- 递归：继续向上追溯
    SELECT ri.source_ci_id, ic.depth + 1
    FROM ci_relation_instance ri
    JOIN ci_relation_rule rr ON ri.rule_id = rr.id
    JOIN impact_chain ic ON ri.target_ci_id = ic.ci_id
    WHERE rr.is_hard_dependency = 1 AND ic.depth < 10
)
SELECT DISTINCT ci.id, ci.name, ci.ci_type_id, ic.depth
FROM impact_chain ic
JOIN ci_instance ci ON ci.id = ic.ci_id
ORDER BY ic.depth;
```

---

## 5. 用户角色与权限设计

### 5.1 角色定义（RBAC）

系统用户存储在 `cmdb_user` 表中，密码使用 bcrypt 哈希。每个用户可分配多个角色，角色以 JSON 数组形式存储（`["super_admin", "viewer"]`）。

| 角色 | 标识 | 权限范围 |
|------|------|---------|
| 超级管理员 | super_admin | 全部权限，包括系统配置、角色分配、用户管理 |
| CMDB 管理员 | cmdb_admin | CI类型/属性定义、关系规则管理、批量操作、采集策略配置 |
| 资产管理员 | asset_mgr | CI实例的CRUD、关系维护、资产盘点 |
| 变更执行人 | change_op | 提交变更单、执行变更(需审批) |
| 只读用户 | viewer | 查看资产、拓扑、报表 |
| API 集成账号 | api_user | 仅API访问，按Token限制scope |

### 5.2 权限矩阵（资源 x 操作）

| 资源 \ 角色 | super_admin | cmdb_admin | asset_mgr | change_op | viewer | api_user |
|------------|:-----------:|:----------:|:---------:|:---------:|:------:|:--------:|
| CI 类型定义  | C/R/U/D | C/R/U/D | R | - | R | R |
| CI 属性定义  | C/R/U/D | C/R/U/D | R | - | R | R |
| CI 实例     | C/R/U/D | C/R/U/D | C/R/U/D | R/U | R | *(按scope) |
| 关系规则     | C/R/U/D | C/R/U/D | R | R | R | R |
| 关系实例     | C/R/U/D | C/R/U/D | C/R/U/D | R | R | *(按scope) |
| 配置快照     | R/D     | R/D     | R | R | R | R |
| 变更单       | C/R/U/D | C/R/U/D | R | C/R/U | R | R |
| 采集策略     | C/R/U/D | C/R/U/D | R | - | - | R |
| 用户管理     | C/R/U/D | - | - | - | - | - |
| 审计日志     | R       | R       | R | R | R | - |
| 系统配置     | C/R/U/D | R | - | - | - | - |
| 角色分配     | C/R/U/D | - | - | - | - | - |

C=Create R=Read U=Update D=Delete

### 5.3 部门级数据隔离（可选扩展）

```sql
-- 所有查询自动注入 department 条件
SELECT * FROM ci_instance
WHERE department_id IN (
    SELECT department_id FROM user_department WHERE user_id = :current_user_id
);
```

### 5.4 JWT Token 设计

```json
{
  "sub": "user_id",
  "roles": ["asset_mgr", "viewer"],
  "departments": [1, 3],
  "scope": ["ci:read", "ci:write", "relation:read"],
  "exp": 1700000000
}
```

### 5.5 用户管理 API

```
GET    /api/v1/users               # 用户列表 (分页+筛选)
POST   /api/v1/users               # 创建用户 (需指定角色和初始密码)
GET    /api/v1/users/:id           # 用户详情
PUT    /api/v1/users/:id           # 更新用户信息/角色/状态
DELETE /api/v1/users/:id           # 删除用户 (受保护: 不能删除最后一个 super_admin)
PUT    /api/v1/users/:id/password  # 管理员重置用户密码
PUT    /api/v1/profile/password    # 当前用户自助修改密码
```

---

## 6. API 设计

### 6.1 设计原则

- RESTful 风格，JSON 请求/响应
- 统一响应格式：`{ "code": 0, "message": "ok", "data": {...} }`
- 版本前缀：`/api/v1`
- 认证：Bearer Token (JWT)
- 分页：`?page=1&page_size=20`
- 查询：`?q=keyword&status=active&ci_type_id=3`
- 排序：`?sort=name&order=asc`

### 6.2 API 端点清单

#### CI 类型管理

```
GET    /api/v1/ci-types                  # 获取CI类型列表(树形)
POST   /api/v1/ci-types                  # 创建CI类型
GET    /api/v1/ci-types/:id              # 获取类型详情
PUT    /api/v1/ci-types/:id              # 更新CI类型
DELETE /api/v1/ci-types/:id              # 删除CI类型(需无实例)
GET    /api/v1/ci-types/:id/attributes   # 获取类型的属性定义
POST   /api/v1/ci-types/:id/attributes   # 为该类型添加属性
```

#### CI 实例管理

```
GET    /api/v1/ci-instances              # 查询CI实例列表(支持多维过滤)
POST   /api/v1/ci-instances              # 创建CI实例
GET    /api/v1/ci-instances/:id          # 获取CI实例详情
PUT    /api/v1/ci-instances/:id          # 更新CI实例(写入快照)
DELETE /api/v1/ci-instances/:id          # 删除CI实例
PATCH  /api/v1/ci-instances/:id/status   # 快速修改状态

# 批量操作
POST   /api/v1/ci-instances/batch        # 批量导入(JSON/CSV)
PUT    /api/v1/ci-instances/batch        # 批量更新
DELETE /api/v1/ci-instances/batch        # 批量删除
GET    /api/v1/ci-instances/export       # 导出为CSV/Excel

# 搜索
GET    /api/v1/ci-instances/search       # 全文搜索(名称/IP/SN/标签)
```

#### 关系管理

```
GET    /api/v1/relations/rules           # 关系规则列表
POST   /api/v1/relations/rules           # 创建关系规则
PUT    /api/v1/relations/rules/:id       # 更新关系规则
DELETE /api/v1/relations/rules/:id       # 删除关系规则

GET    /api/v1/relations/instances       # 关系实例列表(?source_ci_id=xxx)
POST   /api/v1/relations/instances       # 创建关系实例
DELETE /api/v1/relations/instances/:id   # 删除关系实例

# 拓扑查询
GET    /api/v1/relations/topology        # 指定CI的拓扑图谱(?ci_id=xxx&depth=3)
GET    /api/v1/relations/impact          # 影响分析(?ci_id=xxx)
GET    /api/v1/relations/dependency      # 依赖溯源(?ci_id=xxx)
```

#### 配置快照

```
GET    /api/v1/snapshots                 # 快照列表(?ci_id=xxx)
GET    /api/v1/snapshots/:id             # 快照详情
GET    /api/v1/snapshots/diff            # 两次快照的Diff(?from=xxx&to=xxx)
```

#### 变更管理

```
GET    /api/v1/changes                   # 变更单列表
POST   /api/v1/changes                   # 提交变更单
GET    /api/v1/changes/:id               # 变更单详情
PUT    /api/v1/changes/:id               # 更新变更单
POST   /api/v1/changes/:id/approve       # 审批通过
POST   /api/v1/changes/:id/reject        # 审批驳回
POST   /api/v1/changes/:id/execute       # 执行变更
POST   /api/v1/changes/:id/rollback      # 回滚变更
```

#### 自动发现

```
GET    /api/v1/discovery/strategies      # 采集策略列表
POST   /api/v1/discovery/strategies      # 创建采集策略
PUT    /api/v1/discovery/strategies/:id  # 更新策略
POST   /api/v1/discovery/strategies/:id/trigger  # 手动触发一次采集
GET    /api/v1/discovery/strategies/:id/history   # 采集历史
```

#### Dashboard

```
GET    /api/v1/dashboard/summary         # 资产大盘摘要
GET    /api/v1/dashboard/distribution    # 按类型/部门/状态分布
GET    /api/v1/dashboard/trends          # 趋势数据(新增/退役/变更)
GET    /api/v1/dashboard/capacity        # 容量使用率
```

#### 用户管理

```
GET    /api/v1/users                     # 用户列表 (cmdb_admin+)
POST   /api/v1/users                     # 创建用户
GET    /api/v1/users/:id                 # 用户详情
PUT    /api/v1/users/:id                 # 更新用户
DELETE /api/v1/users/:id                 # 删除用户
PUT    /api/v1/users/:id/password        # 管理员重置密码
PUT    /api/v1/profile/password          # 当前用户自助修改密码
```

#### 认证

```
POST   /api/v1/auth/login                # 登录获取 Token
```

### 6.3 请求/响应示例

创建一台物理服务器 POST /api/v1/ci-instances：

```json
{
  "ci_type_id": 5,
  "name": "prod-web-server-01",
  "attributes": {
    "ip_address": "10.0.1.101",
    "mac_address": "00:50:56:9a:3b:2c",
    "sn": "SN-2024-XYZ-001",
    "asset_tag": "AS-2024-00123",
    "os": "CentOS 7.9",
    "cpu_cores": 64,
    "memory_gb": 256,
    "disk_total_gb": 2000,
    "rack_position": "A-03-12",
    "vendor": "Dell",
    "model": "PowerEdge R740",
    "warranty_expire": "2027-06-30"
  },
  "department_id": 3,
  "owner": "zhangsan"
}
```

查询某 MySQL 的影响范围：

```
GET /api/v1/relations/impact?ci_id=2042
返回树形JSON: 所有直接或间接依赖该MySQL的上游应用列表及层级深度
```

---

## 7. 数据采集方案

### 7.1 四层采集架构

```
+------------------------------------------------------------------+
|                      采集调度层 (scheduler)                        |
|  定时Cron | 手动触发 | 事件驱动(Webhook) | 幂等去重                |
+------------------------------------------------------------------+
        |                     |                     |
   +----+----+           +----+----+           +----+----+
   | 采集器A  |           | 采集器B  |           | 采集器C  |
   | K8s API |           | vSphere |           | 云API   |
   +----+----+           +----+----+           +----+----+
        |                     |                     |
+------------------------------------------------------------------+
|                      数据管道层 (pipeline)                         |
|  数据清洗 -> 格式标准化 -> 属性映射 -> 差异检测 -> 持久化              |
+------------------------------------------------------------------+
        |
+------------------------------------------------------------------+
|                      存储层 (storage)                             |
|  ci_instance (更新)  |  config_snapshot (写入)  |  raw_data (存档) |
+------------------------------------------------------------------+
```

### 7.2 采集方式对比

| 方式 | 适用场景 | 优点 | 缺点 |
|------|---------|------|------|
| **Agent 模式** | 物理服务器、VM | 信息全面、实时性高、Push模式 | 需要安装和维护Agent |
| **Agentless (SSH/WMI)** | 无法安装Agent的服务器 | 零侵入 | 依赖凭据、网络消耗大 |
| **K8s API** | Kubernetes 集群内资源 | 原生、权威数据源、事件驱动 | 只覆盖K8s资源 |
| **云 API (SDK)** | 阿里云/AWS/腾讯云资源 | 权威、覆盖全量云资源 | 依赖API配额、延迟 |
| **SNMP** | 网络设备 | 行业标准、设备支持广泛 | 信息有限 |
| **IPMI/Redfish** | 物理服务器带外管理 | 可采集硬件健康、功耗 | 需要规划带外网络 |

### 7.3 采集策略表

```sql
CREATE TABLE discovery_strategy (
    id              BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    name            VARCHAR(256)  NOT NULL COMMENT '策略名称',
    source_type     ENUM('agent','ssh','k8s_api','cloud_api','snmp') NOT NULL,
    target_config   JSON          NOT NULL COMMENT '目标配置(IP段/K8s集群/云账号)',
    schedule_expr   VARCHAR(128)  COMMENT 'Cron表达式,如 0 */6 * * *',
    enabled         TINYINT(1)    DEFAULT 1,
    timeout_sec     INT           DEFAULT 300,
    retry_count     INT           DEFAULT 3,
    last_run_at     DATETIME,
    last_run_status ENUM('success','failed','partial'),
    created_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME      NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='自动发现采集策略';
```

### 7.4 采集器实现（Go 侧）

采集器以插件形式注册到主服务中，统一接口：

```go
// collector/interface.go
type Collector interface {
    Name() string
    Collect(ctx context.Context, config json.RawMessage) ([]CIDiscoveryData, error)
}

type CIDiscoveryData struct {
    ExternalID   string                 // 外部唯一标识(instance-id/k8s-uid)
    CITypeName   string                 // 映射到的CI类型
    Name         string
    Attributes   map[string]interface{} // 采集到的原始属性
    Relations    []RelationHint         // 附带发现的关系
}

type RelationHint struct {
    RuleName         string
    TargetExternalID string
}

// 注册示例
func init() {
    registry.Register("k8s-api", &K8sCollector{})
    registry.Register("aliyun-ecs", &AliyunECSCollector{})
    registry.Register("vmware-vcenter", &VMwareCollector{})
    registry.Register("ssh-server", &SSHServerCollector{})
}
```

### 7.5 差异检测与合并策略

采集到的数据进入 pipeline 后：

1. 按 `ExternalID` 匹配已有 CI（首次匹配不到则自动创建）
2. 逐字段比对 `attributes`，检测变更
3. 有变更时：更新 `ci_instance` + 写入 `config_snapshot`
4. 无变更时：仅更新 `last_seen_at`

```
采集数据 -> 标准格式化 -> 查找现有CI -> Diff -> [有变更] -> 更新 + 快照
                                           -> [无变更] -> 更新 last_seen_at
```

---

## 8. 外部系统集成方案

### 8.1 集成全景图

```
                    +---------------+
                    |     CMDB      |
                    |  (Single Source|
                    |   of Truth)   |
                    +-------+-------+
                            |
        +--------+----------+---------+---------+
        |        |                    |         |
   +----+--+ +---+---+ +--------+ +--+---+ +---+---+
   |监控系统| |告警系统| |工单系统| |自动化 | |CI/CD  |
   |Prometheus |Zabbix  | |Jira/ITSM| |Ansible| |Jenkins|
   |Grafana  | |        | |        | |       | |       |
   +---------+ +--------+ +--------+ +-------+ +-------+
```

### 8.2 各系统集成方式

#### 监控系统（Prometheus + Grafana）

- **CMDB -> 监控：** 提供 `/api/v1/integration/prometheus-targets` 接口，返回按标签分组的采集目标列表。Prometheus 通过 `file_sd_config` 定期拉取。
- **监控 -> CMDB：** Alertmanager Webhook 推送告警，CMDB 将告警关联到对应 CI 实例，在资产详情页展示实时告警状态。
- **拓扑联动：** Grafana 内嵌拓扑图 iframe 指向 CMDB 的 `/relations/topology?ci_id={{ci_id}}`。

#### 工单系统（Jira / ITSM）

- **CMDB -> 工单：** 变更单审批通过后，通过 Webhook 在 Jira 自动创建关联 Ticket。
- **工单 -> CMDB：** Jira Ticket 状态变更时回调 CMDB，更新变更单执行状态。
- **故障关联：** 工单中通过 CI ID 直接查询 CMDB 资产详情，关联到拓扑影响范围。

#### 自动化平台（Ansible / Jenkins）

- **Ansible：** 动态 Inventory 脚本从 CMDB 拉取主机列表（按部门/标签/CI类型筛选），替换静态 hosts 文件。
- **Jenkins：** 部署流水线执行前调用 CMDB API 校验目标环境资产状态（如目标服务器是否处于维护窗口）。
- **回调：** 部署完成后回调 CMDB 更新应用版本属性。

### 8.3 集成接口清单

```
GET  /api/v1/integration/prometheus/targets    # Prometheus file_sd 格式
POST /api/v1/integration/webhook/alertmanager  # Alertmanager 告警回调
POST /api/v1/integration/webhook/jira          # Jira 状态回调
POST /api/v1/integration/webhook/jenkins       # Jenkins 部署回调
GET  /api/v1/integration/ansible/inventory     # Ansible 动态 Inventory
GET  /api/v1/integration/ansible/hosts/:group   # 按分组获取主机列表
```

### 8.4 事件总线（可选升级）

当集成点超过 5 个时，引入轻量级消息队列（如 NATS / Redis Streams）解耦：

```
CMDB 变更事件 -> NATS Topic -> 监控系统订阅(刷新target)
                           -> 工单系统订阅(关联CI)
                           -> 自动化平台订阅(校验资产)
```

---

## 9. MVP 版本与迭代规划

### 9.1 版本路线图

```
Phase 1 (MVP)         Phase 2            Phase 3             Phase 4
  4-6 周               6-8 周              6-8 周               持续
  ---------           ---------          ---------          ---------
  基础资产管理          自动发现            高级拓扑              集成 & 智能
  +----------+        +----------+        +----------+        +----------+
  |CI类型定义 |        |Agent采集 |        |拓扑图谱   |        |监控集成   |
  |CI实例CRUD|        |SSH采集   |        |影响分析   |        |工单集成   |
  |动态属性   |        |K8s同步   |        |依赖溯源   |        |自动化集成 |
  |手动关系   |        |云API同步 |        |变更管理   |        |审计增强   |
  |基础搜索   |        |配置快照  |        |Dashboard |        |AI辅助     |
  |RBAC      |        |Diff比对  |        |批量导入导出|       |合规报表   |
  +----------+        +----------+        +----------+        +----------+

Phase 5 (当前)
  持续迭代
  ---------
  用户管理 & 权限
  +----------+
  |用户CRUD  |
  |角色分配  |
  |密码管理  |
  |登录审计  |
  |多级审批  |
  +----------+
```

### 9.2 Phase 1 — MVP（已完成）

**目标：** 手动管理的资产登记系统，跑通 CI 模型和 RBAC。

| 模块 | 范围 |
|------|------|
| 后端基础 | Gin 框架搭建、MySQL 建表迁移、JWT 认证中间件 |
| CI 类型 | `ci_type` + `ci_attribute` CRUD、分类树展示 |
| CI 实例 | JSON 动态属性持久化、CRUD + 基础搜索（按名称/IP/SN/标签） |
| 关系管理 | 手动创建/删除关系实例，查看关联列表 |
| 用户权限 | 超级管理员 + 资产管理员 + 只读用户三个角色 |
| 前端 | Vue 3 管理界面：资产列表、详情、分类树、关系列表 |

### 9.3 Phase 2 — 自动发现（已完成）

**目标：** 让系统自动感知基础设施变化，减少人工录入。

| 模块 | 范围 |
|------|------|
| Agent | 轻量 Go Agent：采集主机基础信息、Docker 容器、进程 |
| K8s Collector | 对接 K8s API，同步 Node/Pod/Service/Deployment/ConfigMap 等 |
| SSH Collector | 免 agent 采集 Linux/Windows 主机信息 |
| 采集调度 | Cron 调度、采集历史记录、失败重试 |
| Diff 引擎 | 采集数据与已有 CI 比对，自动更新 + 写快照 |
| 配置快照 | 快照列表 + 两次快照 Diff 对比 |

### 9.4 Phase 3 — 高级拓扑与变更（已完成）

**目标：** 可视化拓扑 + 变更管理闭环。

| 模块 | 范围 |
|------|------|
| 拓扑图谱 | D3.js / Cytoscape.js 力导向图，2-3 层展开 |
| 影响分析 | 递归 CTE 查询，返回完整影响链路 |
| 依赖溯源 | 从应用向下追溯到底层物理资源 |
| 变更管理 | 变更单提交 -> 审批 -> 执行 -> 回滚流程 |
| Dashboard | 资产大盘：总数、按类型/状态/部门分布、趋势图 |
| 批量操作 | CSV 批量导入/导出、Excel 模板下载 |

### 9.5 Phase 4 — 集成与审计（已完成）

**目标：** CMDB 成为运维数据中枢。

| 模块 | 范围 |
|------|------|
| 监控集成 | Prometheus file_sd、告警关联、Grafana iframe |
| 工单集成 | Jira/ITSM 双向 Webhook |
| 自动化集成 | Ansible 动态 Inventory、Jenkins 部署校验 |
| 审计增强 | 细粒度操作审计、中间件异步写入 |
| 事件总线 | 内存事件总线，发布/订阅模式 |

### 9.6 Phase 5 — 用户管理 & 权限增强（当前）

**目标：** 完整的用户生命周期管理，替换硬编码登录。

| 模块 | 范围 |
|------|------|
| 用户管理 | `cmdb_user` 表、用户 CRUD、bcrypt 密码哈希 |
| 登录认证 | DB 验证替代硬编码、Token 携带角色信息 |
| 角色分配 | 多人多角色、5 种预设角色 |
| 密码策略 | 自助修改密码、管理员重置密码 |
| 安全策略 | 不能删除最后一个 super_admin、禁用账号拦截登录 |

---

## 10. 技术架构总览

### 10.1 技术选型

| 层次 | 技术 | 说明 |
|------|------|------|
| **后端框架** | Gin (Go 1.22+) | 高性能 HTTP 框架，中间件生态成熟 |
| **数据库** | MySQL 8.4 | JSON 类型、CTE 递归查询、窗口函数 |
| **缓存** | Redis 7 | 采集队列、Session、热点数据缓存 |
| **前端** | Vue 3 + TypeScript + Vite | Composition API、Type-safe |
| **UI 组件库** | Element Plus / Ant Design Vue | 企业级后台组件 |
| **图表** | ECharts / D3.js | Dashboard 图表 + 拓扑图谱 |
| **密码哈希** | golang.org/x/crypto/bcrypt | 防彩虹表攻击 |
| **构建部署** | Docker + Docker Compose / K8s | 容器化部署 |

### 10.2 项目目录结构（建议）

```
github-cmdb/
+-- backend/                    # Go 后端
|   +-- cmd/
|   |   +-- server/main.go      # 主入口
|   +-- internal/
|   |   +-- config/             # 配置管理(Viper)
|   |   +-- model/              # 数据模型 & 迁移
|   |   +-- handler/            # HTTP Handler (按模块)
|   |   |   +-- ci_type.go
|   |   |   +-- ci_instance.go
|   |   |   +-- relation.go
|   |   |   +-- change.go
|   |   |   +-- dashboard.go
|   |   |   +-- user.go         # 用户管理 Handler
|   |   +-- service/            # 业务逻辑层
|   |   +-- repository/         # 数据访问层
|   |   +-- middleware/         # Auth, CORS, Logger, RBAC
|   |   +-- collector/          # 自动发现采集器
|   |   |   +-- interface.go
|   |   |   +-- registry.go
|   |   |   +-- agent.go
|   |   |   +-- ssh.go
|   |   |   +-- k8s.go
|   |   +-- integration/        # 外部集成Webhook
|   +-- pkg/                    # 公共工具包
|   +-- go.mod
|   +-- go.sum
+-- frontend/                   # Vue 3 前端
|   +-- src/
|   |   +-- views/              # 页面组件
|   |   |   +-- assets/         # 资产管理
|   |   |   +-- topology/       # 拓扑图谱
|   |   |   +-- changes/        # 变更管理
|   |   |   +-- dashboard/      # 仪表盘
|   |   +-- components/         # 公共组件
|   |   +-- api/                # API 封装
|   |   +-- router/             # 前端路由
|   |   +-- store/              # Pinia 状态管理
|   +-- package.json
|   +-- vite.config.ts
+-- deploy/                     # 部署配置
|   +-- docker-compose.yml
|   +-- k8s/
+-- docs/
|   +-- CMDB-ARCHITECTURE.md    # 本文档
+-- README.md
```

### 10.3 关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 动态属性方案 | JSON (MySQL 8.4) + 关键字段冗余列 | EAV 表查询性能差，独立子表扩展成本高，JSON 在 MySQL 8.0+ 已成熟可用 |
| 拓扑查询 | MySQL CTE (WITH RECURSIVE) | 避免引入图数据库，500 台服务器规模下 CTE 完全够用 |
| 采集器架构 | 插件注册模式 (Go interface) | 新增采集源只需实现接口+注册，符合开闭原则 |
| 变更版本追溯 | 全量快照 (config_snapshot) | 存储成本低（JSON 增量不大），恢复和 Diff 比事件溯源简单 |
| 权限模型 | RBAC + 部门数据隔离 | 足够覆盖 90% 企业场景，前期无需引入 ABAC 复杂度 |
| 密码存储 | bcrypt 加盐哈希 | 防彩虹表、防暴力破解，Go 标准库生态 |
| 集成方式 | RESTful API + Webhook + 事件总线 | 通用性强，事件总线解耦内部模块 |
| 用户认证 | JWT (令牌) | 无状态、可扩展，适合微服务/前后端分离 |

---

## 附录

### A. 后续可展开的专题

- CMDB 数据质量治理（重复检测、过期清理、数据完整性校验）
- IP 地址管理（IPAM）子模块
- 基于 CMDB 的自动化运维剧本（Ansible Playbook 自动生成）
- 多数据中心联邦架构

---

文档状态: v1.1 已实施
适用范围: 500 台服务器规模的企业 IT 基础设施
技术栈: Gin + Vue 3 + MySQL 8.4