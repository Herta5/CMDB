# CMDB P0 安全一致性迭代实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完成接入源稳定云账号身份、项目与接入源删除保护、同步失败状态及自动调度修正，并以显式 PostgreSQL 17 管理员迁移安全升级已有数据。

**Architecture:** 阿里云与 AWS 通过统一 `ProviderAdapter` 提供严格字段校验、账号识别、轻量探测和采集，共享资源核心负责身份不可变、唯一归属、事务、删除保护和同步终态。数据库以部分唯一索引、限制外键和结构版本提供最终约束，生产应用只检查版本，一次性 `cmdb-migrate` 使用管理员权限执行升级。

**Tech Stack:** Go 1.25.1、Gin、GORM、PostgreSQL 17、Vue 3、TypeScript、Pinia、Vue Router、Element Plus、Vitest、Docker Compose。

**Spec:** `docs/superpowers/specs/2026-09-11-p0-safety-iteration-design.md`

## Global Constraints

- 项目名称统一为 **CMDB**；业务项目是最高级数据归属和普通用户权限隔离边界，系统管理员是授权层最高权限但不能绕过领域不变量。
- 阿里云和 AWS 保持同一 CMDB 服务内边界清晰的独立模块，不新增微服务，不更换核心技术栈。
- 新功能和缺陷修复必须先写能失败的测试，再写最小实现；每个任务完成受影响验证后单独提交。
- 云平台自动测试只使用虚构凭证、接口和模拟响应，不访问真实云账号。
- 所有用户可见文案、业务错误、接口说明和测试描述使用中文；不得泄露凭证、完整密文、原始云错误、令牌或内部用户 ID。
- 应用账号无 DDL 权限，应用启动不自动改表；已有数据卷只由显式 PostgreSQL 管理员迁移命令升级。
- 任何功能新增或业务行为变化必须同步更新 `docs/project/` 中的主责规范、验收场景和实现状态，缺少必要文档不得合并。
- 所有仓库修改保留在 `codex/p0-safety-iteration`，最终纳入最新 `main`、通过完整验证并合并后才算交付。

## File Structure

### 新建文件

- `backend/internal/platform/database/schema_version.go`：应用启动结构版本只读检查。
- `backend/internal/platform/database/migrator.go`：管理员迁移锁、旧结构识别和按版本事务执行。
- `backend/internal/platform/database/migrations/002_p0_safety.sql`：已有 PostgreSQL 数据库的 P0 增量升级。
- `backend/internal/platform/database/schema_version_test.go`、`migrator_test.go`：版本和迁移单元测试。
- `backend/internal/platform/database/postgres_integration_test.go`：PostgreSQL 17 唯一索引、限制外键、迁移和权限集成测试。
- `backend/test/postgres/docker-compose.yml`：仅用于自动化验收的 PostgreSQL 17 临时实例。
- `backend/cmd/migrate/main.go`、`main_test.go`：一次性 `cmdb-migrate` 命令及安全配置测试。
- `backend/internal/security/sensitive.go`、`sensitive_test.go`：跨审计和接入配置复用的敏感键分类。
- `backend/internal/resource/provider.go`、`provider_test.go`：平台适配接口、严格 JSON 对象辅助校验和稳定领域错误。
- `backend/internal/resource/sync_decision.go`、`sync_decision_test.go`：无副作用同步终态决策。
- `backend/internal/resource/deletion_guard.go`：三类资产和活动任务依赖检查及父记录锁。
- `backend/internal/aliyun/identity.go`、`identity_test.go`：阿里云账号识别。
- `backend/internal/aws/identity.go`、`identity_test.go`：AWS 账号识别。

### 重点修改文件

- `backend/database/init/002_schema.sql`：空库最新结构、身份字段、部分唯一索引、限制外键和结构版本。
- `backend/internal/resource/model.go`、`repository.go`、`service.go`、`http.go`：身份状态、原子接入流程、运行门禁、删除保护和同步调度。
- `backend/internal/aliyun/collector.go`、`backend/internal/aws/collector.go`：让现有采集器实现 `ProviderAdapter`。
- `backend/internal/project/repository.go`、`service.go`、`http.go`：项目删除事务内依赖保护与 `409` 映射。
- `backend/internal/platform/httpserver/server.go`、`integration_test.go`、`backend/cmd/server/main.go`：适配器装配、身份验证路由和启动版本检查。
- `Dockerfile`、`docker-compose.yml`、`README.md`：打包并说明迁移命令和已有数据升级。
- `frontend/src/modules/resource/api.ts`、`store.ts`、`CloudPlatformPage.vue` 及测试：待验证状态和专门验证身份流程。
- `frontend/src/modules/project/ProjectFormDialog.vue`、`ProjectListPage.vue`、`ProjectDetailPage.vue`：修复既有生产构建类型错误，不改变业务行为。
- `docs/project/domain-model.md`、`resource-sync.md`、`security.md`、`frontend-guidelines.md`、`acceptance.md`、`implementation-status.md`：同步产品规范、验收和交付证据。

---

### Task 1: 恢复前端生产构建基线

**Files:**
- Modify: `frontend/src/modules/project/ProjectListPage.vue`
- Modify: `frontend/src/modules/project/ProjectDetailPage.vue`
- Test: `frontend/src/modules/project/pages.test.ts`

