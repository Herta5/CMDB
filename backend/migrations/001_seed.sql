-- ============================================================================
-- CMDB Seed Data: CI Types, Attributes, Relation Rules
-- ============================================================================

-- 1. CI Type Hierarchy (using parent_id for tree structure)
-- ============================================================================
-- Level 0: Root
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(1,  'Infrastructure', '基础设施',      NULL, 'server',   '所有CI的根分类',   1, 1),
(2,  'Application',    '应用系统',      NULL, 'monitor',  '应用与服务分类',   1, 2),
(3,  'Network',        '网络设备',      NULL, 'wifi',     '网络基础设施分类', 1, 3),
(4,  'Middleware',     '中间件',        NULL, 'layers',   '中间件服务分类',   1, 4),
(5,  'Facility',       '机房设施',      NULL, 'building', '物理环境分类',     1, 5),
(6,  'Business',       '业务系统',      NULL, 'briefcase','业务视角分类',     1, 6);

-- Level 1: Infrastructure children (abstract type, instances go under concrete types)
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(11, 'PhysicalServer', '物理服务器',    1, 'server',   '裸金属服务器',             0, 11),
(12, 'VirtualMachine', '虚拟机',        1, 'monitor',  'VMware/Hyper-V/KVM 虚拟机', 0, 12),
(13, 'CloudServer',    '云服务器',      1, 'cloud',    '阿里云/腾讯云/AWS等云主机', 0, 13),
(14, 'Container',      '容器',          1, 'box',      'Docker容器实例',          0, 14),
(15, 'K8sCluster',     'Kubernetes集群', 1, 'cpu',      'K8s集群',                 0, 15),
(16, 'K8sNode',        'K8s节点',       1, 'hard-drive','K8s工作节点',             0, 16),
(17, 'K8sNamespace',   'K8s命名空间',   1, 'folder',   'K8s Namespace',           0, 17),
(18, 'K8sPod',         'K8s Pod',       1, 'package',  'K8s Pod实例',             0, 18),
(19, 'OS',             '操作系统',      1, 'settings', '操作系统抽象',            1, 19);

-- Level 1: Application children
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(21, 'MicroService',   '微服务',        2, 'git-branch', 'Spring Cloud/Dubbo等服务', 0, 21),
(22, 'WebApp',         'Web应用',       2, 'globe',      '前端Web应用',             0, 22),
(23, 'APIGateway',     'API网关',       2, 'link',       'API Gateway实例',         0, 23),
(24, 'ScheduledTask',  '定时任务',      2, 'clock',      'CronJob/定时调度任务',    0, 24);

-- Level 1: Network children
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(31, 'Switch',         '交换机',        3, 'git-merge', '网络交换机',             0, 31),
(32, 'Router',         '路由器',        3, 'share-2',   '网络路由器',             0, 32),
(33, 'Firewall',       '防火墙',        3, 'shield',    '防火墙设备',             0, 33),
(34, 'LoadBalancer',   '负载均衡器',    3, 'sliders',   'LB/反向代理',            0, 34),
(35, 'CDN',            'CDN',           3, 'globe',     '内容分发网络',           0, 35),
(36, 'DNS',            'DNS服务',       3, 'target',    '域名解析服务',           0, 36),
(37, 'VLAN',           'VLAN',          3, 'git-branch','虚拟局域网',             0, 37),
(38, 'IPAddress',      'IP地址',        3, 'hash',      'IP地址资源',             0, 38);

-- Level 1: Middleware children
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(41, 'MySQL',          'MySQL数据库',   4, 'database',        'MySQL实例',           0, 41),
(42, 'PostgreSQL',     'PostgreSQL',    4, 'database',        'PostgreSQL实例',      0, 42),
(43, 'Redis',          'Redis缓存',     4, 'database-zap',    'Redis实例',           0, 43),
(44, 'MongoDB',        'MongoDB',       4, 'database',        'MongoDB实例',         0, 44),
(45, 'Elasticsearch',  'Elasticsearch', 4, 'search',          'ES搜索集群',          0, 45),
(46, 'Kafka',          'Kafka',         4, 'message-circle',  'Kafka消息队列',       0, 46),
(47, 'RabbitMQ',       'RabbitMQ',      4, 'message-square',  'RabbitMQ消息队列',    0, 47),
(48, 'Nginx',          'Nginx',         4, 'server-cog',      'Nginx反向代理',       0, 48),
(49, 'Tomcat',         'Tomcat',        4, 'coffee',          'Tomcat应用服务器',    0, 49),
(50, 'Zookeeper',      'Zookeeper',     4, 'tree-pine',       'ZK服务注册中心',      0, 50);

