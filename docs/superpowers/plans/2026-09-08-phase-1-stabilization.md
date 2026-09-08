# CMDB Phase 1 Stabilization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the existing CMDB core flows reliable by fixing authorization, trusted identity, audit redaction, frontend response handling, and CI import consistency.

**Architecture:** Preserve the existing Gin/Vue layering and response envelope. Add small pure helpers at trust boundaries, test those helpers first, then connect handlers and views to them without restructuring unrelated modules.

**Tech Stack:** Go 1.23, Gin, GORM, Vue 3, TypeScript, Pinia, Axios, Vitest.

**Spec:** `docs/superpowers/specs/2026-09-08-phase-1-stabilization-design.md`

## Global Constraints

- Do not delete existing business modules or introduce new product features.
- Preserve unrelated user changes, especially existing dashboard and relation edits.
- `super_admin` must satisfy all role-protected routes.
- The backend remains the final authorization boundary.
- Password-bearing request bodies must never be stored in audit logs.
- Use the existing `{code, message, data}` API envelope.
- Every production behavior change starts with a failing automated test.

---

### Task 1: Backend authorization and JWT identity

**Files:**
- Modify: `backend/internal/middleware/rbac.go`
- Modify: `backend/internal/middleware/auth.go`
- Test: `backend/internal/middleware/auth_test.go`

**Interfaces:**
- Produces: `HasAnyRole(userRoles []string, requiredRoles ...string) bool`
- Produces: authenticated Gin context keys `user_id`, `username`, `roles`, `departments`

- [ ] Write tests proving `super_admin` satisfies any protected role, ordinary roles only satisfy exact matches, and a generated token preserves configured expiry and context identity.
- [ ] Run `go test ./internal/middleware -run 'TestHasAnyRole|TestAuthRequired|TestGenerateToken'` and confirm the new tests fail for the intended missing behavior.
- [ ] Implement or normalize `HasAnyRole`, make `RequireRole` delegate to it, and keep token generation/context population configuration-driven.
- [ ] Re-run the focused tests and confirm they pass.

### Task 2: Trusted change operator identity

**Files:**
- Modify: `backend/internal/handler/change_handler.go`
- Test: `backend/internal/handler/change_handler_test.go`

**Interfaces:**
- Consumes: `middleware.GetCurrentUsername(*gin.Context) string`
- Produces: change transition handlers that ignore client-supplied operator names

- [ ] Write handler tests showing approval, rejection and execution use the authenticated username even when the JSON body contains another name, and return 401 when identity is missing.
- [ ] Run the focused handler tests and confirm failure because request fields are currently trusted.
- [ ] Add one handler helper that requires the authenticated username and pass it to service transition methods; stop binding operator fields from request bodies.
- [ ] Apply the same authenticated-identity check to submit, complete, fail and rollback before transition, without changing service state rules.
- [ ] Re-run focused tests.

### Task 3: Audit redaction and identity

**Files:**
- Modify: `backend/internal/middleware/audit.go`
- Test: `backend/internal/middleware/audit_test.go`

**Interfaces:**
- Produces: `ShouldCaptureAuditBody(path string) bool`
- Produces: `ReadAuditIdentity(*gin.Context) (uint64, string)` using authenticated context with safe JWT fallback

- [ ] Write pure-helper tests covering login, self password change, admin password reset, normal JSON routes, `user_id` claims, and the 4096-byte limit.
- [ ] Run focused tests and confirm failure.
- [ ] Implement sensitive-path redaction before body reading, use authenticated context identity after `c.Next`, and safely decode `user_id` only as fallback for public/pre-auth requests.
- [ ] Keep health/static exclusions and asynchronous persistence unchanged.
- [ ] Re-run focused tests.

### Task 4: CI import source and route precedence

**Files:**
- Modify: `backend/internal/service/ci_instance_svc.go`
- Modify: `backend/internal/router/router.go`
- Test: `backend/internal/service/ci_instance_svc_test.go`
- Test: `backend/internal/router/router_test.go`

**Interfaces:**
- Produces: `NormalizeCISource(source string) string`, defaulting only an empty source to `manual`
- Produces: static `/ci-instances/import`, `/ci-instances/export`, and `/snapshots/diff` routes registered before `/:id`

