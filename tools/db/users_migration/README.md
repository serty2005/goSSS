# Перенос пользователей в новую БД

Набор скриптов переносит:
- пользователей (`users`, без soft-deleted записей);
- роли пользователей (`roles`, только задействованные в переносимых пользователях);
- связи пользователь-роль (`user_roles`);
- интеграции пользователей (`user_integrations`).

## 1. Выгрузка из текущей (старой) БД

```bash
export SOURCE_DATABASE_URL="postgres://user:pass@host:5432/dbname?sslmode=disable"
export EXPORT_DIR="./tmp/user_migration_2026-02-17"
bash tools/db/users_migration/export_users.sh
```

После выполнения будут созданы файлы:
- `users.csv`
- `roles.csv`
- `user_roles.csv`
- `user_integrations.csv`

## 2. Загрузка в новую (чистую) БД после деплоя

Важно:
- сначала должен быть выполнен деплой и миграции схемы;
- затем импорт.

```bash
export TARGET_DATABASE_URL="postgres://user:pass@host:5432/new_db?sslmode=disable"
export IMPORT_DIR="./tmp/user_migration_2026-02-17"
bash tools/db/users_migration/import_users.sh
```

## Поведение импорта

- `users`: upsert по `username` (обновляются логин/хэш/поля профиля, `deleted_at` снимается).
- `roles`: upsert по `name`.
- `user_roles`: связи для импортируемых пользователей пересобираются заново.
- `user_integrations`: интеграции для импортируемых пользователей пересоздаются заново.

Это делает импорт идемпотентным: скрипт можно безопасно запускать повторно на одном и том же наборе CSV.

## Автоматический сценарий релиза

Для полного цикла (выгрузка -> деплой -> сидер -> импорт -> запуск сервисов) используйте:

```bash
bash docker/release_with_user_migration.sh
```

Параметры через переменные окружения:
- `ENV_FILE` (по умолчанию `docker/.env`)
- `OLD_COMPOSE_FILE` (по умолчанию `docker/docker-compose.prod.yml`)
- `NEW_COMPOSE_FILE` (по умолчанию `docker/docker-compose.prod.yml`)
- `RESET_DB_VOLUME` (`true`/`false`, по умолчанию `true`)
- `EXPORT_DIR` (путь для CSV)

Пример перехода со старого compose на новый:

```bash
export OLD_COMPOSE_FILE="docker/docker-compose.prod.old.yml"
export NEW_COMPOSE_FILE="docker/docker-compose.prod.yml"
export ENV_FILE="docker/.env"
export RESET_DB_VOLUME=true
bash docker/release_with_user_migration.sh
```
