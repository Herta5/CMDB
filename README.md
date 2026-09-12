# CMDB

CMDB 是面向公有云的资源配置管理平台，业务项目是最高级的数据归属和权限隔离边界。

当前提供用户名密码登录、用户与业务项目管理、项目成员授权，以及阿里云、AWS 两个独立资源模块。平台模块共享接入源、凭证加密、同步任务、资源地址、失联恢复、24 小时清理和审计能力；默认每 60 分钟自动同步，并支持项目管理员手工触发。

产品长期要求请从 [项目规范入口](docs/project/README.md) 阅读，README 不替代项目规范；实现基线及本轮修复的证据与差距请参阅 [实现状态](docs/project/implementation-status.md)。

## 技术栈

后端使用 Go 1.25.1、Gin、GORM；前端使用 Vue 3、TypeScript、Pinia、Vue Router、Element Plus；数据库使用 PostgreSQL 17。Docker Compose 启动一个 PostgreSQL 容器和一个同时提供前端静态文件与 API 的应用容器。

## 从空库部署

需要 Docker Engine、Docker Compose、Bash 和 OpenSSL。部署使用独立的 `cmdb-postgresql-data` 数据卷：首次创建空卷时，PostgreSQL 按文件名顺序以管理员身份执行 `backend/database/init/001_create_app_role.sh` 和 `002_schema.sql`，先创建受限的 `cmdb` 应用账号，再建立当前版本 2 业务结构；不会创建默认账号、业务项目或云接入源。

先在当前终端设置部署环境。PostgreSQL 管理员密码与 `cmdb` 应用账号密码必须分别设置且不得相同；两个应用密钥独立随机生成。以下命令不会回显输入或生成值：

```bash
read -rsp 'PostgreSQL 管理员密码：' POSTGRES_ADMIN_PASSWORD; echo
read -rsp 'CMDB 数据库用户密码：' DB_PASSWORD; echo
export POSTGRES_ADMIN_PASSWORD DB_PASSWORD
export JWT_SECRET="$(openssl rand -hex 32)"
export CMDB_ENCRYPTION_KEY="$(openssl rand -hex 32)"

# 请将这四个值保存在部署环境的安全配置中；后续重启沿用原值，不要重新生成。
docker compose config --quiet
docker compose up -d --build
docker compose ps
```

所有四个安全变量均无默认值，缺失或为空时 Compose 拒绝启动。应用使用独立的 `cmdb` 数据库用户；管理员 `postgres` 账号仅用于数据库初始化，不得配置给应用。Compose 将 PostgreSQL 的 `5432` 端口发布到宿主机，外部运维连接应使用 `cmdb` 账号并在命令提示时输入数据库密码，例如 `psql -h 127.0.0.1 -p 5432 -U cmdb -d cmdb`；生产环境必须通过网络策略限制该端口来源。普通 `docker compose config` 会展开环境值，请使用 `--quiet` 验证配置，避免将输出贴入日志、工单或版本库。生产环境应在入口代理配置 HTTPS。

PostgreSQL 首次就绪后，显式创建第一个系统管理员。用户名可自行选择，密码必须为 12 至 72 字节；密码通过标准输入传入，不使用命令行参数，也不写入应用环境：

```bash
read -rsp '首次系统管理员密码：' CMDB_INITIAL_PASSWORD; echo
printf '%s' "$CMDB_INITIAL_PASSWORD" | docker compose exec -T app ./cmdb-init-admin --username operator
unset CMDB_INITIAL_PASSWORD
```

初始化命令只保存 bcrypt 哈希；只要 `users` 表已有任何用户就会拒绝再次执行，不修改或覆盖已有身份。系统管理员登录后可在权限管理的用户页面创建和编辑用户，并在同一表单维护启停状态、全局角色以及多个项目的独立角色；密码只以 bcrypt 哈希保存且不会通过接口回显。