- [ ] Write tests showing `import` and `auto_discovery` survive normalization while empty source becomes `manual`; add route-resolution tests for all static paths.
- [ ] Run focused tests and confirm failure.
- [ ] Make CI creation preserve a valid caller-supplied source and reject or normalize unsupported values consistently with the model enum.
- [ ] Reorder static routes before parameter routes.
- [ ] Re-run focused tests.

### Task 5: Frontend test foundation and authentic user state

**Files:**
- Modify: `frontend/package.json`
- Modify: `frontend/src/api/auth.ts`
- Modify: `frontend/src/stores/auth.ts`
- Create: `frontend/src/stores/auth.test.ts`

**Interfaces:**
- Produces: persisted auth state containing token, user ID, username, display name and real roles
- Produces: `hasAnyRole(...roles: string[]): boolean`

- [ ] Add Vitest as a development dependency and a `test` script, using the existing package manager lockfile.
- [ ] Write store tests proving roles come from the login response, survive reload, `super_admin` satisfies any permission, and logout clears only CMDB authentication keys.
- [ ] Run the focused test and confirm it fails because roles are hard-coded and logout clears unrelated storage.
- [ ] Type the login response and implement the minimal auth-state changes.
- [ ] Re-run the focused test.

### Task 6: Frontend API envelopes and affected pages

**Files:**
- Modify: `frontend/src/utils/request.ts`
- Modify: `frontend/src/api/discovery.ts`
- Modify: `frontend/src/api/snapshot.ts`
- Modify: `frontend/src/views/DiscoveryStrategy.vue`
- Modify: `frontend/src/views/DiscoveryHistory.vue`
- Modify: `frontend/src/views/SnapshotDiff.vue`
- Create: `frontend/src/utils/api-response.test.ts`

**Interfaces:**
- Produces: `ApiResponse<T> = { code: number; message: string; data: T }`
- Produces: page payload type `{items: T[]; total: number; page: number; page_size: number}`

- [ ] Write tests for typed payload extraction and strategy `target_config` JSON parsing, including malformed JSON rejection.
- [ ] Run focused tests and confirm failure.
- [ ] Add shared response/page types without changing the runtime envelope behavior.
- [ ] Update the three affected views to read `response.data` consistently.
- [ ] Parse strategy configuration before submission and display a validation error for invalid JSON.
- [ ] Re-run focused tests and TypeScript checking.

### Task 7: Frontend permission presentation

**Files:**
- Modify: `frontend/src/components/AppLayout.vue`
- Modify: `frontend/src/router/index.ts`
- Modify: `frontend/src/views/UserManagement.vue`
- Modify: `frontend/src/views/AuditLog.vue`
- Modify: relevant write-capable CI, relation, change and discovery views only where controls lack checks
- Test: `frontend/src/stores/auth.test.ts`

**Interfaces:**
- Consumes: `hasAnyRole(...roles: string[]): boolean`
- Produces: route metadata `roles?: string[]` and consistent UI permission checks

- [ ] Extend auth tests with menu/route permission cases.
- [ ] Run focused tests and confirm failure.
- [ ] Add role metadata to administrator-only pages and enforce it in navigation guards.
- [ ] Hide unauthorized menu entries and mutating controls while keeping backend enforcement unchanged.
- [ ] Re-run tests and production build.

### Task 8: Integrated verification and documentation alignment

**Files:**
- Modify: `README.md`
- Modify: `docs/CMDB-ARCHITECTURE.md` only where statements contradict verified implementation

**Interfaces:**
- No new runtime interface.

- [ ] Run `go test ./...` from `backend` and record the exact result.
- [ ] Run the frontend test suite and `pnpm build` from `frontend` and record exact results.
- [ ] Inspect `git diff --check` and ensure unrelated working-tree changes were not overwritten.
- [ ] Update documentation to distinguish completed SSH discovery from placeholder Agent/Kubernetes/Cloud collectors and describe the actual Compose scope.
- [ ] Re-run documentation-sensitive checks and final builds after edits.
