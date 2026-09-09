# CMDB Foundation and Project Access Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 从空数据库建立新版 CMDB 的认证、项目、项目成员权限和现代化前端骨架，交付一个可登录、可管理项目且严格隔离项目数据的最小可运行系统。

**Architecture:** 后端采用按领域组织的模块化单体，每个领域包含模型、存储、服务和 HTTP 接口；项目权限由统一中间件强制执行。前端以当前项目为全局上下文，提供登录、项目管理和现代云控制台布局。本计划是全量重写路线的第一份计划，后续资源核心、同步框架、阿里云、AWS、Kubernetes 和完整资源 UI 分别编写独立计划。

**Tech Stack:** Go 1.25.1、Gin 1.12、GORM 1.31、MySQL 8.4、Vue 3.5、TypeScript 5.5、Pinia、Vue Router、Element Plus、Vitest、Docker Compose

**Spec:** `docs/superpowers/specs/2026-09-09-cloud-resource-cmdb-rewrite-design.md`

## Global Constraints

- 项目名统一使用 `CMDB`。
- 业务项目是最高级的数据、权限和资源归属边界。
- 所有新增代码文件、结构体、接口、公开函数、核心业务分支和复杂逻辑必须包含准确的中文注释。
- 新数据库从空库初始化，不迁移或兼容旧业务数据。
- API、日志和测试输出不得暴露密码、令牌或其他凭证。
- 每个任务必须先写失败测试，再写最小实现，并单独提交。

---

## 交付路线

1. 本计划：工程基础、认证、项目及项目权限、现代化 UI 骨架。
2. 统一资源核心：接入源、凭证加密、资源、端点、关系和生命周期。
3. 同步框架：每小时调度、手工同步、并发控制、部分成功和审计。
4. 阿里云模块：ECS、RDS、负载均衡及访问端点。
5. AWS 模块：EC2、RDS、ELB 及访问端点。
6. Kubernetes 模块：七类资源、地址及资源关系。
7. 资源控制台：总览、统一搜索、详情、同步记录和审计页面。
8. 收尾：删除全部旧代码与旧迁移，完成 Compose 和真实环境验收。

### Task 1: 建立新版后端入口和配置

**Files:**
- Create: `backend/internal/platform/config/config.go`
- Create: `backend/internal/platform/config/config_test.go`
- Create: `backend/internal/platform/database/database.go`
- Create: `backend/internal/platform/httpserver/server.go`
- Modify: `backend/cmd/server/main.go`

**Interfaces:**
- Produces: `config.Load() (config.Config, error)`、`database.Open(config.Database) (*gorm.DB, error)`、`httpserver.New(httpserver.Dependencies) *gin.Engine`。

- [ ] **Step 1: 写配置失败测试**

```go
func TestLoadRejectsMissingJWTSecret(t *testing.T) {
    t.Setenv("JWT_SECRET", "")
    _, err := Load()
    if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
        t.Fatalf("期望缺少 JWT_SECRET 时返回明确错误，实际为 %v", err)
    }
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/platform/config -run TestLoadRejectsMissingJWTSecret -v`
Expected: FAIL，提示新版 `Load` 尚不存在。

- [ ] **Step 3: 实现带中文注释的配置、数据库和 HTTP 服务构造器**

配置必须读取 `DB_HOST`、`DB_PORT`、`DB_USER`、`DB_PASSWORD`、`DB_NAME`、`JWT_SECRET` 和 `CMDB_ENCRYPTION_KEY`，禁止提供生产可用的默认密钥；`main.go` 只负责装配和启动。

- [ ] **Step 4: 验证后端基础包**

Run: `cd backend && go test ./internal/platform/... ./cmd/server/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add backend/cmd/server/main.go backend/internal/platform
git commit -m "refactor: establish cmdb backend foundation"
```

### Task 2: 建立新版数据库迁移和用户认证模型

**Files:**
- Create: `backend/migrations/100_new_cmdb_schema.sql`
- Create: `backend/internal/identity/user.go`
- Create: `backend/internal/identity/password.go`
- Create: `backend/internal/identity/password_test.go`
- Create: `backend/internal/identity/repository.go`

**Interfaces:**
- Produces: `identity.User`、`identity.HashPassword(string) (string, error)`、`identity.VerifyPassword(hash, plain string) bool`、`identity.UserRepository`。

- [ ] **Step 1: 写密码哈希测试**

```go
func TestPasswordHashNeverStoresPlaintext(t *testing.T) {
    hash, err := HashPassword("Cmdb-Test-123")
    if err != nil || hash == "Cmdb-Test-123" || !VerifyPassword(hash, "Cmdb-Test-123") {
        t.Fatalf("密码必须安全哈希并可验证，hash=%q err=%v", hash, err)
    }
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/identity -run TestPasswordHashNeverStoresPlaintext -v`
Expected: FAIL，提示函数不存在。

- [ ] **Step 3: 编写首个新迁移和认证模型**

