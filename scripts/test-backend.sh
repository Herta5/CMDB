#!/usr/bin/env bash
# 统一执行后端普通测试和 PostgreSQL 17 集成测试；数据库仅属于本次调用。
set -euo pipefail

repository_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
# 显式覆盖外部 Compose 项目名，随机端口与随机项目名允许多个检出并行验证。
project_name="cmdb-backend-test-$$-${RANDOM}-${RANDOM}"
compose=(docker compose --project-name "$project_name" --file "$repository_root/backend/test/postgres/docker-compose.yml")
active_child=""

# 每条可能长期运行的命令拥有独立会话；只终止本次命令及其子进程，不能向调用方进程组发信号。
run_controlled() {
    local result=0
    setsid "$@" &
    active_child=$!
    # Bash 的 wait 内建可被 trap 中断，前台执行外部命令则会延迟处理信号。
    wait "$active_child" || result=$?
    active_child=""
    return "$result"
}

terminate_child() {
    if [[ -n "$active_child" ]]; then
        kill -TERM -- "-$active_child" 2>/dev/null || true
        # 信号可能早于 setsid 建立会话；补发给精确子 PID，避免留下尚未执行的命令。
        kill -TERM "$active_child" 2>/dev/null || true
        # 中断后不等待不协作的测试；数据库清理由入口负责，独立会话中的后代也必须结束。
        kill -KILL -- "-$active_child" 2>/dev/null || true
        kill -KILL "$active_child" 2>/dev/null || true
        wait "$active_child" 2>/dev/null || true
        active_child=""
    fi
}

cleanup() {
    local result=$?
    trap - EXIT INT TERM
    terminate_child
    if ! "${compose[@]}" down --volumes --remove-orphans; then
        echo '清理本次 PostgreSQL 集成测试环境失败。' >&2
        if (( result == 0 )); then result=1; fi
    fi
    exit "$result"
}

command -v docker >/dev/null || { echo '后端集成测试需要 Docker。' >&2; exit 1; }
command -v go >/dev/null || { echo '后端测试需要 Go。' >&2; exit 1; }
command -v setsid >/dev/null || { echo '后端测试需要 setsid 隔离测试进程。' >&2; exit 1; }
docker compose version >/dev/null
# 启动失败或中断也只清理本次项目，禁止触及生产 Compose 和持久卷。
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
run_controlled "${compose[@]}" up --detach --wait --wait-timeout 90 postgresql-test

published_address="$("${compose[@]}" port postgresql-test 5432)"
if [[ ! "$published_address" =~ ^127\.0\.0\.1:([0-9]+)$ ]]; then
    echo '无法获取本次 PostgreSQL 集成测试的回环端口。' >&2
    exit 1
fi
postgres_port="${BASH_REMATCH[1]}"
if (( 10#$postgres_port < 1 || 10#$postgres_port > 65535 )); then
    echo 'PostgreSQL 集成测试端口超出有效范围。' >&2
    exit 1
fi
# 使用明确虚构的隔离凭证，并覆盖调用方 DSN，防止误连其他数据库。
export CMDB_POSTGRES_TEST_DSN="postgres://postgres:cmdb-integration-only@127.0.0.1:${postgres_port}/postgres?sslmode=disable"
cd "$repository_root/backend"
run_controlled go test -tags=postgres -count=1 ./...
