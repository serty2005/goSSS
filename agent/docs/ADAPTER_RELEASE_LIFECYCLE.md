# Release lifecycle адаптеров

## Цель

Релизы адаптеров управляются server-side через S3/MinIO, а оператор продолжает работать только с `adapter_id`. Версия, бинарник, checksum и channel pointers больше не выбираются вручную в UI.

## Source of truth

Единственный source of truth для published releases:

- bucket `agents` в S3/MinIO
- `catalog/index.json`
- versioned `release.json`
- channel pointers `stable.json` и `latest.json`

Локальная БД сервера хранит синхронизированную проекцию этого каталога для runtime:

- `AgentAdapterRelease`
- `AgentAdapterChannel`

Heartbeat работает только с этой локальной проекцией.

## Layout

Структура объектов:

- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/<file_name>`
- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/release.json`
- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/sha256.txt`
- `adapters/<adapter_id>/channels/stable.json`
- `adapters/<adapter_id>/channels/latest.json`
- `catalog/index.json`

## Модели

`AgentAdapterRelease` хранит:

- `adapter_id`
- `version`
- `title`
- `description`
- `adapter_type`
- `target_os`
- `target_arch`
- `protocol_version`
- `file_name`
- `download_url`
- `sha256`
- `source_key`
- `published`
- `created_at`
- `updated_at`

Уникальность:

- `adapter_id + version + target_os + target_arch`

`AgentAdapterChannel` хранит:

- `adapter_id`
- `channel`
- `release_id`
- `updated_at`

Уникальность:

- `adapter_id + channel`

## Публикация релиза

Основной механизм публикации:

- `go run ./cmd/adapter-publisher publish ...`

CLI принимает:

- путь к собранному бинарнику
- `adapter_id`
- `version`
- `target_os`
- `target_arch`
- `title`
- `description`
- `adapter_type`
- `protocol_version`
- набор каналов для promote после публикации

Порядок публикации атомарный с точки зрения активации релиза:

1. Загружается бинарник.
2. Считается и публикуется `sha256.txt`.
3. Генерируется и публикуется `release.json`.
4. Обновляется `catalog/index.json`.
5. Только затем переключаются channel pointers.

Из этого следуют гарантии:

- versioned release URL immutable
- оборванная публикация не делает релиз selectable
- heartbeat не начнёт выдавать релиз до появления корректного channel pointer и успешного sync

## Синхронизация в БД

Серверный sync:

- загружает `catalog/index.json`
- загружает `release.json` для каждой записи
- загружает `stable/latest` pointers
- проверяет обязательные поля release manifest
- проверяет наличие бинарника и `sha256.txt`
- сверяет checksum
- выполняет upsert release/channel в БД
- удаляет записи, исчезнувшие из каталога

Если `catalog/index.json` битый:

- sync завершается ошибкой
- текущая проекция в БД не затирается

Если релиз неполный:

- запись остаётся в БД как непубликованная
- UI показывает её как недоступную
- heartbeat её не выдаёт

## Runtime и operator flow

Оператор сохраняет только:

- `selected_adapter_ids`

Сервер при heartbeat делает:

1. Берёт `selected_adapter_ids` из конфига агента.
2. Для каждого `adapter_id` находит релиз из default channel, обычно `stable`.
3. Проверяет, что релиз опубликован и manifest полон.
4. Возвращает агенту полный `adapter_manifest`.

UI может показывать `stable/latest` read-only, но happy-path не усложняется.

## Promote и rollback

Переключение канала выполняется без повторной загрузки бинарника:

```bash
go run ./cmd/adapter-publisher promote \
  --adapter-id fiscal-atol \
  --version 1.2.2 \
  --target-os windows \
  --target-arch amd64 \
  --channel stable
```

После переключения:

1. Выполнить ручной refresh каталога или дождаться фонового sync.
2. Проверить, что `stable` указывает на нужную версию.
3. Следующий heartbeat начнёт выдавать новый stable manifest.

Rollback делается тем же `promote`, только на предыдущий versioned release.

## CI/CD основа

В репозитории есть базовый workflow:

- `.github/workflows/adapter-release-publish.yml`

Он предназначен для двух операций:

- `publish`
- `promote`

Workflow не вшивает конкретную команду сборки адаптера, потому что она зависит от типа адаптера. Перед шагом publish нужно добавить проект-специфичную сборку, которая положит бинарник в путь, переданный через `binary_path`.

## Операционный минимум

Для production нужно настроить:

- `AGENT_ADAPTER_S3_ENABLED=true`
- `AGENT_ADAPTER_S3_ENDPOINT=http://minio:9000`
- `AGENT_ADAPTER_S3_BUCKET=agents`
- `AGENT_ADAPTER_S3_ACCESS_KEY`
- `AGENT_ADAPTER_S3_SECRET_KEY`
- `AGENT_ADAPTER_PUBLIC_BASE_URL=https://etalon.serty.top/agents`
- `AGENT_ADAPTER_CATALOG_KEY=catalog/index.json`
- `AGENT_ADAPTER_SYNC_INTERVAL_MIN`
- `AGENT_ADAPTER_DEFAULT_CHANNEL=stable`

Итоговая схема lifecycle:

1. Собрать бинарник адаптера.
2. Опубликовать release через CLI или workflow.
3. Переключить `stable/latest`, если нужно.
4. Синхронизировать каталог в БД сервера.
5. Выдать адаптер агенту на heartbeat по выбранному `adapter_id`.