-- Level 1: Facility children
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(61, 'DataCenter',     '数据中心',      5, 'building-2', '数据中心机房',         0, 61),
(62, 'Cabinet',        '机柜',          5, 'hard-drive', '标准机柜',             0, 62),
(63, 'PDU',            'PDU电源',       5, 'zap',        '电源分配单元',         0, 63),
(64, 'UPS',            'UPS不间断电源', 5, 'battery',    'UPS设备',              0, 64),
(65, 'Cooling',        '制冷设备',      5, 'thermometer','精密空调/制冷系统',    0, 65);

-- Level 1: Business children
INSERT INTO ci_type (id, name, display_name, parent_id, icon, description, is_abstract, sort_order) VALUES
(71, 'Product',        '产品线',        6, 'briefcase', '业务产品线',           0, 71),
(72, 'Project',        '项目',          6, 'folder-git', 'IT项目',              0, 72),
(73, 'Department',     '部门',          6, 'users',      '组织部门',            0, 73);

-- 2. CI Attribute Definitions
-- ============================================================================
-- PhysicalServer attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(11, 'ip_address',     'IP地址',       'string', 1, 1, 1, 1),
(11, 'mac_address',    'MAC地址',      'string', 0, 0, 0, 2),
(11, 'sn',             '序列号',       'string', 0, 1, 1, 3),
(11, 'asset_tag',      '资产标签',     'string', 0, 1, 1, 4),
(11, 'manufacturer',   '制造商',       'string', 0, 0, 0, 5),
(11, 'model',          '型号',         'string', 0, 0, 0, 6),
(11, 'cpu_cores',      'CPU核数',      'int',    0, 0, 0, 7),
(11, 'memory_gb',      '内存(GB)',     'int',    0, 0, 0, 8),
(11, 'disk_size_gb',   '磁盘(GB)',     'int',    0, 0, 0, 9),
(11, 'os_type',        '操作系统',     'enum',   0, 0, 0, 10),
(11, 'os_version',     '系统版本',     'string', 0, 0, 0, 11),
(11, 'datacenter',     '所属机房',     'string', 0, 0, 0, 12),
(11, 'rack',           '机柜位置',     'string', 0, 0, 0, 13),
(11, 'purchase_date',  '采购日期',     'date',   0, 0, 0, 14),
(11, 'warranty_end',   '维保到期',     'date',   0, 0, 0, 15),
(11, 'admin_ip',       '管理IP',       'string', 0, 0, 0, 16);

-- VirtualMachine attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(12, 'ip_address',     'IP地址',       'string', 1, 1, 1, 1),
(12, 'hypervisor',     '虚拟化平台',   'enum',   0, 0, 0, 2),
(12, 'cpu_cores',      'vCPU核数',     'int',    0, 0, 0, 3),
(12, 'memory_gb',      '内存(GB)',     'int',    0, 0, 0, 4),
(12, 'disk_size_gb',   '磁盘(GB)',     'int',    0, 0, 0, 5),
(12, 'os_type',        '操作系统',     'enum',   0, 0, 0, 6),
(12, 'host_server',    '宿主机',       'string', 0, 0, 0, 7),
(12, 'cluster',        '所属集群',     'string', 0, 0, 0, 8);

-- CloudServer attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(13, 'ip_address',     '公网IP',       'string', 0, 0, 1, 1),
(13, 'private_ip',     '内网IP',       'string', 0, 0, 1, 2),
(13, 'cloud_provider', '云厂商',       'enum',   1, 0, 1, 3),
(13, 'instance_type',  '实例规格',     'string', 1, 0, 0, 4),
(13, 'region',         '地域',         'string', 1, 0, 1, 5),
(13, 'zone',           '可用区',       'string', 0, 0, 0, 6),
(13, 'instance_id',    '实例ID(云)',   'string', 0, 1, 1, 7),
(13, 'cpu_cores',      'vCPU核数',     'int',    0, 0, 0, 8),
(13, 'memory_gb',      '内存(GB)',     'int',    0, 0, 0, 9),
(13, 'os_type',        '操作系统',     'enum',   0, 0, 0, 10);

