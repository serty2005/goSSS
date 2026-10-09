import React, { useMemo, useState } from 'react';
import dayjs from 'dayjs';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, Input, Popconfirm, Select, Skeleton, Space, Tag, Typography } from 'antd';
import { PaperClipOutlined, PlusOutlined } from '@ant-design/icons';
import { articlesApi } from '@/api/articles';
import { materialsApi } from '@/api/materials';
import ArticleStatusTag from '@/components/articles/ArticleStatusTag';
import ArticleTypeTag from '@/components/articles/ArticleTypeTag';
import MaterialEditorModal from '@/components/materials/MaterialEditorModal';
import MaterialFilesList from '@/components/materials/MaterialFilesList';
import { useDeleteMaterial, useDeleteMaterialFile } from '@/components/materials/useMaterialMutations';
import type { ArticleDTO, ArticleStatus, ArticleType, MaterialDTO, MaterialEntityRefDTO } from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text, Paragraph } = Typography;

const typeOptions: Array<{ value: ArticleType; label: string }> = [
  { value: 'wiki', label: 'Wiki' },
  { value: 'release_note', label: 'Release notes' },
  { value: 'company_news', label: 'Новости компании' },
  { value: 'incident_note', label: 'Инциденты' },
  { value: 'internal_doc', label: 'Инструкции' },
];

const statusOptions: Array<{ value: ArticleStatus; label: string }> = [
  { value: 'published', label: 'Опубликовано' },
  { value: 'draft', label: 'Черновики' },
  { value: 'archived', label: 'Архив' },
];

const sourceOptions = [
  { value: 'all', label: 'Все материалы' },
  { value: 'articles', label: 'Статьи' },
  { value: 'materials', label: 'Старые материалы' },
  { value: 'files', label: 'Файлы' },
];

// Источники, в которых показываются только материалы: фильтры статей к ним не применяются.
const MATERIAL_ONLY_SOURCES = ['materials', 'files'];

type AggregateItem =
  | { kind: 'article'; id: string; title: string; excerpt: string; updatedAt: string; article: ArticleDTO }
  | { kind: 'material'; id: string; title: string; excerpt: string; updatedAt: string; material: MaterialDTO };

const entityMeta: Record<MaterialEntityRefDTO['entity_type'], { label: string; path: (id: string) => string }> = {
  Company: { label: 'Компания', path: (id) => `/companies/${id}` },
  Server: { label: 'Сервер', path: (id) => `/servers/${id}` },
  Workstation: { label: 'Рабочая станция', path: (id) => `/workstations/${id}` },
  FiscalRegister: { label: 'ККТ', path: (id) => `/fiscals/${id}` },
};

const formatDate = (value?: string) => {
  const date = dayjs(value);
  return date.isValid() ? date.format('DD.MM.YYYY HH:mm') : '';
};