打开 [CMDB 控制台](http://localhost)，用刚创建的身份登录。空库首次登录显示空项目状态；系统管理员通过项目 API 创建项目后即可在页面查看。默认使用 HTTP 端口 `80`，可通过 `CMDB_PORT` 覆盖。健康检查地址为 `/health`，仅报告 HTTP 进程存活，不代表数据库或下游服务就绪。

初始化脚本只在新建的空数据卷首次启动时执行；已有数据卷、已有空库或外部数据库不会自动重新初始化。服务启动只读取 `schema_migrations` 并要求结构精确为版本 2，不会自动迁移、覆盖或转换现有表；版本表缺失、旧版或超前版都会在启动调度器和 HTTP 服务前拒绝启动。受支持的 PostgreSQL 旧库按下一节执行显式升级；未知结构和从 MySQL 迁移到 PostgreSQL 需要另行制定并验证数据迁移方案。停止服务使用 `docker compose down`，不要附加 `-v`，以保留数据。

## 已有数据卷升级

已有 PostgreSQL 数据卷升级必须由运维显式执行，且应用数据库账号不得执行 DDL。长期运行的 `app` 服务只保留受限 `DB_USER=cmdb`，`DB_MIGRATION_USER` 和 `DB_MIGRATION_PASSWORD` 不写入 `docker-compose.yml`、镜像或长期应用环境。开始前确认当前镜像与仓库版本匹配，并按以下顺序操作：

安全迁移前置条件：所有旧接入源的 `config` 必须是空对象 `{}`。如旧版保存过非空或非对象配置，先保留可恢复备份并由管理员确认清理，通过旧版正常管理入口显式清空；随后停止旧应用，重新备份并执行下面的升级步骤。迁移器发现不受支持的旧配置时会在任何结构变化前整体拒绝，不会自动删除、改写或回显旧配置。不要等到升级为待验证后再尝试普通编辑。

1. 停止应用，保持 PostgreSQL 运行；升级期间不要重新启动 `app`。

   ```bash
   docker compose stop app
   ```

2. 备份数据库或数据卷，并按现有恢复流程验证备份可用。以下示例以 PostgreSQL 管理员从容器内生成自定义格式数据库备份，文件保存在当前目录：

   ```bash
   CMDB_BACKUP_FILE="cmdb-before-upgrade-$(date +%Y%m%d-%H%M%S).dump"
   docker compose exec -T postgresql pg_dump -U postgres -d cmdb --format=custom > "$CMDB_BACKUP_FILE"
   test -s "$CMDB_BACKUP_FILE"
   ```

3. 在当前终端读取一次性管理员迁移凭证，并通过 `docker compose run --rm` 显式传入迁移容器。用户名和密码不能与受限应用账号混用；命令不接受密码参数，也不会读取 JWT 或凭证加密密钥。

   ```bash
   read -rp '数据库迁移管理员用户名：' DB_MIGRATION_USER
   read -rsp '数据库迁移管理员密码：' DB_MIGRATION_PASSWORD; echo
   export DB_MIGRATION_USER DB_MIGRATION_PASSWORD
   migration_status=1
   if docker compose run --rm \
       -e DB_MIGRATION_USER \
       -e DB_MIGRATION_PASSWORD \
       app ./cmdb-migrate; then
     migration_status=0
   fi
   ```

4. 确认迁移命令输出“数据库迁移成功”，再核对数据库最高版本为 `2`。重复执行同一迁移命令必须安全成功且不重复修改结构或数据。

   ```bash
   version_status=1
   idempotency_status=1
   schema_version=''
   if [ "$migration_status" -eq 0 ] && \
      schema_version="$(docker compose exec -T postgresql psql -U postgres -d cmdb -Atc 'SELECT COALESCE(MAX(version), 0) FROM schema_migrations;')" && \
      [ "$schema_version" = '2' ]; then
     version_status=0
     if docker compose run --rm \
         -e DB_MIGRATION_USER \
         -e DB_MIGRATION_PASSWORD \
         app ./cmdb-migrate; then
       idempotency_status=0
     fi
   fi
   ```

5. 无论迁移成功或失败，都先清除当前终端中的管理员迁移凭证。只有首次迁移、版本核对和幂等复跑全部成功后才启动应用；任一失败都保持应用停止，并立即进入已验证的备份恢复流程。

   ```bash
   unset DB_MIGRATION_USER DB_MIGRATION_PASSWORD
   if [ "$migration_status" -eq 0 ] && [ "$version_status" -eq 0 ] && [ "$idempotency_status" -eq 0 ]; then
     docker compose up -d app
     docker compose ps
   else
     docker compose stop app
     printf '%s\n' "迁移或版本核对失败，应用保持停止；请使用备份 ${CMDB_BACKUP_FILE} 按已验证流程恢复后重新升级。" >&2
     false
   fi
   unset migration_status version_status idempotency_status schema_version
   ```

6. 登录“云同步管理”，逐个处理显示为“待验证”的历史接入源。可以使用已安全保存的原凭证，也可以输入一套完整新凭证；验证成功前，该接入源只能读取，不能编辑、启停、删除、连接测试、立即同步或重试。

迁移会先精确识别受支持旧结构，并在单版本事务中将历史接入源置为待验证、建立账号唯一约束和三类资产限制外键；未知或部分结构、孤儿及跨项目资产会安全拒绝并回滚。上一步返回失败时不得继续执行后续身份验证：必须确认 `app` 仍为停止状态，使用第 2 步的 `$CMDB_BACKUP_FILE` 按已验证恢复流程还原并核验数据库，再从第 3 步重新升级。不要启动应用继续使用不匹配结构，不要用 `cmdb` 应用账号执行 DDL，也不要尝试自动降级已提交的结构版本。失败终端输出仅用于识别安全错误阶段，不应包含或记录连接密码和底层数据库详情。

若安全摘要为“历史接入源配置非空”，按上述恢复流程核验旧库后恢复匹配的旧版本应用，通过旧版正常管理入口显式清空配置；再次停止旧应用，重新执行第 2 步备份后再用新版本从第 3 步重试。不得在未清空配置的情况下反复升级，也不得用新版本 pending 来源的普通编辑入口绕过迁移前置条件。管理员迁移变量仍必须在失败后立即清除。

## API 与权限

除登录和健康检查外，请求均通过 `Authorization: Bearer <会话令牌>` 认证。登录响应仅包含会话令牌与公开身份资料。API 直接返回 JSON 数据，失败返回稳定的 `code` 与中文 `message`。

| 方法 | 路径 | 权限与用途 |
| --- | --- | --- |
| POST | `/api/v1/auth/login` | 用户名密码登录 |
| GET | `/api/v1/me` | 当前登录身份 |
| GET | `/api/v1/users` | 系统管理员查看用户列表 |
| POST | `/api/v1/users` | 系统管理员创建用户，并原子设置全局角色、状态和多项目权限 |
| DELETE | `/api/v1/users/:username` | 系统管理员删除其他用户及其项目成员关系 |
| PUT | `/api/v1/users/:username/status` | 系统管理员启用或停用用户 |
| PUT | `/api/v1/users/:username` | 系统管理员编辑显示名称、邮箱、全局角色、状态、可选新密码及全量项目权限；用户名不可修改 |
| GET | `/api/v1/projects` | 系统管理员查看全部；普通用户仅查看所属项目 |
| POST | `/api/v1/projects` | 系统管理员创建项目，必填 `code`、`name`，可选 `owner_username` 指定负责人 |
| GET | `/api/v1/projects/:id` | 系统管理员或该项目成员查看详情，负责人字段为 `owner_username` |
| PUT | `/api/v1/projects/:id` | 系统管理员修改项目，可用 `owner_username` 调整负责人；`code` 创建后不可变 |
| DELETE | `/api/v1/projects/:id` | 系统管理员删除项目 |
| GET | `/api/v1/projects/:id/members` | 系统管理员或该项目成员查看成员 |
| POST | `/api/v1/projects/:id/members` | 系统管理员或项目管理员添加成员，提供 `username`、`role` |
| PUT | `/api/v1/projects/:id/members/:username` | 系统管理员或项目管理员修改 `role` |
| DELETE | `/api/v1/projects/:id/members/:username` | 系统管理员或项目管理员移除成员 |
| GET | `/api/v1/projects/:id/member-candidates` | 系统管理员或项目管理员查询可添加用户的最小公开资料 |
| GET / POST | `/api/v1/projects/:id/sources` | 项目成员查询；系统或项目管理员创建接入源 |
| PUT / DELETE | `/api/v1/projects/:id/sources/:sourceId` | 系统或项目管理员更新、启停或删除接入源 |
| POST | `/api/v1/projects/:id/sources/:sourceId/verify-identity` | 系统或项目管理员在启用项目内验证历史接入源身份，可选提交完整新凭证 |
| POST | `/api/v1/projects/:id/sources/:sourceId/sync` | 系统或项目管理员提交后台同步任务，同源并发返回 409 |
| POST | `/api/v1/projects/:id/sources/:sourceId/test` | 系统或项目管理员测试现有凭证和网络，不写入资源 |
| GET | `/api/v1/projects/:id/resources` | 项目成员按平台、类型、资产状态分页查询资源 |
| GET | `/api/v1/projects/:id/sync-jobs` | 项目成员查询脱敏同步历史与类型级统计 |
| POST | `/api/v1/projects/:id/sync-jobs/:jobId/retry` | 系统或项目管理员重试失败或部分成功任务 |

全局角色为 `system_admin`、`user`；项目角色为 `project_admin`、`member`。系统管理员是显式全局权限例外；普通用户必须具有对应项目成员关系，前端切换项目不授予权限。项目管理员可以查看资产并管理本项目成员、接入源及同步任务，项目成员仅可查看项目和资产。未授权项目与不存在项目返回相同错误以隐藏目标存在性，移除成员后原会话的项目权限立即失效。

用户名是用户唯一且不可修改的公开标识，长度为 1 至 64 个字符，只能包含 ASCII 字母、数字和下划线（`^[A-Za-z0-9_]+$`）。所有用户路径参数、项目负责人、项目成员和审计操作人筛选均使用用户名；JWT 仅携带用户名等公开身份声明，不携带用户数字 ID。

部署数据库仍以 `users.id`、`project_members.user_id`、`projects.owner_user_id` 和 `audit_logs.actor_id` 维护内部主键、外键与审计关联。这些字段只在服务端和数据库内部使用，不是公开 API、页面、浏览器存储或 JWT 契约的一部分。

接入凭证由 `CMDB_ENCRYPTION_KEY` 派生的 AES-256-GCM 密钥加密，接口、同步任务和审计均不返回凭证明文或完整密文。新建和显式替换凭证会先按阿里云或 AWS 字段白名单严格校验，再通过对应云平台 STS 的轻量调用识别稳定账号；自动测试使用适配器接口与模拟响应，不连接真实云账号。同平台账号只能由一个已验证接入源占用，历史来源升级后必须在“云同步管理”中逐个验证。手工同步先返回排队任务，页面自动刷新运行状态；服务重启会恢复已验证来源的排队任务，并把待验证来源遗留任务安全结束。资源首次从成功采集结果中缺失时标记“已失联”，重新出现时恢复原记录，连续失联满 24 小时后物理删除；认证失败、零成功类型、无效类型集合和任务未执行不会触发错误失联，只有自动失败会推进下次计划。项目或接入源仍有三类资产或排队/运行任务时不能删除，系统管理员也不能绕过该领域不变量。

## 本地开发与验证

后端需要可连接的 PostgreSQL 17 和已执行初始化结构的数据库；设置 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`JWT_SECRET`、`CMDB_ENCRYPTION_KEY`。`SERVER_PORT` 默认 `8080`，本地独立前端开发不设置 `STATIC_DIR`。

```bash
cd backend
go run ./cmd/server
```

前端要求 Node.js 20 与 Corepack，在另一个终端运行：

```bash
cd frontend
corepack pnpm install --frozen-lockfile
corepack pnpm dev
```

[开发控制台](http://localhost:3000) 将 `/api` 请求代理至本机后端 `8080` 端口。

```bash
# 后端测试使用隔离 SQLite 数据库，需要本机 C 编译器，不依赖真实云账号。
cd backend
go test ./...

# 前端验证：状态与应用流程、类型检查、生产构建。
cd ../frontend
corepack pnpm test
corepack pnpm exec vue-tsc --noEmit
corepack pnpm build
```

后端入口为 `backend/cmd/server`，一次性初始化入口为 `backend/cmd/init-admin`，一次性 PostgreSQL 管理员升级入口为 `backend/cmd/migrate`；共享资源核心位于 `backend/internal/resource`，平台采集器分别位于 `backend/internal/aliyun`、`aws`。前端共享资源模块位于 `frontend/src/modules/resource`。
