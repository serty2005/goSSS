# Etalon Server API

API-сервер, который служит «эталонным хранилищем» сущностей компании и обеспечивает их синхронизацию с внешними системами.

## Особенности

- **Clean Architecture**: Четкое разделение на слои (handlers, services, repositories).
- **Синхронизация**: Фоновая инкрементальная синхронизация с ServiceDesk.
- **Надежность**: Graceful Shutdown, логирование, повторные запросы (retry).
- **Конфигурация**: Управляется через переменные окружения (`.env` файл).
- **База данных**: PostgreSQL с миграциями через `golang-migrate`.
- **API**: RESTful API для CRUD операций, поиска и запуска синхронизаций.

## Требования

- [Go](https://golang.org/dl/) (версия 1.21+)
- [Docker](https://www.docker.com/get-started) и `docker-compose` (для легкого запуска PostgreSQL)
- [golang-migrate](https://github.com/golang-migrate/migrate/tree/master/cmd/migrate) CLI

## Установка и запуск

1.  **Клонируйте репозиторий:**
    ```bash
    git clone <your-repo-url>
    cd etalon-server
    ```

2.  **Настройте переменные окружения:**
    Скопируйте файл `.env.example` в `.env` и заполните его своими данными.
    ```bash
    cp .env.example .env
    ```
    Отредактируйте `.env`:
    - `DATABASE_URL`: Строка подключения к вашей PostgreSQL.
    - `BASE_URL`: URL вашего ServiceDesk.
    - `SDKEY`: Ключ доступа к API ServiceDesk.

3.  **Запустите базу данных (используя Docker):**
    ```bash
    docker-compose up -d
    ```
    Это запустит PostgreSQL сервер на порту 5432 с данными из `.env`.

4.  **Примените миграции:**
    Убедитесь, что `golang-migrate` установлен.
    ```bash
    migrate -database "${DATABASE_URL}" -path internal/db/migrations up
    ```
    *Примечание: возможно, потребуется передать `DATABASE_URL` в кавычках.*

5.  **Установите зависимости:**
    ```bash
    go mod tidy
    ```

6.  **Запустите сервер:**
    ```bash
    go run ./cmd/etalon-server
    ```
    Сервер будет доступен по адресу `http://localhost:8080` (или на порту, указанном в `.env`).

## API Endpoints

### Синхронизация

- **`POST /sync/servicedesk`**: Запускает синхронизацию с ServiceDesk.
  - **Тело запроса:**
    ```json
    {
      "full": false
    }
    ```
    `full: true` для полной синхронизации (в текущей реализации не меняет логику), `false` для инкрементальной.
  - **Ответ:** `202 Accepted`
    ```json
    {
      "message": "synchronization started"
    }
    ```
  - **Пример `curl`:**
    ```bash
    curl -X POST http://localhost:8080/sync/servicedesk -H "Content-Type: application/json" -d '{"full": false}'
    ```

### CRUD и Поиск (Примеры)

- **`GET /api/companies/{uuid}`**: Получить информацию о компании.
- **`GET /api/search?term=example`**: Поиск по всем сущностям.

## План дальнейшего развития

1.  **Soft Deletion / Архивирование**: Вместо удаления записей, которых нет в ServiceDesk, помечать их как архивные. Это сохранит историю и связанные данные.
2.  **Разработка UI**: Создание простого Web UI на React или Vue для наглядного отображения данных, статуса синхронизации и ручного управления.
3.  **RBAC (Role-Based Access Control)**: Внедрение системы ролей и прав доступа к API, чтобы ограничить операции для разных групп пользователей (например, "администратор", "только чтение").