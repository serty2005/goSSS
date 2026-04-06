#!/usr/bin/env bash
set -euo pipefail

ENV_FILE="${ENV_FILE:-docker/.env}"
COMPOSE_FILE="${COMPOSE_FILE:-docker/docker-compose.yml}"
DB_SERVICE="${DB_SERVICE:-db}"
BACKUP_ROOT="${BACKUP_ROOT:-./tmp/db_backups}"
BACKUP_NAME="${BACKUP_NAME:-release-$(date -u +%Y%m%d-%H%M%S)}"
BACKUP_DIR="${BACKUP_ROOT%/}/${BACKUP_NAME}"

if ! command -v docker >/dev/null 2>&1; then
  echo "Не найден docker в PATH" >&2
  exit 1
fi

mkdir -p "$BACKUP_DIR"
chmod 700 "$BACKUP_DIR"

DUMP_PATH="${BACKUP_DIR}/postgres.dump"
ENV_SNAPSHOT_PATH="${BACKUP_DIR}/env.snapshot"
COMPOSE_SNAPSHOT_PATH="${BACKUP_DIR}/compose.snapshot.yml"
METADATA_PATH="${BACKUP_DIR}/metadata.txt"

echo "Создаю backup PostgreSQL в ${BACKUP_DIR}"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" config > "$COMPOSE_SNAPSHOT_PATH"
cp "$ENV_FILE" "$ENV_SNAPSHOT_PATH"

docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE" exec -T "$DB_SERVICE" sh -lc '
  set -eu
  export PGPASSWORD="${POSTGRES_PASSWORD}"
  pg_dump \
    -U "${POSTGRES_USER}" \
    -d "${POSTGRES_DB}" \
    -Fc \
    --no-owner \
    --no-privileges
' > "$DUMP_PATH"

if command -v sha256sum >/dev/null 2>&1; then
  sha256sum "$DUMP_PATH" > "${DUMP_PATH}.sha256"
fi

{
  echo "created_at_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "env_file=${ENV_FILE}"
  echo "compose_file=${COMPOSE_FILE}"
  echo "db_service=${DB_SERVICE}"
  echo "dump_file=$(basename "$DUMP_PATH")"
} > "$METADATA_PATH"

echo "Backup успешно создан:"
echo "  dump: ${DUMP_PATH}"
echo "  env snapshot: ${ENV_SNAPSHOT_PATH}"
echo "  compose snapshot: ${COMPOSE_SNAPSHOT_PATH}"
echo "  metadata: ${METADATA_PATH}"
