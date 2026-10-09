import React from 'react';
import { Button, Popconfirm, Space, Tooltip, Typography, message } from 'antd';
import { CopyOutlined, DeleteOutlined, DownloadOutlined, PaperClipOutlined } from '@ant-design/icons';
import type { MaterialFileDTO } from '@/types/api';
import { formatFileSize } from '@/utils/formatters';

const { Text } = Typography;

type Props = {
  files: MaterialFileDTO[];
  onDelete?: (file: MaterialFileDTO) => void;
  deletingFileID?: string;
};

// toAbsoluteURL приводит ссылку к абсолютной, чтобы скопированный адрес работал вне приложения.
const toAbsoluteURL = (url: string) => {
  try {
    return new URL(url, window.location.origin).toString();
  } catch {
    return url;
  }
};

const copyFileLink = async (file: MaterialFileDTO) => {
  try {
    await navigator.clipboard.writeText(toAbsoluteURL(file.url));
    message.success('Ссылка на файл скопирована');
  } catch {
    message.error('Не удалось скопировать ссылку');
  }
};

// MaterialFilesList показывает файлы материала с прямыми ссылками на скачивание.
const MaterialFilesList: React.FC<Props> = ({ files, onDelete, deletingFileID }) => {
  if (files.length === 0) {
    return null;
  }
  return (
    <ul className="material-files-list">
      {files.map((file) => (
        <li key={file.id} className="material-files-list__item">
          <PaperClipOutlined />
          <a
            className="material-files-list__name"
            href={file.url}
            download={file.name}
            target="_blank"
            rel="noreferrer"
            title={file.name}
            onClick={(event) => event.stopPropagation()}
          >
            {file.name}
          </a>
          <Text type="secondary" className="material-files-list__size">{formatFileSize(file.size)}</Text>
          <Space size={0} className="material-files-list__actions">
            <Tooltip title="Скачать">
              <Button
                type="text"
                size="small"
                icon={<DownloadOutlined />}
                href={file.url}
                download={file.name}
                target="_blank"
                rel="noreferrer"
                aria-label={`Скачать ${file.name}`}
                onClick={(event) => event.stopPropagation()}
              />
            </Tooltip>
            <Tooltip title="Копировать прямую ссылку">
              <Button
                type="text"
                size="small"
                icon={<CopyOutlined />}
                aria-label={`Копировать ссылку на ${file.name}`}
                onClick={(event) => {
                  event.stopPropagation();
                  void copyFileLink(file);
                }}
              />
            </Tooltip>
            {onDelete ? (
              <Popconfirm
                title="Удалить файл?"
                description="Прямая ссылка перестанет работать."
                okText="Удалить"
                cancelText="Отмена"
                onConfirm={() => onDelete(file)}
              >
                <Button
                  type="text"
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  loading={deletingFileID === file.id}
                  aria-label={`Удалить ${file.name}`}
                  onClick={(event) => event.stopPropagation()}
                />
              </Popconfirm>
            ) : null}
          </Space>
        </li>
      ))}
    </ul>
  );
};

export default MaterialFilesList;
