#!/usr/bin/env bash
set -euo pipefail

# Полный релизный сценарий:
# 1) выгрузка пользователей (users, roles, user_roles, user_integrations) из старой БД;
# 2) остановка старого стека;
# 3) подъем нового стека и (опционально) очистка volume БД;
# 4) запуск сидера;
# 5) импорт пользователей в свежую БД;
# 6) запуск сервера и фронтенда.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

if [[ -n "${OLD_COMPOSE_FILE:-}" ]]; then
  OLD_COMPOSE_FILE="${OLD_COMPOSE_FILE}"
elif [[ -f "${SCRIPT_DIR}/docker-compose.prod.yml" ]]; then
  OLD_COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.prod.yml"
elif [[ -f "${SCRIPT_DIR}/docker-compose.yml" ]]; then
  OLD_COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.yml"
else
  OLD_COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.prod.new.yml"
fi

if [[ -n "${NEW_COMPOSE_FILE:-}" ]]; then
  NEW_COMPOSE_FILE="${NEW_COMPOSE_FILE}"
elif [[ -f "${SCRIPT_DIR}/docker-compose.prod.new.yml" ]]; then
  NEW_COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.prod.new.yml"
else
  NEW_COMPOSE_FILE="${SCRIPT_DIR}/docker-compose.prod.yml"
fi
ENV_FILE="${ENV_FILE:-${SCRIPT_DIR}/.env}"
RESET_DB_VOLUME="${RESET_DB_VOLUME:-true}"
EXPORT_DIR="${EXPORT_DIR:-${ROOT_DIR}/tmp/user_migration_$(date +%Y%m%d_%H%M%S)}"

POSTGRES_USER="${POSTGRES_USER:-}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-}"
POSTGRES_DB="${POSTGRES_DB:-}"

if [[ ! -f "${ENV_FILE}" ]]; then
  echo "Ошибка: env-файл не найден: ${ENV_FILE}"
  exit 1
fi

if [[ ! -f "${OLD_COMPOSE_FILE}" ]]; then
  echo "Ошибка: старый compose-файл не найден: ${OLD_COMPOSE_FILE}"
  exit 1
fi

if [[ ! -f "${NEW_COMPOSE_FILE}" ]]; then
  echo "Ошибка: новый compose-файл не найден: ${NEW_COMPOSE_FILE}"
  exit 1
fi

set -a
source "${ENV_FILE}"
set +a

if [[ -z "${POSTGRES_USER}" || -z "${POSTGRES_PASSWORD}" || -z "${POSTGRES_DB}" ]]; then
  echo "Ошибка: в ${ENV_FILE} должны быть заданы POSTGRES_USER, POSTGRES_PASSWORD, POSTGRES_DB"
  exit 1
fi

mkdir -p "${EXPORT_DIR}"

dc_old() {
  docker compose --env-file "${ENV_FILE}" -f "${OLD_COMPOSE_FILE}" "$@"
}

dc_new() {
  docker compose --env-file "${ENV_FILE}" -f "${NEW_COMPOSE_FILE}" "$@"
}

has_service_new() {
  local service="$1"
  dc_new config --services | grep -qx "${service}"
}

wait_db_old() {
  echo "Ожидание готовности БД в старом стеке..."
  dc_old up -d db >/dev/null
  for _ in $(seq 1 60); do
    if dc_old exec -T -e PGPASSWORD="${POSTGRES_PASSWORD}" db \
      pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; then
      echo "Старая БД готова."
      return 0
    fi
    sleep 2
  done
  echo "Ошибка: старая БД не стала готовой вовремя."
  exit 1
}

wait_db_new() {
  echo "Ожидание готовности БД в новом стеке..."
  for _ in $(seq 1 90); do
    if dc_new exec -T -e PGPASSWORD="${POSTGRES_PASSWORD}" db \
      pg_isready -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" >/dev/null 2>&1; then
      echo "Новая БД готова."
      return 0
    fi
    sleep 2
  done
  echo "Ошибка: новая БД не стала готовой вовремя."
  exit 1
}

export_csv() {
  local name="$1"
  local sql="$2"
  local out="${EXPORT_DIR}/${name}.csv"
  echo "Выгрузка ${name} -> ${out}"
  dc_old exec -T -e PGPASSWORD="${POSTGRES_PASSWORD}" db \
    psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" \
    -v ON_ERROR_STOP=1 -c "COPY (${sql}) TO STDOUT WITH (FORMAT CSV, HEADER true, ENCODING 'UTF8')" > "${out}"
}

