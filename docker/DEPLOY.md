# Docker: сборка, деплой и релизы адаптеров

## 1. Что теперь поднимается в production

Production compose `docker/docker-compose.prod.new.yml` поднимает:

- `db`
- `redis`
- `server`
- `frontend`
- `minio`
- `minio-init`
- опциональный reference `agents-proxy`

`minio` не публикует S3 API наружу. Публичная раздача бинарников должна идти через доменный путь `/agents/`.

## 2. Подготовка `.env`

Скопируйте шаблон и заполните значения:

```bash
cd docker
cp .env.prod.example .env
```

Минимально важные переменные:

- `BACKEND_IMAGE`
- `FRONTEND_IMAGE`
- `POSTGRES_USER`
- `POSTGRES_PASSWORD`
- `POSTGRES_DB`
- `DATABASE_URL`
- `JWT_SECRET`
- `SEEDER_KEY`
- `MINIO_ROOT_USER`
- `MINIO_ROOT_PASSWORD`
- `AGENT_ADAPTER_S3_ENABLED=true`
- `AGENT_ADAPTER_S3_ENDPOINT=http://minio:9000`
- `AGENT_ADAPTER_S3_BUCKET=agents`
- `AGENT_ADAPTER_S3_ACCESS_KEY`
- `AGENT_ADAPTER_S3_SECRET_KEY`
- `AGENT_ADAPTER_PUBLIC_BASE_URL=https://etalon.serty.top/agents`
- `AGENT_ADAPTER_CATALOG_KEY=catalog/index.json`
- `AGENT_ADAPTER_SYNC_INTERVAL_MIN`
- `AGENT_ADAPTER_DEFAULT_CHANNEL=stable`

Если используете сидер, дополнительно задайте:

- `SEEDER_MOCK_DATA_PATH=/absolute/path/to/mock_data`

Важно:

- в production `AGENT_ADAPTER_S3_ACCESS_KEY` и `AGENT_ADAPTER_S3_SECRET_KEY` обычно совпадают с `MINIO_ROOT_USER` и `MINIO_ROOT_PASSWORD`, если не заведён отдельный MinIO-пользователь;
- demo-seed каталога применяется только когда S3-контур отключён и таблицы релизов пустые;
- `AGENT_ADAPTER_PUBLIC_BASE_URL` должен указывать именно на публичный `/agents/`, а не на внутренний `http://minio:9000`.

## 3. Сборка и публикация образов

```bash
cd docker
docker compose --env-file .env -f docker-compose.build.yml build
docker compose --env-file .env -f docker-compose.build.yml push
```

`BACKEND_IMAGE` и `FRONTEND_IMAGE` должны содержать полный путь и тег реестра.

## 4. Запуск production

Базовый запуск:

```bash
cd docker
docker compose --env-file .env -f docker-compose.prod.new.yml pull
docker compose --env-file .env -f docker-compose.prod.new.yml up -d
```

Запуск с reference reverse-proxy для `/agents/`:

```bash
cd docker
docker compose --env-file .env -f docker-compose.prod.new.yml --profile agents-proxy up -d
```

После старта:

- `minio-init` создаёт bucket `agents`;
- `minio-init` включает анонимное чтение объектов bucket для download-пути через proxy;
- сервер синхронизирует `catalog/index.json` в локальную БД по расписанию;
- heartbeat дальше работает только с БД и не зависит от live-S3 на каждый запрос.

## 5. Reverse proxy для `https://etalon.serty.top/agents/...`

Боевой reverse-proxy обычно живёт вне этого репозитория. В репозитории добавлен reference-конфиг:

- `docker/docs/nginx-agents-proxy.conf`

Его можно:

- использовать как отдельный контейнер `agents-proxy`;
- перенести в внешний Nginx/Traefik/Ingress, сохранив маршрут `/agents/ -> http://minio:9000/agents/...`.

Рекомендация по кешированию:

- versioned release URL под `/agents/adapters/<adapter_id>/releases/...` кешировать как immutable;
- `catalog/index.json` и `channels/*.json` не кешировать долго, потому что это mutable pointers.

## 6. Первичная публикация адаптера

Публикация выполняется CLI из репозитория:

```bash
go run ./cmd/adapter-publisher publish \
  --file C:\build\fiscal-atol.exe \
  --adapter-id fiscal-atol \
  --version 1.2.3 \
  --title "Фискальный адаптер АТОЛ" \
  --description "Windows release" \
  --target-os windows \
  --target-arch amd64 \
  --promote latest \
  --promote stable
```

CLI делает атомарную последовательность:

1. Загружает бинарник в `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/`.
2. Считает и публикует `sha256.txt`.
3. Генерирует и публикует `release.json`.
4. Обновляет `catalog/index.json`.
5. Только после этого переключает `stable/latest`.

Если публикация оборвалась до обновления channel pointer, новый релиз не станет активным для heartbeat.

## 7. Ручной refresh после publish/promote

Фоновый sync работает по `AGENT_ADAPTER_SYNC_INTERVAL_MIN`, но сервер также умеет ручной refresh:

```text
POST /agent-diagnostics/adapters/refresh
```

Практический сценарий после публикации:

1. Выполнить `publish` или `promote`.
2. Вызвать ручной refresh.
3. Убедиться, что в operator UI появились актуальные `stable/latest`.
4. Следующий heartbeat уже выдаст агенту manifest для `stable`.

## 8. Rollback

Rollback делается без перезаливки бинарника:

```bash
go run ./cmd/adapter-publisher promote \
  --adapter-id fiscal-atol \
  --version 1.2.2 \
  --target-os windows \
  --target-arch amd64 \
  --channel stable
```

После этого достаточно выполнить refresh каталога. Операторский UX не меняется: он по-прежнему отмечает только `adapter_id`.

## 9. CI/CD основа

В репозитории добавлен базовый manual workflow:

- `.github/workflows/adapter-release-publish.yml`

Он умеет:

- публиковать новый релиз через `adapter-publisher`;
- переключать каналы `stable/latest` для rollback и promote;
- использовать стандартные S3 env/secrets без привязки к конкретному провайдеру.

Шаг сборки конкретного адаптера intentionally не зашит в workflow, потому что команды сборки зависят от самого адаптера. Его нужно добавить в этот workflow перед вызовом `adapter-publisher`.
