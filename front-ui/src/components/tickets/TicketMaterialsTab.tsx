import React, { useMemo, useState } from 'react';
import { Button, Empty, List, Modal, Space, Tag, Typography } from 'antd';
import { EditOutlined, FileTextOutlined } from '@ant-design/icons';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import dayjs from 'dayjs';
import type { CompanyScopedMaterialDTO, MaterialSourceDTO, MaterialSourceScope } from '@/types/api';

const { Text } = Typography;

type Props = {
  materials: CompanyScopedMaterialDTO[];
};

const SCOPE_ORDER: MaterialSourceScope[] = ['company', 'parent', 'equipment'];

const SCOPE_LABELS: Record<MaterialSourceScope, string> = {
  company: 'Компания',
  parent: 'Родительская компания',
  equipment: 'Оборудование компании',
};

const ENTITY_TYPE_LABELS: Record<MaterialSourceDTO['entity_type'], string> = {
  Company: 'Компания',
  Server: 'Сервер',
  Workstation: 'РС',
  FiscalRegister: 'ФР',
};

const ENTITY_PATHS: Record<MaterialSourceDTO['entity_type'], string> = {
  Company: '/companies',
  Server: '/servers',
  Workstation: '/workstations',
  FiscalRegister: '/fiscals',
};

// buildMaterialEditorLink ведет в редактор материалов сущности-источника с открытым материалом.
const buildMaterialEditorLink = (source: MaterialSourceDTO, materialID: string) => {
  const params = new URLSearchParams({ tab: 'materials', material: materialID, edit: '1' });
  return `${ENTITY_PATHS[source.entity_type]}/${encodeURIComponent(source.entity_id)}?${params.toString()}`;
};

const buildPreview = (content: string) => {
  const plain = String(content || '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/!\[[^\]]*\]\([^)]*\)/g, ' ')
    .replace(/\[([^\]]*)\]\([^)]*\)/g, '$1')
    .replace(/[#>*_`~|-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
  return plain.length > 180 ? `${plain.slice(0, 180)}…` : plain;
};

const primaryScope = (item: CompanyScopedMaterialDTO): MaterialSourceScope => item.sources[0]?.scope || 'company';

const renderSourceTag = (source: MaterialSourceDTO) => (
  <Tag key={`${source.entity_type}:${source.entity_id}`} style={{ marginInlineEnd: 0 }}>
    {source.entity_type === 'Company' ? source.title : `${ENTITY_TYPE_LABELS[source.entity_type]}: ${source.title}`}
  </Tag>
);

const TicketMaterialsTab: React.FC<Props> = ({ materials }) => {
  const [openedID, setOpenedID] = useState('');
  const opened = useMemo(() => materials.find((item) => item.id === openedID) || null, [materials, openedID]);

  const groups = useMemo(
    () => SCOPE_ORDER
      .map((scope) => ({ scope, items: materials.filter((item) => primaryScope(item) === scope) }))
      .filter((group) => group.items.length > 0),
    [materials],
  );

  if (materials.length === 0) {
    return <Empty description="Материалов нет" />;
  }

  return (
    <div className="ticket-materials-tab">
      {groups.map((group) => (
        <div key={group.scope} className="ticket-materials-tab__group">
          <Text type="secondary" strong className="ticket-materials-tab__group-title">
            {SCOPE_LABELS[group.scope]} · {group.items.length}
          </Text>
          <List
            size="small"
            dataSource={group.items}
            renderItem={(item) => (
              <List.Item
                key={item.id}
                className="ticket-materials-tab__item"
                onClick={() => setOpenedID(item.id)}
                actions={[
                  <Button
                    key="edit"
                    type="link"
                    size="small"
                    icon={<EditOutlined />}
                    href={buildMaterialEditorLink(item.sources[0], item.id)}
                    target="_blank"
                    onClick={(event) => event.stopPropagation()}
                  >
                    Редактировать
                  </Button>,
                ]}
              >
                <Space direction="vertical" size={2} style={{ width: '100%', minWidth: 0 }}>
                  <Space size={6} wrap>
                    <FileTextOutlined />
                    <Text strong>{item.subject}</Text>
                    {item.sources.map(renderSourceTag)}
                  </Space>
                  {buildPreview(item.content) ? (
                    <Text type="secondary" className="ticket-materials-tab__preview">{buildPreview(item.content)}</Text>
                  ) : null}
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    {item.author_name || 'Сотрудник'} • обновлён {dayjs(item.updated_at).format('DD.MM.YYYY HH:mm')}
                  </Text>
                </Space>
              </List.Item>
            )}
          />
        </div>
      ))}

      <Modal
        open={Boolean(opened)}
        title={opened?.subject}
        width={900}
        onCancel={() => setOpenedID('')}
        footer={opened ? [
          <Button key="close" onClick={() => setOpenedID('')}>Закрыть</Button>,
          <Button
            key="edit"
            type="primary"
            icon={<EditOutlined />}
            href={buildMaterialEditorLink(opened.sources[0], opened.id)}
            target="_blank"
          >
            Открыть в редакторе
          </Button>,
        ] : null}
      >
        {opened ? (
          <Space direction="vertical" size={12} style={{ width: '100%' }}>
            <Space size={6} wrap>
              {opened.sources.map(renderSourceTag)}
            </Space>
            <Text type="secondary">
              Автор: {opened.author_name || 'Сотрудник'} • Создан: {dayjs(opened.created_at).format('DD.MM.YYYY HH:mm')} • Обновлён: {dayjs(opened.updated_at).format('DD.MM.YYYY HH:mm')}
            </Text>
            <div className="markdown-body ticket-materials-tab__content">
              <ReactMarkdown remarkPlugins={[remarkGfm]}>{opened.content}</ReactMarkdown>
            </div>
          </Space>
        ) : null}
      </Modal>

      <style>{`
        .ticket-materials-tab__group + .ticket-materials-tab__group {
          margin-top: 12px;
        }
        .ticket-materials-tab__group-title {
          display: block;
          font-size: 12px;
          text-transform: uppercase;
          letter-spacing: 0.02em;
          margin-bottom: 4px;
        }
        .ticket-materials-tab__item {
          cursor: pointer;
          border-radius: 6px;
          padding-inline: 8px !important;
        }
        .ticket-materials-tab__item:hover {
          background: rgba(22, 119, 255, 0.06);
        }
        .ticket-materials-tab__preview {
          display: -webkit-box;
          -webkit-line-clamp: 2;
          -webkit-box-orient: vertical;
          overflow: hidden;
          font-size: 13px;
        }
        .ticket-materials-tab__content {
          white-space: normal;
          max-height: 65vh;
          overflow: auto;
        }
      `}</style>
    </div>
  );
};

export default TicketMaterialsTab;
