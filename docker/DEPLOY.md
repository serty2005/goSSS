# Docker: сборка, деплой и релизы адаптеров

## 1. Что теперь поднимается в production

Для текущего production-стэка с внешним `traefik` используйте `docker/docker-compose.2403.yml`.

Он поднимает:

- `traefik`
- `dozzle`
- `db`
- `redis`
- `server`
- `frontend`
- `minio`
- `minio-init`

`traefik` продолжает маршрутизировать весь трафик стэка:

- `https://<domain>/` -> `frontend`
- `https://<domain>/logs` -> `dozzle`
- `https://<domain>/agents/...` -> `minio:9000`
- `https://minio.<domain>/` -> `minio:9001`

Консоль MinIO в production лучше публиковать на отдельном host, а не под path-prefix.

Reference compose `docker/docker-compose.prod.new.yml` по-прежнему доступен как упрощённый standalone-вариант без встроенного `traefik`.
`minio` не нужно публиковать наружу через `ports`. Публичная раздача бинарников должна идти только через доменный путь `/agents/`.

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
- `AGENT_API_KEY`
- `SEEDER_KEY`
- `MINIO_ROOT_USER`
- `MINIO_ROOT_PASSWORD`
- `MINIO_CONSOLE_DOMAIN=minio.<domain>`
- `MINIO_BROWSER_REDIRECT_URL=https://minio.<domain>/`
- `REDIS_ADDR=redis:6379`
- `AGENT_ADAPTER_S3_ENABLED=true`
- `AGENT_ADAPTER_S3_ENDPOINT=http://minio:9000`
- `AGENT_ADAPTER_S3_BUCKET=agents`
- `AGENT_ADAPTER_S3_ACCESS_KEY`
- `AGENT_ADAPTER_S3_SECRET_KEY`
- `AGENT_ADAPTER_PUBLIC_BASE_URL=https://<domain>/agents`
- `AGENT_ADAPTER_CATALOG_KEY=catalog/index.json`
- `AGENT_ADAPTER_SYNC_INTERVAL_MIN`
- `AGENT_ADAPTER_DEFAULT_CHANNEL=stable`

Для MinIO-контейнеров в production не используйте `latest`.
В шаблоне по умолчанию зафиксированы CPU-совместимые теги `*-cpuv1`, потому что на старых `amd64`-хостах `minio/mc:latest` может завершаться с ошибкой `Fatal glibc error: CPU does not support x86-64-v2`.

Если используете сидер, дополнительно задайте:

- `SEEDER_MOCK_DATA_PATH=/absolute/path/to/mock_data`

Важно:

- в production `AGENT_ADAPTER_S3_ACCESS_KEY` и `AGENT_ADAPTER_S3_SECRET_KEY` обычно совпадают с `MINIO_ROOT_USER` и `MINIO_ROOT_PASSWORD`, если не заведён отдельный MinIO-пользователь;
- demo-seed каталога применяется только когда S3-контур отключён и таблицы релизов пустые;
- `AGENT_ADAPTER_PUBLIC_BASE_URL` должен указывать именно на публичный `/agents/`, а не на внутренний `http://minio:9000`.
- `MINIO_CONSOLE_DOMAIN` должен резолвиться на тот же production ingress, что и основной домен;
- `MINIO_BROWSER_REDIRECT_URL` должен совпадать с внешним admin URL консоли и заканчиваться `/`, например `https://minio.sd.myhoreca.io/`.

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
docker compose --env-file .env -f docker-compose.2403.yml pull
docker compose --env-file .env -f docker-compose.2403.yml up -d
```

После старта:

- `minio-init` создаёт bucket `agents`;
- `minio-init` включает анонимное чтение объектов bucket для download-пути через `traefik`;
- сервер синхронизирует `catalog/index.json` в локальную БД по расписанию;
- heartbeat дальше работает только с БД и не зависит от live-S3 на каждый запрос.
- `minio-init` больше не зависит от bind-mounted файла `./docs/minio-init.sh`, поэтому запуск не ломается, если production compose лежит не рядом с директорией `docs`.

## 5. Reverse proxy для `/agents` и консоли MinIO

Для production с `docker-compose.2403.yml` отдельный `agents-proxy` не нужен.

Используется текущий `traefik`:

- маршрут `/agents` проксирует публичную раздачу бинарников и каталогов из bucket `agents`;
- отдельный host `minio.<domain>` открывает встроенную консоль MinIO;
- `AGENT_ADAPTER_S3_ENDPOINT` при этом остаётся внутренним `http://minio:9000`.

Важно:

- MinIO S3 API не нужно публиковать наружу как отдельный порт;
- path `/agents` используется только как публичный download URL для агентов;
- консоль MinIO не стоит держать под path-prefix вроде `/minio`, потому что в этой схеме возможны проблемы с auth/session и внутренними запросами UI за reverse proxy;
- publish/promote CLI и серверный sync должны работать по внутреннему endpoint `http://minio:9000`.

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
POST /api/agent-diagnostics/adapters/refresh
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
