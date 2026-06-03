import React, { useEffect, useMemo } from 'react';
import { useNavigate, useParams } from 'react-router-dom';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { Button, Card, Checkbox, Form, Input, Select, Space, Typography, message } from 'antd';
import { articlesApi } from '@/api/articles';
import type { ArticleContentFormat, ArticlePayload, ArticleStatus, ArticleType } from '@/types/api';

const { Title, Text } = Typography;

const releaseTemplate = `## Что изменилось

-

## Исправления

-

## Миграции/важные действия

-

## Риски

-

## Связанные тикеты/задачи

-`;

const typeOptions: Array<{ value: ArticleType; label: string }> = [
  { value: 'wiki', label: 'Wiki' },
  { value: 'release_note', label: 'Release notes' },
  { value: 'company_news', label: 'Новости компании' },
  { value: 'incident_note', label: 'Заметка по инциденту' },
  { value: 'internal_doc', label: 'Внутренняя инструкция' },
];

const statusOptions: Array<{ value: ArticleStatus; label: string }> = [
  { value: 'draft', label: 'Черновик' },
  { value: 'published', label: 'Опубликовано' },
  { value: 'archived', label: 'Архив' },
];

type FormValues = ArticlePayload;

const defaultValues: FormValues = {
  title: '',
  summary: '',
  content: '',
  content_format: 'markdown' as ArticleContentFormat,
  type: 'wiki',
  status: 'draft',
  project_key: '',
  version: '',
  tags: [],
  is_pinned: false,
  show_on_home: false,
  links: [],
};

const ArticleEditorPage: React.FC = () => {
  const { id } = useParams();
  const isEdit = Boolean(id);
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [form] = Form.useForm<FormValues>();
  const articleType = Form.useWatch('type', form);
  const content = Form.useWatch('content', form);

  const { data, isLoading } = useQuery({
    queryKey: ['article', id],
    queryFn: () => articlesApi.get(id || ''),
    enabled: isEdit,
    staleTime: 60_000,
  });

  useEffect(() => {
    if (data?.data) {
      const article = data.data;
      form.setFieldsValue({
        title: article.title,
        summary: article.summary,
        content: article.content,
        content_format: article.content_format,
        type: article.type,
        status: article.status,
        project_key: article.project_key || '',
        version: article.version || '',
        tags: article.tags || [],
        is_pinned: article.is_pinned,
        show_on_home: article.show_on_home,
        links: article.links || [],
      });
    }
  }, [data, form]);

  const mutation = useMutation({
    mutationFn: (payload: ArticlePayload) => (
      isEdit && id ? articlesApi.update(id, payload) : articlesApi.create(payload)
    ),
    onSuccess: async (response) => {
      message.success(isEdit ? 'Публикация обновлена' : 'Публикация создана');
      await queryClient.invalidateQueries({ queryKey: ['articles'] });
      await queryClient.invalidateQueries({ queryKey: ['articles-featured'] });
      navigate(`/info/articles/${response.data.id}`);
    },
    onError: () => message.error('Не удалось сохранить публикацию'),
  });

  const initialValues = useMemo(() => {
    if (isEdit) return defaultValues;
    const params = new URLSearchParams(window.location.search);
    const type = (params.get('type') || 'wiki') as ArticleType;
    return {
      ...defaultValues,
      type,
      content: type === 'release_note' ? releaseTemplate : '',
    };
  }, [isEdit]);

  const handleTypeChange = (value: ArticleType) => {
    if (value === 'release_note' && !form.getFieldValue('content')) {
      form.setFieldValue('content', releaseTemplate);
    }
  };

  const handleFinish = (values: FormValues) => {
    mutation.mutate({
      ...values,
      content_format: 'markdown',
      summary: values.summary || '',
      project_key: values.project_key || '',
      version: values.version || '',
      tags: values.tags || [],
      is_pinned: Boolean(values.is_pinned),
      show_on_home: Boolean(values.show_on_home),
      links: [],
    });
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space direction="vertical" size={2}>
        <Title level={2} style={{ margin: 0 }}>{isEdit ? 'Редактирование публикации' : 'Новая публикация'}</Title>
        <Text type="secondary">Markdown поддерживает заголовки, списки, ссылки, таблицы и блоки кода.</Text>
      </Space>

      <div className="article-editor-layout">
        <Card className="glass-panel">
          <Form
            form={form}
            layout="vertical"
            initialValues={initialValues}
            onFinish={handleFinish}
            disabled={isLoading}
          >
            <Form.Item name="title" label="Заголовок" rules={[{ required: true, message: 'Введите заголовок' }]}>
              <Input />
            </Form.Item>
            <Form.Item name="summary" label="Краткое описание" rules={[{ required: articleType === 'company_news', message: 'Введите краткое описание' }]}>
              <Input.TextArea rows={3} />
            </Form.Item>
            <Space size="middle" wrap>
              <Form.Item name="type" label="Тип" rules={[{ required: true }]} style={{ minWidth: 220 }}>
                <Select options={typeOptions} onChange={handleTypeChange} />
              </Form.Item>
              <Form.Item name="status" label="Статус" style={{ minWidth: 180 }}>
                <Select options={statusOptions} />
              </Form.Item>
              <Form.Item name="is_pinned" valuePropName="checked" label="Закрепление">
                <Checkbox>Закрепить</Checkbox>
              </Form.Item>
              <Form.Item name="show_on_home" valuePropName="checked" label="Главная">
                <Checkbox>На главную</Checkbox>
              </Form.Item>
            </Space>
            {articleType === 'release_note' ? (
              <Space size="middle" wrap>
                <Form.Item name="project_key" label="Проект" rules={[{ required: true, message: 'Укажите проект' }]} style={{ minWidth: 220 }}>
                  <Input />
                </Form.Item>
                <Form.Item name="version" label="Версия" rules={[{ required: true, message: 'Укажите версию' }]} style={{ minWidth: 220 }}>
                  <Input />
                </Form.Item>
              </Space>
            ) : null}
            <Form.Item name="tags" label="Теги">
              <Select mode="tags" tokenSeparators={[',']} placeholder="Введите тег и нажмите Enter" />
            </Form.Item>
            <Form.Item name="content" label="Содержимое" rules={[{ required: true, message: 'Введите содержимое' }]}>
              <Input.TextArea rows={18} className="article-editor-textarea" />
            </Form.Item>
            <Space wrap>
              <Button type="primary" htmlType="submit" loading={mutation.isPending}>Сохранить</Button>
              <Button onClick={() => navigate(-1)}>Отмена</Button>
            </Space>
          </Form>
        </Card>

        <Card title="Предпросмотр" className="glass-panel article-editor-preview">
          <div className="article-markdown">
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{content || ''}</ReactMarkdown>
          </div>
        </Card>
      </div>
    </Space>
  );
};

export default ArticleEditorPage;