-- K8sCluster attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(15, 'version',         'K8s版本',      'string', 1, 0, 0, 1),
(15, 'api_server',      'API Server',   'string', 1, 0, 0, 2),
(15, 'kubeconfig_path', 'Kubeconfig路径','string', 0, 0, 0, 3),
(15, 'node_count',      '节点数',       'int',    0, 0, 0, 4),
(15, 'pod_cidr',        'Pod CIDR',     'string', 0, 0, 0, 5),
(15, 'service_cidr',    'Service CIDR', 'string', 0, 0, 0, 6);

-- K8sNode attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(16, 'ip_address',     '节点IP',       'string', 1, 1, 1, 1),
(16, 'role',           '角色',         'enum',   0, 0, 0, 2),
(16, 'cpu_cores',      'CPU核数',      'int',    0, 0, 0, 3),
(16, 'memory_gb',      '内存(GB)',     'int',    0, 0, 0, 4),
(16, 'os_image',       '系统镜像',     'string', 0, 0, 0, 5),
(16, 'container_runtime','容器运行时',  'string', 0, 0, 0, 6);

-- MySQL attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(41, 'ip_address',     '连接地址',     'string', 1, 0, 1, 1),
(41, 'port',           '端口',         'int',    1, 0, 0, 2),
(41, 'version',        '版本',         'string', 0, 0, 0, 3),
(41, 'db_name',        '数据库名',     'string', 0, 0, 0, 4),
(41, 'charset',        '字符集',       'string', 0, 0, 0, 5),
(41, 'max_connections','最大连接数',   'int',    0, 0, 0, 6),
(41, 'is_master',      '是否主库',     'bool',   0, 0, 0, 7),
(41, 'cluster_name',   '所属集群',     'string', 0, 0, 0, 8);

-- Redis attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(43, 'ip_address',     '连接地址',     'string', 1, 0, 1, 1),
(43, 'port',           '端口',         'int',    1, 0, 0, 2),
(43, 'version',        '版本',         'string', 0, 0, 0, 3),
(43, 'max_memory_mb',  '最大内存(MB)', 'int',    0, 0, 0, 4),
(43, 'cluster_mode',   '集群模式',     'enum',   0, 0, 0, 5),
(43, 'persistence',    '持久化方式',   'enum',   0, 0, 0, 6);

-- Kafka attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(46, 'broker_list',    'Broker列表',   'string', 1, 0, 0, 1),
(46, 'version',        '版本',         'string', 0, 0, 0, 2),
(46, 'zookeeper',      'ZK地址',       'string', 0, 0, 0, 3),
(46, 'topic_count',    'Topic数量',    'int',    0, 0, 0, 4),
(46, 'partition_default','默认分区数', 'int',    0, 0, 0, 5);

-- Nginx attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(48, 'ip_address',     '监听地址',     'string', 1, 0, 1, 1),
(48, 'port',           '端口',         'int',    0, 0, 0, 2),
(48, 'version',        '版本',         'string', 0, 0, 0, 3),
(48, 'upstream_count', '上游数量',     'int',    0, 0, 0, 4),
(48, 'config_path',    '配置路径',     'string', 0, 0, 0, 5);

-- Network Switch attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(31, 'ip_address',     '管理IP',       'string', 1, 1, 1, 1),
(31, 'sn',             '序列号',       'string', 0, 1, 1, 2),
(31, 'manufacturer',   '厂商',         'enum',   0, 0, 0, 3),
(31, 'model',          '型号',         'string', 0, 0, 0, 4),
(31, 'port_count',     '端口数',       'int',    0, 0, 0, 5),
(31, 'firmware',       '固件版本',     'string', 0, 0, 0, 6),
(31, 'location',       '物理位置',     'string', 0, 0, 0, 7);

-- LoadBalancer attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(34, 'ip_address',     'VIP地址',      'string', 1, 1, 1, 1),
(34, 'type',           '类型',         'enum',   0, 0, 0, 2),
(34, 'algorithm',      '调度算法',     'enum',   0, 0, 0, 3),
(34, 'backend_count',  '后端数量',     'int',    0, 0, 0, 4);

