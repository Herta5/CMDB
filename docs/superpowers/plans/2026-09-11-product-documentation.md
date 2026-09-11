# CMDB 产品文档补全实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 补全 CMDB 产品文档体系，使产品目标、领域规则、同步流程、权限安全、前端体验、验收标准和当前实现状态都有清晰且一致的权威定义。

**Architecture:** 以 `docs/project/` 为规范中心，现有五份文件分别承担产品范围、领域模型、资源同步、安全权限和前端体验职责；新增索引、端到端验收和实现状态文件。规范文件描述目标要求，实现状态文件描述提交 `8c88e59` 的代码事实，根 README 只提供入口。

**Tech Stack:** Markdown、Git、Go 1.25.1、Vue 3、TypeScript、Pinia、Vue Router、Element Plus、PostgreSQL 17

**Spec:** `docs/superpowers/specs/2026-09-11-product-documentation-design.md`

## Global Constraints

- 项目名称统一为 **CMDB**；业务项目是最高级的数据归属和普通用户权限隔离边界。
- 系统管理员是可跨项目管理的最高权限角色，无需预先加入项目。
- 控制台统一使用“云同步管理”；阿里云与 AWS 采集器保持独立模块边界。
- 首期资源固定为阿里云 ECS、RDS、SLB 和 AWS EC2、RDS、ELB。
- 资源身份、同步失败保护、24 小时删除确认和敏感信息红线保持不变。
- 排序、分页和列配置是资源列表目标要求；未实现部分只在实现状态中记录。
- 不新增云平台、资源类型、角色、微服务、手机端专项适配或云属性手工编辑能力。
- 用户可见文案和验收描述使用中文，固定产品名、协议名和代码值可以保留英文。
- 所有修改在 `codex/product-docs` 分支完成，每个提交只包含一个可验证逻辑变更。

---

### Task 1: 建立产品文档入口与权威关系

**Files:**

- Modify: `.gitignore`
- Create: `docs/project/README.md`

**Interfaces:**

- Consumes: `AGENTS.md` 的文档读取规则和全局约束。
- Produces: 全部项目规范的阅读顺序、权威层级和文件职责。

- [ ] **Step 1: 运行缺失检查**

Run: `test -f docs/project/README.md`

Expected: FAIL，退出码 1。

- [ ] **Step 2: 更新 Git 白名单**

在 `.gitignore` 中放行 `docs/project/README.md`、`acceptance.md`、`implementation-status.md`，使新增规范可被正常跟踪。

- [ ] **Step 3: 创建索引**

`docs/project/README.md` 必须包含“文档用途、权威顺序、阅读顺序、文档职责、维护规则”。权威顺序明确为：用户确认与 `AGENTS.md` 业务红线、项目规范、实现状态、README 与代码说明。

- [ ] **Step 4: 验证并提交**

Run: `for name in overview domain-model resource-sync security frontend-guidelines acceptance implementation-status; do grep -q "${name}.md" docs/project/README.md || exit 1; done`

Expected: PASS。随后运行 `git diff --check`，提交 `.gitignore` 和索引，提交信息为 `docs: 建立产品文档索引`。

---

### Task 2: 补全产品定位、角色与首期范围

**Files:**

- Modify: `docs/project/overview.md`

**Interfaces:**

- Consumes: 已确认的导航、资源列表和最高权限口径。
- Produces: 其他规范引用的产品目标、角色、模块边界、范围和术语。

- [ ] **Step 1: 运行缺失检查**

Run: `grep -q '^## 目标用户与角色$' docs/project/overview.md`

Expected: FAIL。

- [ ] **Step 2: 补充产品目标与非目标**

说明 CMDB 解决多云资产分散、归属不清、状态不可追溯和权限边界不明确的问题；明确不承担云资源编排、费用、监控告警、工单和写回云端。

- [ ] **Step 3: 补充角色和业务闭环**

定义系统管理员、项目管理员、项目成员，并写出“初始化管理员 → 创建用户和项目 → 授权 → 创建并测试接入源 → 同步 → 查询资产 → 审计”的标准闭环。

- [ ] **Step 4: 校准首期范围和模块边界**

逐个平台列出资源类型；规定统一“云同步管理”入口。平台模块只负责凭证格式和云 API 采集，项目、权限、接入源抽象、任务、资产、端点和审计属于公共核心。

- [ ] **Step 5: 补充术语与变更规则**

至少定义业务项目、接入源、云资源、云端状态、资产状态、访问端点和同步任务。范围变更继续要求用户确认。

- [ ] **Step 6: 验证并提交**

Run: `grep -q '统一.*云同步管理' docs/project/overview.md && grep -q '系统管理员.*最高权限' docs/project/overview.md && grep -q '不在首期范围' docs/project/overview.md`

