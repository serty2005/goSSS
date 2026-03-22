# Stage Review и operator flow

## Кратко

Основной операторский сценарий для `sssruner` остаётся простым:

1. Оператор открывает карточку агента.
2. Видит блок `Доступные адаптеры`.
3. Отмечает нужные `adapter_id` галками.
4. Нажимает `Сохранить`.
5. На следующем heartbeat сервер отдаёт агенту уже готовый `adapter_manifests`.

Оператор по-прежнему не редактирует вручную:

- `download_url`
- `sha256`
- `version`
- `file_name`
- raw `adapter_manifests`
- channel pointers
- release manifest JSON

## Что изменилось в server-side контуре

Source of truth для релизов адаптеров теперь хранится в S3/MinIO:

- бинарники лежат в bucket `agents`
- versioned release metadata лежит рядом с бинарником
- channel pointers `stable/latest` управляют активной версией
- сервер синхронизирует каталог из S3 в локальную БД
- heartbeat работает только от локальной БД и не ходит в S3 на каждый запрос

Runtime-модель в БД:

- `AgentAdapterRelease`
- `AgentAdapterChannel`

Старая сущность `PublishedAgentAdapter` может мигрироваться в новую модель для совместимости, но больше не является runtime-source.

## S3 layout

Каталог релизов использует layout:

- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/<file_name>`
- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/release.json`
- `adapters/<adapter_id>/releases/<version>/<target_os>/<target_arch>/sha256.txt`
- `adapters/<adapter_id>/channels/stable.json`
- `adapters/<adapter_id>/channels/latest.json`
- `catalog/index.json`

Публичные download URL для агента должны иметь вид:

- `https://etalon.serty.top/agents/...`

Внутренний endpoint для сервера и publish CLI:

- `http://minio:9000`

## Что хранится у агента в config

В `agent.config` основной happy-path хранит только выбор оператора:

- `selected_adapter_ids`

Это отделено от release-каталога:

- конфиг агента хранит только компактный выбор
- релизы и channel pointers живут на сервере
- heartbeat резолвит `selected_adapter_ids -> stable release -> полный adapter_manifest`

Legacy-поле `adapter_manifests` остаётся только как fallback для ранее сохранённых конфигураций.

## Новый operator flow

### Happy-path

1. Агент проходит bootstrap и начинает слать heartbeat.
2. Оператор открывает карточку агента.
3. Сервер отдаёт:
   - список `available_adapters`
   - текущий `selected_adapter_ids`
   - read-only сведения о `stable/latest`, если они есть
4. Оператор отмечает галки напротив нужных адаптеров.
5. UI отправляет payload:

```json
{
  "selected_adapter_ids": ["fiscal-atol", "fiscal-shtrih"]
}
```

6. Сервер валидирует, что для каждого выбранного `adapter_id` есть publishable release в `stable`.
7. Сервер сохраняет только `selected_adapter_ids`.
8. Следующий heartbeat возвращает агенту полноценный manifest для stable-релиза:

```json
{
  "status": "ok",
  "adapter_manifests": [
    {
      "adapter_id": "fiscal-atol",
      "adapter_type": "fiscal-atol",
      "version": "1.2.3",
      "target_os": "windows",
      "target_arch": "amd64",
      "protocol_version": "1",
      "download_url": "https://etalon.serty.top/agents/adapters/fiscal-atol/releases/1.2.3/windows/amd64/fiscal-atol.exe",
      "sha256": "...",
      "file_name": "fiscal-atol.exe"
    }
  ]
}
```

### Что видит оператор в UI

В карточке агента показывается:

- чекбокс
- имя адаптера
- краткое описание
- статус публикации
- read-only теги `stable` и `latest`, если версии уже известны
- целевая платформа

Если адаптер нельзя выбрать, UI показывает причину:

- канал `stable` не назначен
- release manifest неполон
- релиз не опубликован

## Синхронизация каталога

Сервер синхронизирует release catalog из S3:

- периодически по `AGENT_ADAPTER_SYNC_INTERVAL_MIN`
- вручную через `POST /agent-diagnostics/adapters/refresh`

Во время синхронизации:

- загружается `catalog/index.json`
- загружаются `release.json` и `stable/latest` pointers
- обязательные поля валидируются до активации релиза
- неполные релизы не становятся selectable
- битый catalog index не должен затирать уже синхронизированную БД

## Валидация

Сервер не даёт сохранить выбор, если:

- `adapter_id` отсутствует в server-side каталоге релизов
- для него не назначен default channel
- релиз канала не опубликован
- manifest неполон

Под неполным manifest понимается отсутствие обязательных полей:

- `adapter_id`
- `version`
- `title`
- `adapter_type`
- `target_os`
- `target_arch`
- `protocol_version`
- `download_url`
- `sha256`
- `file_name`
- `source_key`

## Demo seed

Для локальной разработки и тестов остаётся demo-каталог:

- `fiscal-atol`
- `fiscal-mitsu`
- `fiscal-shtrih`

Он сидится только если S3-контур отключён и release-таблицы пустые. Demo URL используют `example.test/agents/...`, чтобы не конфликтовать с production layout.

## Rollback и publish lifecycle

Rollback больше не требует редактировать карточки агентов и не заставляет оператора выбирать версию руками:

- публикуется новый versioned release
- `stable/latest` переключаются через channel pointers
- сервер выполняет refresh каталога
- heartbeat начинает выдавать новую стабильную версию
- rollback делается обратным переключением `stable` на предыдущий релиз

## Meaningful heartbeat

Логика meaningful heartbeat и noop heartbeat не меняется:

- сервер по-прежнему обновляет liveness и snapshot-данные
- публикует observation только при meaningful change
- различает `heartbeat_result=noop` и `heartbeat_result=meaningful_change`
- не ломает существующий heartbeat-контракт агента

Изменился только server-side источник adapter manifests, но не доменная семантика heartbeat.
