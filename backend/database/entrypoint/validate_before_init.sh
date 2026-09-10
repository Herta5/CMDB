#!/usr/bin/env sh
# 本脚本在 PostgreSQL 官方入口前校验 CMDB 数据库凭证，避免错误配置初始化不可复用的数据卷。
set -eu

# 管理员密码是 PostgreSQL 初始化的必要条件，缺失时必须在创建 PGDATA 前中止且不输出凭证。
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
	echo "POSTGRES_PASSWORD 未设置，无法初始化 CMDB PostgreSQL 数据库。" >&2
	exit 1
fi

# 应用账号密码由初始化脚本消费，提前校验可防止 initdb 完成后才留下半初始化卷。
if [ -z "${DB_PASSWORD:-}" ]; then
	echo "DB_PASSWORD 未设置，无法初始化 CMDB PostgreSQL 数据库。" >&2
	exit 1
fi

# 两类密码相同会使应用凭证等同于管理员权限，必须在 PostgreSQL 创建任何数据前拒绝启动。
if [ "$POSTGRES_PASSWORD" = "$DB_PASSWORD" ]; then
	echo "POSTGRES_PASSWORD 与 DB_PASSWORD 不得相同。" >&2
	exit 1
fi

# 校验完成后委托官方入口处理 initdb、初始化脚本和最终 PostgreSQL 进程。
exec /usr/local/bin/docker-entrypoint.sh "$@"