迁移只创建新版 `users`、`projects`、`project_members`、`audit_logs` 表；用户全局角色限定为 `system_admin` 和 `user`，密码使用 bcrypt。所有表和关键字段添加中文 SQL 注释。

- [ ] **Step 4: 运行领域测试**

Run: `cd backend && go test ./internal/identity -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add backend/migrations/100_new_cmdb_schema.sql backend/internal/identity
git commit -m "feat: add cmdb identity schema"
```

### Task 3: 实现登录、JWT 和当前用户接口

**Files:**
- Create: `backend/internal/identity/service.go`
- Create: `backend/internal/identity/http.go`
- Create: `backend/internal/identity/http_test.go`
- Create: `backend/internal/platform/httpserver/auth.go`
- Modify: `backend/internal/platform/httpserver/server.go`

**Interfaces:**
- Produces: `POST /api/v1/auth/login`、`GET /api/v1/me`、`auth.RequireUser()`、`auth.CurrentUser(*gin.Context) identity.UserClaims`。

- [ ] **Step 1: 写错误密码接口测试**

```go
func TestLoginDoesNotRevealWhetherUserExists(t *testing.T) {
    response := performLogin(t, "missing", "wrong")
    if response.Code != http.StatusUnauthorized || response.Body.String() != `{"code":"AUTH_INVALID_CREDENTIALS","message":"用户名或密码错误"}` {
        t.Fatalf("登录失败响应不得泄露用户是否存在：%s", response.Body.String())
    }
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/identity -run TestLoginDoesNotRevealWhetherUserExists -v`
Expected: FAIL。

- [ ] **Step 3: 实现登录和身份中间件**

JWT 只包含用户 ID、全局角色和过期时间；所有错误使用稳定错误码和中文消息；日志不得记录密码或令牌。

- [ ] **Step 4: 运行认证测试**

Run: `cd backend && go test ./internal/identity ./internal/platform/httpserver -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/identity backend/internal/platform/httpserver
git commit -m "feat: implement cmdb authentication"
```

### Task 4: 实现项目领域规则和项目接口

**Files:**
- Create: `backend/internal/project/model.go`
- Create: `backend/internal/project/repository.go`
- Create: `backend/internal/project/service.go`
- Create: `backend/internal/project/service_test.go`
- Create: `backend/internal/project/http.go`
- Modify: `backend/internal/platform/httpserver/server.go`

**Interfaces:**
- Produces: `project.Project`、`project.MemberRole`、`project.Service.Create/Update/ListForUser`、`/api/v1/projects` CRUD。

- [ ] **Step 1: 写项目编码唯一和负责人可空测试**

```go
func TestCreateProjectAllowsEmptyOwnerAndRejectsDuplicateCode(t *testing.T) {
    first := CreateInput{Name: "云平台", Code: "cloud", OwnerUserID: nil}
    if _, err := service.Create(ctx, first); err != nil { t.Fatal(err) }
    if _, err := service.Create(ctx, first); !errors.Is(err, ErrDuplicateCode) {
        t.Fatalf("期望重复编码错误，实际为 %v", err)
    }
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/project -run TestCreateProjectAllowsEmptyOwnerAndRejectsDuplicateCode -v`
Expected: FAIL。

- [ ] **Step 3: 实现项目领域和接口**

项目状态限定为 `enabled`、`disabled`；负责人为可空用户外键；编码创建后不可修改；只有系统管理员可创建和删除项目。

- [ ] **Step 4: 运行项目测试**

Run: `cd backend && go test ./internal/project -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/project backend/internal/platform/httpserver/server.go
git commit -m "feat: add project management"
```

### Task 5: 实现项目成员权限隔离

**Files:**
- Create: `backend/internal/project/access.go`
- Create: `backend/internal/project/access_test.go`
- Create: `backend/internal/project/member_http.go`
- Modify: `backend/internal/project/http.go`

**Interfaces:**
- Produces: `project.RequireRole(repository, roles...) gin.HandlerFunc`；项目角色 `project_admin`、`member`、`viewer`。

- [ ] **Step 1: 写跨项目拒绝测试**

```go
func TestMemberCannotReadAnotherProject(t *testing.T) {
    response := requestProjectAsUser(t, projectB.ID, memberOfProjectA.ID)
    if response.Code != http.StatusNotFound {
        t.Fatalf("跨项目访问必须隐藏资源存在性，状态码=%d", response.Code)
    }
}
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd backend && go test ./internal/project -run TestMemberCannotReadAnotherProject -v`
Expected: FAIL。

- [ ] **Step 3: 实现权限中间件和成员接口**

系统管理员绕过项目成员检查；项目管理员管理成员；成员和只读成员仅可读取；无权访问统一返回 404，防止枚举项目。

- [ ] **Step 4: 运行权限测试**

Run: `cd backend && go test ./internal/project -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/project
git commit -m "feat: enforce project access isolation"
```

### Task 6: 建立新版前端基础和身份状态