Expected: PASS。运行 `git diff --check` 后提交，信息为 `docs: 补全产品定位与首期范围`。

---

### Task 3: 补全领域对象、关系与状态

**Files:**

- Modify: `docs/project/domain-model.md`

**Interfaces:**

- Consumes: `overview.md` 的角色和范围。
- Produces: 安全、同步、前端和验收共用的实体与状态语义。

- [ ] **Step 1: 运行缺失检查**

Run: `grep -q '^## 核心对象关系$' docs/project/domain-model.md`

Expected: FAIL。

- [ ] **Step 2: 定义核心关系**

写清用户与项目成员关系、项目可选负责人、接入源单项目单平台归属、任务与资产归属、三类资产独立模型与统一视图，以及审计删除后保留。

- [ ] **Step 3: 定义管理对象约束**

保留用户名格式和内部 ID 红线；补充项目编码创建后不可修改、项目启停状态、仅有 `project_admin` 与 `member` 两种项目角色，以及同一云账号/租户不得跨项目重复接入。

- [ ] **Step 4: 定义资产字段**

列出公共资产字段；ECS/EC2 保存内外网 IP，RDS 保存引擎、版本和端点，SLB/ELB 保存网络类型和端点。端点定义类型、地址、端口、协议及最近解析 IP。

- [ ] **Step 5: 区分状态和时间语义**

区分云端状态与资产状态；明确在库状态只有正常和已失联，“已删除”是物理删除结果。未变化仅刷新最近发现时间，易变观测属性不虚增更新统计。

- [ ] **Step 6: 验证并提交**

Run: `grep -q '接入源 + 资源类型 + 云端唯一 ID' docs/project/domain-model.md && grep -q '云端状态.*资产状态' docs/project/domain-model.md && grep -q '易变观测' docs/project/domain-model.md`

Expected: PASS。运行 `git diff --check` 后提交，信息为 `docs: 补全领域模型与资产状态`。

---

### Task 4: 补全接入源、同步任务与生命周期

**Files:**

- Modify: `docs/project/resource-sync.md`

**Interfaces:**

- Consumes: `domain-model.md` 的接入源、任务、资产和端点。
- Produces: 前端状态、权限矩阵和验收使用的同步契约。

- [ ] **Step 1: 运行缺失检查**

Run: `grep -q '^## 任务状态与结果$' docs/project/resource-sync.md`

Expected: FAIL。

- [ ] **Step 2: 定义接入源与连接测试**

记录平台、名称、区域、凭证、非敏感配置、启停和同步周期；默认 60 分钟，允许 5–10080 分钟。连接测试使用轻量探测，不写资源、不建任务、不推进生命周期。

- [ ] **Step 3: 定义触发、并发和状态机**

手工同步先返回排队任务；自动调度只处理启用且到期来源；同源自动、手工和重试互斥，异源并行。完整定义 `queued`、`running`、`success`、`partial_success`、`failed`。

- [ ] **Step 4: 定义类型级处理和统计**

每类记录新增、更新、恢复、失联、删除和失败。类型失败不影响其他成功类型且不得推进本类型生命周期；认证或整体采集失败不得改变任何资产状态。

- [ ] **Step 5: 定义生命周期与事务边界**

依次定义新增、幂等刷新、真实更新、失联、恢复和删除。删除必须满 24 小时后由同类成功采集再次确认；任务资产变化、统计、终态、调度时间和审计保持一致。

- [ ] **Step 6: 定义重试、重启恢复和安全摘要**

失败及部分成功任务重试时创建新任务并引用原任务；服务重启恢复排队任务、结束异常中断任务；只保存中文安全摘要，不保存云端原始错误或凭证。

- [ ] **Step 7: 验证并提交**

Run: `grep -q '5.*10080' docs/project/resource-sync.md && grep -q '排队' docs/project/resource-sync.md && grep -q '满 24 小时' docs/project/resource-sync.md`

Expected: PASS。运行 `git diff --check` 后提交，信息为 `docs: 补全云资源同步规范`。

---

### Task 5: 补全身份、权限、凭证与审计

**Files:**

- Modify: `docs/project/security.md`

**Interfaces:**

- Consumes: 三类角色、身份引用和同步安全摘要。
- Produces: 前端入口和验收共用的授权矩阵与安全契约。

- [ ] **Step 1: 运行缺失检查**

Run: `grep -q '^## 权限矩阵$' docs/project/security.md`

Expected: FAIL。

- [ ] **Step 2: 定义认证和会话安全**

写清密码 12–72 字节、bcrypt、统一登录失败、24 小时 HS256 JWT、最小公开声明及每次请求重读用户；停用、删除、同名重建或权限变化后旧会话不得保留旧权限。

- [ ] **Step 3: 建立权限矩阵**

