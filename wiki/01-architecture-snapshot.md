# 01. Архитектурный снимок

## Назначение

`goSSS` (XenionDesk) — ServiceDesk для техподдержки: заявки, инфраструктура клиентов, агентские наблюдения, интеграции с внешними системами и операционная аналитика.

## Крупные подсистемы

- **Backend (Go)**: API, бизнес-логика, воркеры, интеграционные циклы.
- **Frontend (React + TypeScript)**: интерфейс операторов и администраторов.
- **Windows agent (Go)**: сбор инвентаризации и запуск локальных адаптеров.
- **Инфраструктура (Docker, PostgreSQL, Redis, S3/MinIO)**.

## Топология каталогов

- `cmd/etalon-server` — вход в backend.
- `internal/app` — сборка зависимостей, маршруты, фоновый runtime.
- `internal/services` — прикладные сервисы.
- `internal/transport/http` — HTTP handlers/middleware.
- `internal/core` — gateways/workers/orchestrator/event-потоки.
- `front-ui` — UI-клиент.
- `agent` — core-agent и внешние адаптеры.

## Принятые архитектурные решения

1. Слоистый путь запроса для API: `handler -> service -> repository`.
2. Для части интеграций используется событийный контур с `pkg/eventbus`.
3. Миграции выполняются через `gorm.AutoMigrate` при старте backend.
4. Legacy endpoint `/api/submit_json` сохранен для совместимости getad-агентов и живет отдельно от JWT-контура.

## Открытые вопросы

- Зафиксировать отдельной статьей единый формат версионирования API и обратной совместимости для frontend.
- Разделить wiki по bounded contexts (tickets/company/agents/telephony), чтобы снизить когнитивную нагрузку.
