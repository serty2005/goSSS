import React, { useMemo, useState } from 'react';
import dayjs from 'dayjs';
import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { Button, Card, Empty, Skeleton, Space, Tag, Typography } from 'antd';
import { PlusOutlined } from '@ant-design/icons';
import { articlesApi } from '@/api/articles';
import ArticleTypeTag from '@/components/articles/ArticleTypeTag';
import type { ArticleDTO } from '@/types/api';
import { useAuthStore } from '@/store/authStore';

const { Title, Text, Paragraph } = Typography;

const formatArticleDate = (article: ArticleDTO) => {
  const value = article.published_at || article.updated_at;
  const date = dayjs(value);
  return date.isValid() ? date.format('DD.MM.YYYY') : '';
};

const makeExcerpt = (article: ArticleDTO) => {
  const source = article.summary || article.content;
  return source
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#>*_`[\]()|-]/g, ' ')
    .replace(/\s+/g, ' ')
    .trim();
};

const FeaturedArticlesPanel: React.FC<{ limit?: number }> = ({ limit = 6 }) => {
  const [expandedID, setExpandedID] = useState<string | null>(null);
  const user = useAuthStore((state) => state.user);
  const canCreate = Boolean(user?.roles?.includes('admin') || user?.roles?.includes('support_specialist'));
  const { data, isLoading } = useQuery({
    queryKey: ['articles-featured', limit],
    queryFn: () => articlesApi.featured(limit),
    staleTime: 60_000,
  });
  const articles = useMemo(() => data?.data || [], [data?.data]);
  const excerpts = useMemo(
    () => Object.fromEntries(articles.map((article) => [article.id, makeExcerpt(article)])),
    [articles],
  );

  return (
    <Card className="glass-panel" bodyStyle={{ padding: 16 }}>
      <Space direction="vertical" size="middle" style={{ width: '100%' }}>
        <Space style={{ justifyContent: 'space-between', width: '100%' }} align="start" wrap>
          <Space direction="vertical" size={0}>
            <Title level={4} style={{ margin: 0 }}>База знаний</Title>
            <Text type="secondary">Публикации, отмеченные для главной страницы</Text>
          </Space>
          <Link to="/info">
            <Button type="link" size="small">Все материалы</Button>
          </Link>
        </Space>

        {isLoading ? (
          <Skeleton active paragraph={{ rows: 5 }} />
        ) : articles.length === 0 ? (
          <Empty
            description={canCreate ? 'Публикаций на главной пока нет' : 'Опубликованных материалов пока нет'}
          >
            {canCreate ? (
              <Link to="/info/articles/new">
                <Button type="primary" icon={<PlusOutlined />}>Создать первую статью</Button>
              </Link>
            ) : null}
          </Empty>
        ) : (
          <Space direction="vertical" size="small" style={{ width: '100%' }}>
            {articles.map((article) => {
              const expanded = expandedID === article.id;
              return (
                <Card
                  key={article.id}
                  size="small"
                  hoverable
                  className="home-article-card"
                  bodyStyle={{ padding: 12 }}
                  onClick={() => setExpandedID(expanded ? null : article.id)}
                >
                  <Space direction="vertical" size={8} style={{ width: '100%' }}>
                    <Space size={6} wrap>
                      <ArticleTypeTag type={article.type} />
                      {article.is_pinned ? <Tag color="gold" style={{ marginInlineEnd: 0 }}>Закреплено</Tag> : null}
                    </Space>
                    <Title level={5} style={{ margin: 0 }}>{article.title}</Title>
                    <Space size={6} wrap>
                      {article.type === 'release_note' && (article.project_key || article.version) ? (
                        <Text type="secondary">{[article.project_key, article.version].filter(Boolean).join(' / ')}</Text>
                      ) : null}
                      <Text type="secondary">{formatArticleDate(article)}</Text>
                    </Space>
                    {expanded ? (
                      <>
                        <Paragraph type="secondary" ellipsis={{ rows: 4 }} style={{ margin: 0 }}>
                          {excerpts[article.id]}
                        </Paragraph>
                        <Link to={`/info/articles/${article.id}`} onClick={(event) => event.stopPropagation()}>
                          <Button size="small" type="primary">Посмотреть полностью</Button>
                        </Link>
                      </>
                    ) : null}
                  </Space>
                </Card>
              );
            })}
          </Space>
        )}
      </Space>
    </Card>
  );
};

export default FeaturedArticlesPanel;
