# Stage Review и operator flow

## Кратко

Текущий операторский сценарий для `sssruner` упрощён до модели:

1. Оператор открывает карточку агента.
2. Видит блок `Доступные адаптеры`.
3. Отмечает нужные published adapters галками.
4. Нажимает `Сохранить`.
5. На следующем heartbeat сервер отдаёт агенту уже готовый `adapter_manifests`.

Оператор больше не редактирует вручную:

- `download_url`;
- `sha256`;
- `version`;
- `file_name`;
- raw `adapter_manifests` JSON;
- профиль машины как обязательный шаг;
- правила `signature_key` как обязательный шаг.

## Meaningful heartbeat

Логика meaningful heartbeat и noop heartbeat остаётся прежней.

Сервер по-прежнему:

- всегда обновляет liveness и последние snapshot-данные;
- публикует observation только при meaningful change;
- различает `heartbeat_result=noop` и `heartbeat_result=meaningful_change`;
- не ломает существующий heartbeat-контракт агента.

Это важно: новый операторский flow не меняет доменную семантику heartbeat, а только упрощает назначение адаптеров поверх уже существующего контура.

## Published adapter catalog

Для выдачи адаптеров введён отдельный server-side слой `published adapters`.

Он хранится в модели `PublishedAgentAdapter` и содержит как минимум:

- `adapter_id`;
- `title`;
- `description`;
- `published`;
- `version`;
- `adapter_type`;
- `target_os`;
- `target_arch`;
- `protocol_version`;
- `download_url`;
- `sha256`;
- `file_name`.

Смысл этого слоя:

- оператор выбирает только `adapter_id`;
- полный manifest живёт только на сервере;
- heartbeat-ответ агенту собирается из server-side каталога;
- UI больше не просит оператора заполнять manifest-поля руками.

### Demo seed catalog

Для локальной разработки и тестов сервер поднимает demo-каталог:

- `fiscal-atol`;
- `fiscal-mitsu`;
- `fiscal-shtrih`.

Для этих записей используются `example.test` URL и тестовые `sha256`.
Это осознанный demo-режим: в репозитории нет боевых `.exe`, поэтому перед реальной эксплуатацией эти значения должны быть заменены на настоящие опубликованные бинарники.

## Что хранится у агента в config

В `agent.config` основной сценарий теперь хранит только выбор оператора:

- `selected_adapter_ids`.

Это отделено от published catalog:

- конфиг агента хранит только компактный выбор;
- published catalog хранит полные manifest-поля;
- при heartbeat сервер резолвит `selected_adapter_ids` в полный `adapter_manifests`.

Legacy-поле `adapter_manifests` остаётся только как fallback для уже сохранённых старых конфигураций.
После следующего сохранения через новый UI сервер нормализует конфиг на `selected_adapter_ids`.

## Новый operator flow

### Happy-path

1. Агент проходит bootstrap и начинает слать heartbeat.
2. Оператор открывает карточку агента.
3. Сервер отдаёт:
   - список `available_adapters`;
   - текущий `selected_adapter_ids`;
   - read-only подсказки, если сервер что-то рекомендует по inventory.
4. Оператор отмечает галки напротив нужных адаптеров.
5. UI отправляет простой payload:

```json
{
  "selected_adapter_ids": ["fiscal-atol", "fiscal-shtrih"]
}
```

6. Сервер валидирует выбор.
7. Сервер сохраняет `selected_adapter_ids` в конфиг агента.
8. Следующий heartbeat возвращает агенту:

```json
{
  "status": "ok",
  "adapter_manifests": [
    {
      "adapter_id": "fiscal-atol",
      "adapter_type": "fiscal-atol",
      "version": "0.1.0-demo",
      "target_os": "windows",
      "target_arch": "amd64",
      "protocol_version": "1",
      "download_url": "https://example.test/adapters/fiscal-atol-0.1.0-demo.exe",
      "sha256": "...",
      "file_name": "fiscal-atol-0.1.0-demo.exe"
    }
  ]
}
```

### Что видит оператор в UI

В карточке агента показывается:

- чекбокс;
- имя адаптера;
- краткое описание;
- статус публикации;
- служебные теги вроде версии и целевой платформы.

Если адаптер нельзя выбрать, UI показывает причину:

- адаптер не опубликован;
- manifest неполон.

## Валидация

Сервер не даёт сохранить выбор, если:

- `adapter_id` отсутствует в published catalog;
- запись не опубликована;
- у записи неполный manifest.

Под неполным manifest в текущем этапе понимается отсутствие обязательных полей, необходимых для выдачи агенту:

- `adapter_id`;
- `version`;
- `adapter_type`;
- `target_os`;
- `target_arch`;
- `protocol_version`;
- `download_url`;
- `sha256`;
- `file_name`.

## Рекомендации, machine profile и signature rules

Серверные рекомендации по inventory не удалены полностью, но переведены во вторичный режим.

Что это значит:

- recommendation logic можно использовать как read-only hint;
- `machine profile` больше не является обязательным действием оператора;
- `signature_key` rules больше не входят в happy-path назначения адаптеров;
- оператор может выполнить базовую задачу назначения адаптеров, вообще не заходя в эти сущности.

Если в будущем понадобится углублённая классификация COM-устройств, её можно развивать отдельно, не возвращая raw manifest editor в основной сценарий.

## Итог

Новый flow делает ровно одно действие основным:

- сохранить набор `selected_adapter_ids`.

Всё остальное теперь либо внутренний механизм сервера, либо вторичный диагностический hint.

Это снижает операторскую сложность и сохраняет правильный контракт с агентом:

- UI простой;
- сервер хранит published catalog отдельно;
- агент по heartbeat продолжает получать полноценный `adapter_manifests`.
