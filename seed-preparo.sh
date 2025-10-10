#!/bin/bash

# Параметры подключения
HOST="postgres"
PORT="5432"
USER="etalon"
PASSWORD="1"
DB_NAME="etalon_db_stab"
ADMIN_DB="postgres"

export PGPASSWORD=$PASSWORD

# Функция для принудительного завершения подключений к базе данных
force_disconnect() {
    local db_name=$1
    echo "Завершение активных подключений к базе данных $db_name..."
    
    # Получаем список всех активных подключений к целевой базе данных
    psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -c "
    SELECT pg_terminate_backend(pg_stat_activity.pid)
    FROM pg_stat_activity
    WHERE pg_stat_activity.datname = '$db_name'
      AND pid <> pg_backend_pid();
    " > /dev/null 2>&1
    
    # Небольшая пауза для завершения процессов
    sleep 1
}

# Проверяем существование базы данных перед удалением
echo "Проверка существования базы данных $DB_NAME..."
DB_EXISTS=$(psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -t -c "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME';")

if [ -n "$DB_EXISTS" ]; then
    echo "База данных $DB_NAME существует. Принудительное удаление..."
    
    # Завершаем все активные подключения
    force_disconnect $DB_NAME
    
    # Удаление базы данных с проверкой
    echo "Удаление базы данных $DB_NAME..."
    psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -c "DROP DATABASE IF EXISTS $DB_NAME WITH (FORCE);"
    
    # Проверяем успешность удаления
    sleep 1
    DB_STILL_EXISTS=$(psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -t -c "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME';")
    
    if [ -n "$DB_STILL_EXISTS" ]; then
        echo "Ошибка: Не удалось удалить базу данных $DB_NAME"
        exit 1
    fi
else
    echo "База данных $DB_NAME не существует, пропускаем удаление."
fi

# Создание базы данных
echo "Создание базы данных $DB_NAME..."
psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -c "CREATE DATABASE $DB_NAME;"

# Проверяем успешность создания
DB_CREATED=$(psql -h $HOST -p $PORT -U $USER -d $ADMIN_DB -t -c "SELECT 1 FROM pg_database WHERE datname = '$DB_NAME';")
if [ -n "$DB_CREATED" ]; then
    echo "База данных $DB_NAME успешно создана."
else
    echo "Ошибка: Не удалось создать базу данных $DB_NAME"
    exit 1
fi

echo "Операции завершены успешно."