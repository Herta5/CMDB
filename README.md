# CMDB - 企业级配置管理数据库系统

基于 Gin + Vue 3 + MySQL 8.4 构建的 IT 资产与配置管理平台，适用于 500+ 服务器规模的企业基础设施。

## 快速开始

### 1. 环境要求

| 组件 | 版本 |
|------|------|
| Go | 1.25.1+ |
| Node.js | 18+ |
| MySQL | 8.4 |
| Docker (可选) | 24+ |

### 2. 方式一：Docker Compose（推荐）

```bash
# 构建并启动 MySQL + 单体应用镜像
docker compose up -d --build

# 查看日志
docker compose logs -f
```

访问 `http://服务器IP`（本机访问 `http://localhost`），首次启动自动创建默认账号 `admin / admin123`。

Docker Compose 会启动独立的 MySQL 容器和一个应用镜像。该应用镜像同时包含前端静态文件与 Go 后端，由 Go 在 80 端口同时提供 UI 和 API；MySQL 3306 仅在容器内部网络开放。当前未提供 Kubernetes 部署配置。

### 3. 方式二：本地开发

**后端:**

```bash
cd backend

# 设置环境变量 (Windows PowerShell)
$env:DB_HOST='127.0.0.1'
$env:DB_PORT='3306'
$env:DB_USER='root'
$env:DB_PASSWORD='your-password'
$env:DB_NAME='cmdb'
$env:JWT_SECRET='your-secret-key'

# 下载依赖 & 构建
go mod tidy
go build -o cmdb-server.exe ./cmd/server

# 运行
./cmdb-server.exe
```

**前端:**

```bash
cd frontend
corepack pnpm install --frozen-lockfile
corepack pnpm dev
```

前端开发服务器运行在 http://localhost:3000，自动代理 API 到后端 8080 端口。

### 4. 初始化数据

Docker Compose 启动时会自动执行 `backend/migrations/001_seed.sql` 初始化 CI 类型、属性和关系规则。默认管理员账号由 `SeedDefaultAdmin()` 在首次迁移时自动创建。

本地开发时可手动导入：

```bash
mysql -u root -p cmdb < backend/migrations/001_seed.sql
```

## 项目结构

```
github-cmdb/
├── backend/                    # Go 后端
│   ├── cmd/server/main.go      # 入口
│   ├── internal/
│   │   ├── config/             # 配置
│   │   ├── model/              # 数据模型 (GORM)
│   │   ├── handler/            # HTTP Handler
│   │   ├── service/            # 业务逻辑
│   │   ├── repository/         # 数据访问
│   │   ├── middleware/         # JWT/RBAC/CORS/审计
│   │   ├── collector/          # 自动发现采集器
│   │   ├── eventbus/           # 事件总线
│   │   └── router/             # 路由定义
│   ├── migrations/             # SQL 初始化脚本
│   └── pkg/response/           # 统一响应格式
├── frontend/                   # Vue 3 前端
│   ├── src/
│   │   ├── views/              # 页面组件
│   │   ├── components/         # 公共组件
│   │   ├── api/                # API 封装
│   │   ├── router/             # 路由
│   │   ├── stores/             # Pinia 状态
│   │   └── utils/              # 工具(axios)
│   └── vite.config.ts
└── docker-compose.yml
```

## API 概览

### 认证
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | 登录获取 Token |

### 用户管理 (Phase 5)
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/users` | 用户列表 (分页+筛选) |
| POST | `/api/v1/users` | 创建用户 |
| GET  | `/api/v1/users/:id` | 用户详情 |
| PUT  | `/api/v1/users/:id` | 更新用户 |
| DELETE | `/api/v1/users/:id` | 删除用户 |
| PUT  | `/api/v1/users/:id/password` | 管理员重置密码 |
| PUT  | `/api/v1/profile/password` | 当前用户自助修改密码 |

### CI 管理
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/ci-types` | CI 类型树 |
| POST | `/api/v1/ci-types` | 创建 CI 类型 |
| GET  | `/api/v1/ci-types/:id/attributes` | 获取类型属性 |
| GET  | `/api/v1/ci-instances` | CI 实例列表 (支持分页/筛选) |
| POST | `/api/v1/ci-instances` | 创建 CI 实例 |
| GET  | `/api/v1/ci-instances/:id` | CI 实例详情 |
| POST | `/api/v1/ci-instances/import` | CSV 批量导入 |
| GET  | `/api/v1/ci-instances/export` | CSV 批量导出 |

