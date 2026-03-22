#!/bin/sh
set -eu

until mc alias set local http://minio:9000 "${MINIO_ROOT_USER}" "${MINIO_ROOT_PASSWORD}"; do
    echo "Ожидание готовности MinIO..."
    sleep 2
done

mc mb --ignore-existing "local/${AGENT_ADAPTER_S3_BUCKET}"
mc anonymous set download "local/${AGENT_ADAPTER_S3_BUCKET}"