const normalizeExcerpt = (value: string) => value
  .replace(/```[\s\S]*?```/g, ' ')
  .replace(/[#>*_`[\]()|-]/g, ' ')
  .replace(/\s+/g, ' ')
  .trim();

const InfoPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [term, setTerm] = useState(searchParams.get('term') || '');
  const user = useAuthStore((state) => state.user);
  const canCreate = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  const type = (searchParams.get('type') || undefined) as ArticleType | undefined;
  const status = (searchParams.get('status') || undefined) as ArticleStatus | undefined;
  const source = searchParams.get('source') || 'all';
  const [editorOpen, setEditorOpen] = useState(false);
  const [editingMaterialID, setEditingMaterialID] = useState<string | null>(null);
  const deleteMaterialMutation = useDeleteMaterial();
  const { deleteFileMutation, deletingFileID } = useDeleteMaterialFile();
  const appliedTerm = searchParams.get('term') || undefined;

  const articleParams = useMemo(() => ({
    term: appliedTerm,
    type,
    status,
    limit: 100,
  }), [appliedTerm, status, type]);

  const materialsParams = useMemo(() => ({
    term: appliedTerm,
    has_files: source === 'files' ? true : undefined,
    limit: 100,
  }), [appliedTerm, source]);

  const articlesQuery = useQuery({
    queryKey: ['articles', articleParams],
    queryFn: () => articlesApi.list(articleParams),
    enabled: !MATERIAL_ONLY_SOURCES.includes(source),
    staleTime: 60_000,
  });

  const materialsQuery = useQuery({
    queryKey: ['materials', 'aggregate', materialsParams],
    queryFn: () => materialsApi.list(materialsParams),
    enabled: source !== 'articles' && !type && !status,
    staleTime: 60_000,
  });

  const updateParam = (key: string, value?: string) => {
    const next = new URLSearchParams(searchParams);
    if (value && value !== 'all') next.set(key, value);
    else next.delete(key);
    if (key === 'source' && value && MATERIAL_ONLY_SOURCES.includes(value)) {
      next.delete('type');
      next.delete('status');
    }
    setSearchParams(next);
  };

  const submitSearch = () => updateParam('term', term.trim() || undefined);

  const items = useMemo<AggregateItem[]>(() => {
    const articles = !MATERIAL_ONLY_SOURCES.includes(source)
      ? (articlesQuery.data?.data || []).map((article): AggregateItem => ({
        kind: 'article',
        id: article.id,
        title: article.title,
        excerpt: article.summary || normalizeExcerpt(article.content),
        updatedAt: article.published_at || article.updated_at,
        article,
      }))
      : [];
    const materials = source !== 'articles' && !type && !status
      ? (materialsQuery.data?.data || []).map((material): AggregateItem => ({
        kind: 'material',
        id: material.id,
        title: material.subject,
        excerpt: normalizeExcerpt(material.content),
        updatedAt: material.updated_at,
        material,
      }))
      : [];
    return [...articles, ...materials].sort((a, b) => dayjs(b.updatedAt).valueOf() - dayjs(a.updatedAt).valueOf());
  }, [articlesQuery.data?.data, materialsQuery.data?.data, source, status, type]);

  const isLoading = articlesQuery.isLoading || materialsQuery.isLoading;
  const editingMaterial = useMemo(
    () => (editingMaterialID ? (materialsQuery.data?.data || []).find((item) => item.id === editingMaterialID) || null : null),
    [editingMaterialID, materialsQuery.data?.data],
  );

  const openMaterialEditor = (material: MaterialDTO | null) => {
    setEditingMaterialID(material?.id || null);
    setEditorOpen(true);
  };

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start" wrap>
        <Space direction="vertical" size={2}>
          <Title level={2} style={{ margin: 0 }}>База знаний</Title>
          <Text type="secondary">Статьи, новости, release notes, материалы и файлы - в том числе без привязки к компании или оборудованию.</Text>
        </Space>
        {canCreate ? (
          <Space wrap>
            <Button icon={<PlusOutlined />} onClick={() => openMaterialEditor(null)}>Добавить материал или файл</Button>
            <Link to="/info/articles/new">
              <Button type="primary" icon={<PlusOutlined />}>Создать статью</Button>
            </Link>
          </Space>
        ) : null}
      </Space>

      <Card className="glass-panel" bodyStyle={{ padding: 12 }}>
        <Space wrap style={{ width: '100%' }}>
          <Input.Search
            value={term}
            onChange={(event) => setTerm(event.target.value)}
            onSearch={submitSearch}
            placeholder="Поиск по базе знаний"
            style={{ width: 280 }}
          />
          <Select
            value={source}
            options={sourceOptions}
            onChange={(value) => updateParam('source', value)}
            style={{ width: 180 }}
          />
          <Select
            allowClear
            placeholder="Тип статьи"
            value={type}
            options={typeOptions}
            onChange={(value) => updateParam('type', value)}
            style={{ width: 190 }}
            disabled={MATERIAL_ONLY_SOURCES.includes(source)}
          />
          {canCreate ? (
            <Select
              allowClear
              placeholder="Статус"
              value={status}
              options={statusOptions}
              onChange={(value) => updateParam('status', value)}
              style={{ width: 170 }}
              disabled={MATERIAL_ONLY_SOURCES.includes(source)}
            />
          ) : null}
        </Space>
      </Card>

      {isLoading ? (
        <Skeleton active paragraph={{ rows: 8 }} />
      ) : items.length === 0 ? (
        <Empty description="Материалов по этим условиям не найдено" />
      ) : (
        <div className="articles-list-grid" aria-busy={isLoading}>
          {items.map((item) => (
            <Card key={`${item.kind}-${item.id}`} size="small" className="article-preview-card" bodyStyle={{ padding: 16 }}>
              <Space direction="vertical" size={8} style={{ width: '100%' }}>
                {item.kind === 'article' ? (
                  <>
                    <Space size={6} wrap>
                      <ArticleTypeTag type={item.article.type} />
                      {canCreate ? <ArticleStatusTag status={item.article.status} /> : null}
                      {item.article.show_on_home ? <Tag color="blue" style={{ marginInlineEnd: 0 }}>На главной</Tag> : null}
                      {item.article.is_pinned ? <Tag color="gold" style={{ marginInlineEnd: 0 }}>Закреплено</Tag> : null}
                    </Space>
                    <Link to={`/info/articles/${item.article.id}`} style={{ color: 'inherit' }}>
                      <Title level={4} style={{ margin: 0 }}>{item.title}</Title>
                    </Link>
                  </>
                ) : (
                  <>
                    <Space size={6} wrap>
                      <Tag color="default" style={{ marginInlineEnd: 0 }}>Материал</Tag>
                      {item.material.files.length > 0 ? (
                        <Tag icon={<PaperClipOutlined />} style={{ marginInlineEnd: 0 }}>Файлы: {item.material.files.length}</Tag>
                      ) : null}
                      {item.material.entity_refs.length === 0 ? (
                        <Tag style={{ marginInlineEnd: 0 }}>Без привязки</Tag>
                      ) : null}
                      {item.material.entity_refs.map((ref) => {
                        const meta = entityMeta[ref.entity_type];
                        return (
                          <Link key={`${ref.entity_type}-${ref.entity_id}`} to={meta.path(ref.entity_id)}>
                            <Tag color="processing" style={{ marginInlineEnd: 0 }}>
                              {meta.label}: {ref.entity_id}
                            </Tag>
                          </Link>
                        );
                      })}
                    </Space>
                    <Title level={4} style={{ margin: 0 }}>{item.title}</Title>
                    {item.material.files.length > 0 ? (
                      <MaterialFilesList
                        files={item.material.files}
                        onDelete={canCreate
                          ? (file) => deleteFileMutation.mutate({ materialID: item.material.id, fileID: file.id })
                          : undefined}
                        deletingFileID={deletingFileID}
                      />
                    ) : null}
                  </>
                )}
                {item.excerpt ? (
                  <Paragraph type="secondary" ellipsis={{ rows: 3 }} style={{ margin: 0 }}>
                    {item.excerpt}
                  </Paragraph>
                ) : null}
                <Space size={8} wrap style={{ justifyContent: 'space-between', width: '100%' }}>
                  <Text type="secondary">{formatDate(item.updatedAt)}</Text>
                  {item.kind === 'material' && canCreate ? (
                    <Space size={4}>
                      <Button size="small" onClick={() => openMaterialEditor(item.material)}>Редактировать</Button>
                      <Popconfirm
                        title="Удалить материал?"
                        description="Файлы материала тоже будут удалены."
                        okText="Удалить"
                        cancelText="Отмена"
                        onConfirm={() => deleteMaterialMutation.mutate(item.material.id)}
                      >
                        <Button size="small" danger>Удалить</Button>
                      </Popconfirm>
                    </Space>
                  ) : null}
                </Space>
              </Space>
            </Card>
          ))}
        </div>
      )}

      <MaterialEditorModal
        open={editorOpen}
        material={editingMaterial}
        onClose={() => {
          setEditorOpen(false);
          setEditingMaterialID(null);
        }}
      />
    </Space>
  );
};

export default InfoPage;
