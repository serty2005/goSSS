# Docker: сборка и деплой

## 1. Подготовка env

Скопируйте шаблон и заполните значения:

```bash
cp .env.prod.example .env
```

Минимально обязательные переменные:

- `BACKEND_IMAGE`
- `FRONTEND_IMAGE`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `DATABASE_URL`
- `JWT_SECRET`
- `SEEDER_KEY`
- `BASE_URL`
- `SDKEY`

Для сидинга обязательно задайте путь на хосте:

- `SEEDER_MOCK_DATA_PATH=/absolute/path/to/mock_data`

Папка должна содержать файлы мок-данных для сидера.

## 2. Сборка и публикация образов (CI/локально)

```bash
cd docker
docker compose --env-file .env -f docker-compose.build.yml build
docker compose --env-file .env -f docker-compose.build.yml push
```

`BACKEND_IMAGE` и `FRONTEND_IMAGE` должны содержать полный путь и тег в Docker Hub, например:

- `myteam/gosss-backend:1.0.0`
- `myteam/gosss-frontend:1.0.0`

Важно: мок-данные в образ не включаются.

## 3. Запуск production из Docker Hub

```bash
cd docker
docker compose --env-file .env -f docker-compose.prod.yml pull
docker compose --env-file .env -f docker-compose.prod.yml up -d
```

## 4. Однократный сидинг (опционально)

```bash
cd docker
docker compose --env-file .env -f docker-compose.prod.yml --profile seed run --rm init-seeder
```

`init-seeder` получает мок-данные через bind-mount из `SEEDER_MOCK_DATA_PATH`.

## 5. Обновление до нового тега

1. Обновите `BACKEND_IMAGE` и `FRONTEND_IMAGE` в `.env`.
2. Выполните:

```bash
cd docker
docker compose --env-file .env -f docker-compose.prod.yml pull
docker compose --env-file .env -f docker-compose.prod.yml up -d
```