**Interfaces:**
- Consumes: `CreateProjectInput`、`UpdateProjectInput` 和 `ProjectFormDialog` 的联合 `submit` 事件。
- Produces: `submitCreateProject(input: CreateProjectInput | UpdateProjectInput)`、`submitUpdateProject(input: CreateProjectInput | UpdateProjectInput)` 两个类型收窄包装函数。

- [ ] **Step 1: 保留现有页面测试并复现类型失败**

Run:

```bash
cd frontend
corepack pnpm exec vue-tsc --noEmit
```

Expected: FAIL，错误指向 `ProjectListPage.vue` 与 `ProjectDetailPage.vue` 的 `@submit` 回调参数不兼容。

- [ ] **Step 2: 添加类型收窄包装函数**

在列表页使用稳定创建输入：

```ts
async function submitCreateProject(input: CreateProjectInput | UpdateProjectInput) {
  if (!('code' in input)) return
  await createProject(input)
}
```

在详情页使用稳定更新输入：

```ts
async function submitUpdateProject(input: CreateProjectInput | UpdateProjectInput) {
  if (!('status' in input)) return
  await updateProject(input)
}
```

模板分别绑定新函数，原 `createProject`、`updateProject` 的请求和中文错误处理不变。

- [ ] **Step 3: 验证页面行为、类型和生产构建**

Run:

```bash
cd frontend
corepack pnpm test src/modules/project/pages.test.ts
corepack pnpm exec vue-tsc --noEmit
corepack pnpm build
```

Expected: 项目页面测试、类型检查和生产构建全部 PASS。

- [ ] **Step 4: 提交基线修复**

```bash
git add frontend/src/modules/project/ProjectListPage.vue frontend/src/modules/project/ProjectDetailPage.vue frontend/src/modules/project/pages.test.ts
git commit -m "fix: 恢复项目表单生产构建"
```

### Task 2: 建立数据库版本门禁并实现 P0 结构迁移

**Files:**
- Create: `backend/internal/platform/database/schema_version.go`
- Create: `backend/internal/platform/database/schema_version_test.go`
- Create: `backend/internal/platform/database/migrator.go`
- Create: `backend/internal/platform/database/migrator_test.go`
- Create: `backend/internal/platform/database/migrations/002_p0_safety.sql`
- Create: `backend/cmd/migrate/main.go`
- Create: `backend/cmd/migrate/main_test.go`
- Modify: `backend/database/init/002_schema.sql`
- Modify: `backend/internal/platform/database/migration_comments_test.go`
- Modify: `backend/internal/platform/config/config.go`
- Modify: `backend/internal/platform/config/config_test.go`
- Modify: `backend/cmd/server/main.go`
- Modify: `Dockerfile`

**Interfaces:**
- Produces: `const CurrentSchemaVersion = 2`。
- Produces: `func CheckSchemaVersion(ctx context.Context, db *gorm.DB) error`。
- Produces: `func Migrate(ctx context.Context, db *gorm.DB) error`。
- Produces: `config.LoadMigrationDatabase() (config.Database, error)`，只读取数据库地址、库名、`DB_MIGRATION_USER` 和 `DB_MIGRATION_PASSWORD`。

- [ ] **Step 1: 写版本、结构和迁移配置失败测试**

测试必须覆盖：版本表不存在、版本为 1、版本为 2、版本为 3；迁移用户名或密码缺失；错误文本不得包含测试密码；空库 SQL 和增量 SQL 必须包含身份检查、部分唯一索引、六个限制外键和版本记录。

```go
func TestCheckSchemaVersionRequiresExactVersion(t *testing.T) {
    for _, version := range []int{1, 2, 3} {
        err := CheckSchemaVersion(context.Background(), databaseAtVersion(t, version))
        if version == CurrentSchemaVersion && err != nil {
            t.Fatalf("当前结构版本应通过：%v", err)
        }
        if version != CurrentSchemaVersion && err == nil {
            t.Fatalf("结构版本 %d 不应允许应用启动", version)
        }
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/platform/database ./internal/platform/config ./cmd/migrate`

Expected: FAIL，提示版本检查、迁移函数、命令或 P0 结构约束尚不存在。

- [ ] **Step 3: 实现只读版本检查和迁移框架**

核心契约固定为：

```go
const CurrentSchemaVersion = 2

func CheckSchemaVersion(ctx context.Context, db *gorm.DB) error {
    var version int
    err := db.WithContext(ctx).Raw("SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version).Error
    if err != nil || version != CurrentSchemaVersion {
        return errors.New("数据库结构版本不匹配，请先执行管理员迁移")
    }
    return nil
}
```

`Migrate` 使用 `pg_advisory_xact_lock` 串行化迁移，每个版本在一个事务中执行嵌入的 `002_p0_safety.sql` 并写 `schema_migrations`；没有版本表时只接受精确匹配的旧版表/列指纹，未知结构返回“数据库结构不受支持，未执行迁移”。

- [ ] **Step 4: 写空库最新结构和已有库增量 SQL**

`resource_sources` 新列和一致性检查使用：

