#!/bin/bash
set -e

# Читаем переменные из окружения контейнера (они прокидываются из .env)
# POSTGRES_DB - основная БД
# MCP_DB_USER - имя пользователя для MCP
# MCP_DB_PASS - пароль для MCP

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    -- 1. Создаем пользователя, если его нет
    DO
    \$do\$
    BEGIN
       IF NOT EXISTS (
          SELECT FROM pg_catalog.pg_roles
          WHERE  rolname = '$MCP_DB_USER') THEN

          CREATE ROLE $MCP_DB_USER LOGIN PASSWORD '$MCP_DB_PASSWORD';
       END IF;
    END
    \$do\$;

    -- 2. Даем права на подключение к БД
    GRANT CONNECT ON DATABASE $POSTGRES_DB TO $MCP_DB_USER;

    -- 3. Переключаемся в контекст схемы public
    \c $POSTGRES_DB

    GRANT USAGE ON SCHEMA public TO $MCP_DB_USER;

    -- 4. Даем права на чтение ВСЕХ СУЩЕСТВУЮЩИХ таблиц
    GRANT SELECT ON ALL TABLES IN SCHEMA public TO $MCP_DB_USER;

    -- 5. !!! ВАЖНО !!! Даем права на чтение БУДУЩИХ таблиц (которые создаст GORM)
    -- Это нужно, чтобы пользователь видел таблицы, созданные мигратором (обычно он работает от superuser или владельца БД)
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO $MCP_DB_USER;

    -- На всякий случай даем права на sequences (если вдруг захочется посмотреть last_value)
    GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO $MCP_DB_USER;
    ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON SEQUENCES TO $MCP_DB_USER;
EOSQL

echo "MCP Read-Only user '$MCP_DB_USER' created and configured."