**Files:**
- Create: `frontend/src/modules/auth/api.ts`
- Create: `frontend/src/modules/auth/store.ts`
- Create: `frontend/src/modules/auth/LoginPage.vue`
- Create: `frontend/src/modules/auth/store.test.ts`
- Rewrite: `frontend/src/router/index.ts`
- Rewrite: `frontend/src/App.vue`

**Interfaces:**
- Produces: `useAuthStore()`、登录页 `/login`、认证路由守卫。

- [ ] **Step 1: 写登出清理测试**

```ts
it('登出后清除令牌和当前用户', () => {
  const auth = useAuthStore()
  auth.acceptSession('token', { id: 1, username: 'admin', globalRole: 'system_admin' })
  auth.logout()
  expect(auth.token).toBe('')
  expect(auth.currentUser).toBeNull()
})
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd frontend && corepack pnpm test -- src/modules/auth/store.test.ts`
Expected: FAIL。

- [ ] **Step 3: 实现认证模块和路由守卫**

令牌只通过统一请求模块注入；401 时清理会话并回到登录页；页面和 TypeScript 类型添加中文说明注释。

- [ ] **Step 4: 运行前端测试和类型检查**

Run: `cd frontend && corepack pnpm test -- src/modules/auth/store.test.ts && corepack pnpm exec vue-tsc --noEmit`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/App.vue frontend/src/router frontend/src/modules/auth
git commit -m "feat: rebuild frontend authentication"
```

### Task 7: 实现现代化控制台布局和项目上下文

**Files:**
- Create: `frontend/src/layouts/ConsoleLayout.vue`
- Create: `frontend/src/modules/project/api.ts`
- Create: `frontend/src/modules/project/store.ts`
- Create: `frontend/src/modules/project/ProjectListPage.vue`
- Create: `frontend/src/modules/project/ProjectDetailPage.vue`
- Create: `frontend/src/modules/project/store.test.ts`
- Create: `frontend/src/styles/tokens.css`
- Create: `frontend/src/styles/base.css`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/main.ts`

**Interfaces:**
- Produces: `useProjectStore()`、`currentProjectId`、项目切换器、`/projects`、`/projects/:projectId`。

- [ ] **Step 1: 写项目切换持久化测试**

```ts
it('只恢复当前用户有权访问的项目', async () => {
  localStorage.setItem('cmdb.currentProjectId', '9')
  mockProjects([{ id: 2, name: '平台项目', code: 'platform' }])
  const store = useProjectStore()
  await store.loadProjects()
  expect(store.currentProjectId).toBe(2)
})
```

- [ ] **Step 2: 运行测试并确认失败**

Run: `cd frontend && corepack pnpm test -- src/modules/project/store.test.ts`
Expected: FAIL。

- [ ] **Step 3: 实现控制台和项目页面**

左侧分组导航，顶部项目切换和用户菜单；全局搜索在资源核心交付前保持隐藏；使用低饱和浅色变量；项目列表完整实现加载、空数据、失败和无权限状态；不加入旧版页面入口。

- [ ] **Step 4: 验证前端**

Run: `cd frontend && corepack pnpm test && corepack pnpm build`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add frontend/src/layouts frontend/src/modules/project frontend/src/styles frontend/src/main.ts frontend/src/router
git commit -m "feat: add modern project console"
```

### Task 8: 完成第一阶段集成验收

**Files:**
- Create: `backend/internal/platform/httpserver/integration_test.go`
- Create: `frontend/src/app-flow.test.ts`
- Modify: `Dockerfile`
- Modify: `docker-compose.yml`
- Modify: `README.md`

**Interfaces:**
- Consumes: Tasks 1-7 的全部公开接口。
- Produces: 可通过 Docker Compose 启动的新版 CMDB 第一阶段系统。

- [ ] **Step 1: 写端到端权限集成测试**

```go
func TestProjectBoundaryEndToEnd(t *testing.T) {
    token := loginFixtureUser(t, "member-a")
    assertStatus(t, token, "/api/v1/projects/1", http.StatusOK)
    assertStatus(t, token, "/api/v1/projects/2", http.StatusNotFound)
}
```

- [ ] **Step 2: 运行集成测试并确认失败**

Run: `cd backend && go test ./internal/platform/httpserver -run TestProjectBoundaryEndToEnd -v`
Expected: FAIL，直到真实路由和测试数据库装配完成。

- [ ] **Step 3: 完成测试装配、部署配置和中文说明**

Compose 必须要求显式提供 `JWT_SECRET` 和 `CMDB_ENCRYPTION_KEY`；README 只描述新版项目管理能力和空库启动方式；不得保留默认生产密码。

- [ ] **Step 4: 执行完整验收**

Run: `cd backend && go test ./...`
Expected: PASS。

Run: `cd frontend && corepack pnpm test && corepack pnpm build`
Expected: PASS。

Run: `docker compose config`
Expected: PASS，且配置中不存在硬编码真实凭证。

- [ ] **Step 5: 提交**

```bash
git add backend/internal/platform/httpserver/integration_test.go frontend/src/app-flow.test.ts Dockerfile docker-compose.yml README.md
git commit -m "test: verify cmdb foundation delivery"
```