```sql
cloud_account_id VARCHAR(128),
identity_status VARCHAR(32) NOT NULL DEFAULT 'verified',
identity_verified_at TIMESTAMPTZ(3),
CONSTRAINT ck_resource_sources_identity CHECK (
  (identity_status = 'pending' AND cloud_account_id IS NULL AND identity_verified_at IS NULL)
  OR
  (identity_status = 'verified' AND cloud_account_id IS NOT NULL AND cloud_account_id <> '' AND identity_verified_at IS NOT NULL)
)
```

空库结构创建版本表并记录版本 2。增量 SQL 先检查孤儿资产，再新增字段并把历史源设为 `pending`，随后添加检查约束和部分唯一索引，最后把三类资产到项目/接入源的六个外键改为 `ON DELETE RESTRICT`。对 `cmdb` 撤销版本表写权限，只授予 `SELECT`。

- [ ] **Step 5: 装配命令和启动门禁**

`cmd/migrate` 只加载管理员数据库配置、连接 PostgreSQL、调用 `database.Migrate`。`cmd/server` 在启动调度器和 HTTP 服务前调用 `CheckSchemaVersion`。Dockerfile 构建并复制 `cmdb-migrate`：

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -buildvcs=false -o cmdb-migrate ./cmd/migrate
COPY --from=backend-builder /src/backend/cmdb-migrate ./cmdb-migrate
```

- [ ] **Step 6: 验证受影响包**

Run:

```bash
cd backend
GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/platform/config ./internal/platform/database ./cmd/migrate ./cmd/server
```

Expected: PASS；服务器旧版、超前版均拒绝启动，迁移命令错误不泄露凭证，静态结构测试确认空库与增量迁移约束一致。

- [ ] **Step 7: 提交可运行迁移**

```bash
git add backend/database backend/internal/platform/database backend/internal/platform/config backend/cmd/migrate backend/cmd/server/main.go Dockerfile
git commit -m "feat: 建立显式 P0 数据库迁移门禁"
```

### Task 3: 验证 PostgreSQL 17 升级、约束和账号权限

**Files:**
- Create: `backend/internal/platform/database/postgres_integration_test.go`
- Create: `backend/test/postgres/docker-compose.yml`
- Modify: `backend/internal/platform/database/compose_test.go`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: Task 2 的 `Migrate`、`CurrentSchemaVersion`、空库结构和增量 SQL。
- Produces: 可重复运行的 PostgreSQL 17 集成套件，证明 `resource_sources` 身份约束、部分唯一索引、三类资产限制外键、迁移原子性和应用账号权限。

- [ ] **Step 1: 写真实 PostgreSQL 失败测试**

测试在旧版结构上运行迁移，并确认最终存在以下约束：

```sql
identity_status VARCHAR(32) NOT NULL CHECK (identity_status IN ('pending', 'verified'))
CREATE UNIQUE INDEX uk_resource_sources_provider_account_verified
ON resource_sources (provider, cloud_account_id)
WHERE identity_status = 'verified' AND cloud_account_id IS NOT NULL;
```

测试 Compose 使用固定的隔离端口和纯测试凭证：

```yaml
services:
  postgresql-test:
    image: postgres:17
    environment:
      POSTGRES_PASSWORD: cmdb-integration-only
    ports:
      - "127.0.0.1:55432:5432"
    tmpfs:
      - /var/lib/postgresql/data
