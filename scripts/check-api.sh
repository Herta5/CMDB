#!/usr/bin/env bash
# 拒绝遗漏生成的变更；可指定目标提交，使用真实 merge-base 检查不兼容的公开接口变化。
set -euo pipefail
repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repository_root"
if (( $# > 1 )); then
    echo '用法：scripts/check-api.sh [目标分支或提交]' >&2
    exit 2
fi
./scripts/generate-api.sh
artifacts=(api/openapi.bundled.yaml backend/internal/api/generated/api.gen.go frontend/src/api/generated)
if ! git diff --quiet -- "${artifacts[@]}"; then
    echo '接口生成产物与已记录内容不一致，请重新生成并纳入当前变更。' >&2
    exit 1
fi
if [[ -n "$(git ls-files --others --exclude-standard -- "${artifacts[@]}")" ]]; then
    echo '存在未纳入版本控制的接口生成产物。' >&2
    exit 1
fi
(
    cd backend
    go test ./internal/api/generated -count=1
)
if (( $# == 0 )); then
    echo '契约与生成检查通过；未指定目标提交，本次未执行兼容比较。'
    exit 0
fi
comparison_base="$(git merge-base HEAD "$1")"
if ! git cat-file -e "$comparison_base:api/openapi.yaml" 2>/dev/null; then
    echo '比较基线尚无 OpenAPI，本次建立首个契约基线。'
    exit 0
fi
comparison_dir="$(mktemp -d "${TMPDIR:-/tmp}/cmdb-api-compare.XXXXXX")"
trap 'rm -rf -- "$comparison_dir"' EXIT
# 提取整棵契约目录，让历史版本的相对引用也以历史内容解析。
git archive "$comparison_base" api | tar -x -C "$comparison_dir"
(
    cd backend
    go run github.com/oasdiff/oasdiff@v1.11.7 breaking \
        "$comparison_dir/api/openapi.yaml" "$repository_root/api/openapi.yaml" --fail-on WARN
)
echo '契约、生成产物和接口兼容检查通过。'
