# CMDB - 企业级配置管理数据库系统

基于 Gin + Vue 3 + MySQL 8.4 构建的 IT 资产与配置管理平台，适用于 500+ 服务器规模的企业基础设施。

## 快速开始

### 1. 环境要求

| 组件 | 版本 |
|------|------|
| Go | 1.23+ |
| Node.js | 18+ |
| MySQL | 8.4 |
| Docker (可选) | 24+ |

### 2. 方式一：Docker Compose（推荐）

```bash
# 启动 MySQL + 后端
docker-compose up -d

# 查看日志
docker-compose logs -f backend
```

访问 http://localhost:8080，默认账号 `admin / admin123`。

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
npm install
npm run dev
```

前端开发服务器运行在 http://localhost:3000，自动代理 API 到后端 8080 端口。

### 4. 初始化数据

Docker Compose 启动时会自动执行 `backend/migrations/001_seed.sql` 初始化 CI 类型、属性和关系规则。本地开发时可手动导入：

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
│   │   ├── middleware/         # JWT/RBAC/CORS
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
├── docs/CMDB-ARCHITECTURE.md   # 完整架构设计文档
└── docker-compose.yml
```

## API 概览

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/api/v1/auth/login` | 登录获取 Token |
| GET  | `/api/v1/ci-types` | CI 类型树 |
| POST | `/api/v1/ci-types` | 创建 CI 类型 |
| GET  | `/api/v1/ci-types/:id/attributes` | 获取类型属性 |
| GET  | `/api/v1/ci-instances` | CI 实例列表 (支持分页/筛选) |
| POST | `/api/v1/ci-instances` | 创建 CI 实例 |
| GET  | `/api/v1/relations/rules` | 关系规则列表 |
| GET  | `/api/v1/relations/instances` | 关系实例列表 |
| GET  | `/api/v1/relations/topology?ci_id=1&depth=3` | 多层拓扑图谱 (nodes+edges) |
POST | `/api/v1/ci-instances/import` | CSV 批量导入 |
GET  | `/api/v1/ci-instances/export` | CSV 批量导出 |
GET  | `/api/v1/discovery/collectors` | 采集器类型列表 |
POST | `/api/v1/discovery/strategies` | 创建发现策略 |
GET  | `/api/v1/discovery/strategies` | 发现策略列表 |
GET  | `/api/v1/discovery/strategies/:id` | 获取发现策略 |
PUT  | `/api/v1/discovery/strategies/:id` | 更新发现策略 |
DELETE | `/api/v1/discovery/strategies/:id` | 删除发现策略 |
POST | `/api/v1/changes` | 创建变更单 |
GET  | `/api/v1/changes` | 变更单列表 |
GET  | `/api/v1/changes/:id` | 变更单详情 |
PUT  | `/api/v1/changes/:id` | 更新变更单 |
POST | `/api/v1/changes/:id/submit` | 提交审批 |
POST | `/api/v1/changes/:id/approve` | 批准变更 |
POST | `/api/v1/changes/:id/reject` | 驳回变更 |
POST | `/api/v1/changes/:id/execute` | 执行变更 |
POST | `/api/v1/changes/:id/complete` | 标记完成 |
POST | `/api/v1/changes/:id/rollback` | 回滚变更 |
POST | `/api/v1/changes/:id/fail` | 标记失败 |
GET  | `/api/v1/discovery/strategies/:id/history` | 策略执行历史 |
GET  | `/api/v1/snapshots` | 配置快照列表 |
GET  | `/api/v1/snapshots/:id` | 获取快照详情 |
GET  | `/api/v1/snapshots/diff?from=X&to=Y` | 快照差异对比 |
| GET  | `/api/v1/dashboard/summary` | 仪表盘汇总 |
GET  | `/api/v1/dashboard/capacity` | 容量概览 |

详细 API 设计见 [CMDB-ARCHITECTURE.md](docs/CMDB-ARCHITECTURE.md)。

## 设计决策

- **EAV + JSON 混合模型**：动态属性存 JSON，高频查询字段冗余索引列，兼顾灵活性与性能
- **MySQL CTE 递归查询**：实现拓扑影响分析，500 台规模下无需图数据库
- **RBAC 权限**：超级管理员 / CMDB管理员 / 资产管理员 / 只读用户
- **插件式采集器**：Go interface 注册模式，新增采集源无需改核心代码

## MVP 路线图

- **Phase 1** (已完成): 资产 CRUD、CI模型、RBAC、基础搜索
- **Phase 2** (规划中): 自动发现(Agent/SSH/K8s/Cloud)、配置快照Diff
- **Phase 3** (规划中): 拓扑图谱可视化、变更管理、Dashboard
- **Phase 4** (规划中): 监控/工单/自动化平台集成、审计增强

## License

MIT