```

真实 PostgreSQL 测试还要验证同账号并发插入只有一个成功、三张资产表都阻止父删除、`cmdb` 账号不能 `ALTER TABLE` 但能读取 `schema_migrations`。

- [ ] **Step 2: 启动 PostgreSQL 17 并运行失败测试**

Run:

```bash
docker compose -f backend/test/postgres/docker-compose.yml up -d --wait
cd backend
CMDB_POSTGRES_TEST_DSN='host=127.0.0.1 port=55432 user=postgres password=cmdb-integration-only dbname=postgres sslmode=disable' GOCACHE=/tmp/cmdb-p0-go-cache go test -tags=postgres ./internal/platform/database
```

Expected: FAIL，真实 PostgreSQL 集成夹具或迁移验收尚未完成。

- [ ] **Step 3: 完成隔离数据库夹具和约束断言**

测试为每个子场景创建独立数据库并在清理时删除，避免约束和迁移版本互相污染。旧库升级断言历史源全部为 `pending`；未知结构和孤儿数据断言整个版本事务回滚；重复执行版本不变。

- [ ] **Step 4: 运行 PostgreSQL 17 集成测试并清理**

Run:

```bash
docker compose -f backend/test/postgres/docker-compose.yml up -d --wait
cd backend
CMDB_POSTGRES_TEST_DSN='host=127.0.0.1 port=55432 user=postgres password=cmdb-integration-only dbname=postgres sslmode=disable' GOCACHE=/tmp/cmdb-p0-go-cache go test -tags=postgres ./internal/platform/database
docker compose -f test/postgres/docker-compose.yml down -v
```

Expected: PASS，覆盖旧库升级、重复迁移、唯一索引、限制外键、版本和应用账号 DDL 拒绝。

- [ ] **Step 5: 提交 PostgreSQL 验收夹具**

```bash
git add backend/database backend/internal/platform/database backend/test/postgres docker-compose.yml
git commit -m "test: 验证 PostgreSQL P0 迁移约束"
```

### Task 4: 统一敏感键分类和平台适配契约

**Files:**
- Create: `backend/internal/security/sensitive.go`
- Create: `backend/internal/security/sensitive_test.go`
- Create: `backend/internal/resource/provider.go`
- Create: `backend/internal/resource/provider_test.go`
- Modify: `backend/internal/audit/repository.go`
- Modify: `backend/internal/audit/repository_internal_test.go`
- Modify: `backend/internal/resource/collector.go`

**Interfaces:**
- Produces: `security.IsSensitiveKey(key string) bool`、`security.ContainsSensitiveKey(raw json.RawMessage) (bool, error)`。
- Produces: `ProviderAdapter` 的 `ValidateCredential`、`ValidateConfig`、`ResolveCloudAccountID`、`ResourceTypes`、`Probe`、`Collect`。
- Produces: `DecodeStrictStringObject` 和 `ValidateEmptyConfig` 供两个平台复用。

- [ ] **Step 1: 写敏感分类和严格对象失败测试**

测试大小写、蛇形、短横线和嵌套数组中的 `password`、`token`、`access_key`、`secret`、`authorization`、`ciphertext`、原始错误正文；测试未知凭证字段、非字符串值、空白必填值和非空配置被拒绝。

```go
func TestValidateEmptyConfigRejectsSensitiveNestedValue(t *testing.T) {
    err := ValidateEmptyConfig(json.RawMessage(`{"options":{"access-key":"x"}}`))
    if !errors.Is(err, ErrInvalidProviderConfig) {
        t.Fatalf("敏感配置必须被拒绝：%v", err)
    }
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/security ./internal/audit ./internal/resource`

Expected: FAIL，缺少共享分类和平台适配接口。

- [ ] **Step 3: 提取共享敏感键分类**

审计递归清理继续保留在审计包，但键判断统一改为：

```go
if security.IsSensitiveKey(key) {
    continue
}
```

`ContainsSensitiveKey` 先把 JSON 解码为对象/数组，再递归调用同一分类；不得把值写进错误文本。

- [ ] **Step 4: 定义平台适配器与领域错误**

```go
type ProviderAdapter interface {
    Collector
    ValidateCredential(json.RawMessage) error
    ValidateConfig(json.RawMessage) error
    ResolveCloudAccountID(context.Context, Source, []byte) (string, error)
    ResourceTypes() []string
}
```

固定错误为 `ErrInvalidProviderCredential`、`ErrInvalidProviderConfig`、`ErrCloudAuthentication`、`ErrCloudPermission`、`ErrCloudNetwork`，HTTP 和审计只按这些类型分类。

- [ ] **Step 5: 验证共享分类未弱化审计**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/security ./internal/audit ./internal/resource`

Expected: PASS，既有审计脱敏测试和新增配置拦截测试全部通过。

- [ ] **Step 6: 提交共享契约**

```bash
git add backend/internal/security backend/internal/audit backend/internal/resource/provider.go backend/internal/resource/provider_test.go backend/internal/resource/collector.go
git commit -m "refactor: 统一平台校验与敏感键契约"
```

### Task 5: 实现阿里云账号识别适配器

**Files:**
- Create: `backend/internal/aliyun/identity.go`
- Create: `backend/internal/aliyun/identity_test.go`
- Modify: `backend/internal/aliyun/collector.go`
- Modify: `backend/internal/aliyun/collector_test.go`

**Interfaces:**
- Consumes: Task 4 的 `ProviderAdapter` 和严格 JSON 辅助函数。
- Produces: 阿里云 `Collector` 对 `access_key_id`、`access_key_secret` 的校验和 STS `GetCallerIdentity` 账号 ID。

- [ ] **Step 1: 写模拟 STS 失败测试**

覆盖完整凭证、缺字段、未知字段、空配置、非空配置、空 Account ID、认证失败、权限不足、网络失败；断言只调用一次身份 API，错误不含虚构 AccessKey。

```go
type aliyunIdentityAPI interface {
    GetCallerIdentity(*sts.GetCallerIdentityRequest) (*sts.GetCallerIdentityResponse, error)
}
```

- [ ] **Step 2: 运行阿里云测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/aliyun`

Expected: FAIL，`Collector` 尚未实现账号识别和严格校验。

- [ ] **Step 3: 实现阿里云校验和身份解析**

`ValidateCredential` 只允许两个必填字符串字段；`ValidateConfig` 调用 `ValidateEmptyConfig`；`ResourceTypes` 固定返回 `[]string{"ecs", "rds", "slb"}`。`ResolveCloudAccountID` 构造 STS 客户端，调用 `GetCallerIdentity`，校验非空账号 ID并把 SDK 错误收敛为 Task 4 的领域错误。

- [ ] **Step 4: 验证平台模块**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/aliyun`

Expected: PASS，测试不访问真实阿里云。

- [ ] **Step 5: 提交阿里云适配器**

```bash
git add backend/internal/aliyun
git commit -m "feat: 识别并校验阿里云账号身份"
```

### Task 6: 实现 AWS 账号识别适配器

**Files:**
- Create: `backend/internal/aws/identity.go`
- Create: `backend/internal/aws/identity_test.go`
- Modify: `backend/internal/aws/collector.go`
- Modify: `backend/internal/aws/collector_test.go`
- Modify: `backend/go.mod`
- Modify: `backend/go.sum`

**Interfaces:**
- Consumes: Task 4 的 `ProviderAdapter` 和严格 JSON 辅助函数。
- Produces: AWS `Collector` 对两个必填字段、可选 `session_token` 的校验和 STS `GetCallerIdentity` 12 位 Account ID。

- [ ] **Step 1: 写模拟 AWS STS 失败测试**

```go
type awsIdentityAPI interface {
    GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}
```

覆盖字段白名单、Session Token 可选、空配置、Account ID 非 12 位数字、认证/权限/网络错误和原始错误脱敏。

- [ ] **Step 2: 运行 AWS 测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/aws`

Expected: FAIL，缺少 STS 身份实现或 `ProviderAdapter` 方法。

- [ ] **Step 3: 实现 AWS 校验和身份解析**

`ResourceTypes` 固定返回 `[]string{"ec2", "rds", "elb"}`。使用现有静态凭证提供器加载 AWS 配置，只调用 STS `GetCallerIdentity`；用 `^[0-9]{12}$` 校验 Account ID，SDK 错误只映射为 Task 4 领域分类。

- [ ] **Step 4: 验证平台模块和依赖整理**

Run:

```bash
cd backend
go mod tidy
GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/aws
```

Expected: PASS，测试不访问真实 AWS。

- [ ] **Step 5: 提交 AWS 适配器**

```bash
git add backend/internal/aws backend/go.mod backend/go.sum
git commit -m "feat: 识别并校验 AWS 账号身份"
```

### Task 7: 实现接入源身份状态、唯一归属和待验证门禁

**Files:**
- Modify: `backend/internal/resource/model.go`
- Modify: `backend/internal/resource/repository.go`
- Modify: `backend/internal/resource/service.go`
- Modify: `backend/internal/resource/source_service_test.go`
- Modify: `backend/internal/resource/service_test.go`
- Modify: `backend/internal/platform/httpserver/server.go`
- Modify: `backend/internal/platform/httpserver/integration_test.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Consumes: `map[string]ProviderAdapter`、Task 3 身份字段和唯一索引。
- Produces: `VerifySourceIdentity(ctx context.Context, projectID, sourceID uint64, credential json.RawMessage) (*Source, error)`。
- Produces: 仓储方法 `ProjectIsEnabled(ctx context.Context, projectID uint64) (bool, error)`，只供新增身份验证动作执行项目状态门禁。
- Produces: `ErrCloudAccountConflict`、`ErrSourceIdentityPending`、`ErrSourceIdentityMismatch`。

- [ ] **Step 1: 写创建和替换凭证失败测试**

覆盖：身份识别失败不落库；创建直接保存 `verified`；同项目、跨项目和两个并发事务重复账号只成功一次；新凭证识别为原账号时原子替换；不同账号或事务失败时保留原密文、配置和验证时间。

```go
if persisted.IdentityStatus != IdentityStatusVerified || persisted.CloudAccountID != "123456789012" {
    t.Fatalf("新接入源必须保存已验证账号身份：%+v", persisted)
}
```

- [ ] **Step 2: 写历史待验证流程失败测试**

测试 `pending` 源只能查询；更新、启停、删除、普通连接测试、同步和重试返回 `ErrSourceIdentityPending`；自动调度不选择；使用原凭证或完整新凭证验证成功；冲突或云失败保持 `pending`；遗留排队/运行任务安全收敛为失败且不改变资产。

- [ ] **Step 3: 运行资源包测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/resource`

Expected: FAIL，缺少身份字段、验证入口和运行门禁。

- [ ] **Step 4: 修改服务依赖和模型**

```go
const (
    IdentityStatusPending  = "pending"
    IdentityStatusVerified = "verified"
)

type Service struct {
    repository *Repository
    cipher *CredentialCipher
    adapters map[string]ProviderAdapter
    now func() time.Time
    sourceLocks sync.Map
    auditRecorder audit.Recorder
}
```

`Source.CloudAccountID` 和 `Source.IdentityVerifiedAt` 使用 `json:"-"`，只有 `identity_status` 对页面公开。`NewService` 接收适配器映射；同一步更新所有生产和测试调用点，确保仓库在本任务提交时完整编译。`CreateSource` 先校验和联网识别，再加密并事务写入；`UpdateSource` 只有提交新凭证时重新识别且必须等于原账号。

- [ ] **Step 5: 实现专门验证身份和运行门禁**

```go
func (s *Service) requireVerified(source *Source) error {
    if source.IdentityStatus != IdentityStatusVerified || source.CloudAccountID == "" {
        return ErrSourceIdentityPending
    }
    return nil
}
```

`VerifySourceIdentity` 要求项目启用，允许使用现有明文解密结果或请求中的完整新凭证；唯一确认、可选密文替换、状态和验证时间、审计在同一事务完成。`ListDueSources` 只查询 `verified`。启动恢复对 pending 源的 queued/running 任务写安全失败摘要并清空统计。

生产装配在本任务同时统一为：

```go
adapters := map[string]cloudresource.ProviderAdapter{
    cloudresource.ProviderAliyun: aliyuncollector.NewCollector(),
    cloudresource.ProviderAWS: awscollector.NewCollector(),
}
```

服务、调度器和 HTTP 共用同一适配器映射，不再维护可能指向不同实现的采集器映射。

- [ ] **Step 6: 验证资源服务**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./...`

Expected: PASS，所有失败路径不留下新密文、身份、成功审计或资产变化。

- [ ] **Step 7: 提交接入源核心**

```bash
git add backend/internal/resource backend/internal/platform/httpserver backend/cmd/server/main.go
git commit -m "feat: 强制云账号唯一归属与身份验证"
```

### Task 8: 接入身份 API 和错误映射

**Files:**
- Modify: `backend/internal/resource/http.go`
- Modify: `backend/internal/platform/httpserver/server.go`
- Modify: `backend/internal/platform/httpserver/integration_test.go`
- Modify: `backend/cmd/server/main_test.go`

**Interfaces:**
- Consumes: Task 7 已装配的平台适配器和服务方法。
- Produces: `POST /api/v1/projects/:id/sources/:sourceId/verify-identity`，请求 `{ "credential": object | null }`，响应为脱敏 `Source`。

- [ ] **Step 1: 写 HTTP 端到端失败测试**

覆盖项目成员被拒绝、项目管理员可验证、停用项目冲突、跨项目对象仍返回统一不存在、账号冲突/身份不一致/待验证/云故障状态码，以及所有响应不含凭证或账号 ID。

```go
integrationRequest(t, server, projectAdmin, http.MethodPost,
    sourcePath+"/verify-identity", map[string]any{}, http.StatusOK)
```

- [ ] **Step 2: 运行集成测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/platform/httpserver ./cmd/server`

Expected: FAIL，身份验证路由或适配器装配尚不存在。

- [ ] **Step 3: 实现稳定 HTTP 映射**

固定响应：字段错误 `400 SOURCE_INVALID_INPUT`；账号冲突 `409 CLOUD_ACCOUNT_CONFLICT`；身份不一致 `409 SOURCE_IDENTITY_MISMATCH`；待验证 `409 SOURCE_IDENTITY_PENDING`；删除依赖冲突使用 Task 10 的 `409`；云认证/权限失败 `502 CLOUD_IDENTITY_UNAVAILABLE`；内部错误 `500 SOURCE_SERVICE_UNAVAILABLE`。

- [ ] **Step 4: 验证接口和启动**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/platform/httpserver ./cmd/server`

Expected: PASS；接口和审计均不返回账号 ID、凭证、原始云错误或内部用户 ID。

- [ ] **Step 5: 提交 API 装配**

```bash
git add backend/internal/resource/http.go backend/internal/platform/httpserver backend/cmd/server
git commit -m "feat: 提供接入源身份验证接口"
```

### Task 9: 实现云同步管理待验证交互

**Files:**
- Modify: `frontend/src/modules/resource/api.ts`
- Modify: `frontend/src/modules/resource/store.ts`
- Modify: `frontend/src/modules/resource/store.test.ts`
- Modify: `frontend/src/modules/resource/CloudPlatformPage.vue`
- Modify: `frontend/src/app-flow.test.ts`

**Interfaces:**
- Consumes: Task 8 的 `identity_status` 和验证身份接口。
- Produces: `Source.identityStatus: 'pending' | 'verified'`、`verifySourceIdentity(projectId, sourceId, credential?)`、store 的 `verifyIdentity` 与 `verifyingSourceId`。

- [ ] **Step 1: 写页面和状态失败测试**

断言待验证卡片显示“待验证”，只启用“验证身份”；编辑、启停、删除、连接测试、同步和对应任务重试不可操作；验证可使用现有凭证或完整新凭证；成功刷新服务端事实；失败显示中文安全错误并清空凭证对象。

- [ ] **Step 2: 运行前端测试确认失败**

Run: `corepack pnpm test src/modules/resource/store.test.ts src/app-flow.test.ts`

Expected: FAIL，前端模型和验证操作不存在。

- [ ] **Step 3: 扩展 API 和 Pinia 状态**

```ts
export interface Source {
  id: number
  projectId: number
  provider: Provider
  identityStatus: 'pending' | 'verified'
  name: string
  region: string
  credentialHint: string
  enabled: boolean
  syncIntervalMinutes: number
  lastSyncAt?: string
  nextSyncAt?: string
}
```

`verifySourceIdentity` 只在参数存在时发送 `credential`；store 在 `finally` 清空局部凭证引用和 `verifyingSourceId`，随后重新加载当前单平台或统一管理视图。

- [ ] **Step 4: 实现待验证卡片和弹窗**

卡片显示身份中文状态。`source.identityStatus === 'pending'` 时只渲染“验证身份”，弹窗提供“使用现有安全凭证”和“输入完整新凭证”两种方式；提交按钮显示“正在验证云账号身份…”。关闭、成功和失败都把 AccessKey、Secret、Session Token 设为空字符串。

- [ ] **Step 5: 验证前端完整行为**

Run:

```bash
cd frontend
corepack pnpm test
corepack pnpm exec vue-tsc --noEmit
corepack pnpm build
```

Expected: 全部 PASS，页面和浏览器持久状态不含凭证或账号 ID。

- [ ] **Step 6: 提交前端身份流程**

```bash
git add frontend/src/modules/resource frontend/src/app-flow.test.ts
git commit -m "feat: 增加接入源待验证流程"
```

### Task 10: 实现项目与接入源删除保护

**Files:**
- Create: `backend/internal/resource/deletion_guard.go`
- Modify: `backend/internal/resource/repository.go`
- Modify: `backend/internal/resource/service.go`
- Modify: `backend/internal/resource/source_service_test.go`
- Modify: `backend/internal/project/repository.go`
- Modify: `backend/internal/project/service.go`
- Modify: `backend/internal/project/http.go`
- Modify: `backend/internal/project/service_test.go`
- Modify: `backend/internal/platform/httpserver/server.go`
- Modify: `backend/internal/platform/httpserver/integration_test.go`
- Modify: `backend/internal/platform/database/postgres_integration_test.go`

**Interfaces:**
- Consumes: Task 3 的限制外键。
- Produces: `ErrDeleteDependencyConflict` 和事务内 `LockProjectForOperation`、`LockSourceForOperation`、`CheckProjectDependencies`、`CheckSourceDependencies`。

- [ ] **Step 1: 写三类资产和活动任务失败测试**

项目、接入源分别覆盖服务器、数据库、负载均衡的正常和已失联资产；覆盖 queued/running 拒绝、终态任务不阻止；断言冲突时父对象、资产、端点和历史记录均保留且没有成功删除审计。

- [ ] **Step 2: 写 PostgreSQL 删除竞争失败测试**

使用两个事务并发执行删除检查和同步入队/资产插入。断言最终只能得到“删除成功且没有新依赖”或“依赖成功且删除返回冲突”，绝不能出现资产被级联删除。

- [ ] **Step 3: 运行受影响测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/project ./internal/resource ./internal/platform/httpserver`

Expected: FAIL，当前父删除仍会级联或缺少 `409`。

- [ ] **Step 4: 实现统一锁顺序和依赖查询**

所有写路径先锁项目、再按 ID 锁接入源。删除事务内用 `SELECT ... FOR UPDATE` 锁父记录，再对三张资产表做 `EXISTS`，并检查：

```sql
SELECT EXISTS (
  SELECT 1 FROM sync_jobs
  WHERE source_id = ? AND status IN ('queued', 'running')
)
```

项目范围使用 `project_id`。命中任一依赖返回同一 `ErrDeleteDependencyConflict`，不返回数量或其他对象信息。

- [ ] **Step 5: 调整删除审计和 HTTP 冲突映射**

依赖检查通过后才在同一事务写成功删除审计并删除父对象。业务冲突和 PostgreSQL 外键冲突统一返回：项目 `409 PROJECT_DELETE_CONFLICT`、“项目仍有资产或运行中的同步任务，暂不能删除”；接入源 `409 SOURCE_DELETE_CONFLICT`、“接入源仍有资产或运行中的同步任务，暂不能删除”。

- [ ] **Step 6: 验证单元和 PostgreSQL 集成测试**

Run:

```bash
docker compose -f backend/test/postgres/docker-compose.yml up -d --wait
cd backend
GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/project ./internal/resource ./internal/platform/httpserver
CMDB_POSTGRES_TEST_DSN='host=127.0.0.1 port=55432 user=postgres password=cmdb-integration-only dbname=postgres sslmode=disable' GOCACHE=/tmp/cmdb-p0-go-cache go test -tags=postgres ./internal/platform/database
cd ..
docker compose -f backend/test/postgres/docker-compose.yml down -v
```

Expected: PASS，系统管理员也不能绕过删除不变量。

- [ ] **Step 7: 提交删除保护**

```bash
git add backend/internal/resource backend/internal/project backend/internal/platform/httpserver backend/internal/platform/database/postgres_integration_test.go
git commit -m "fix: 阻止父对象绕过资产生命周期删除"
```

### Task 11: 修正同步终态和自动失败调度

**Files:**
- Create: `backend/internal/resource/sync_decision.go`
- Create: `backend/internal/resource/sync_decision_test.go`
- Modify: `backend/internal/resource/service.go`
- Modify: `backend/internal/resource/repository.go`
- Modify: `backend/internal/resource/service_test.go`

**Interfaces:**
- Produces: `DecideSyncResult(expectedTypes []string, results []CollectionResult, collectErr error) SyncDecision`。
- Produces: `convergeFailedJob(ctx context.Context, job *SyncJob, source *Source, summary string, statistics json.RawMessage) error`，统一处理所有失败和计划推进。

- [ ] **Step 1: 写纯决策表失败测试**

表驱动覆盖全成功、一个成功一个失败、全部类型失败、空结果、缺少预期类型、重复类型、整体认证失败、整体网络失败。核心断言：

```go
if len(decision.Successful) == 0 && decision.Status != "failed" {
    t.Fatalf("没有成功类型时必须失败：%+v", decision)
}
```

- [ ] **Step 2: 写调度和回滚失败测试**

使用可控完成时间覆盖 scheduled/manual/带 previous_job_id 的重试；认证失败、全部类型失败和最终事务失败都必须走相同规则。断言自动失败只更新 `next_sync_at = finished_at + interval`，手工/重试失败不改变计划，所有失败不更新 `last_sync_at` 或资产时间。

- [ ] **Step 3: 运行资源测试确认失败**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/resource`

Expected: FAIL，全部类型失败当前被写为 `partial_success`，自动失败计划未推进。

- [ ] **Step 4: 实现确定性决策函数**

```go
type SyncDecision struct {
    Status string
    Successful []CollectionResult
    Statistics map[string]map[string]int
    ErrorSummary string
}
```

先按 `ProviderAdapter.ResourceTypes()` 验证完整、无重复的结果集合；成功数为零固定 `failed`，成功与失败并存才是 `partial_success`，全部成功才是 `success`。全部类型失败只保留每类 `failed: 1` 和安全分类，其他五项为零。

- [ ] **Step 5: 统一失败收敛事务**

整体错误、零成功类型和最终资源事务失败都调用 `convergeFailedJob`。仅当 `job.Trigger == "scheduled" && job.PreviousJobID == nil` 时更新下次计划；任务终态、完成时间、允许保留的安全统计、计划和同步失败审计在同一事务提交。

- [ ] **Step 6: 验证资源和 HTTP 集成行为**

Run: `GOCACHE=/tmp/cmdb-p0-go-cache go test ./internal/resource ./internal/platform/httpserver`

Expected: PASS；不存在零成功的部分成功任务，失败不推进资产生命周期或最近成功时间。

- [ ] **Step 7: 提交同步修正**

```bash
git add backend/internal/resource backend/internal/platform/httpserver/integration_test.go
git commit -m "fix: 收敛同步失败状态与自动计划"
```

### Task 12: 同步产品文档、升级说明并完成全量验收

**Files:**
- Modify: `docs/project/domain-model.md`
- Modify: `docs/project/resource-sync.md`
- Modify: `docs/project/security.md`
- Modify: `docs/project/frontend-guidelines.md`
- Modify: `docs/project/acceptance.md`
- Modify: `docs/project/implementation-status.md`
- Modify: `docs/project/README.md`
- Modify: `README.md`
- Modify: `docker-compose.yml`

**Interfaces:**
- Consumes: Tasks 2–11 的最终接口、行为、测试证据和提交。
- Produces: 与最终实现一致的长期产品规范、AC-011 至 AC-015、AC-019、AC-022、AC-028 更新，以及新增 AC-041 数据库升级验收。

- [ ] **Step 1: 先更新主责规范**

按已实现事实写入：身份字段和不可变规则、创建/替换前联网识别、历史 `pending`、专门验证动作、平台字段白名单、待验证页面、限制删除、零成功失败和自动失败计划。不得把未实现的其他差距写成已完成。

- [ ] **Step 2: 更新验收场景和覆盖索引**

扩展 AC-011 至 AC-015、AC-019、AC-022、AC-028；新增：

```markdown
### AC-041 PostgreSQL 已有数据升级与版本门禁（安全场景）
```

场景覆盖旧库备份、管理员迁移、历史源待验证、重复执行、未知结构拒绝、应用账号 DDL 拒绝、旧/超前版本应用拒绝启动和失败不泄密。

- [ ] **Step 3: 更新实现状态**

在文档提交前运行 `git rev-parse HEAD`，把输出的完整实现提交缩写写为本轮实现快照基线；“项目与接入源删除保护”“接入源凭证、配置与连接测试”“云账号唯一归属”“同步任务、类型级处理与事务”按真实证据改为“已实现”。其他既有差距保持“部分实现”或“待实现”，证据路径必须能在仓库中解析。

- [ ] **Step 4: 更新运维升级步骤**

README 明确顺序：停止 app、备份数据卷或数据库、以 `DB_MIGRATION_USER`/`DB_MIGRATION_PASSWORD` 执行 `docker compose run --rm app ./cmdb-migrate`、核对版本、清除管理员凭证环境、启动 app、逐个验证历史接入源。迁移失败时保持应用停止并从备份恢复，不使用应用账号执行 DDL。

- [ ] **Step 5: 检查文档结构和差异**

Run:

```bash
grep -RInE 'T[B]D|T[O]DO|[待]定|暂[定]' docs/project README.md
git diff --check
```

Expected: 搜索无输出，`git diff --check` PASS；所有相对链接和实现证据路径存在。

- [ ] **Step 6: 运行后端、PostgreSQL 和前端全量验收**

Run:

```bash
docker compose -f backend/test/postgres/docker-compose.yml up -d --wait
cd backend
GOCACHE=/tmp/cmdb-p0-go-cache go test ./...
CMDB_POSTGRES_TEST_DSN='host=127.0.0.1 port=55432 user=postgres password=cmdb-integration-only dbname=postgres sslmode=disable' GOCACHE=/tmp/cmdb-p0-go-cache go test -tags=postgres ./internal/platform/database
cd ../frontend
corepack pnpm test
corepack pnpm exec vue-tsc --noEmit
corepack pnpm build
cd ..
docker compose -f backend/test/postgres/docker-compose.yml down -v
docker compose config --quiet
docker build -t cmdb:p0-verification .
```

Expected: 全部命令成功；云测试均为模拟响应；旧数据升级演练、重复迁移和应用账号权限测试均通过。

- [ ] **Step 7: 提交产品文档和部署说明**

```bash
git add docs/project README.md docker-compose.yml
git commit -m "docs: 同步 P0 安全迭代产品规范"
```

- [ ] **Step 8: 执行提交级复核和分支集成**

确认 `git status --short` 无输出；按 `superpowers:requesting-code-review` 完成需求与代码质量审查，修复后重新运行 Step 6。随后使用 `superpowers:verification-before-completion` 取得新鲜验证输出，并按 `superpowers:finishing-a-development-branch` 将工作分支纳入最新 `main` 后合并。
