import React, { useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button, Empty, Input, Select, Space, Typography } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { articlesApi } from '@/api/articles';
import ArticlePreviewCard from '@/components/articles/ArticlePreviewCard';
import type { ArticleStatus, ArticleType } from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text } = Typography;

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

const ArticlesListPage: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [term, setTerm] = useState(searchParams.get('term') || '');
  const user = useAuthStore((state) => state.user);
  const canCreate = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  const type = (searchParams.get('type') || undefined) as ArticleType | undefined;
  const status = (searchParams.get('status') || undefined) as ArticleStatus | undefined;

  const queryParams = useMemo(() => ({
    term: searchParams.get('term') || undefined,
    type,
    status,
    limit: 50,
  }), [searchParams, status, type]);

  const { data, isLoading } = useQuery({
    queryKey: ['articles', queryParams],
    queryFn: () => articlesApi.list(queryParams),
    staleTime: 60_000,
  });
  const articles = data?.data || [];

  const updateParam = (key: string, value?: string) => {
    const next = new URLSearchParams(searchParams);
    if (value) next.set(key, value);
    else next.delete(key);
    setSearchParams(next);
  };

  const submitSearch = () => updateParam('term', term.trim() || undefined);

  return (
    <Space direction="vertical" size="middle" style={{ width: '100%' }}>
      <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start" wrap>
        <Space direction="vertical" size={2}>
          <Title level={2} style={{ margin: 0 }}>База знаний</Title>
          <Text type="secondary">Wiki, release notes, новости компании и внутренние инструкции.</Text>
        </Space>
        {canCreate ? (
          <Link to="/info/articles/new">
            <Button type="primary" icon={<PlusOutlined />}>Создать статью</Button>
          </Link>
        ) : null}
      </Space>

      <Space wrap style={{ width: '100%' }}>
        <Input.Search
          value={term}
          onChange={(event) => setTerm(event.target.value)}
          onSearch={submitSearch}
          placeholder="Поиск по статьям"
          style={{ width: 280 }}
        />
        <Select
          allowClear
          placeholder="Тип"
          value={type}
          options={typeOptions}
          onChange={(value) => updateParam('type', value)}
          style={{ width: 190 }}
        />
        {canCreate ? (
          <Select
            allowClear
            placeholder="Статус"
            value={status}
            options={statusOptions}
            onChange={(value) => updateParam('status', value)}
            style={{ width: 170 }}
          />
        ) : null}
      </Space>

      {articles.length === 0 && !isLoading ? (
        <Empty description="Публикаций по этим условиям не найдено" />
      ) : (
        <div className="articles-list-grid" aria-busy={isLoading}>
          {articles.map((article) => (
            <ArticlePreviewCard key={article.id} article={article} showStatus={canCreate} />
          ))}
        </div>
      )}
    </Space>
  );
};

export default ArticlesListPage;
