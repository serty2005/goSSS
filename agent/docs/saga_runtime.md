# Saga Runtime

## Назначение

`core-agent` поддерживает отдельный task type `saga_run` для оркестрации многошаговых локальных сценариев.
Этот контур не заменяет существующий `run_adapter`, а дополняет его:

- `run_adapter` по-прежнему запускает внешний адаптер как отдельный процесс;
- `saga_run` исполняет декларативный план шагов внутри runtime агента;
- отдельные шаги saga могут быть:
  - runner-based, когда логика шага исполняется самим saga engine;
  - native, когда вызывается встроенная capability агента;
  - adapter-based, когда saga вызывает внешний адаптер по действующему adapter contract;
  - external-command based, когда saga вызывает локальную CLI-программу по явному шагу.

Legacy task type `self_update` сохранён для совместимости.
Он не содержит отдельной бизнес-логики и внутри конвертируется в `saga_run` с `saga_type = agent_self_update`.

## Контракт Задачи `saga_run`

Минимальный payload от сервера к агенту:

```json
{
  "saga_id": "saga-agent-update-2026-03-23-01",
  "saga_type": "agent_self_update",
  "request_id": "req-123",
  "correlation_id": "corr-123",
  "timeout_seconds": 300,
  "input": {
    "target_version": "2.0.0",
    "download_url": "https://example.test/agent-2.0.0.exe",
    "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "file_name": "agent-2.0.0.exe",
    "restart_policy": "immediate"
  },
  "steps": [
    {
      "id": "preflight",
      "type": "runner.self_update_preflight"
    },
    {
      "id": "version_check",
      "type": "runner.self_update_target_version_check"
    },
    {
      "id": "metadata_check",
      "type": "runner.self_update_download_metadata_check"
    },
    {
      "id": "apply_update",
      "type": "native.agent_self_update"
    }
  ],
  "retry_policy": {
    "max_attempts": 1
  },
  "idempotency_hint": {
    "key": "saga-agent-update-2026-03-23-01"
  },
  "metadata": {
    "source": "operator-flow"
  }
}
```

Обязательные поля:

- `saga_id`;
- `saga_type`;
- `input` для тех saga types, где он нужен;
- `steps`, если сервер хочет передать собственный execution plan.

Для `agent_self_update` список `steps` можно не передавать.
Тогда агент сам строит стандартный plan из встроенного definition.

## Жизненный Цикл Saga Runtime

1. Агент получает `saga_run` через heartbeat.
2. Workflow `saga_run` валидирует payload и выбирает definition по `saga_type`.
3. Definition строит execution plan и нормализует input/steps.
4. Engine создаёт или загружает `state-file` по `saga_id`.
5. Если saga уже завершена, агент возвращает ранее сохранённый результат.
6. Если найден незавершённый `state-file`, engine возобновляет выполнение и пропускает уже завершённые шаги.
7. Перед каждым шагом и после него engine обновляет execution journal на диске.
8. При ошибке saga останавливается на проблемном шаге и возвращает структурированный failure result.
9. После завершения результат кладётся в текущую очередь `task_results` и уходит на сервер следующим heartbeat.

## State File И Resume

Журнал выполнения хранится локально в:

```text
<data_dir>/saga-state/<saga_id>.json
```

В журнале сохраняются:

- нормализованный request;
- статус saga;
- step results;
- execution log;
- final result;
- флаг `resumed`.

Текущая граница идемпотентности определяется по `saga_id`.
`idempotency_hint` сохраняется в журнале и в task result для дальнейшего расширения серверной логики dedup/replay.

## Наблюдаемость

В обычном режиме engine пишет только ключевые события:

- старт саги;
- старт шага;
- завершение шага;
- остановка на ошибке;
- итог саги.

В `debug` режиме дополнительно пишутся:

- исходный `input` саги;
- входные параметры каждого шага;
- выходные данные шага;
- финальный `final_result`.

Те же данные сериализуются в `execution_log` task result.

## Task Result Для `saga_run`

Результат укладывается в текущий `task_results`, но расширен новыми опциональными полями:

```json
{
  "id": 91,
  "type": "saga_run",
  "status": "completed",
  "saga_id": "saga-agent-update-2026-03-23-01",
  "saga_type": "agent_self_update",
  "request_id": "req-123",
  "correlation_id": "corr-123",
  "duration_ms": 4200,
  "final_result": {
    "target_version": "2.0.0",
    "downloaded_path": "C:/ProgramData/Vendor/Agent/agent-2.0.0.exe"
  },
  "steps": [
    {
      "id": "preflight",
      "type": "runner.self_update_preflight",
      "status": "completed"
    },
    {
      "id": "apply_update",
      "type": "native.agent_self_update",
      "status": "completed"
    }
  ],
  "execution_log": [
    {
      "timestamp": "2026-03-23T01:00:00Z",
      "level": "info",
      "event": "saga.started",
      "message": "Старт саги"
    }
  ]
}
```

Для обратной совместимости `result` также дублирует `final_result`.

## Первая Saga: `agent_self_update`

Первая целевая saga не нарушает adapter contract.
Она не использует внешний адаптер для замены бинарника `core-agent`.

Стандартный plan:

1. `runner.self_update_preflight`
2. `runner.self_update_target_version_check`
3. `runner.self_update_download_metadata_check`
4. `native.agent_self_update`

Критично:

- проверка целевой версии выполняется до изменения бинарника;
- проверка download metadata выполняется отдельно от apply;
- сам replace/restart выполняется только через встроенную capability агента;
- внешний адаптер не получает права переписывать binary `core-agent`.

## Зарегистрированные Step Types

Текущий registry step handlers:

- `runner.self_update_preflight`
- `runner.self_update_target_version_check`
- `runner.self_update_download_metadata_check`
- `native.agent_self_update`
- `adapter.run`
- `external.command_run`

Это означает, что следующая saga может вызывать:

- существующий внешний адаптер по текущему adapter contract;
- внешний CLI вроде `goMH` через `external.command_run`;
- нативные возможности агента без посредника.

## Точки Расширения

Без изменения engine можно добавлять:

- новые `saga_type` через отдельные definitions;
- новые `step type` через `StepRegistry`;
- новые native capabilities через отдельные handler constructors;
- новые server-side enqueue helpers, которые строят payload для конкретного saga type;
- новые политики retry/idempotency на сервере поверх уже сериализуемых полей.

Ближайшие кандидаты:

- `external_program_run`;
- `goMH_module_run`;
- `chain_update_install`;
- `patch_verify_restart`;
- `collect_remediate`.