### 关系与拓扑
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/relations/rules` | 关系规则列表 |
| GET  | `/api/v1/relations/instances` | 关系实例列表 |
| GET  | `/api/v1/relations/topology?ci_id=1&depth=3` | 多层拓扑图谱 (nodes+edges) |
| GET  | `/api/v1/relations/impact?ci_id=1` | 影响分析 |

### 变更管理
| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/changes` | 创建变更单 |
| GET  | `/api/v1/changes` | 变更单列表 |
| POST | `/api/v1/changes/:id/submit` | 提交审批 |
| POST | `/api/v1/changes/:id/approve` | 批准变更 |
| POST | `/api/v1/changes/:id/execute` | 执行变更 |
| POST | `/api/v1/changes/:id/complete` | 标记完成 |
| POST | `/api/v1/changes/:id/rollback` | 回滚变更 |

### 自动发现
目前可执行的采集器为 SSH（Agentless）主机发现；Agent 和 Kubernetes API 采集器仅保留接口占位，Cloud 采集器尚未注册。

| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/discovery/collectors` | 采集器类型列表 |
| POST | `/api/v1/discovery/strategies` | 创建发现策略 |
| GET  | `/api/v1/discovery/strategies/:id/history` | 策略执行历史 |

### 快照
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/snapshots` | 配置快照列表 |
| GET  | `/api/v1/snapshots/diff?from=X&to=Y` | 快照差异对比 |

### 仪表盘
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/dashboard/summary` | 仪表盘汇总 |
| GET  | `/api/v1/dashboard/distribution` | CI 分布统计 |
| GET  | `/api/v1/dashboard/trends` | 趋势数据 |
| GET  | `/api/v1/dashboard/capacity` | 容量概览 |

### 系统集成
| 方法 | 路径 | 说明 |
|------|------|------|
| GET  | `/api/v1/integration/prometheus/targets` | Prometheus HTTP SD 目标 (公开) |
| GET  | `/api/v1/integration/ansible/inventory` | Ansible 动态清单 (公开) |
| POST | `/api/v1/integration/webhook/alertmanager` | 接收 Alertmanager 告警 (公开) |
| POST | `/api/v1/integration/webhook/generic` | 通用 Webhook 接收端 (公开) |
| GET  | `/api/v1/integration/webhooks` | Webhook 接收历史 |
| GET  | `/api/v1/audit/logs` | 操作审计日志 (cmdb_admin) |

## 设计决策

- **EAV + JSON 混合模型**：动态属性存 JSON，高频查询字段冗余索引列，兼顾灵活性与性能
- **MySQL CTE 递归查询**：实现拓扑影响分析，500 台规模下无需图数据库
- **RBAC 权限**：super_admin / cmdb_admin / asset_mgr / change_op / viewer 五种角色
- **bcrypt 密码哈希**：用户密码加盐存储，防彩虹表与暴力破解
- **插件式采集器**：Go interface 注册模式，新增采集源无需改核心代码
- **事件总线**：发布/订阅模式，change 状态变更和 CI 生命周期变更可被外部系统订阅
- **审计日志**：基于 Gin 中间件，异步写入，记录用户操作、请求参数、响应状态等

## MVP 路线图

- **Phase 1 (已完成)**: 资产 CRUD、CI模型、RBAC、基础搜索
- **Phase 2 (部分完成)**: SSH（Agentless）自动发现、发现策略/采集历史、配置快照 Diff；Agent、K8s 和 Cloud 采集器仍待实现
- **Phase 3 (已完成)**: 拓扑图谱可视化、变更管理(审批流)、Dashboard、批量导入导出
- **Phase 4 (已完成)**: 系统集成(Prometheus/Ansible/Webhook)、审计日志、事件总线
- **Phase 5 (进行中)**: 用户管理(DB-backed登录、用户CRUD、角色分配、密码策略)
- **Phase 6 (规划中)**: 多级审批流、集成自动化平台(Jenkins/GitLab CI)、合规报表

## License

MIT