-- MicroService attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(21, 'service_name',   '服务名',       'string', 1, 1, 1, 1),
(21, 'version',        '版本',         'string', 0, 0, 0, 2),
(21, 'language',       '开发语言',     'enum',   0, 0, 0, 3),
(21, 'framework',      '框架',         'enum',   0, 0, 0, 4),
(21, 'port',           '服务端口',     'int',    0, 0, 0, 5),
(21, 'health_check',   '健康检查URL',  'string', 0, 0, 0, 6),
(21, 'git_repo',       'Git仓库',      'string', 0, 0, 0, 7),
(21, 'owner_team',     '负责团队',     'string', 0, 0, 0, 8);

-- WebApp attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(22, 'domain',         '域名',         'string', 0, 1, 1, 1),
(22, 'port',           '端口',         'int',    0, 0, 0, 2),
(22, 'framework',      '前端框架',     'enum',   0, 0, 0, 3),
(22, 'cdn_enabled',    '是否使用CDN',  'bool',   0, 0, 0, 4),
(22, 'git_repo',       'Git仓库',      'string', 0, 0, 0, 5);

-- DataCenter attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(61, 'location',       '地理位置',     'string', 1, 0, 0, 1),
(61, 'tier_level',     '等级(Tier)',   'enum',   0, 0, 0, 2),
(61, 'total_racks',    '机柜总数',     'int',    0, 0, 0, 3),
(61, 'power_capacity', '电力容量(KW)', 'float',  0, 0, 0, 4),
(61, 'contact',        '机房联系人',   'string', 0, 0, 0, 5),
(61, 'contact_phone',  '联系电话',     'string', 0, 0, 0, 6);

-- Department attributes
INSERT INTO ci_attribute (ci_type_id, name, display_name, value_type, is_required, is_unique, is_indexed, sort_order) VALUES
(73, 'dept_code',      '部门编码',     'string', 1, 1, 1, 1),
(73, 'manager',        '负责人',       'string', 0, 0, 0, 2),
(73, 'parent_dept',    '上级部门',     'string', 0, 0, 0, 3);

-- 3. Relation Rules
-- Requires UNIQUE (name, source_type_id, target_type_id), matching the model.
-- Existing schemas are upgraded by server startup or 002_relation_rule_unique.sql.
-- ============================================================================
INSERT INTO ci_relation_rule (id, name, display_name, reverse_name, source_type_id, target_type_id, cardinality, is_hard_dependency) VALUES
(1,  'runs_on',           '运行在',          '运行着',       12, 11, 'N:1', 1),
(2,  'runs_on',           '运行在',          '运行着',       13, 11, 'N:1', 0),
(3,  'hosts_vm',          '承载虚拟机',      '运行在',       11, 12, '1:N', 1),
(4,  'deployed_on',       '部署在',          '部署着',       21, 12, 'N:1', 1),
(5,  'deployed_on',       '部署在',          '部署着',       22, 12, 'N:1', 0),
(6,  'connects_to',       '连接到',          '被连接',       21, 41, 'N:1', 1),
(7,  'connects_to',       '连接到',          '被连接',       21, 43, 'N:1', 1),
(8,  'connects_to',       '连接到',          '被连接',       21, 46, 'N:1', 0),
(9,  'belongs_to_ns',     '属于命名空间',    '包含',         18, 17, 'N:1', 0),
(10, 'scheduled_on',      '调度在',          '运行',         18, 16, 'N:1', 1),
(11, 'routes_to',         '路由到',          '被路由',       34, 12, '1:N', 1),
(12, 'routes_to',         '路由到',          '被路由',       48, 21, '1:N', 1),
(13, 'protected_by',      '受防护',          '防护',         12, 33, 'N:1', 0),
(14, 'connects_to_switch','接入交换机',      '接入',         11, 31, 'N:1', 1),
(15, 'located_in',        '位于',            '安放',         62, 61, 'N:1', 1),
(16, 'contains_rack',     '包含机柜',        '位于',         11, 62, 'N:1', 1),
(17, 'belongs_to',        '属于',            '拥有',         12, 73, 'N:1', 0),
(18, 'belongs_to',        '属于',            '拥有',         21, 72, 'N:1', 0),
(19, 'depends_on',        '软件依赖',        '被依赖',       21, 41, 'N:1', 1),
(20, 'depends_on',        '软件依赖',        '被依赖',       21, 43, 'N:1', 1);
