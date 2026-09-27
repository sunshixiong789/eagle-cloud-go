#!/bin/sh
# 为本地开发的每个服务建立独立 database。
#
# 本地开发让 Keycloak 与业务共用一个 PostgreSQL 实例以省一个容器，
# 但数据所有权必须隔离。生产可复用 PostgreSQL 集群，但每个服务仍应使用
# 独立 database 与账号，禁止跨库查询。
set -e

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    SELECT format('CREATE DATABASE %I OWNER %I', name, '$POSTGRES_USER')
    FROM (VALUES
        ('keycloak'), ('eagle_admin'), ('eagle_product'), ('eagle_order')
    ) AS databases(name)
    WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = databases.name)\gexec
EOSQL
