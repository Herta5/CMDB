#!/usr/bin/env bash
# 同一入口生成离线契约、Go 严格接口和前端调用；工具版本由各自依赖文件锁定。
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
python3 api/bundle.py
(
    cd backend
    go tool oapi-codegen --config oapi-codegen.yaml ../api/openapi.bundled.yaml
)
(
    cd frontend
    corepack pnpm generate:api
)
