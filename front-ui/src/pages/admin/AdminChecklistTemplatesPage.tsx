import React, { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import {
  Alert,
  Button,
  Card,
  Col,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Row,
  Space,
  Switch,
  Table,
  Tag,
  Tree,
  Typography,
  message,
} from 'antd';
import type { ColumnsType } from 'antd/es/table';
import type { DataNode } from 'antd/es/tree';
import { DeleteOutlined, EditOutlined, PlusOutlined } from '@ant-design/icons';
import dayjs from 'dayjs';
import { checklistsApi } from '@/api/checklists';
import type { ChecklistTemplateDTO, ChecklistTemplatePayload } from '@/types/api';
import {
  CHECKLIST_MAX_DEPTH,
  type ChecklistTemplateNode,
  countChecklistTemplateNodes,
  parseChecklistTemplateText,
  serializeChecklistTemplate,
} from '@/features/tickets/checklist/checklistTree';
import { withApiError } from '@/utils/apiError';

const { Title, Text } = Typography;
const { TextArea } = Input;

type FormValues = {
  title: string;
  description: string;
  items_text: string;
  is_active: boolean;
};

const TEMPLATES_QUERY_KEY = ['checklist-templates', 'admin'];

const toTreeData = (nodes: ChecklistTemplateNode[], prefix = 'n'): DataNode[] =>
  nodes.map((node, index) => {
    const key = `${prefix}-${index}`;
    return {
      key,
      title: node.title,
      children: node.children?.length ? toTreeData(node.children, key) : undefined,
    };
  });

const collectKeys = (nodes: DataNode[]): React.Key[] =>
  nodes.flatMap((node) => [node.key, ...collectKeys(node.children || [])]);

const AdminChecklistTemplatesPage: React.FC = () => {
  const queryClient = useQueryClient();
  const [form] = Form.useForm<FormValues>();
  const [editing, setEditing] = useState<ChecklistTemplateDTO | null>(null);
  const [isModalOpen, setIsModalOpen] = useState(false);
  const itemsText = Form.useWatch('items_text', form) || '';

  const { data, isLoading } = useQuery({
    queryKey: TEMPLATES_QUERY_KEY,
    queryFn: () => checklistsApi.listTemplates(true),
  });
  const templates = data?.data || [];

  const parsed = useMemo(() => parseChecklistTemplateText(itemsText), [itemsText]);
  const previewTree = useMemo(() => toTreeData(parsed.nodes), [parsed.nodes]);

  const invalidate = () => {
    void queryClient.invalidateQueries({ queryKey: ['checklist-templates'] });
  };

  const saveMutation = useMutation({
    mutationFn: (payload: ChecklistTemplatePayload) =>
      (editing ? checklistsApi.updateTemplate(editing.id, payload) : checklistsApi.createTemplate(payload)),
    onSuccess: () => {
      message.success(editing ? 'Шаблон обновлён' : 'Шаблон создан');
      setIsModalOpen(false);
      setEditing(null);
      form.resetFields();
      invalidate();
    },
    onError: (error) => message.error(withApiError('Не удалось сохранить шаблон', error)),
  });

  const deleteMutation = useMutation({
    mutationFn: (id: string) => checklistsApi.deleteTemplate(id),
    onSuccess: () => {
      message.success('Шаблон удалён');
      invalidate();
    },
    onError: (error) => message.error(withApiError('Не удалось удалить шаблон', error)),
  });

  const toggleActiveMutation = useMutation({
    mutationFn: (template: ChecklistTemplateDTO) =>
      checklistsApi.updateTemplate(template.id, {
        title: template.title,
        description: template.description,
        items: template.items,
        is_active: !template.is_active,
      }),
    onSuccess: () => invalidate(),
    onError: (error) => message.error(withApiError('Не удалось изменить активность шаблона', error)),
  });

  const openCreate = () => {
    setEditing(null);
    form.setFieldsValue({ title: '', description: '', items_text: '', is_active: true });
    setIsModalOpen(true);
  };

  const openEdit = (template: ChecklistTemplateDTO) => {
    setEditing(template);
    form.setFieldsValue({
      title: template.title,
      description: template.description,
      items_text: serializeChecklistTemplate(template.items || []),
      is_active: template.is_active,
    });
    setIsModalOpen(true);
  };

  const submit = async () => {
    const values = await form.validateFields();
    const result = parseChecklistTemplateText(values.items_text);
    if (result.nodes.length === 0) {
      message.error('Добавьте хотя бы один пункт шаблона');
      return;
    }
    if (result.errors.length > 0) {
      message.error(result.errors[0]);
      return;
    }
    saveMutation.mutate({
      title: values.title.trim(),
      description: (values.description || '').trim(),
      items: result.nodes,
      is_active: values.is_active,
    });
  };

  const columns: ColumnsType<ChecklistTemplateDTO> = [
    {
      title: 'Название',
      dataIndex: 'title',
      key: 'title',
      render: (_value, record) => (
        <Space direction="vertical" size={0}>
          <Text strong>{record.title}</Text>
          {record.description ? <Text type="secondary" style={{ fontSize: 12 }}>{record.description}</Text> : null}
        </Space>
      ),
    },
    {
      title: 'Пунктов',
      key: 'count',
      width: 110,
      render: (_value, record) => countChecklistTemplateNodes(record.items || []),
    },
    {
      title: 'Статус',
      key: 'active',
      width: 150,
      render: (_value, record) => (
        <Space size={8}>
          <Switch
            size="small"
            checked={record.is_active}
            loading={toggleActiveMutation.isPending && toggleActiveMutation.variables?.id === record.id}
            onChange={() => toggleActiveMutation.mutate(record)}
          />
          <Tag color={record.is_active ? 'success' : 'default'} style={{ marginInlineEnd: 0 }}>
            {record.is_active ? 'Активен' : 'Скрыт'}
          </Tag>
        </Space>
      ),
    },
    {
      title: 'Обновлён',
      dataIndex: 'updated_at',
      key: 'updated_at',
      width: 170,
      render: (value: string) => (value ? dayjs(value).format('DD.MM.YYYY HH:mm') : '-'),
    },
    {
      title: '',
      key: 'actions',
      width: 110,
      render: (_value, record) => (
        <Space size={4}>
          <Button size="small" icon={<EditOutlined />} aria-label="Редактировать шаблон" onClick={() => openEdit(record)} />
          <Popconfirm
            title="Удалить шаблон?"
            description="Уже созданные по шаблону чеклисты в тикетах не изменятся."
            okText="Удалить"
            okButtonProps={{ danger: true }}
            cancelText="Отмена"
            onConfirm={() => deleteMutation.mutateAsync(record.id)}
          >
            <Button size="small" danger icon={<DeleteOutlined />} aria-label="Удалить шаблон" />
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <Space direction="vertical" size={16} style={{ width: '100%' }}>
      <Card className="glass-panel">
        <Space style={{ width: '100%', justifyContent: 'space-between' }} wrap>
          <Space direction="vertical" size={4}>
            <Title level={4} style={{ marginBottom: 0 }}>Шаблоны чеклистов</Title>
            <Text type="secondary">Шаблоны применяются во вкладке «Чеклист» тикета: пункты добавляются в конец чеклиста</Text>
          </Space>
          <Button type="primary" icon={<PlusOutlined />} onClick={openCreate}>Новый шаблон</Button>
        </Space>
      </Card>

      <Card className="glass-panel" size="small">
        <Table<ChecklistTemplateDTO>
          rowKey="id"
          size="small"
          dataSource={templates}
          columns={columns}
          pagination={false}
          locale={{ emptyText: isLoading ? ' ' : <Empty description="Шаблонов пока нет" /> }}
        />
      </Card>

      <Modal
        open={isModalOpen}
        width={960}
        title={editing ? `Шаблон «${editing.title}»` : 'Новый шаблон чеклиста'}
        okText="Сохранить"
        cancelText="Отмена"
        confirmLoading={saveMutation.isPending}
        onOk={() => void submit()}
        onCancel={() => {
          setIsModalOpen(false);
          setEditing(null);
        }}
        forceRender
      >
        <Form<FormValues> form={form} layout="vertical" initialValues={{ is_active: true }}>
          <Row gutter={16}>
            <Col xs={24} md={16}>
              <Form.Item name="title" label="Название" rules={[{ required: true, whitespace: true, message: 'Укажите название шаблона' }]}>
                <Input maxLength={200} placeholder="Например, Установка кассы" />
              </Form.Item>
            </Col>
            <Col xs={24} md={8}>
              <Form.Item name="is_active" label="Доступен в тикетах" valuePropName="checked">
                <Switch />
              </Form.Item>
            </Col>
          </Row>
          <Form.Item name="description" label="Описание">
            <Input maxLength={500} placeholder="Когда применять шаблон" />
          </Form.Item>
          <Row gutter={16}>
            <Col xs={24} md={14}>
              <Form.Item
                name="items_text"
                label="Пункты"
                extra={`Каждая строка — пункт. Отступ в 2 пробела или Tab делает строку подпунктом предыдущей (до ${CHECKLIST_MAX_DEPTH} уровней).`}
                rules={[{ required: true, whitespace: true, message: 'Добавьте пункты шаблона' }]}
              >
                <TextArea
                  autoSize={{ minRows: 10, maxRows: 22 }}
                  style={{ fontFamily: 'monospace' }}
                  placeholder={'Подготовка\n  Проверить ФН\n  Обновить драйвер\nЗапуск\nПроверка чека'}
                  onKeyDown={(event) => {
                    if (event.key !== 'Tab') return;
                    event.preventDefault();
                    const target = event.currentTarget;
                    const { selectionStart, selectionEnd, value } = target;
                    const next = `${value.slice(0, selectionStart)}  ${value.slice(selectionEnd)}`;
                    form.setFieldValue('items_text', next);
                    requestAnimationFrame(() => {
                      target.selectionStart = selectionStart + 2;
                      target.selectionEnd = selectionStart + 2;
                    });
                  }}
                />
              </Form.Item>
            </Col>
            <Col xs={24} md={10}>
              <Text type="secondary" style={{ display: 'block', marginBottom: 8 }}>
                Предпросмотр · {countChecklistTemplateNodes(parsed.nodes)} пунктов
              </Text>
              {parsed.errors.length > 0 ? (
                <Alert type="warning" showIcon message={parsed.errors[0]} style={{ marginBottom: 8 }} />
              ) : null}
              {previewTree.length > 0 ? (
                <Tree
                  selectable={false}
                  checkable
                  disabled
                  blockNode
                  expandedKeys={collectKeys(previewTree)}
                  treeData={previewTree}
                />
              ) : (
                <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="Нет пунктов" />
              )}
            </Col>
          </Row>
        </Form>
      </Modal>
    </Space>
  );
};

export default AdminChecklistTemplatesPage;
