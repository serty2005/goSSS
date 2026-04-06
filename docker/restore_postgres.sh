#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${ENV_FILE:-docker/.env}"
COMPOSE_FILE="${COMPOSE_FILE:-docker/docker-compose.yml}"
DB_SERVICE="${DB_SERVICE:-db}"
APP_SERVICES="${APP_SERVICES:-server frontend}"
STOP_APP_SERVICES="${STOP_APP_SERVICES:-true}"
START_APP_SERVICES="${START_APP_SERVICES:-false}"
BACKUP_PATH="${1:-}"

if [[ -z "$BACKUP_PATH" ]]; then
  echo "Использование: bash docker/restore_postgres.sh <путь_к_postgres.dump>" >&2
  exit 1
fi

if [[ ! -f "$BACKUP_PATH" ]]; then
  echo "Файл backup не найден: ${BACKUP_PATH}" >&2
  exit 1
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Не найден docker в PATH" >&2
  exit 1
fi

echo "Проверяю, что контейнер БД запущен"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d "$DB_SERVICE"

if [[ "$STOP_APP_SERVICES" == "true" ]] && [[ -n "${APP_SERVICES// }" ]]; then
  echo "Останавливаю сервисы приложения перед восстановлением: ${APP_SERVICES}"
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" stop $APP_SERVICES
fi

echo "Пересоздаю целевую базу и завершаю активные подключения"
docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T "$DB_SERVICE" sh -lc '
  set -eu
  export PGPASSWORD="${POSTGRES_PASSWORD}"
  psql -U "${POSTGRES_USER}" -d postgres -v ON_ERROR_STOP=1 <<SQL
SELECT pg_terminate_backend(pid)
FROM pg_stat_activity
WHERE datname = '\''${POSTGRES_DB}'\''
  AND pid <> pg_backend_pid();
DROP DATABASE IF EXISTS "${POSTGRES_DB}";
CREATE DATABASE "${POSTGRES_DB}";
SQL
'

echo "Восстанавливаю базу из ${BACKUP_PATH}"
cat "$BACKUP_PATH" | docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T "$DB_SERVICE" sh -lc '
  set -eu
  export PGPASSWORD="${POSTGRES_PASSWORD}"
  pg_restore \
    -U "${POSTGRES_USER}" \
    -d "${POSTGRES_DB}" \
    --no-owner \
    --no-privileges
'

if [[ "$START_APP_SERVICES" == "true" ]] && [[ -n "${APP_SERVICES// }" ]]; then
  echo "Поднимаю сервисы приложения после восстановления: ${APP_SERVICES}"
  docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" up -d $APP_SERVICES
fi

echo "Восстановление PostgreSQL завершено"
