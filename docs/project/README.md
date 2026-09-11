# CMDB 项目规范入口

## 文档用途

本目录是 CMDB 产品规范的统一入口，面向产品、研发、测试和维护人员，定义公有云资源配置管理平台的长期业务要求、领域约束、交互要求和验收口径。本文档只负责导航、说明权威关系和划分文件职责，不复制具体业务规则。

## 权威顺序

当不同来源的描述不一致时，按以下顺序判断权威性：

1. 用户确认与根目录 `AGENTS.md` 中的业务红线和执行约束；
2. 本目录中的项目规范；
3. `implementation-status.md` 中针对特定提交记录的实现状态；
4. 根目录 `README.md` 与代码说明。

实现状态是交付快照，只用于说明某个版本已经完成、部分完成或尚未完成的事实，不得覆盖项目规范中的产品要求。README 和代码说明服务于部署、开发和实现核对，不能替代项目规范或自行改变产品口径。

## 阅读顺序

首次了解产品时，先阅读 [overview.md](overview.md)，确认产品定位、首期范围、角色和公共能力边界；再阅读 [domain-model.md](domain-model.md)，掌握数据归属、资源身份和领域关系。

涉及同步流程、并发、采集结果或资源生命周期时，阅读 [resource-sync.md](resource-sync.md)；涉及身份、权限、凭证和审计时，阅读 [security.md](security.md)；涉及页面、导航、交互或响应式适配时，阅读 [frontend-guidelines.md](frontend-guidelines.md)。完成跨领域开发或测试前，使用 [acceptance.md](acceptance.md) 按场景核对验收要求，最后参阅 [implementation-status.md](implementation-status.md) 了解指定提交的交付状态和证据。

## 文档职责

| 文件 | 职责 |
| --- | --- |
| [overview.md](overview.md) | 产品定位、目标用户、首期范围、核心业务闭环、平台边界和术语。 |
| [domain-model.md](domain-model.md) | 用户、业务项目、成员、接入源、同步任务、资产、访问端点和审计日志的领域关系及约束。 |
| [resource-sync.md](resource-sync.md) | 接入源配置、同步触发与并发、采集结果、资源生命周期和失败保护。 |
| [security.md](security.md) | 身份认证、授权矩阵、凭证保护、审计和错误披露边界。 |
| [frontend-guidelines.md](frontend-guidelines.md) | 导航、页面职责、列表交互、状态展示、可访问性和中文界面要求。 |
| [acceptance.md](acceptance.md) | 以“前置条件—操作—预期结果”组织跨领域端到端验收场景。 |
| [implementation-status.md](implementation-status.md) | 以指定提交为基线的实现状态、差距和代码或测试证据。 |

跨文档规则只在一个主责文件中完整定义，其他文件通过相对链接引用；具体职责以表中说明为准。

## 维护规则

- 产品规范描述应长期遵守的要求，使用“必须”“不得”表达强制约束，使用“支持”“提供”表达产品能力。
- 除 `implementation-status.md` 外，本目录文件不得把尚未实现的目标能力写成当前实现事实；实现差距统一记录在实现状态文件中。
- 新需求与现有规范冲突时，必须先确认用户口径和 `AGENTS.md` 约束，再更新唯一主责文档及受影响的引用和验收场景。
- 修改规范后必须检查相对链接、文档覆盖范围和关键业务红线；不得通过 README 或代码说明绕过本目录的权威关系。
- 文档中的角色、状态、动作和错误语义使用中文；云平台、资源类型和协议等业界固定名称可保留英文。