import_from_csv_in_new_db() {
  local container_path="/tmp/user_migration"
  local db_container_id
  db_container_id="$(dc_new ps -q db)"
  if [[ -z "${db_container_id}" ]]; then
    echo "Ошибка: не удалось определить контейнер db нового стека."
    exit 1
  fi

  echo "Копирование CSV в контейнер новой БД..."
  docker cp "${EXPORT_DIR}" "${db_container_id}:${container_path}"

  echo "Импорт данных пользователей в новую БД..."
  dc_new exec -T -e PGPASSWORD="${POSTGRES_PASSWORD}" db \
    psql -U "${POSTGRES_USER}" -d "${POSTGRES_DB}" -v ON_ERROR_STOP=1 <<SQL
BEGIN;

CREATE TEMP TABLE tmp_users (
  username TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  full_name TEXT,
  first_name VARCHAR(100),
  last_name VARCHAR(100),
  position VARCHAR(50),
  external_id VARCHAR(128),
  external_type VARCHAR(50),
  schedule_type VARCHAR(10),
  has_logged_in BOOLEAN,
  profile_config JSONB,
  email VARCHAR(255),
  phone VARCHAR(50),
  department VARCHAR(100),
  is_active BOOLEAN,
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ
);

CREATE TEMP TABLE tmp_roles (
  name VARCHAR(50) NOT NULL,
  description TEXT
);

CREATE TEMP TABLE tmp_user_roles (
  username TEXT NOT NULL,
  role_name VARCHAR(50) NOT NULL
);

CREATE TEMP TABLE tmp_user_integrations (
  username TEXT NOT NULL,
  integration_type VARCHAR(50) NOT NULL,
  external_id VARCHAR(255) NOT NULL,
  is_verified BOOLEAN,
  is_locked BOOLEAN,
  verified_name VARCHAR(255),
  created_at TIMESTAMPTZ,
  updated_at TIMESTAMPTZ
);

\copy tmp_users FROM '${container_path}/users.csv' WITH (FORMAT CSV, HEADER true, ENCODING 'UTF8');
\copy tmp_roles FROM '${container_path}/roles.csv' WITH (FORMAT CSV, HEADER true, ENCODING 'UTF8');
\copy tmp_user_roles FROM '${container_path}/user_roles.csv' WITH (FORMAT CSV, HEADER true, ENCODING 'UTF8');
\copy tmp_user_integrations FROM '${container_path}/user_integrations.csv' WITH (FORMAT CSV, HEADER true, ENCODING 'UTF8');

INSERT INTO roles (name, description, created_at, updated_at)
SELECT tr.name, tr.description, NOW(), NOW()
FROM tmp_roles tr
WHERE tr.name IS NOT NULL
ON CONFLICT (name) DO UPDATE
SET description = EXCLUDED.description,
    updated_at = NOW();

INSERT INTO users (
  username,
  password_hash,
  full_name,
  first_name,
  last_name,
  position,
  external_id,
  external_type,
  schedule_type,
  has_logged_in,
  profile_config,
  email,
  phone,
  department,
  is_active,
  created_at,
  updated_at,
  deleted_at
)
SELECT
  tu.username,
  tu.password_hash,
  tu.full_name,
  COALESCE(tu.first_name, ''),
  COALESCE(tu.last_name, ''),
  COALESCE(tu.position, 'intern'),
  tu.external_id,
  tu.external_type,
  COALESCE(tu.schedule_type, '5/2'),
  COALESCE(tu.has_logged_in, false),
  COALESCE(tu.profile_config, '{}'::jsonb),
  tu.email,
  tu.phone,
  COALESCE(tu.department, ''),
  COALESCE(tu.is_active, true),
  COALESCE(tu.created_at, NOW()),
  COALESCE(tu.updated_at, NOW()),
  NULL
FROM tmp_users tu
ON CONFLICT (username) DO UPDATE
SET
  password_hash = EXCLUDED.password_hash,
  full_name = EXCLUDED.full_name,
  first_name = EXCLUDED.first_name,
  last_name = EXCLUDED.last_name,
  position = EXCLUDED.position,
  external_id = EXCLUDED.external_id,
  external_type = EXCLUDED.external_type,
  schedule_type = EXCLUDED.schedule_type,
  has_logged_in = EXCLUDED.has_logged_in,
  profile_config = EXCLUDED.profile_config,
  email = EXCLUDED.email,
  phone = EXCLUDED.phone,
  department = EXCLUDED.department,
  is_active = EXCLUDED.is_active,
  updated_at = EXCLUDED.updated_at,
  deleted_at = NULL;

DELETE FROM user_roles ur
USING users u
JOIN tmp_users tu ON tu.username = u.username
WHERE ur.user_id = u.id;

INSERT INTO user_roles (user_id, role_id)
SELECT DISTINCT u.id, r.id
FROM tmp_user_roles tur
JOIN users u ON u.username = tur.username
JOIN roles r ON r.name = tur.role_name
WHERE tur.role_name <> ''
ON CONFLICT DO NOTHING;

DELETE FROM user_integrations ui
USING users u
JOIN tmp_users tu ON tu.username = u.username
WHERE ui.user_id = u.id;

INSERT INTO user_integrations (
  user_id,
  integration_type,
  external_id,
  is_verified,
  is_locked,
  verified_name,
  created_at,
  updated_at
)
SELECT
  u.id,
  tui.integration_type,
  tui.external_id,
  COALESCE(tui.is_verified, false),
  COALESCE(tui.is_locked, false),
  tui.verified_name,
  COALESCE(tui.created_at, NOW()),
  COALESCE(tui.updated_at, NOW())
FROM tmp_user_integrations tui
JOIN users u ON u.username = tui.username;

COMMIT;
SQL
}

