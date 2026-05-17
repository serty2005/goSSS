# 04. Контур агента и совместимость legacy getad

## Core-agent

Агентский runtime ориентирован на Windows-среду:

- конфиг из `agent-config.json`;
- хранение identity/токенов в HKLM;
- защита токенов через DPAPI;
- heartbeat + inventory цикл;
- выполнение задач адаптеров.

## Поддерживаемые типы задач

- `run_adapter`
- `adapter_run`
- `saga_run`
- `self_update` (legacy)

Изменения, ломающие эти типы, требуют отдельной миграционной стратегии и обновления docs в `agent/docs`.

## Legacy getad endpoint

Для старых пассивных агентов сохранен отдельный flow:

- endpoint: `POST /api/submit_json`;
- авторизация: `X-API-Key` против `AGENT_API_KEY` только когда `AGENT_API_KEY` задан;
- UUID из `agent_uuid` или legacy `uuid`;
- если `AGENT_API_KEY` пустой, проверка ключа отключена и запрос не отклоняется по `X-API-Key`;
- `agent_type` принудительно трактуется как `getad`.

Этот путь совместимости нельзя смешивать с bootstrap/access-token flow `sssruner` без явного архитектурного решения.
