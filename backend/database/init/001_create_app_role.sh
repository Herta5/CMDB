#!/usr/bin/env sh
# 本脚本由 PostgreSQL 容器首次初始化时执行，创建只能访问 CMDB 数据库的受限应用账号。
set -eu

# 初始化对象必须由固定管理员拥有，禁止通过修改容器管理员用户名改变对象归属边界。
if [ "${POSTGRES_USER:-}" != "postgres" ]; then
	echo "POSTGRES_USER 必须为 postgres，才能保证 CMDB 对象由管理员拥有。" >&2
	exit 1
fi

# 两类密码必须同时存在且不同，避免应用凭证泄露时等同于泄露数据库管理员权限。
if [ -z "${POSTGRES_PASSWORD:-}" ]; then
	echo "POSTGRES_PASSWORD 未设置，无法验证 CMDB 管理员与应用账号隔离。" >&2
	exit 1
fi

if [ -z "${DB_PASSWORD:-}" ]; then
	echo "DB_PASSWORD 未设置，无法创建 CMDB 应用账号。" >&2
	exit 1
fi

if [ "$POSTGRES_PASSWORD" = "$DB_PASSWORD" ]; then
	echo "POSTGRES_PASSWORD 与 DB_PASSWORD 不得相同，无法创建 CMDB 应用账号。" >&2
	exit 1
fi

# 密码通过 psql 变量传入并由 :'app_password' 安全转义，脚本不输出密码或完整连接信息。
psql \
	--username "$POSTGRES_USER" \
	--dbname "$POSTGRES_DB" \
	--set=ON_ERROR_STOP=1 \
	--set=app_password="$DB_PASSWORD" \
	--set=db_name="$POSTGRES_DB" <<'SQL'
-- 管理员账号仅用于初始化；cmdb 禁止提权、建库、建角色与复制权限。
CREATE ROLE cmdb LOGIN PASSWORD :'app_password'
  NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION NOBYPASSRLS;

-- 去除默认 PUBLIC 的连接和临时表权限，仅向 cmdb 授权当前 CMDB 数据库连接。
REVOKE ALL ON DATABASE :"db_name" FROM PUBLIC;
GRANT CONNECT ON DATABASE :"db_name" TO cmdb;

-- PostgreSQL 新角色默认继承 PUBLIC；撤销其他可连接数据库的 PUBLIC 权限以隔离 cmdb。
SELECT format('REVOKE ALL ON DATABASE %I FROM PUBLIC;', datname)
FROM pg_database
WHERE datallowconn AND datname <> :'db_name'
\gexec
SQL
