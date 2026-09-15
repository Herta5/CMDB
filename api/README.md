# CMDB 接口契约

`openapi.yaml` 与 `schemas/` 是公开 HTTP 结构的维护入口；`openapi.bundled.yaml` 为可离线使用的生成产物，不直接编辑。产品规则与验收见 [接口规范](../docs/project/api-contract.md)。

## 安装开发依赖

使用项目指定的 Go 1.25.1、Node 20 和 pnpm 9.15.9。Python 仅用于将领域 YAML 引用合并为离线规范，不进入应用运行时。

```bash
python3 -m pip install -r api/requirements.txt
corepack pnpm --dir frontend install --frozen-lockfile
```

Go 生成器由 `backend/go.mod` 的 tool 指令锁定，Orval 由前端锁文件固定。无需全局安装生成器。

## 修改与检查

1. 修改 `openapi.yaml` 中的操作与 `schemas/` 中的公开模型；响应不得直接暴露数据库模型。
2. 执行统一生成入口，再实现 Go 严格接口与前端调用所需的类型变更。
3. 补充真实接口回归和产品验收，连同生成文件一起纳入变更。

```bash
./scripts/generate-api.sh
./scripts/check-api.sh main
./scripts/test-backend.sh
corepack pnpm --dir frontend test
corepack pnpm --dir frontend type-check
corepack pnpm --dir frontend build
```

生成检查要求产物已经纳入 Git，重新生成后不能存在差异或未跟踪文件。传入 `main` 时按当前分支与它的 merge-base 检查兼容性；不传参数仅检查契约和生成产物，并明确报告未执行兼容比较。首次引入契约会明确建立基线，后续不兼容变化由固定版本 oasdiff 拒绝。

Go 的内嵌规范必须保留原始 operationId，不能因 Go 导出命名改变用于授权匹配的标识。认证和项目授权必须先于正文校验；原始凭证重复键、更新凭证保留语义及安全错误由公共 HTTP 边界与领域服务共同验证。

离线工具可以直接导入 `openapi.bundled.yaml`；CI 同时提供该文件的下载产物。项目不默认部署公开在线接口调试器。