echo "Шаг 1/7: Проверка и запуск старой БД"
wait_db_old

echo "Шаг 2/7: Выгрузка пользователей из старой БД"
export_csv "users" "
  SELECT
    u.username,
    u.password_hash,
    u.full_name,
    u.first_name,
    u.last_name,
    u.position,
    u.external_id,
    u.external_type,
    u.schedule_type,
    u.has_logged_in,
    u.profile_config,
    u.email,
    u.phone,
    u.department,
    u.is_active,
    u.created_at,
    u.updated_at
  FROM users u
  WHERE u.deleted_at IS NULL
  ORDER BY u.id
"
export_csv "roles" "
  SELECT DISTINCT
    r.name,
    r.description
  FROM roles r
  JOIN user_roles ur ON ur.role_id = r.id
  JOIN users u ON u.id = ur.user_id
  WHERE u.deleted_at IS NULL
  ORDER BY r.name
"
export_csv "user_roles" "
  SELECT
    u.username,
    r.name AS role_name
  FROM user_roles ur
  JOIN users u ON u.id = ur.user_id
  JOIN roles r ON r.id = ur.role_id
  WHERE u.deleted_at IS NULL
  ORDER BY u.username, r.name
"
export_csv "user_integrations" "
  SELECT
    u.username,
    ui.integration_type,
    ui.external_id,
    ui.is_verified,
    ui.is_locked,
    ui.verified_name,
    ui.created_at,
    ui.updated_at
  FROM user_integrations ui
  JOIN users u ON u.id = ui.user_id
  WHERE u.deleted_at IS NULL
  ORDER BY u.username, ui.integration_type, ui.external_id
"

echo "Шаг 3/7: Остановка старого стека"
dc_old down --remove-orphans

echo "Шаг 4/7: Подготовка нового стека"
if [[ "${RESET_DB_VOLUME}" == "true" ]]; then
  echo "Очистка volume БД (RESET_DB_VOLUME=true)"
  dc_new down -v --remove-orphans || true
else
  echo "Очистка volume БД отключена (RESET_DB_VOLUME=false)"
  dc_new down --remove-orphans || true
fi

dc_new pull

if has_service_new redis; then
  dc_new up -d db redis
else
  dc_new up -d db
fi
wait_db_new

echo "Шаг 5/7: Сидирование новой БД"
dc_new --profile seed run --rm init-seeder

echo "Шаг 6/7: Импорт пользователей в новую БД"
import_from_csv_in_new_db

echo "Шаг 7/7: Запуск приложений"
dc_new up -d

echo "Готово. CSV с выгрузкой сохранены в: ${EXPORT_DIR}"