覆盖用户、项目、成员、候选用户、接入源、连接测试、同步/重试、资产、任务历史、项目审计和全局审计。系统管理员全部允许且无需成员关系；项目管理员限所属项目管理；项目成员限所属项目只读。

- [ ] **Step 4: 定义隔离、自我保护和防枚举**

后端按路径项目重新授权；前端隐藏不能替代授权。未授权与不存在项目返回一致响应；系统管理员不能删除、停用或降级自己，项目管理员不能移除自己。

- [ ] **Step 5: 定义凭证和敏感信息保护**

说明 `CMDB_ENCRYPTION_KEY` 派生 AES-256-GCM 密钥、密文存储、脱敏提示、留空保留和显式替换。列出日志、审计、响应、页面状态、持久化、测试和仓库的禁止泄露项。

- [ ] **Step 6: 定义审计查询与保留**

覆盖用户、项目、成员、接入源、连接测试、同步和资源生命周期动作；定义操作人快照、请求 IP、后台系统操作、筛选条件、固定快照稳定分页及对象删除后保留。

- [ ] **Step 7: 验证并提交**

Run: `grep -q '系统管理员.*无需.*成员' docs/project/security.md && grep -q 'AES-256-GCM' docs/project/security.md && grep -q '稳定分页' docs/project/security.md`

Expected: PASS。运行 `git diff --check` 后提交，信息为 `docs: 补全安全权限与审计规范`。

---

### Task 6: 补全控制台信息架构与交互

**Files:**

- Modify: `docs/project/frontend-guidelines.md`

**Interfaces:**

- Consumes: 统一入口、权限矩阵和同步状态。
- Produces: 页面、列表、状态、响应式和可达性验收标准。

- [ ] **Step 1: 运行缺失检查**

Run: `grep -q '^## 页面职责$' docs/project/frontend-guidelines.md`

Expected: FAIL。

- [ ] **Step 2: 校准导航与项目上下文**

左侧分组为资产与管理；管理区统一包含项目、云同步、审计及系统管理员可见的用户管理。顶部项目切换是全局边界；系统管理员可选所有项目，普通用户仅可选授权项目。

- [ ] **Step 3: 定义页面职责与权限可见性**

逐页描述登录、三类资产、项目列表/详情、用户、云同步和审计。路由守卫只改善体验，后端仍是最终授权边界。

- [ ] **Step 4: 定义资源列表要求**

规定必要列、平台/类型/状态筛选、允许列升降序与稳定次级排序、服务端分页及回首页规则、显示/隐藏列与恢复默认、资源身份列不可隐藏、失联资源保留最近有效属性。

- [ ] **Step 5: 定义管理操作和页面状态**

规定凭证不回填、连接测试、立即同步、任务轮询、失败重试、表单校验、危险操作确认、防重复提交和服务端刷新；所有数据页覆盖加载、空、失败、无权限和提交中。

- [ ] **Step 6: 定义视觉、适配和可达性**

保留浅色低饱和与品牌识别规则；常见桌面完整可用、平板允许横向滚动、不做手机专项；提供语义标签、键盘焦点和中文提示，状态不可只依赖颜色。

- [ ] **Step 7: 验证并提交**

Run: `grep -q '统一.*云同步管理' docs/project/frontend-guidelines.md && grep -q '排序' docs/project/frontend-guidelines.md && grep -q '服务端分页' docs/project/frontend-guidelines.md && grep -q '列配置' docs/project/frontend-guidelines.md`

Expected: PASS。运行 `git diff --check` 后提交，信息为 `docs: 补全前端体验规范`。

---

### Task 7: 建立端到端验收标准

**Files:**

- Create: `docs/project/acceptance.md`

**Interfaces:**

- Consumes: 五份领域规范的强制要求。
- Produces: 可直接执行的端到端产品验收清单。

- [ ] **Step 1: 运行缺失检查**

Run: `test -f docs/project/acceptance.md`

Expected: FAIL。

- [ ] **Step 2: 定义场景格式**

每个场景使用唯一 `AC-` 编号，并包含“前置条件、操作、预期结果”。真实云采集一律使用接口与模拟响应；安全场景同时检查响应、日志、审计和页面状态。

- [ ] **Step 3: 编写身份与权限场景**

覆盖初始化、成功/失败登录、停用失效、最高权限、项目管理员、项目成员、跨项目防枚举、自我保护和用户名公开契约。

- [ ] **Step 4: 编写接入源与同步场景**

覆盖创建与脱敏、保留/替换凭证、连接测试无副作用、周期、手工排队、同源冲突、异源并行、部分成功、认证失败、重试、重启恢复和安全摘要。

- [ ] **Step 5: 编写资产、审计与页面场景**

