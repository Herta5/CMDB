# CMDB 实现状态

## 用途与基线

本文记录提交 `8c88e59` 的实际实现快照，用于追溯当前交付能力和后续差距；它不改变 [项目规范入口](README.md) 及其链接规范中的任何长期要求。状态仅使用“已实现”“部分实现”“待实现”：已实现表示基线代码和测试已覆盖该能力，部分实现表示已有可用实现但未满足全部规范边界，待实现表示基线未提供该能力。

后续功能提交必须同时更新受影响行的状态、证据和差距。证据均为基线中的真实代码或测试路径；测试使用模拟响应或隔离数据库，不连接真实云账号。

## 能力矩阵

| 能力 | 规范 | 状态 | 基线证据 | 差距 |
| --- | --- | --- | --- | --- |
| 身份认证与会话 | [security.md：身份认证与会话安全](security.md#身份认证与会话安全) | 已实现 | `backend/internal/identity/service.go`、`backend/internal/platform/httpserver/auth.go`、`backend/internal/identity/http_test.go` | 已实现 HS256、24 小时会话、账户代际绑定、实时读取账户状态与统一登录失败提示。 |
| 用户与用户名公开契约 | [domain-model.md：用户](domain-model.md#用户)、[security.md：身份标识暴露边界](security.md#身份标识暴露边界) | 已实现 | `backend/internal/identity/user.go`、`backend/internal/identity/username.go`、`backend/internal/identity/service.go`、`backend/internal/identity/http_test.go`、`frontend/src/modules/user/` | 用户名校验、唯一性、不可修改、公开 API 转换及用户管理均已实现。 |
| 业务项目 | [domain-model.md：业务项目与成员](domain-model.md#业务项目与成员)、[security.md：权限矩阵](security.md#权限矩阵) | 部分实现 | `backend/internal/project/model.go`、`backend/internal/project/service.go`、`backend/internal/project/http.go`、`backend/internal/project/service_test.go` | 创建、编辑、启停、删除和项目编码约束已实现；停用项目的运行操作拦截见“停用项目运行边界”。 |
| 项目成员与自我保护 | [domain-model.md：业务项目与成员](domain-model.md#业务项目与成员)、[security.md：自我保护](security.md#自我保护) | 已实现 | `backend/internal/project/member_http.go`、`backend/internal/project/service.go`、`backend/internal/project/access_test.go`、`backend/internal/project/service_test.go` | 成员角色、唯一成员关系、负责人不授予权限及管理员自我保护已有实现和测试。 |
| 系统管理员跨项目最高权限 | [overview.md：目标用户与角色](overview.md#目标用户与角色)、[security.md：权限矩阵](security.md#权限矩阵) | 已实现 | `backend/internal/project/access.go`、`backend/internal/project/service.go`、`frontend/src/modules/project/store.ts`、`frontend/src/layouts/ConsoleLayout.vue` | 系统管理员可绕过项目成员关系访问具体项目，并有“所有项目”只读上下文；项目级操作仍使用具体项目路径。 |
| 项目隔离与对象归属 | [domain-model.md：核心对象关系](domain-model.md#核心对象关系)、[security.md：项目隔离与授权执行](security.md#项目隔离与授权执行) | 已实现 | `backend/internal/project/access.go`、`backend/internal/resource/service.go`、`backend/internal/platform/httpserver/server.go`、`backend/internal/project/access_test.go` | 路径项目作为授权边界，接入源和任务均二次核对项目归属，无权与不存在项目统一处理。 |
| 阿里云与 AWS 接入源及模块边界 | [overview.md：首期范围](overview.md#首期范围)、[resource-sync.md：接入源与连接测试](resource-sync.md#接入源与连接测试) | 已实现 | `backend/internal/aliyun/collector.go`、`backend/internal/aws/collector.go`、`backend/internal/resource/collector.go`、`backend/cmd/server/main.go`、`backend/internal/aliyun/collector_test.go`、`backend/internal/aws/collector_test.go` | 阿里云与 AWS 采集器保持独立模块，经共享资源核心接入；首期类型为 ECS/RDS/SLB 与 EC2/RDS/ELB。 |
| 接入源凭证、配置与连接测试 | [resource-sync.md：接入源与连接测试](resource-sync.md#接入源与连接测试)、[security.md：凭证加密与更新](security.md#凭证加密与更新) | 已实现 | `backend/internal/resource/credential.go`、`backend/internal/resource/service.go`、`backend/internal/resource/http.go`、`backend/internal/resource/credential_test.go`、`backend/internal/resource/service_test.go` | AES-256-GCM 凭证保护、留空保留、轻量 Probe、无资源副作用和安全审计均已实现。 |
| 同步任务、类型级处理与事务 | [resource-sync.md：触发、调度与并发](resource-sync.md#触发调度与并发)、[resource-sync.md：任务状态与结果](resource-sync.md#任务状态与结果)、[resource-sync.md：事务与一致性边界](resource-sync.md#事务与一致性边界) | 部分实现 | `backend/internal/resource/service.go`、`backend/internal/resource/repository.go`、`backend/internal/resource/service_test.go` | 手工任务入队、后台执行、状态、统计、部分成功与最终事务已实现；自动同步失败后未将下次执行时间推进到“完成时间 + 同步周期”。 |
| 同一接入源互斥与异源并行 | [resource-sync.md：触发、调度与并发](resource-sync.md#触发调度与并发) | 已实现 | `backend/internal/resource/service.go`、`backend/internal/resource/service_test.go` | 以接入源锁统一约束手工、自动和重试；不同接入源可并行。 |
| 重试 | [resource-sync.md：重试与服务重启恢复](resource-sync.md#重试与服务重启恢复) | 部分实现 | `backend/internal/resource/service.go`、`backend/internal/resource/http.go`、`backend/internal/resource/service_test.go` | 失败/部分成功任务会新建关联原任务的 `queued`、`manual` 重试；停用项目未被统一拦截新的重试。 |
| 服务重启恢复 | [resource-sync.md：重试与服务重启恢复](resource-sync.md#重试与服务重启恢复) | 部分实现 | `backend/internal/resource/service.go`、`backend/internal/resource/repository.go`、`backend/cmd/server/main.go` | 已恢复 `queued` 任务并将遗留 `running` 收敛为安全失败；基线未为中断收敛写入“系统任务”审计，且停用项目未纳入新的运行边界统一校验。 |
| 停用项目运行边界 | [security.md：权限矩阵](security.md#权限矩阵)、[frontend-guidelines.md：信息架构](frontend-guidelines.md#信息架构) | 待实现 | `backend/internal/platform/httpserver/server.go`、`backend/internal/resource/http.go`、`backend/internal/resource/service.go`、`frontend/src/modules/resource/store.ts` | 基线路由与服务未统一在项目停用时阻止新的连接测试、手工同步、重试和自动任务创建；前端也未据项目状态统一禁用这些操作。已受理任务继续运行的既有语义不受此差距影响。 |
| 三类资产模型与统一只读视图 | [domain-model.md：云资源模型](domain-model.md#云资源模型)、[overview.md：首期范围](overview.md#首期范围) | 已实现 | `backend/internal/resource/model.go`、`backend/internal/resource/asset_storage.go`、`backend/internal/resource/service.go`、`backend/internal/resource/service_test.go` | ECS/EC2、两家 RDS、SLB/ELB 分别写入三类模型，并由资源核心统一查询。 |
| 资源身份与生命周期 | [domain-model.md：状态与时间语义](domain-model.md#状态与时间语义)、[resource-sync.md：资源生命周期](resource-sync.md#资源生命周期) | 已实现 | `backend/internal/resource/model.go`、`backend/internal/resource/service.go`、`backend/internal/resource/service_test.go` | 以接入源、类型、云端唯一 ID 识别；新增、幂等刷新、更新、失联、恢复及成功确认后的 24 小时清理均在类型级事务内处理。 |
| 审计与敏感信息 | [security.md：审计动作与一致性](security.md#审计动作与一致性)、[security.md：审计查询分页与保留](security.md#审计查询分页与保留) | 部分实现 | `backend/internal/audit/context.go`、`backend/internal/audit/repository.go`、`backend/internal/audit/http.go`、`backend/internal/audit/repository_test.go`、`frontend/src/modules/audit/` | 重要动作审计、事务绑定、权限查询、稳定分页和已知敏感键递归过滤已实现；未知敏感键尚未按规范拒绝持久化，重启中断任务的系统审计也未补齐。 |
| 统一云同步管理入口 | [overview.md：首期范围](overview.md#首期范围)、[frontend-guidelines.md：信息架构](frontend-guidelines.md#信息架构) | 已实现 | `frontend/src/router/index.ts`、`frontend/src/layouts/ConsoleLayout.vue`、`frontend/src/modules/resource/CloudSyncManagementPage.vue`、`frontend/src/modules/resource/store.ts` | 控制台使用统一“云同步管理”入口汇总阿里云与 AWS 接入源和任务，不合并两个采集器模块。 |
| 资源列表筛选 | [frontend-guidelines.md：资源列表](frontend-guidelines.md#资源列表) | 部分实现 | `backend/internal/resource/http.go`、`backend/internal/resource/service.go`、`backend/internal/resource/repository.go`、`frontend/src/modules/resource/AssetListPage.vue` | 后端支持平台、资源类型和资产状态筛选；前端仅提供资产状态选择，资源类型由页面类别固定且未明确展示当前条件，实际请求未传 `provider`，因此缺少云平台、资源类型和资产状态的组合筛选。 |
| 资源列表必显字段 | [frontend-guidelines.md：资源列表](frontend-guidelines.md#资源列表) | 部分实现 | `frontend/src/modules/resource/AssetListPage.vue` | 已展示名称、云端唯一 ID、项目（所有项目上下文）、云平台、类型、地域或可用区、云端状态和资产状态；表头缺少规范要求必显的接入源和最近发现时间。 |
| 资源列表分页 | [frontend-guidelines.md：资源列表](frontend-guidelines.md#资源列表) | 部分实现 | `backend/internal/resource/http.go`、`backend/internal/resource/service.go`、`backend/internal/resource/repository.go`、`frontend/src/modules/resource/store.ts`、`frontend/src/modules/resource/AssetListPage.vue` | 后端分页已实现；前端完整分页交互仅部分实现，资产页固定请求大页并在“所有项目”上下文聚合，未提供完整页码、每页数量和总条数交互。 |
| 资源列表排序 | [frontend-guidelines.md：资源列表](frontend-guidelines.md#资源列表) | 待实现 | `backend/internal/resource/repository.go`、`frontend/src/modules/resource/AssetListPage.vue` | 排序待实现：接口没有排序参数，仓储仅按既有更新时间排序，页面没有单列排序控制。 |
| 资源列表列配置 | [frontend-guidelines.md：资源列表](frontend-guidelines.md#资源列表) | 待实现 | `frontend/src/modules/resource/AssetListPage.vue`、`frontend/src/modules/resource/store.ts` | 列配置待实现：尚无可选列显示/隐藏、重排、恢复默认或按用户名、页面类型、项目上下文持久化。 |
| 控制台页面、状态与项目切换 | [frontend-guidelines.md：页面职责](frontend-guidelines.md#页面职责)、[frontend-guidelines.md：管理操作与页面状态](frontend-guidelines.md#管理操作与页面状态) | 部分实现 | `frontend/src/router/index.ts`、`frontend/src/layouts/ConsoleLayout.vue`、`frontend/src/modules/project/store.ts`、`frontend/src/session-races.test.ts`、`frontend/src/app-flow.test.ts` | 已有中文导航、角色路由边界、项目上下文、请求竞态清理及主要加载/空/失败状态；仍受资源列表完整分页、排序、列配置和停用项目操作限制缺口影响。 |

## 后续更新规则

实现任何“部分实现”或“待实现”项时，必须先满足对应规范和验收场景，再在本文件更新状态、具体证据路径及剩余差距；不得仅因页面入口、模拟调用或单一单元测试存在就改变状态。
