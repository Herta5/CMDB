#!/usr/bin/env sh
# 本脚本由 PostgreSQL 容器首次初始化时执行，创建只能访问 CMDB 数据库的受限应用账号。
set -eu

# 应用密码只能来自部署环境；缺失时立即失败，避免创建无密码或弱隔离的账号。
if [ -z "${DB_PASSWORD:-}" ]; then
	echo "DB_PASSWORD 未设置，无法创建 CMDB 应用账号。" >&2
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
SQL