覆盖幂等、更新、易变字段、失败保护、失联、恢复、24 小时删除、筛选/排序/分页/列配置、审计可追溯、递归脱敏、项目切换、页面状态和平板/键盘可用。

- [ ] **Step 6: 验证并提交**

Run: `test "$(grep -c '^### AC-' docs/project/acceptance.md)" -ge 20 && grep -q '前置条件' docs/project/acceptance.md && grep -q '预期结果' docs/project/acceptance.md`

Expected: 至少 20 个完整场景。运行 `git diff --check` 后提交，信息为 `docs: 建立端到端产品验收标准`。

---

### Task 8: 记录实现状态并补 README 入口

**Files:**

- Create: `docs/project/implementation-status.md`
- Modify: `README.md`

**Interfaces:**

- Consumes: 全部规范、验收场景及基线提交的前后端代码和测试。
- Produces: 当前交付差距与仓库首页的规范入口。

- [ ] **Step 1: 运行缺失检查**

Run: `test -f docs/project/implementation-status.md` 和 `grep -q 'docs/project/README.md' README.md`

Expected: 两项均 FAIL。

- [ ] **Step 2: 定义状态与基线**

基线固定为 `8c88e59`；状态只有已实现、部分实现、待实现；明确本文件不改变规范，后续功能提交需更新证据。

- [ ] **Step 3: 填写能力矩阵**

覆盖身份、用户、项目、成员、最高权限、项目隔离、两家云接入源、连接测试、同步、并发、重试、重启恢复、三类资产、生命周期、审计和控制台。每行包含规范链接、状态、证据路径和差距。

- [ ] **Step 4: 准确记录列表差距**

拆分记录：筛选已实现；后端分页已实现但前端完整分页交互为部分实现；排序待实现；列配置待实现。统一云同步入口和系统管理员跨项目权限为已实现。

- [ ] **Step 5: 增加 README 文档入口**

在简介后链接 `docs/project/README.md`，说明规范定义长期要求，`implementation-status.md` 记录当前状态，不复制矩阵内容。

- [ ] **Step 6: 验证并提交**

Run: `grep -q '8c88e59' docs/project/implementation-status.md && grep -q '排序.*待实现' docs/project/implementation-status.md && grep -q '列配置.*待实现' docs/project/implementation-status.md && grep -q 'docs/project/README.md' README.md`

Expected: PASS。核对证据路径存在并运行 `git diff --check`，提交信息为 `docs: 记录产品实现状态`。

---

### Task 9: 全局一致性审查与交付

**Files:**

- Modify: `docs/project/*.md`（只修正审查发现的矛盾、链接或遗漏）
- Modify: `README.md`（只修正入口）

**Interfaces:**

- Consumes: Tasks 1–8 的完整文档集。
- Produces: 无占位、无冲突、链接有效且可合并的最终文档。

- [ ] **Step 1: 检查文件范围与格式**

Run: `git diff main...HEAD --name-only`、`git diff --check`。只允许 `.gitignore`、`README.md`、`docs/project/*.md` 和本次设计/计划。

- [ ] **Step 2: 搜索未决内容和旧范围**

Run: `if grep -RniE '待定|稍后补充|另行确认|Kubernetes|拓扑管理|变更工单' docs/project; then exit 1; fi`

Expected: PASS，无输出。

- [ ] **Step 3: 复核确认口径和红线**

逐项确认统一云同步入口、排序/分页/列配置、系统管理员最高权限、项目隔离、资源唯一身份、失败保护、24 小时删除确认和敏感信息保护均有唯一明确表述。

- [ ] **Step 4: 检查链接与文档数量**

解析 `docs/project/*.md` 与 README 的本地 Markdown 链接，目标文件必须存在；运行 `test "$(find docs/project -maxdepth 1 -name '*.md' | wc -l)" -eq 8`。

- [ ] **Step 5: 审查验收覆盖与实现证据**

五份规范中的每项强制规则至少对应一个验收场景；逐行打开实现状态引用路径。重点确认统一入口已实现、列表前端分页部分实现、排序和列配置待实现、最高权限已实现。

- [ ] **Step 6: 运行最终文档验证**

Run: `test "$(grep -c '^### AC-' docs/project/acceptance.md)" -ge 20 && git diff --check && git status --short`

Expected: 文档数量和场景数量满足要求，无空白错误，无意外文件。

- [ ] **Step 7: 提交审查修正**

如有修正，提交信息为 `docs: 校准产品规范一致性`；没有修正则不创建空提交。

- [ ] **Step 8: 合并前复核**

运行 `git log --oneline main..HEAD`、`git diff --stat main...HEAD`、`git status --short`。随后使用 `superpowers:finishing-a-development-branch` 纳入最新 `main`、复核验证并选择本地合并或 Pull Request。
